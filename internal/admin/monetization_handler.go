package admin

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	apierrors "github.com/medha/backend/pkg/errors"
	"github.com/medha/backend/pkg/response"
)

// SetPremiumRequest represents the payload to update user premium status.
type SetPremiumRequest struct {
	IsPremium       bool            `json:"is_premium"`
	PremiumUntil    *int64          `json:"premium_until"`
	PremiumBadge    string          `json:"premium_badge"`
	PremiumFeatures json.RawMessage `json:"premium_features" swaggertype:"object"`
}

// GetMonetizationSummary returns a high-level monetization overview.
// @Summary Monetization summary
// @Description Get monetization dashboard metrics, total revenue, and active ads count.
// @Tags admin
// @Security AdminToken
// @Produce json
// @Success 200 {object} response.DataResponse "Summary metrics"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router /api/v2/admin/monetization/summary [get]
func (h *Handler) GetMonetizationSummary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var totalPremiumUsers int
	err := h.pool.QueryRow(ctx, "SELECT COUNT(*) FROM users WHERE is_premium = true AND deleted_at IS NULL").Scan(&totalPremiumUsers)
	if err != nil {
		h.internal(w, r, "get premium users count", err)
		return
	}

	summary := map[string]any{
		"currency":            "INR",
		"total_revenue":       0.00,
		"active_ads":          0,
		"total_impressions":   0,
		"total_clicks":        0,
		"total_ad_spent":      0.00,
		"total_premium_users": totalPremiumUsers,
		"type_breakdown":      map[string]any{},
		"recent_transactions": []any{},
	}

	response.WriteData(w, http.StatusOK, summary)
}

// SetUserPremiumStatus toggles premium properties for a Yajman or Pandit.
// @Summary Set user premium status
// @Description Grant premium benefits, badge, and configure premium features JSON block.
// @Tags admin
// @Security AdminToken
// @Accept json
// @Produce json
// @Param id path string true "User ID"
// @Param body body SetPremiumRequest true "Premium Configurations"
// @Success 200 {object} response.DataResponse "Success"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router /api/v2/admin/monetization/users/{id}/premium [post]
func (h *Handler) SetUserPremiumStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userIDStr := chi.URLParam(r, "id")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("invalid user id", r.URL.Path))
		return
	}

	var req SetPremiumRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	features := req.PremiumFeatures
	if len(features) == 0 {
		features = []byte("{}")
	}

	query := `
		UPDATE users
		SET is_premium = $2, premium_until = $3, premium_badge = $4, premium_features = $5, updated_at = $6
		WHERE id = $1 AND deleted_at IS NULL
	`
	now := time.Now().Unix()
	tag, err := h.pool.Exec(ctx, query, userID, req.IsPremium, req.PremiumUntil, req.PremiumBadge, features, now)
	if err != nil {
		h.internal(w, r, "set user premium status", err)
		return
	}

	if tag.RowsAffected() == 0 {
		apierrors.WriteProblemDetail(w, apierrors.NotFound("user not found", r.URL.Path))
		return
	}

	// Create a transaction log if the admin enabled premium
	if req.IsPremium {
		// Log a zero amount or manual subscription transaction
		txID := uuid.New()
		h.pool.Exec(ctx, `
			INSERT INTO revenue_transactions (id, user_id, amount, type, status, payment_method, transaction_ref, created_at)
			VALUES ($1, $2, 0.00, 'subscription', 'success', 'NetBanking', 'ADMIN_MANUAL_ACTIVATE', $3)
		`, txID, userID, now)
	}

	response.WriteData(w, http.StatusOK, map[string]any{"status": "premium status updated"})
}

// GetAnalyticsOverview aggregates statistics for user demographics, growth, and posts.
// @Summary Analytics overview
// @Description Retrieve overview charts, growth indexes, user states breakdown, and posting analytics.
// @Tags admin
// @Security AdminToken
// @Produce json
// @Success 200 {object} response.DataResponse "Analytics overview data"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router /api/v2/admin/analytics/overview [get]
func (h *Handler) GetAnalyticsOverview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// 1. User Growths (Monthly Registrations for the last 6 months)
	userGrowth := make([]map[string]any, 0)
	growthRows, err := h.pool.Query(ctx, `
		SELECT TO_CHAR(TO_TIMESTAMP(created_at), 'YYYY-MM') AS month, COUNT(*) 
		FROM users 
		WHERE deleted_at IS NULL AND created_at > 0
		GROUP BY month 
		ORDER BY month ASC 
		LIMIT 12
	`)
	if err == nil {
		defer growthRows.Close()
		for growthRows.Next() {
			var month string
			var count int
			if err := growthRows.Scan(&month, &count); err == nil {
				userGrowth = append(userGrowth, map[string]any{
					"month": month,
					"count": count,
				})
			}
		}
	}

	// 2. State distribution
	stateBreakdown := make([]map[string]any, 0)
	stateRows, err := h.pool.Query(ctx, `
		SELECT COALESCE(state, 'Unknown') as state, COUNT(*) 
		FROM users 
		WHERE deleted_at IS NULL
		GROUP BY state 
		ORDER BY COUNT(*) DESC
		LIMIT 10
	`)
	if err == nil {
		defer stateRows.Close()
		for stateRows.Next() {
			var state string
			var count int
			if err := stateRows.Scan(&state, &count); err == nil {
				stateBreakdown = append(stateBreakdown, map[string]any{
					"state": state,
					"count": count,
				})
			}
		}
	}

	// 3. User Roles Breakdown
	roleBreakdown := make(map[string]int)
	roleRows, err := h.pool.Query(ctx, `
		SELECT COALESCE(role::text, 'unknown') as role, COUNT(*) 
		FROM users 
		WHERE deleted_at IS NULL 
		GROUP BY role
	`)
	if err == nil {
		defer roleRows.Close()
		for roleRows.Next() {
			var role string
			var count int
			if err := roleRows.Scan(&role, &count); err == nil {
				roleBreakdown[role] = count
			}
		}
	}

	// 4. General Post statistics
	var totalPosts int
	err = h.pool.QueryRow(ctx, "SELECT COUNT(*) FROM posts WHERE deleted_at IS NULL").Scan(&totalPosts)
	if err != nil {
		totalPosts = 0
	}

	analytics := map[string]any{
		"user_growth":     userGrowth,
		"state_breakdown": stateBreakdown,
		"role_breakdown":  roleBreakdown,
		"engagement": map[string]any{
			"total_posts": totalPosts,
		},
	}

	response.WriteData(w, http.StatusOK, analytics)
}
