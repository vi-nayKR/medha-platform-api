package handler

import (
	"net/http"

	apierrors "github.com/medha/backend/pkg/errors"
	"github.com/medha/backend/pkg/response"
)

// Keep imports referenced to avoid unused import errors.
var (
	_ = response.DataResponse{}
	_ = apierrors.ProblemDetail{}
)

// GetTodayPanchangaV2 handles GET /api/v2/panchanga/today.
//
// @Summary      Today's Panchanga
// @Description  Returns the Hindu calendar details for today (IST). No authentication required.
// @Tags         panchanga-v2
// @Produce      json
// @Success      200  {object}  response.DataResponse{data=PanchangaResponse}  "Today's panchanga"
// @Failure      404  {object}  apierrors.ProblemDetail  "No data for today"
// @Router       /api/v2/panchanga/today [get]
func (h *PanchangaHandler) GetTodayPanchangaV2(w http.ResponseWriter, r *http.Request) {
	h.GetTodayPanchanga(w, r)
}

// GetPanchangaByDateV2 handles GET /api/v2/panchanga?date=YYYY-MM-DD.
//
// @Summary      Panchanga by Date
// @Description  Returns full Hindu calendar details for a given date (YYYY-MM-DD). No authentication required.
// @Tags         panchanga-v2
// @Produce      json
// @Param        date  query  string  true  "Date in YYYY-MM-DD format"
// @Success      200  {object}  response.DataResponse{data=PanchangaResponse}  "Panchanga for the date"
// @Failure      400  {object}  apierrors.ProblemDetail  "Invalid date format"
// @Failure      404  {object}  apierrors.ProblemDetail  "No data for that date"
// @Router       /api/v2/panchanga [get]
func (h *PanchangaHandler) GetPanchangaByDateV2(w http.ResponseWriter, r *http.Request) {
	h.GetPanchangaByDate(w, r)
}

// GetPanchangaRangeV2 handles GET /api/v2/panchanga/range?from=...&to=...
//
// @Summary      Panchanga Range
// @Description  Returns full Hindu calendar details for every day between `from` and `to` (YYYY-MM-DD, max 366 days). No authentication required.
// @Tags         panchanga-v2
// @Produce      json
// @Param        from  query  string  true  "Start date YYYY-MM-DD"
// @Param        to    query  string  true  "End date YYYY-MM-DD"
// @Success      200  {object}  response.DataResponse{data=[]PanchangaResponse}  "Panchanga list"
// @Failure      400  {object}  apierrors.ProblemDetail  "Invalid params"
// @Router       /api/v2/panchanga/range [get]
func (h *PanchangaHandler) GetPanchangaRangeV2(w http.ResponseWriter, r *http.Request) {
	h.GetPanchangaRange(w, r)
}

// GetUpcomingFestivalsV2 handles GET /api/v2/panchanga/festivals.
//
// @Summary      Upcoming Festivals
// @Description  Returns upcoming Hindu festivals from today, ordered by date. Use `limit` to control results (default 100, max 500). No authentication required.
// @Tags         panchanga-v2
// @Produce      json
// @Param        limit  query  int  false  "Max number of festivals to return (default 100)"
// @Success      200  {object}  response.DataResponse{data=[]FestivalResponse}  "Festival list"
// @Router       /api/v2/panchanga/festivals [get]
func (h *PanchangaHandler) GetUpcomingFestivalsV2(w http.ResponseWriter, r *http.Request) {
	h.GetUpcomingFestivals(w, r)
}
