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

// ExpressInterestV2 handles POST /api/v2/interest.
//
// @Summary      Express interest in an event
// @Description  Allows a pandit to express interest in a yajman's event.
// @Tags         interest-v2
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param body body ExpressInterestRequest true "Interest details"
// @Success      201 {object} response.DataResponse{data=InterestResponse} "Interest expressed"
// @Failure      400 {object} apierrors.ProblemDetail "Bad request"
// @Failure      401 {object} apierrors.ProblemDetail "Unauthorized"
// @Failure      409 {object} apierrors.ProblemDetail "Already expressed interest"
// @Router       /api/v2/interest [post]
func (h *InterestHandler) ExpressInterestV2(w http.ResponseWriter, r *http.Request) {
	h.ExpressInterest(w, r)
}

// RespondToInterestV2 handles PUT /api/v2/interest/{id}/respond.
//
// @Summary      Respond to interest
// @Description  Allows a yajman to accept or reject a pandit's interest in their event.
// @Tags         interest-v2
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "Interest UUID"
// @Param body body RespondToInterestRequest true "Accept or reject"
// @Success      200 {object} response.DataResponse{data=InterestResponse} "Interest updated"
// @Failure      400 {object} apierrors.ProblemDetail "Bad request"
// @Failure      401 {object} apierrors.ProblemDetail "Unauthorized"
// @Failure      403 {object} apierrors.ProblemDetail "Forbidden — not event owner"
// @Failure      404 {object} apierrors.ProblemDetail "Interest not found"
// @Router       /api/v2/interest/{id}/respond [put]
func (h *InterestHandler) RespondToInterestV2(w http.ResponseWriter, r *http.Request) {
	h.RespondToInterest(w, r)
}

// ListEventInterestsV2 handles GET /api/v2/event/{id}/interest.
//
// @Summary      List interests for an event
// @Description  Returns paginated pandit interests for a specific event. Only the event owner can view.
// @Tags         interest-v2
// @Produce      json
// @Security     BearerAuth
// @Param        id     path  string true  "Event UUID"
// @Param        cursor query string false "Pagination cursor"
// @Param        limit  query int    false "Page size (default 20)"
// @Success      200 {object} response.ListResponse{data=[]InterestDetailResponse} "Interests list"
// @Failure      401 {object} apierrors.ProblemDetail "Unauthorized"
// @Failure      403 {object} apierrors.ProblemDetail "Forbidden — not event owner"
// @Failure      404 {object} apierrors.ProblemDetail "Event not found"
// @Router       /api/v2/event/{id}/interest [get]
func (h *InterestHandler) ListEventInterestsV2(w http.ResponseWriter, r *http.Request) {
	h.ListEventInterests(w, r)
}

// ListMyInterestsV2 handles GET /api/v2/interest/mine.
//
// @Summary      List my interests
// @Description  Returns paginated list of interests expressed by the authenticated pandit.
// @Tags         interest-v2
// @Produce      json
// @Security     BearerAuth
// @Param        cursor query string false "Pagination cursor"
// @Param        limit  query int    false "Page size (default 20)"
// @Success      200 {object} response.ListResponse{data=[]InterestDetailResponse} "My interests"
// @Failure      401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router       /api/v2/interest/mine [get]
func (h *InterestHandler) ListMyInterestsV2(w http.ResponseWriter, r *http.Request) {
	h.ListMyInterests(w, r)
}

// AcceptInterestV2 handles PUT /api/v2/interest/{id}/accept.
//
// @Summary      Accept interest
// @Description  Allows a yajman to accept a pandit's interest (moving it to connected, unlocking chat).
// @Tags         interest-v2
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "Interest UUID"
// @Success      200 {object} response.DataResponse{data=InterestResponse} "Interest updated to connected"
// @Failure      400 {object} apierrors.ProblemDetail "Bad request"
// @Failure      401 {object} apierrors.ProblemDetail "Unauthorized"
// @Failure      403 {object} apierrors.ProblemDetail "Forbidden — not event owner"
// @Failure      404 {object} apierrors.ProblemDetail "Interest not found"
// @Failure      409 {object} apierrors.ProblemDetail "Conflict — interest already resolved"
// @Router       /api/v2/interest/{id}/accept [put]
func (h *InterestHandler) AcceptInterestV2(w http.ResponseWriter, r *http.Request) {
	h.AcceptInterest(w, r)
}

// ConfirmBookingV2 handles PUT /api/v2/interest/{id}/book.
//
// @Summary      Confirm booking
// @Description  Allows a yajman to book a specific connected pandit (moving interest to accepted, rejecting others, marking event booked).
// @Tags         interest-v2
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "Interest UUID"
// @Success      200 {object} response.DataResponse{data=InterestResponse} "Booking confirmed, event booked"
// @Failure      400 {object} apierrors.ProblemDetail "Bad request"
// @Failure      401 {object} apierrors.ProblemDetail "Unauthorized"
// @Failure      403 {object} apierrors.ProblemDetail "Forbidden — not event owner"
// @Failure      404 {object} apierrors.ProblemDetail "Interest not found"
// @Failure      409 {object} apierrors.ProblemDetail "Conflict — interest already booked/rejected"
// @Router       /api/v2/interest/{id}/book [put]
func (h *InterestHandler) ConfirmBookingV2(w http.ResponseWriter, r *http.Request) {
	h.ConfirmBooking(w, r)
}

// WithdrawInterestV2 handles PUT /api/v2/interest/{id}/withdraw.
//
// @Summary      Withdraw interest
// @Description  Allows a pandit to withdraw their interest, marking themselves unavailable.
// @Tags         interest-v2
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "Interest UUID"
// @Success      200 {object} response.DataResponse{data=InterestResponse} "Interest withdrawn"
// @Failure      400 {object} apierrors.ProblemDetail "Bad request"
// @Failure      401 {object} apierrors.ProblemDetail "Unauthorized"
// @Failure      403 {object} apierrors.ProblemDetail "Forbidden — not interest owner"
// @Failure      404 {object} apierrors.ProblemDetail "Interest not found"
// @Failure      409 {object} apierrors.ProblemDetail "Conflict — interest already resolved"
// @Router       /api/v2/interest/{id}/withdraw [put]
func (h *InterestHandler) WithdrawInterestV2(w http.ResponseWriter, r *http.Request) {
	h.WithdrawInterest(w, r)
}

