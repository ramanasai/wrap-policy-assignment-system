package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ramanasai/wrap-policy-assignment-system/internal/utils"
	"github.com/ramanasai/wrap-policy-assignment-system/resolver"
)

// ---------------------------------------------------------------------------
// Rules
// ---------------------------------------------------------------------------

type CreateRuleRequest struct {
	ID         string          `json:"id"`
	CategoryID string          `json:"category_id"`
	PolicyID   string          `json:"policy_id"`
	Priority   int             `json:"priority"`
	Predicate  json.RawMessage `json:"predicate"`
	ValidFrom  string          `json:"valid_from"`
	Source     string          `json:"source"` // authored|manual|system (default authored)
}

// handleCreateRule: POST /rules — validate against the registry, persist
// rule + v1 version in one tx.
func handleCreateRule(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req CreateRuleRequest
		if !decodeBody(w, r, &req) {
			return
		}
		if req.ID == "" || req.CategoryID == "" || req.PolicyID == "" {
			writeError(w, http.StatusBadRequest, "bad_request", "id, category_id and policy_id are required")
			return
		}
		if req.ValidFrom == "" {
			req.ValidFrom = utils.TodayUTC()
		}
		if _, err := utils.ParseDate(req.ValidFrom); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		pred, err := resolver.ParsePredicate(req.Predicate)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_predicate", err.Error())
			return
		}
		defs := d.Store.Definitions()
		if err := pred.Validate(defs); err != nil {
			writeError(w, http.StatusBadRequest, "bad_predicate", err.Error())
			return
		}
		source := resolver.Source(req.Source)
		if source == "" {
			source = resolver.SourceAuthored
		}
		if source != resolver.SourceAuthored && source != resolver.SourceManual && source != resolver.SourceSystem {
			writeError(w, http.StatusBadRequest, "bad_request", fmt.Sprintf("unknown source %q", req.Source))
			return
		}

		// Specificity is computed from the AST server-side (cached per version
		// in the column) — admins never declare it.
		if err := d.Store.CreateRule(r.Context(), req.ID, "co_demo", req.CategoryID, req.PolicyID,
			source, req.Priority, resolver.Specificity(pred), req.Predicate, req.ValidFrom); err != nil {
			repoError(w, err)
			return
		}

		writeJSON(w, http.StatusCreated, map[string]any{
			"id":               req.ID,
			"rule_version_id":  req.ID + ":v1",
			"specificity_rank": resolver.Specificity(pred),
			"valid_from":       req.ValidFrom,
		})
	}
}

// ---------------------------------------------------------------------------
// Preview — the save gate (UX_FLOWS §2): who gains/loses, before any write.
// ---------------------------------------------------------------------------

type PreviewRequest struct {
	CategoryID string          `json:"category_id"`
	PolicyID   string          `json:"policy_id"`
	Priority   int             `json:"priority"`
	Predicate  json.RawMessage `json:"predicate"`
	ValidFrom  string          `json:"valid_from"`
}

type PreviewRow struct {
	EmployeeID string `json:"employee_id"`
	PolicyID   string `json:"policy_id"`
	Via        string `json:"via,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

type PreviewResponse struct {
	Gained     []PreviewRow `json:"gained"`
	Lost       []PreviewRow `json:"lost"`
	MatchCount int          `json:"matches_now"`
}

// handlePreview: POST /rules/preview — resolve the category for every
// employee without and with the candidate rule; diff. Nothing is written.
// Demo-scale O(employees) resolution; the inverted-index affected-set
// recompute (reconciler) is the production path for this at scale.
func handlePreview(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req PreviewRequest
		if !decodeBody(w, r, &req) {
			return
		}
		if req.CategoryID == "" || req.PolicyID == "" {
			writeError(w, http.StatusBadRequest, "bad_request", "category_id and policy_id are required")
			return
		}
		pred, err := resolver.ParsePredicate(req.Predicate)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_predicate", err.Error())
			return
		}
		if err := pred.Validate(d.Store.Definitions()); err != nil {
			writeError(w, http.StatusBadRequest, "bad_predicate", err.Error())
			return
		}
		date := req.ValidFrom
		if date == "" {
			date = utils.TodayUTC()
		}
		cat, err := d.Store.Category(r.Context(), req.CategoryID)
		if err != nil {
			repoError(w, err)
			return
		}
		baseRules, err := d.Store.EffectiveRules(r.Context(), req.CategoryID, date)
		if err != nil {
			repoError(w, err)
			return
		}
		candidate := resolver.RuleVersion{
			RuleID:          "candidate",
			RuleVersionID:   "candidate:v1",
			CategoryID:      req.CategoryID,
			PolicyID:        req.PolicyID,
			PolicyVersionID: req.PolicyID + ":v1",
			Source:          resolver.SourceAuthored,
			Priority:        req.Priority,
			CreatedAt:       time.Now().UTC(),
			Predicate:       pred,
		}

		employeeIDs, err := d.Store.ListEmployeeIDs(r.Context())
		if err != nil {
			repoError(w, err)
			return
		}

		resp := PreviewResponse{Gained: []PreviewRow{}, Lost: []PreviewRow{}}
		for _, empID := range employeeIDs {
			facts, err := d.Store.FactsAt(r.Context(), empID, date)
			if err != nil {
				repoError(w, err)
				return
			}
			before, err := resolver.Resolve(resolver.Input{
				Category: cat, Date: date, Facts: facts,
				Rules: baseRules, Definitions: d.Store.Definitions(),
			})
			if err != nil {
				repoError(w, err)
				return
			}
			after, err := resolver.Resolve(resolver.Input{
				Category: cat, Date: date, Facts: facts,
				Rules:       append(append([]resolver.RuleVersion(nil), baseRules...), candidate),
				Definitions: d.Store.Definitions(),
			})
			if err != nil {
				repoError(w, err)
				return
			}

			beforePolicies := policySet(before)
			afterPolicies := policySet(after)

			for pol := range afterPolicies {
				if !beforePolicies[pol] {
					resp.Gained = append(resp.Gained, PreviewRow{
						EmployeeID: empID, PolicyID: pol,
						Via: winnerRuleID(after, pol),
					})
				}
			}
			for pol := range beforePolicies {
				if !afterPolicies[pol] {
					resp.Lost = append(resp.Lost, PreviewRow{
						EmployeeID: empID, PolicyID: pol,
						Reason: "lost to the candidate rule's tiebreak position",
					})
				}
			}
			if afterPolicies[req.PolicyID] {
				resp.MatchCount++
			}
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func policySet(res resolver.Result) map[string]bool {
	set := map[string]bool{}
	for _, a := range res.Assignments {
		set[a.PolicyID] = true
	}
	return set
}

func winnerRuleID(res resolver.Result, policyID string) string {
	for _, e := range res.Trace.Evaluated {
		if e.Outcome == resolver.RuleOutcomeWinner {
			for _, a := range res.Assignments {
				if a.PolicyID == policyID {
					return e.RuleID
				}
			}
		}
	}
	return ""
}

var _ = chi.URLParam
