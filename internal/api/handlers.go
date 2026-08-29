package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ramanasai/wrap-policy-assignment-system/internal/repo"
	"github.com/ramanasai/wrap-policy-assignment-system/internal/utils"
	"github.com/ramanasai/wrap-policy-assignment-system/resolver"
)

// ---------------------------------------------------------------------------
// DTOs — mirroring docs/API.md exactly
// ---------------------------------------------------------------------------

type CreateEmployeeRequest struct {
	Name      string         `json:"name"`
	CompanyID string         `json:"company_id"`
	HiredOn   string         `json:"hired_on"`
	Facts     map[string]any `json:"facts"`
}

type AddFactRequest struct {
	AttributeKey string `json:"attribute_key"`
	Value        any    `json:"value"`
	ValidFrom    string `json:"valid_from"` // omit = today; past = backdated correction
	Trigger      string `json:"trigger"`
}

type PolicyAssignment struct {
	CategoryID string `json:"category_id"`
	PolicyID   string `json:"policy_id"`
	RuleID     string `json:"rule_id"`
	Source     string `json:"source"`
}

type DecisionNeeded struct {
	CategoryID string                    `json:"category_id"`
	Options    []resolver.DecisionOption `json:"options"`
}

type ReadinessResponse struct {
	AutoApplied    []PolicyAssignment `json:"auto_applied"`
	NeedsDecision  []DecisionNeeded   `json:"needs_decision"`
	ManualRequired []string           `json:"manual_required"`
	ComputedAt     string             `json:"computed_at"`
}

type DiffRow struct {
	EmployeeID string `json:"employee_id"`
	PolicyID   string `json:"policy_id"`
	Reason     string `json:"reason,omitempty"`
}

type DiffResponse struct {
	Gained []DiffRow `json:"gained"`
	Lost   []DiffRow `json:"lost"`
}

// ---------------------------------------------------------------------------
// Employees
// ---------------------------------------------------------------------------

// handleCreateEmployee: POST /employees — create + seed facts + readiness.
func handleCreateEmployee(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req CreateEmployeeRequest
		if !decodeBody(w, r, &req) {
			return
		}
		if req.Name == "" || req.HiredOn == "" {
			writeError(w, http.StatusBadRequest, "bad_request", "name and hired_on are required")
			return
		}
		if req.CompanyID == "" {
			req.CompanyID = "co_demo"
		}
		if _, err := utils.ParseDate(req.HiredOn); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}

		empID := fmt.Sprintf("emp_%d", time.Now().UnixNano())
		if err := d.Store.AddEmployee(r.Context(), empID, req.CompanyID, req.HiredOn); err != nil {
			repoError(w, err)
			return
		}
		for k, v := range req.Facts {
			if _, err := d.Store.AddFact(r.Context(), empID, k, v, req.HiredOn, "hr_edit"); err != nil {
				repoError(w, err)
				return
			}
		}
		// hire_date is the anchor for derived attributes (tenure_days) — always
		// record it, matching the seed script's behavior.
		if _, has := req.Facts["hire_date"]; !has {
			if _, err := d.Store.AddFact(r.Context(), empID, "hire_date", req.HiredOn, req.HiredOn, "hr_edit"); err != nil {
				repoError(w, err)
				return
			}
		}

		readiness, err := readinessFor(r.Context(), d, empID, req.HiredOn)
		if err != nil {
			repoError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"id":        empID,
			"name":      req.Name,
			"readiness": readiness,
		})
	}
}

// readinessFor derives the onboarding checklist live from the resolver —
// never stored mutable state (docs/UX_FLOWS.md §3).
func readinessFor(ctx context.Context, d Deps, employeeID, date string) (ReadinessResponse, error) {
	out := ReadinessResponse{ComputedAt: time.Now().UTC().Format(time.RFC3339)}

	cats, err := d.Store.ListCategories(ctx)
	if err != nil {
		return out, err
	}
	knownCats := map[string]bool{}
	for _, c := range cats {
		knownCats[c.ID] = true
		out.ManualRequired = append(out.ManualRequired, c.ID) // replaced below when resolved
	}

	resolved := map[string]bool{}
	for _, cat := range cats {
		res, err := d.Store.ResolveForEmployee(ctx, employeeID, cat.ID, date, repo.ResolveOptions{})
		if err != nil {
			return out, err
		}
		// Category produced a decision → remove from manual-required.
		if res.Outcome == resolver.OutcomeAssigned || res.Outcome == resolver.OutcomeShadowed {
			resolved[cat.ID] = true
			for _, a := range res.Assignments {
				out.AutoApplied = append(out.AutoApplied, PolicyAssignment{
					CategoryID: cat.ID,
					PolicyID:   a.PolicyID,
					RuleID:     a.RuleID,
					Source:     string(a.Source),
				})
			}
		} else if res.Outcome == resolver.OutcomeConflictNeedsDecision {
			resolved[cat.ID] = true
			out.NeedsDecision = append(out.NeedsDecision, DecisionNeeded{
				CategoryID: cat.ID,
				Options:    res.Options,
			})
		}
		// no_match → stays in ManualRequired.
	}

	// ManualRequired = categories that produced no automated assignment.
	manual := out.ManualRequired[:0]
	for _, c := range out.ManualRequired {
		if !resolved[c] {
			manual = append(manual, c)
		}
	}
	out.ManualRequired = manual
	_ = knownCats
	return out, nil
}

// ---------------------------------------------------------------------------
// Facts — the "downstream implications" flow (UX_FLOWS §4)
// ---------------------------------------------------------------------------

