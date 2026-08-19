package handler

import (
	"net/http"
	"strings"

	"github.com/medha/backend/internal/notification/service"
	"github.com/medha/backend/pkg/response"
)

// RiskHandler handles manual triggers of the risk check.
type RiskHandler struct {
	riskChecker   *service.RiskChecker
	internalToken string
}

// NewRiskHandler creates a new RiskHandler.
func NewRiskHandler(riskChecker *service.RiskChecker, internalToken string) *RiskHandler {
	return &RiskHandler{
		riskChecker:   riskChecker,
		internalToken: internalToken,
	}
}

// CheckRisk handles POST /internal/notifications/risk-check
// Requires valid X-Internal-Token or Bearer Authorization header to match the internalToken.
func (h *RiskHandler) CheckRisk(w http.ResponseWriter, r *http.Request) {
	if h.internalToken == "" || h.riskChecker == nil {
		response.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "service unavailable"})
		return
	}
	token := r.Header.Get("X-Internal-Token")
	if token == "" {
		token = r.Header.Get("Authorization")
		if strings.HasPrefix(token, "Bearer ") {
			token = strings.TrimPrefix(token, "Bearer ")
		}
	}
	if token != h.internalToken {
		response.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	if err := h.riskChecker.Run(r.Context()); err != nil {
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "risk check failed"})
		return
	}
	response.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok", "message": "risk check completed"})
}
