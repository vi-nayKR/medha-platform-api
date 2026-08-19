package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/medha/backend/internal/matching/domain"
	matchservice "github.com/medha/backend/internal/matching/service"
	"github.com/medha/backend/internal/platform/epoch"
	"github.com/medha/backend/internal/server/middleware"
	apierrors "github.com/medha/backend/pkg/errors"
	"github.com/medha/backend/pkg/response"
)

// MatchHandler handles match-related HTTP endpoints.
type MatchHandler struct {
	matchService *matchservice.MatchService
	logger       *slog.Logger
}

// NewMatchHandler creates a new MatchHandler.
func NewMatchHandler(matchService *matchservice.MatchService, logger *slog.Logger) *MatchHandler {
	return &MatchHandler{
		matchService: matchService,
		logger:       logger,
	}
}

// --- Response DTOs ---

// MatchResponse represents a match in API responses.
type MatchResponse struct {
	ID        uuid.UUID `json:"id"`
	YajmanID  uuid.UUID `json:"yajman_id"`
	PanditID  uuid.UUID `json:"pandit_id"`
	EventID   uuid.UUID `json:"event_id"`
	Status    string    `json:"status"`
	MatchedAt string    `json:"matched_at"`
}

// MatchDetailResponse adds joined user/event details.
type MatchDetailResponse struct {
	MatchResponse
	YajmanFirstName string  `json:"yajman_first_name"`
	YajmanLastName  string  `json:"yajman_last_name"`
	YajmanPhotoURL  *string `json:"yajman_photo_url"`
	YajmanPhone     string  `json:"yajman_phone"`
	PanditFirstName string  `json:"pandit_first_name"`
	PanditLastName  string  `json:"pandit_last_name"`
	PanditPhotoURL  *string `json:"pandit_photo_url"`
	PanditPhone     string  `json:"pandit_phone"`
	CeremonyType    string  `json:"ceremony_type"`
	EventDate       string  `json:"event_date"`
	EventAddress    string  `json:"event_address"`
}

// --- Handlers ---

// GetMatch handles GET /api/v2/match/{id}.
// Summary Get match details
// Description Returns the full details of a match. Only the yajman or pandit of the match can access.
// Tags Matches
// Accept json
// Produce json
// Security bearerAuth
// Param id path string true "Match UUID"
// Success 200 {object} response.DataResponse{data=MatchDetailResponse} "Match details"
// Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// Failure 403 {object} apierrors.ProblemDetail "Forbidden"
// Failure 404 {object} apierrors.ProblemDetail "Match not found"
// Router /api/v2/match/{id} [get]
func (h *MatchHandler) GetMatch(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.UserIDFromContext(r.Context())
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.Unauthorized("authentication required", r.URL.Path))
		return
	}

	matchID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("The match ID provided is not valid.", r.URL.Path))
		return
	}

	match, err := h.matchService.GetMatch(r.Context(), matchID, userID)
	if err != nil {
		if errors.Is(err, domain.ErrMatchNotFound) {
			apierrors.WriteProblemDetail(w, apierrors.NotFound("This match could not be found.", r.URL.Path))
			return
		}
		if errors.Is(err, domain.ErrNotMatchParty) {
			apierrors.WriteProblemDetail(w, apierrors.NewProblemDetail(
				http.StatusForbidden,
				"https://medha.app/errors/forbidden",
				"Forbidden",
				"You are not a party in this match.",
				r.URL.Path,
			))
			return
		}
		h.logger.Error("get match failed", "error", err, "match_id", matchID)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Unable to load match details. Please try again later.", r.URL.Path))
		return
	}

	response.WriteData(w, http.StatusOK, toMatchDetailResponse(match))
}

