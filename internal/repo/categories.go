package repo

import (
	"context"
	"encoding/json"
	"fmt"

	db "github.com/ramanasai/wrap-policy-assignment-system/gen/db"
	"github.com/ramanasai/wrap-policy-assignment-system/resolver"
)

// CategoryAt loads a category's declarative semantics for the resolver.
// There is no date parameter: category semantics are immutable-after-use
// (docs/DATA_MODEL.md), so there is exactly one config per category.
func (s *Store) Category(ctx context.Context, id string) (resolver.CategoryConfig, error) {
	row, err := s.Q.GetCategory(ctx, id)
	if err != nil {
		return resolver.CategoryConfig{}, fmt.Errorf("repo: category %s: %w", id, err)
	}
	return resolver.CategoryConfig{
		ID:                 row.ID,
		Cardinality:        resolver.Cardinality(row.Cardinality),
		ResolutionStrategy: resolver.ResolutionStrategy(row.ResolutionStrategy),
		DefaultPriority:    int(row.DefaultPriority),
		Tiebreaker:         row.Tiebreaker,
	}, nil
}

// ListCategories returns all category configs (assignment listing, seed).
func (s *Store) ListCategories(ctx context.Context) ([]resolver.CategoryConfig, error) {
	rows, err := s.Q.ListCategories(ctx)
	if err != nil {
		return nil, fmt.Errorf("repo: list categories: %w", err)
	}
	out := make([]resolver.CategoryConfig, 0, len(rows))
	for _, r := range rows {
		out = append(out, resolver.CategoryConfig{
			ID:                 r.ID,
			Cardinality:        resolver.Cardinality(r.Cardinality),
			ResolutionStrategy: resolver.ResolutionStrategy(r.ResolutionStrategy),
			DefaultPriority:    int(r.DefaultPriority),
			Tiebreaker:         r.Tiebreaker,
		})
	}
	return out, nil
}

// AddPolicy + AddPolicyVersion support seeding/demo flows.
func (s *Store) AddPolicy(ctx context.Context, id, categoryID, name string) error {
	_, err := s.Q.InsertPolicy(ctx, db.InsertPolicyParams{
		ID:         id,
		CategoryID: categoryID,
		Name:       name,
		Payload:    json.RawMessage(`{}`),
	})
	if err != nil {
		return fmt.Errorf("repo: add policy %s: %w", id, err)
	}
	return nil
}

func (s *Store) AddPolicyVersion(ctx context.Context, id, policyID string, version int, validFrom string) error {
	validRange, err := pgRangeFrom(validFrom)
	if err != nil {
		return err
	}
	if _, err := s.Q.InsertPolicyVersion(ctx, db.InsertPolicyVersionParams{
		ID:         id,
		PolicyID:   policyID,
		Version:    int32(version),
		Payload:    json.RawMessage(`{}`),
		ValidRange: validRange,
	}); err != nil {
		return fmt.Errorf("repo: add policy version %s: %w", id, err)
	}
	return nil
}

// AddAttributeDefinition extends the registry — a data change, never a code change.
func (s *Store) AddAttributeDefinition(ctx context.Context, key, valueType string, allowedOps []string, enumValues []string, description string) error {
	_, err := s.Q.InsertAttributeDefinition(ctx, db.InsertAttributeDefinitionParams{
		Key:         key,
		ValueType:   valueType,
		AllowedOps:  allowedOps,
		EnumValues:  enumValues,
		Description: &description,
	})
	if err != nil {
		return fmt.Errorf("repo: add attribute definition %s: %w", key, err)
	}
	return nil
}
