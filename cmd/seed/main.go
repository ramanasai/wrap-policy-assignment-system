// Command seed populates the demo company: policies, rules spanning the
// cardinality spectrum, and 1,000 employees with bitemporal facts.
//
// Idempotent: exits 0 without changes when employees already exist.
// Deterministic: fixed rand seed → the same company every run.
package main

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"time"

	"github.com/rs/zerolog"

	"github.com/ramanasai/wrap-policy-assignment-system/resolver"

	"github.com/ramanasai/wrap-policy-assignment-system/internal/config"
	"github.com/ramanasai/wrap-policy-assignment-system/internal/logging"
	"github.com/ramanasai/wrap-policy-assignment-system/internal/repo"
	"github.com/ramanasai/wrap-policy-assignment-system/internal/utils"
)

type zerologLogger = zerolog.Logger

func main() {
	if err := utils.LoadDotEnv(); err != nil {
		fmt.Fprintf(os.Stderr, "seed: .env: %v\n", err)
		os.Exit(1)
	}
	cfg := config.MustLoad()
	logger := logging.New(logging.SetupFromEnv(), logging.ComponentSeed)

	ctx := context.Background()
	store, err := repo.New(ctx, cfg)
	if err != nil {
		logger.Fatal().Err(err).Msg("cannot connect — run `make migrate` and start Postgres first")
	}
	defer store.Close()

	seedCount := cfg.SeedEmployees
	if seedCount <= 0 {
		seedCount = 1000
	}

	existing, err := store.CountEmployees(ctx)
	if err != nil {
		logger.Fatal().Err(err).Msg("count employees")
	}
	if existing > 0 {
		logger.Info().Int64("existing", existing).Msg("already seeded — nothing to do (drop schema and re-migrate for a fresh seed)")
		return
	}

	t0 := time.Now()
	if err := seedPoliciesAndRules(ctx, store, logger); err != nil {
		logger.Fatal().Err(err).Msg("seed policies/rules")
	}
	if err := seedEmployees(ctx, store, logger, seedCount); err != nil {
		logger.Fatal().Err(err).Msg("seed employees")
	}
	logger.Info().
		Int("employees", seedCount).
		Dur("elapsed", time.Since(t0)).
		Msg("seed complete")
}