// ListMyMatches handles GET /api/v2/match/mine.
// Summary List my matches
// Description Returns a paginated list of matches for the authenticated user.
// Tags Matches
// Accept json
// Produce json
// Security bearerAuth
// Param cursor query string false "Pagination cursor"
// Param limit query int false "Pagination limit" default(20)
// Success 200 {object} response.ListResponse{data=[]MatchDetailResponse} "List of matches"
// Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// Router /api/v2/match/mine [get]
func (h *MatchHandler) ListMyMatches(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.UserIDFromContext(r.Context())
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.Unauthorized("authentication required", r.URL.Path))
		return
	}

	cursor := r.URL.Query().Get("cursor")
	limit := parseIntParam(r, "limit", 20)

	matches, nextCursor, err := h.matchService.ListMyMatches(r.Context(), userID, cursor, limit)
	if err != nil {
		h.logger.Error("list my matches failed", "error", err, "user_id", userID)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Unable to load your matches. Please try again later.", r.URL.Path))
		return
	}

	responses := make([]MatchDetailResponse, len(matches))
	for i, m := range matches {
		responses[i] = toMatchDetailResponse(m)
	}

	var cursorPtr *string
	if nextCursor != "" {
		cursorPtr = &nextCursor
	}
	response.WriteList(w, http.StatusOK, responses, cursorPtr)
}

// CompleteMatch handles PUT /api/v2/match/{id}/complete.
// Summary Complete match
// Description Transitions a match from 'active' to 'completed'. Only the yajman can complete a match.
// Tags Matches
// Accept json
// Produce json
// Security bearerAuth
// Param id path string true "Match UUID"
// Success 200 {object} response.DataResponse{data=map[string]string} "Match completed"
// Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// Failure 403 {object} apierrors.ProblemDetail "Forbidden"
// Failure 404 {object} apierrors.ProblemDetail "Match not found"
// Router /api/v2/match/{id}/complete [put]
func (h *MatchHandler) CompleteMatch(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.UserIDFromContext(r.Context())
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.Unauthorized("authentication required", r.URL.Path))
		return
	}

	matchID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("invalid match ID", r.URL.Path))
		return
	}

	if err := h.matchService.CompleteMatch(r.Context(), matchID, userID); err != nil {
		if errors.Is(err, domain.ErrMatchNotFound) {
			apierrors.WriteProblemDetail(w, apierrors.NotFound("The requested match could not be found.", r.URL.Path))
			return
		}
		if errors.Is(err, domain.ErrNotMatchParty) {
			apierrors.WriteProblemDetail(w, apierrors.NewProblemDetail(
				http.StatusForbidden,
				"https://medha.app/errors/forbidden",
				"Forbidden",
				"Only the Yajman can mark a match as completed.",
				r.URL.Path,
			))
			return
		}
		if errors.Is(err, domain.ErrInvalidStatusTransition) {
			apierrors.WriteProblemDetail(w, apierrors.BadRequest(
				"This match cannot be completed in its current state.",
				r.URL.Path,
			))
			return
		}
		h.logger.Error("complete match failed", "error", err, "match_id", matchID)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Unable to complete the match. Please try again later.", r.URL.Path))
		return
	}

	response.WriteData(w, http.StatusOK, map[string]string{"message": "match completed"})
}

// AcceptMatch handles PUT /api/v2/match/{id}/accept.
// Transitions match from 'created' → 'matched' (yajman accepts pandit's interest).
// Summary Accept match
// Description Transitions a match from 'created' to 'matched'. Only the yajman can accept a match.
// Tags Matches
// Accept json
// Produce json
// Security bearerAuth
// Param id path string true "Match UUID"
// Success 200 {object} response.DataResponse{data=map[string]any} "Match accepted"
// Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// Failure 403 {object} apierrors.ProblemDetail "Forbidden"
// Failure 404 {object} apierrors.ProblemDetail "Match not found"
// Router /api/v2/match/{id}/accept [put]
func (h *MatchHandler) AcceptMatch(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.UserIDFromContext(r.Context())
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.Unauthorized("authentication required", r.URL.Path))
		return
	}

	matchID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("invalid match ID", r.URL.Path))
		return
	}

	match, err := h.matchService.AcceptMatch(r.Context(), matchID, userID)
	if err != nil {
		if errors.Is(err, domain.ErrMatchNotFound) {
			apierrors.WriteProblemDetail(w, apierrors.NotFound("The requested match could not be found.", r.URL.Path))
			return
		}
		if errors.Is(err, domain.ErrNotMatchParty) {
			apierrors.WriteProblemDetail(w, apierrors.NewProblemDetail(
				http.StatusForbidden,
				"https://medha.app/errors/forbidden",
				"Forbidden",
				"Only the Yajman can accept a match.",
				r.URL.Path,
			))
			return
		}
		if errors.Is(err, domain.ErrInvalidStatusTransition) {
			apierrors.WriteProblemDetail(w, apierrors.BadRequest(
				"This match cannot be accepted in its current state.",
				r.URL.Path,
			))
			return
		}
		h.logger.Error("accept match failed", "error", err, "match_id", matchID)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Unable to accept the match. Please try again later.", r.URL.Path))
		return
	}

	response.WriteData(w, http.StatusOK, map[string]any{
		"message": "match accepted",
		"status":  match.Status.String(),
	})
}