// handleAddFact: POST /employees/{id}/facts — commit the change, then diff.
func handleAddFact(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		employeeID := chi.URLParam(r, "employeeID")
		var req AddFactRequest
		if !decodeBody(w, r, &req) {
			return
		}
		if req.AttributeKey == "" || req.Value == nil {
			writeError(w, http.StatusBadRequest, "bad_request", "attribute_key and value are required")
			return
		}
		if req.Trigger == "" {
			req.Trigger = "hr_edit"
		}
		validFrom := req.ValidFrom
		if validFrom == "" {
			validFrom = utils.TodayUTC()
		}

		// "Before" resolution across all categories (committed state).
		before, err := resolveAllCategories(r.Context(), d, employeeID, validFrom)
		if err != nil {
			repoError(w, err)
			return
		}

		factID, err := d.Store.AddFact(r.Context(), employeeID, req.AttributeKey, req.Value, validFrom, req.Trigger)
		if err != nil {
			repoError(w, err)
			return
		}

		// Transactional outbox: the change event (idempotency = fact ID).
		if _, err := d.Store.EmitEvent(r.Context(), "fact_changed", "co_demo",
			map[string]any{
				"employee_id":   employeeID,
				"attribute_key": req.AttributeKey,
				"fact_id":       factID,
			},
			fmt.Sprintf("fact_changed:%s:%d", employeeID, factID)); err != nil {
			repoError(w, err)
			return
		}

		// "After" resolution — the diff is the exact gain/lose picture.
		after, err := resolveAllCategories(r.Context(), d, employeeID, validFrom)
		if err != nil {
			repoError(w, err)
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"fact_id": factID,
			"diff":    diffResolutions(before, after, employeeID),
		})
	}
}

// resolveAllCategories resolves every category for one employee; used for
// diffs and the assignments listing.
func resolveAllCategories(ctx context.Context, d Deps, employeeID, date string) (map[string]resolver.Result, error) {
	cats, err := d.Store.ListCategories(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string]resolver.Result{}
	for _, cat := range cats {
		res, err := d.Store.ResolveForEmployee(ctx, employeeID, cat.ID, date, repo.ResolveOptions{})
		if err != nil {
			return nil, err
		}
		out[cat.ID] = res
	}
	return out, nil
}

// diffResolutions compares before/after per category. Only `single`
// categories can "lose" (additive stacks, so gains only).
func diffResolutions(before, after map[string]resolver.Result, employeeID string) DiffResponse {
	diff := DiffResponse{Gained: []DiffRow{}, Lost: []DiffRow{}}
	for catID, res := range after {
		prev, had := before[catID]
		prevPolicy := ""
		if had {
			for _, a := range prev.Assignments {
				prevPolicy = a.PolicyID
			}
		}
		for _, a := range res.Assignments {
			if a.PolicyID != prevPolicy {
				reason := "newly covered"
				if prevPolicy != "" {
					reason = fmt.Sprintf("switched from %s", prevPolicy)
				}
				diff.Gained = append(diff.Gained, DiffRow{
					EmployeeID: employeeID, PolicyID: a.PolicyID, Reason: reason,
				})
			}
		}
		if had && prev.Outcome == resolver.OutcomeAssigned || had && prev.Outcome == resolver.OutcomeShadowed {
			for _, pa := range prev.Assignments {
				stillHas := false
				if res2, ok := after[catID]; ok {
					for _, a := range res2.Assignments {
						if a.PolicyID == pa.PolicyID {
							stillHas = true
						}
					}
				}
				if !stillHas {
					diff.Lost = append(diff.Lost, DiffRow{
						EmployeeID: employeeID, PolicyID: pa.PolicyID,
						Reason: "no rule covers this policy after the change",
					})
				}
			}
		}
	}
	return diff
}

// ---------------------------------------------------------------------------
// Assignments + explain
// ---------------------------------------------------------------------------

// handleAssignments: GET /employees/{id}/assignments?as_of=DATE
func handleAssignments(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		employeeID := chi.URLParam(r, "employeeID")
		date := r.URL.Query().Get("as_of")
		if date == "" {
			date = utils.TodayUTC()
		}
		if _, err := utils.ParseDate(date); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}

		results, err := resolveAllCategories(r.Context(), d, employeeID, date)
		if err != nil {
			repoError(w, err)
			return
		}
		out := map[string]any{"as_of": date, "categories": map[string]any{}}
		for catID, res := range results {
			if res.Outcome == resolver.OutcomeConflictNeedsDecision {
				out["categories"].(map[string]any)[catID] = map[string]any{
					"outcome": res.Outcome, "options": res.Options,
				}
				continue
			}
			out["categories"].(map[string]any)[catID] = map[string]any{
				"outcome":     res.Outcome,
				"assignments": res.Assignments,
			}
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// handleExplain: GET /employees/{id}/explain?category=X&as_of=DATE
// Returns the STORED trace — never recomputed (invariant #6).
func handleExplain(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		employeeID := chi.URLParam(r, "employeeID")
		categoryID := r.URL.Query().Get("category")
		if categoryID == "" {
			writeError(w, http.StatusBadRequest, "bad_request", "category query parameter is required")
			return
		}
		date := r.URL.Query().Get("as_of")
		if date == "" {
			date = utils.TodayUTC()
		}
		_, _, factsSnap, policySnap, evaluated, err := d.Store.LatestTrace(r.Context(), employeeID, categoryID, date)
		if err != nil {
			repoError(w, err)
			return
		}
		// Snapshots are stored as JSON — pass through verbatim.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"category_id":%q,"as_of_date":%q,"facts_snapshot":%s,"policy_snapshot":%s,"evaluated":%s}`,
			categoryID, date, factsSnap, policySnap, evaluated)
	}
}
