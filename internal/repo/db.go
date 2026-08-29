// Package repo is the ONLY place where sqlc row types convert to resolver
// types and back (AGENTS.md invariant #2). The resolver package never sees
// the database; this package never contains resolution logic.
package repo

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ramanasai/wrap-policy-assignment-system/gen/db"
	"github.com/ramanasai/wrap-policy-assignment-system/internal/config"
	"github.com/ramanasai/wrap-policy-assignment-system/resolver"
)

// Store wraps the pgx pool and the generated query set.
type Store struct {
	Pool *pgxpool.Pool
	Q    *db.Queries

	// defs caches the attribute registry (small, static per deployment).
	defs map[string]resolver.AttributeDefinition
}

// New connects, pings, and loads the attribute definitions.
func New(ctx context.Context, cfg config.Config) (*Store, error) {
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("repo: connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("repo: ping: %w", err)
	}
	s := &Store{Pool: pool, Q: db.New(pool)}
	if err := s.loadDefinitions(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return s, nil
}

// loadDefinitions mirrors the attribute registry into resolver space.
// Registering a new attribute is a data change — zero code changes.
func (s *Store) loadDefinitions(ctx context.Context) error {
	rows, err := s.Q.ListAttributeDefinitions(ctx)
	if err != nil {
		return fmt.Errorf("repo: load attribute definitions: %w", err)
	}
	defs := make(map[string]resolver.AttributeDefinition, len(rows))
	for _, r := range rows {
		ops := make([]resolver.ClauseOp, 0, len(r.AllowedOps))
		for _, o := range r.AllowedOps {
			ops = append(ops, resolver.ClauseOp(o))
		}
		defs[r.Key] = resolver.AttributeDefinition{
			Key:        r.Key,
			ValueType:  resolver.ValueType(r.ValueType),
			AllowedOps: ops,
			EnumValues: r.EnumValues,
		}
	}
	s.defs = defs
	return nil
}

// Definitions returns the cached registry (read-only copy).
func (s *Store) Definitions() map[string]resolver.AttributeDefinition {
	out := make(map[string]resolver.AttributeDefinition, len(s.defs))
	for k, v := range s.defs {
		out[k] = v
	}
	return out
}

// Close releases the pool.
func (s *Store) Close() { s.Pool.Close() }

// Ping is the readiness probe's DB check.
func (s *Store) Ping(ctx context.Context) error {
	return s.Pool.Ping(ctx)
}