// ActivateMatch handles PUT /api/v2/match/{id}/activate.
// Transitions match from 'matched' → 'active' (ceremony in progress).
// Summary Activate match
// Description Transitions a match from 'matched' to 'active'. Only a party in the match can activate it.
// Tags Matches
// Accept json
// Produce json
// Security bearerAuth
// Param id path string true "Match UUID"
// Success 200 {object} response.DataResponse{data=map[string]string} "Match activated"
// Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// Failure 403 {object} apierrors.ProblemDetail "Forbidden"
// Failure 404 {object} apierrors.ProblemDetail "Match not found"
// Router /api/v2/match/{id}/activate [put]
func (h *MatchHandler) ActivateMatch(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.UserIDFromContext(r.Context())
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.Unauthorized("authentication required", r.URL.Path))
		return
	}

	matchID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("invalid match ID", r.URL.Path))
		return
	}

	if err := h.matchService.ActivateMatch(r.Context(), matchID, userID); err != nil {
		if errors.Is(err, domain.ErrMatchNotFound) {
			apierrors.WriteProblemDetail(w, apierrors.NotFound("The requested match could not be found.", r.URL.Path))
			return
		}
		if errors.Is(err, domain.ErrNotMatchParty) {
			apierrors.WriteProblemDetail(w, apierrors.NewProblemDetail(
				http.StatusForbidden,
				"https://medha.app/errors/forbidden",
				"Forbidden",
				"Only a party in this match can activate it.",
				r.URL.Path,
			))
			return
		}
		if errors.Is(err, domain.ErrInvalidStatusTransition) {
			apierrors.WriteProblemDetail(w, apierrors.BadRequest(
				"This match cannot be activated in its current state.",
				r.URL.Path,
			))
			return
		}
		h.logger.Error("activate match failed", "error", err, "match_id", matchID)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Unable to activate the match. Please try again later.", r.URL.Path))
		return
	}

	response.WriteData(w, http.StatusOK, map[string]string{"message": "match activated"})
}

// --- Helpers ---

func getMatchDisplayStatus(status string, eventDateStr string) string {
	if status == "cancelled" {
		return status
	}

	if eventDateStr == "" {
		return status
	}

	eventTime, err := time.Parse("2006-01-02", eventDateStr)
	if err != nil {
		return status
	}

	today := time.Now().UTC().Truncate(24 * time.Hour)
	eventDay := eventTime.UTC().Truncate(24 * time.Hour)

	if eventDay.Before(today) {
		return "completed"
	}
	if eventDay.Equal(today) {
		if status == "matched" {
			return "active"
		}
	}

	return status
}

func toMatchDetailResponse(m *domain.MatchWithDetails) MatchDetailResponse {
	status := getMatchDisplayStatus(m.Status.String(), m.EventDate)
	return MatchDetailResponse{
		MatchResponse: MatchResponse{
			ID:        m.ID,
			YajmanID:  m.YajmanID,
			PanditID:  m.PanditID,
			EventID:   m.EventID,
			Status:    status,
			MatchedAt: epoch.ToTime(m.MatchedAt).Format("2006-01-02T15:04:05Z07:00"),
		},
		YajmanFirstName: m.YajmanFirstName,
		YajmanLastName:  m.YajmanLastName,
		YajmanPhotoURL:  m.YajmanPhotoURL,
		YajmanPhone:     m.YajmanPhone,
		PanditFirstName: m.PanditFirstName,
		PanditLastName:  m.PanditLastName,
		PanditPhotoURL:  m.PanditPhotoURL,
		PanditPhone:     m.PanditPhone,
		CeremonyType:    m.CeremonyType,
		EventDate:       m.EventDate,
		EventAddress:    m.EventAddress,
	}
}

func parseIntParam(r *http.Request, key string, defaultVal int) int {
	valStr := r.URL.Query().Get(key)
	if valStr == "" {
		return defaultVal
	}
	val, err := strconv.Atoi(valStr)
	if err != nil || val <= 0 {
		return defaultVal
	}
	return val
}