// seedPoliciesAndRules creates the demo policies and 15 rules spanning every
// engine capability: exclusive vs additive, specificity conflicts, manual
// override, tenure gates, and future-dated changes.
func seedPoliciesAndRules(ctx context.Context, store *repo.Store, logger zerologLogger) error {
	type pol struct{ id, cat, name string }
	policies := []pol{
		{"pol_vac_us_default", "time_off_vacation", "US Default Vacation"},
		{"pol_vac_ca_enhanced", "time_off_vacation", "California Enhanced Vacation"},
		{"pol_vac_ny_future", "time_off_vacation", "New York Enhanced Vacation (2027)"},
		{"pol_pay_biweekly", "pay_schedule", "US Bi-Weekly Pay"},
		{"pol_pay_monthly", "pay_schedule", "Global Monthly Pay"},
		{"pol_app_slack", "app_access", "Slack"},
		{"pol_app_github", "app_access", "GitHub"},
		{"pol_app_figma", "app_access", "Figma"},
		{"pol_train_meal_break", "compliance_training", "CA Meal Break Policy"},
		{"pol_train_harassment", "compliance_training", "Manager Harassment Training"},
		{"pol_train_security", "compliance_training", "Security Awareness Training"},
		{"pol_benefits_401k", "benefits_plan", "401k Match"},
	}
	for _, p := range policies {
		if err := store.AddPolicy(ctx, p.id, p.cat, p.name); err != nil {
			return fmt.Errorf("policy %s: %w", p.id, err)
		}
		if err := store.AddPolicyVersion(ctx, p.id+":v1", p.id, 1, "2024-01-01"); err != nil {
			return fmt.Errorf("policy version %s: %w", p.id, err)
		}
	}

	// Each rule: (id, category, policy, priority, specificity-hint, predicate, validFrom)
	type rule struct {
		id        string
		cat       string
		pol       string
		priority  int
		specHint  int
		predicate string
		validFrom string
		source    resolver.Source
	}
	rules := []rule{
		// Vacation: broad US default vs narrow CA 2yr+ → specificity conflict demo.
		{"r_vac_us", "time_off_vacation", "pol_vac_us_default", 5, 2,
			`{"op":"and","clauses":[{"attr":"location","op":"in","value":["US-CA","US-NY","US-WA"]}]}`, "2024-01-01", resolver.SourceAuthored},
		{"r_vac_ca_2yr", "time_off_vacation", "pol_vac_ca_enhanced", 5, 6,
			`{"op":"and","clauses":[{"attr":"location","op":"eq","value":"US-CA"},{"attr":"tenure_days","op":"gte","value":730}]}`, "2024-01-01", resolver.SourceAuthored},
		// Pay schedule: US full-time → bi-weekly.
		{"r_pay_us_ft", "pay_schedule", "pol_pay_biweekly", 5, 6,
			`{"op":"and","clauses":[{"attr":"employment_type","op":"eq","value":"full_time"},{"attr":"location","op":"in","value":["US-CA","US-NY","US-WA"]}]}`, "2024-01-01", resolver.SourceAuthored},
		// Contractor shift policy — attribute-based (problem statement example).
		{"r_pay_contractor", "pay_schedule", "pol_pay_monthly", 10, 4,
			`{"op":"and","clauses":[{"attr":"employment_type","op":"eq","value":"contractor"}]}`, "2024-01-01", resolver.SourceAuthored},
		// Manager: two competing rules → explicit_user_choice readiness decision.
		{"r_mgr_eng", "manager", "pol_train_harassment", 5, 4,
			`{"op":"and","clauses":[{"attr":"department","op":"eq","value":"Engineering"},{"attr":"is_manager","op":"eq","value":true}]}`, "2024-01-01", resolver.SourceAuthored},
		{"r_mgr_ca", "manager", "pol_train_harassment", 0, 4,
			`{"op":"and","clauses":[{"attr":"location","op":"eq","value":"US-CA"},{"attr":"is_manager","op":"eq","value":true}]}`, "2024-01-01", resolver.SourceAuthored},
		// App access: additive — engineers get Figma + GitHub; everyone gets Slack.
		{"r_app_slack", "app_access", "pol_app_slack", 0, 1,
			`{"op":"and","clauses":[{"attr":"employment_type","op":"ne","value":"intern"}]}`, "2024-01-01", resolver.SourceAuthored},
		{"r_app_github", "app_access", "pol_app_github", 0, 4,
			`{"op":"and","clauses":[{"attr":"department","op":"eq","value":"Engineering"}]}`, "2024-01-01", resolver.SourceAuthored},
		{"r_app_figma", "app_access", "pol_app_figma", 0, 6,
			`{"op":"and","clauses":[{"attr":"department","op":"eq","value":"Engineering"},{"attr":"employment_type","op":"eq","value":"full_time"}]}`, "2024-01-01", resolver.SourceAuthored},
		// Compliance: CA meal break (location-based, problem statement example).
		{"r_train_ca_meal", "compliance_training", "pol_train_meal_break", 0, 3,
			`{"op":"and","clauses":[{"attr":"location","op":"eq","value":"US-CA"}]}`, "2024-01-01", resolver.SourceAuthored},
		{"r_train_security", "compliance_training", "pol_train_security", 0, 4,
			`{"op":"and","clauses":[{"attr":"tenure_days","op":"gte","value":365}]}`, "2024-01-01", resolver.SourceAuthored},
		// Benefits: US full-time → 401k (exclusive, hybrid manual+rule demo).
		{"r_ben_401k", "benefits_plan", "pol_benefits_401k", 0, 6,
			`{"op":"and","clauses":[{"attr":"employment_type","op":"eq","value":"full_time"},{"attr":"location","op":"in","value":["US-CA","US-NY","US-WA"]}]}`, "2024-01-01", resolver.SourceAuthored},
		// Future-dated: proves "takes effect Jan 1" with zero code-path change.
		{"r_vac_ny_future", "time_off_vacation", "pol_vac_ny_future", 6, 3,
			`{"op":"and","clauses":[{"attr":"location","op":"eq","value":"US-NY"}]}`, "2027-01-01", resolver.SourceAuthored},
	}
	for _, r := range rules {
		if err := store.CreateRule(ctx, r.id, "co_demo", r.cat, r.pol, r.source, r.priority, r.specHint, []byte(r.predicate), r.validFrom); err != nil {
			return fmt.Errorf("rule %s: %w", r.id, err)
		}
	}
	logger.Info().Int("policies", len(policies)).Int("rules", len(rules)).Msg("policies and rules seeded")
	return nil
}

// seedEmployees creates deterministic employees with facts.
func seedEmployees(ctx context.Context, store *repo.Store, logger zerologLogger, count int) error {
	rng := rand.New(rand.NewSource(42)) // deterministic demo data
	locations := []string{"US-CA", "US-NY", "US-WA"}
	departments := []string{"Engineering", "Sales", "HR"}
	empTypes := []string{"full_time", "full_time", "full_time", "contractor"} // 75% full-time
	levels := []string{"IC1", "IC2", "IC3", "M1", "M2"}
	asOf := time.Now().UTC().Format("2006-01-02")

	for i := 1; i <= count; i++ {
		id := fmt.Sprintf("emp_%04d", i)
		hire := time.Now().UTC().AddDate(0, 0, -(rng.Intn(4*365) + 1)).Format("2006-01-02")
		if err := store.AddEmployee(ctx, id, "co_demo", hire); err != nil {
			return fmt.Errorf("employee %s: %w", id, err)
		}
		facts := map[string]any{
			"location":        locations[rng.Intn(len(locations))],
			"department":      departments[rng.Intn(len(departments))],
			"employment_type": empTypes[rng.Intn(len(empTypes))],
			"level":           levels[rng.Intn(len(levels))],
			"hire_date":       hire,
			"is_manager":      rng.Intn(10) == 0,
		}
		for k, v := range facts {
			// Facts start at hire date; interval closure chains them properly.
			if _, err := store.AddFact(ctx, id, k, v, hire, "hr_edit"); err != nil {
				return fmt.Errorf("employee %s fact %s: %w", id, k, err)
			}
		}
		_ = asOf
	}
	return nil
}
