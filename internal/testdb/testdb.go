// Package testdb provides live-Postgres test harnesses that SKIP cleanly
// when no server is reachable. Each caller gets its own database name so
// packages never share state, and migrations are re-applied hermetically.
package testdb

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib" // sql driver for CREATE DATABASE
)

// Port probed for a local dev Postgres (the temp-cluster convention).
const defaultPort = "55432"

// URL returns the connection URL for a freshly-migrated database named
// dbName, or skips the test when no Postgres is reachable — integration
// tests must never fail because infra is off; only because behavior is wrong.
func URL(t *testing.T, dbName string) string {
	t.Helper()

	port := os.Getenv("TEST_DATABASE_PORT")
	if port == "" {
		port = defaultPort
	}
	if !reachable("localhost:" + port) {
		t.Skipf("no Postgres on localhost:%s — start a local cluster to run integration tests", port)
	}

	baseURL := fmt.Sprintf("postgres://postgres:postgres@localhost:%s", port)
	adminURL := baseURL + "/postgres?sslmode=disable"

	// Ensure the database exists (create if missing).
	adb, err := sql.Open("pgx", adminURL)
	if err != nil {
		t.Fatalf("testdb: open admin connection: %v", err)
	}
	var exists bool
	if err := adb.QueryRow(
		"SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)", dbName,
	).Scan(&exists); err != nil {
		t.Fatalf("testdb: check database: %v", err)
	}
	if !exists {
		if _, err := adb.Exec("CREATE DATABASE " + dbName); err != nil {
			t.Fatalf("testdb: create database %s: %v", dbName, err)
		}
	}
	adb.Close()

	url := baseURL + "/" + dbName + "?sslmode=disable"
	if err := applyMigrations(url); err != nil {
		t.Fatalf("testdb: apply migrations: %v", err)
	}

	return url
}

// applyMigrations runs down-then-up migrations via pgx with statement
// splitting (comments stripped; BEGIN/COMMIT skipped in autocommit mode).
// applyMigrations runs ALL up migrations (sorted) then ALL down migrations
// (reverse) via pgx with statement splitting (comments stripped;
// BEGIN/COMMIT skipped in autocommit mode). Mirrors golang-migrate: new
// migration files flow automatically.
func applyMigrations(url string) error {
	upFiles, err := filepath.Glob("../../db/migrations/*.up.sql")
	if err != nil {
		return err
	}
	downFiles, err := filepath.Glob("../../db/migrations/*.down.sql")
	if err != nil {
		return err
	}
	sort.Strings(upFiles)
	sort.Sort(sort.Reverse(sort.StringSlice(downFiles)))

	// Wipe first (downs, newest→oldest), then apply (ups, oldest→newest).
	for _, file := range append(downFiles, upFiles...) {
		raw, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		conn, err := pgx.Connect(context.Background(), url)
		if err != nil {
			return err
		}
		for _, stmt := range splitStatements(string(raw)) {
			if stmt == "" {
				continue
			}
			if _, err := conn.Exec(context.Background(), stmt); err != nil {
				conn.Close(context.Background())
				return fmt.Errorf("%s: exec %q: %w", filepath.Base(file), trunc(stmt, 60), err)
			}
		}
		conn.Close(context.Background())
	}
	return nil
}

// splitStatements splits a migration on TOP-LEVEL semicolons, aware of
// single-quoted strings and dollar-quoted blocks (PL/pgSQL bodies), and
// strips whole-line and inline comments outside quotes. Safe for this schema
// (no nested dollar tags; no "--" inside literals).
// splitStatements splits a migration on TOP-LEVEL semicolons with a single
// pass over the whole file, tracking single-quoted strings, dollar-quoted
// blocks (PL/pgSQL bodies), and comments. BEGIN/COMMIT transactional
// wrappers are dropped (autocommit mode). This handles 0002's CREATE
// FUNCTION $$ bodies and inline comments in 0001.
func splitStatements(s string) []string {
	var out []string
	cur := strings.Builder{}
	depth := 0 // dollar-quote nesting
	inSingle := false

	flush := func() {
		stmt := strings.TrimSpace(cur.String())
		if stmt != "" && stmt != "BEGIN" && stmt != "BEGIN;" &&
			stmt != "COMMIT" && stmt != "COMMIT;" {
			out = append(out, stmt)
		}
		cur.Reset()
	}

	i := 0
	for i < len(s) {
		c := s[i]

		// Comments: strip to end of line.
		if !inSingle && depth == 0 && c == '-' && i+1 < len(s) && s[i+1] == '-' {
			for i < len(s) && s[i] != '\n' {
				i++
			}
			continue
		}
		// Dollar-quote open/close ($$).
		if !inSingle && i+1 < len(s) && s[i:i+2] == "$$" {
			if depth == 0 {
				depth = 1
			} else {
				depth = 0
			}
			cur.WriteString("$$")
			i += 2
			continue
		}
		// Single-quoted strings ('' = escaped quote).
		if depth == 0 && c == '\'' {
			if inSingle && i+1 < len(s) && s[i+1] == '\'' {
				cur.WriteString("''")
				i += 2
				continue
			}
			inSingle = !inSingle
		}
		// Newline inside a statement becomes a space.
		if c == '\n' {
			if cur.Len() > 0 && !endsWithSpace(cur.String()) {
				cur.WriteByte(' ')
			}
			i++
			continue
		}
		// Top-level terminator.
		if !inSingle && depth == 0 && c == ';' && cur.Len() > 0 {
			cur.WriteByte(';')
			flush()
			i++
			continue
		}
		cur.WriteByte(c)
		i++
	}
	flush()
	return out
}

func endsWithSpace(st string) bool {
	return len(st) > 0 && st[len(st)-1] == ' '
}

func lineEndsStatement(s string) bool {
	t := strings.TrimSpace(s)
	return strings.HasSuffix(t, ";")
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}

func trunc(s string, n int) string {
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}

func reachable(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, 300*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
