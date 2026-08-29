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
func applyMigrations(url string) error {
	down, err := os.ReadFile("../../db/migrations/0001_init.down.sql")
	if err != nil {
		return err
	}
	up, err := os.ReadFile("../../db/migrations/0001_init.up.sql")
	if err != nil {
		return err
	}
	for _, file := range [][]byte{down, up} {
		conn, err := pgx.Connect(context.Background(), url)
		if err != nil {
			return err
		}
		for _, stmt := range splitStatements(string(file)) {
			if stmt == "" || stmt == "BEGIN" || stmt == "BEGIN;" ||
				stmt == "COMMIT" || stmt == "COMMIT;" {
				continue // autocommit mode: tx wrappers are no-ops
			}
			if _, err := conn.Exec(context.Background(), stmt); err != nil {
				conn.Close(context.Background())
				return fmt.Errorf("exec %q: %w", trunc(stmt, 60), err)
			}
		}
		conn.Close(context.Background())
	}
	return nil
}

// splitStatements splits a migration on top-level semicolons, stripping
// whole-line and inline comments. Safe for this schema: no "--" occurs
// inside string literals.
func splitStatements(s string) []string {
	var out []string
	var cur string
	for _, line := range strings.Split(s, "\n") {
		if idx := strings.Index(line, "--"); idx >= 0 {
			line = line[:idx]
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		cur += " " + line
		if strings.HasSuffix(trimmed, ";") {
			out = append(out, strings.TrimSpace(cur))
			cur = ""
		}
	}
	if strings.TrimSpace(cur) != "" {
		out = append(out, strings.TrimSpace(cur))
	}
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
