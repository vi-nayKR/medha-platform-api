package handler

import (
	"net/http"

	apierrors "github.com/medha/backend/pkg/errors"
	"github.com/medha/backend/pkg/response"
)

var (
	_ = response.DataResponse{}
	_ = apierrors.ProblemDetail{}
)

// GetMatchV2 handles GET /api/v2/match/{id}.
//
// @Summary      Get match details
// @Description  Returns the full details of a match including both parties' info. Only the yajman or pandit can access.
// @Tags         match-v2
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "Match UUID"
// @Success      200 {object} response.DataResponse{data=MatchDetailResponse} "Match details"
// @Failure      401 {object} apierrors.ProblemDetail "Unauthorized"
// @Failure      403 {object} apierrors.ProblemDetail "Forbidden — not a party"
// @Failure      404 {object} apierrors.ProblemDetail "Match not found"
// @Router       /api/v2/match/{id} [get]
func (h *MatchHandler) GetMatchV2(w http.ResponseWriter, r *http.Request) {
	h.GetMatch(w, r)
}

// ListMyMatchesV2 handles GET /api/v2/match/mine.
//
// @Summary      List my matches
// @Description  Returns paginated matches for the authenticated user (as yajman or pandit).
// @Tags         match-v2
// @Produce      json
// @Security     BearerAuth
// @Param        cursor query string false "Pagination cursor"
// @Param        limit  query int    false "Page size (default 20)"
// @Success      200 {object} response.ListResponse{data=[]MatchDetailResponse} "My matches"
// @Failure      401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router       /api/v2/match/mine [get]
func (h *MatchHandler) ListMyMatchesV2(w http.ResponseWriter, r *http.Request) {
	h.ListMyMatches(w, r)
}

// AcceptMatchV2 handles PUT /api/v2/match/{id}/accept.
//
// @Summary      Accept match
// @Description  Transitions a match from 'created' to 'matched'. Only the yajman can accept.
// @Tags         match-v2
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "Match UUID"
// @Success      200 {object} response.DataResponse{data=interface{}} "Match accepted"
// @Failure      401 {object} apierrors.ProblemDetail "Unauthorized"
// @Failure      403 {object} apierrors.ProblemDetail "Forbidden"
// @Failure      404 {object} apierrors.ProblemDetail "Match not found"
// @Router       /api/v2/match/{id}/accept [put]
func (h *MatchHandler) AcceptMatchV2(w http.ResponseWriter, r *http.Request) {
	h.AcceptMatch(w, r)
}

// ActivateMatchV2 handles PUT /api/v2/match/{id}/activate.
//
// @Summary      Activate match
// @Description  Transitions a match from 'matched' to 'active' (ceremony in progress).
// @Tags         match-v2
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "Match UUID"
// @Success      200 {object} response.DataResponse{data=interface{}} "Match activated"
// @Failure      401 {object} apierrors.ProblemDetail "Unauthorized"
// @Failure      403 {object} apierrors.ProblemDetail "Forbidden"
// @Failure      404 {object} apierrors.ProblemDetail "Match not found"
// @Router       /api/v2/match/{id}/activate [put]
func (h *MatchHandler) ActivateMatchV2(w http.ResponseWriter, r *http.Request) {
	h.ActivateMatch(w, r)
}

// CompleteMatchV2 handles PUT /api/v2/match/{id}/complete.
//
// @Summary      Complete match
// @Description  Transitions a match from 'active' to 'completed'. Only the yajman can complete.
// @Tags         match-v2
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "Match UUID"
// @Success      200 {object} response.DataResponse{data=interface{}} "Match completed"
// @Failure      401 {object} apierrors.ProblemDetail "Unauthorized"
// @Failure      403 {object} apierrors.ProblemDetail "Forbidden"
// @Failure      404 {object} apierrors.ProblemDetail "Match not found"
// @Router       /api/v2/match/{id}/complete [put]
func (h *MatchHandler) CompleteMatchV2(w http.ResponseWriter, r *http.Request) {
	h.CompleteMatch(w, r)
}
