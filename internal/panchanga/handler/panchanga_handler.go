package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/medha/backend/internal/panchanga/domain"
	"github.com/medha/backend/internal/panchanga/service"
	apierrors "github.com/medha/backend/pkg/errors"
	"github.com/medha/backend/pkg/response"
)

// PanchangaHandler handles HTTP requests for panchanga and festival data.
type PanchangaHandler struct {
	svc *service.PanchangaService
}

// NewPanchangaHandler creates a PanchangaHandler.
func NewPanchangaHandler(svc *service.PanchangaService) *PanchangaHandler {
	return &PanchangaHandler{svc: svc}
}

// ─── Swagger DTOs ─────────────────────────────────────────────────────────────

// PanchangaResponse is the API response shape for a single panchanga day.
//
// @Description Daily Hindu calendar details.
type PanchangaResponse struct {
	Date            int64  `json:"date"`
	Samvatsara      string `json:"samvatsara"`
	Ayana           string `json:"ayana"`
	Rutu            string `json:"rutu"`
	Masa            string `json:"masa"`
	Paksha          string `json:"paksha"`
	Tithi           string `json:"tithi"`
	Nakshatra       string `json:"nakshatra"`
	Yoga            string `json:"yoga"`
	Karana          string `json:"karana"`
	Vasara          string `json:"vasara"`
	ShraddhaThithi  string `json:"shraddha_tithi"`
	MasaNiyamaka    string `json:"masa_niyamaka"`
	FestivalsEvents string `json:"festivals_events"`
	Sunrise         string `json:"sunrise"`
	Sunset          string `json:"sunset"`
	Rahukala        string `json:"rahukala"`
	Gulikala        string `json:"gulikala"`
	Yamaganda       string `json:"yamaganda"`
}

// FestivalResponse is the API response shape for a single festival.
//
// @Description A named Hindu festival with its calendar date.
type FestivalResponse struct {
	ID          string `json:"id"`
	Festival    string `json:"festival"`
	Date        int64  `json:"date"`
	Description string `json:"description"`
}

func toResponse(p *domain.Panchanga) PanchangaResponse {
	return PanchangaResponse{
		Date:            p.Date,
		Samvatsara:      p.Samvatsara,
		Ayana:           p.Ayana,
		Rutu:            p.Rutu,
		Masa:            p.Masa,
		Paksha:          p.Paksha,
		Tithi:           p.Tithi,
		Nakshatra:       p.Nakshatra,
		Yoga:            p.Yoga,
		Karana:          p.Karana,
		Vasara:          p.Vasara,
		ShraddhaThithi:  p.ShraddhaThithi,
		MasaNiyamaka:    p.MasaNiyamaka,
		FestivalsEvents: p.FestivalsEvents,
		Sunrise:         p.Sunrise,
		Sunset:          p.Sunset,
		Rahukala:        p.Rahukala,
		Gulikala:        p.Gulikala,
		Yamaganda:       p.Yamaganda,
	}
}

// ─── Handlers ─────────────────────────────────────────────────────────────────

// GetTodayPanchanga returns today's IST panchanga.
func (h *PanchangaHandler) GetTodayPanchanga(w http.ResponseWriter, r *http.Request) {
	p, err := h.svc.GetTodayPanchanga(r.Context())
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.InternalError(err.Error(), r.URL.Path))
		return
	}
	if p == nil {
		apierrors.WriteProblemDetail(w, apierrors.NotFound("Panchanga data for today is not available yet.", r.URL.Path))
		return
	}
	resp := toResponse(p)
	response.WriteJSON(w, http.StatusOK, resp)
}

// GetPanchangaByDate returns panchanga for a specific date.
func (h *PanchangaHandler) GetPanchangaByDate(w http.ResponseWriter, r *http.Request) {
	dateStr := r.URL.Query().Get("date")
	if dateStr == "" {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Please provide a date (YYYY-MM-DD).", r.URL.Path))
		return
	}
	p, err := h.svc.GetPanchangaByDate(r.Context(), dateStr)
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest(err.Error(), r.URL.Path))
		return
	}
	if p == nil {
		apierrors.WriteProblemDetail(w, apierrors.NotFound("Panchanga data for this date is not available.", r.URL.Path))
		return
	}
	response.WriteJSON(w, http.StatusOK, toResponse(p))
}

// GetPanchangaRange returns panchanga for a date range.
func (h *PanchangaHandler) GetPanchangaRange(w http.ResponseWriter, r *http.Request) {
	fromStr := r.URL.Query().Get("from")
	toStr := r.URL.Query().Get("to")
	if fromStr == "" || toStr == "" {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Please provide start and end dates.", r.URL.Path))
		return
	}
	list, err := h.svc.GetPanchangaByRange(r.Context(), fromStr, toStr)
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest(err.Error(), r.URL.Path))
		return
	}
	var out []PanchangaResponse
	for _, p := range list {
		out = append(out, toResponse(p))
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "success",
		"message": "Panchanga range retrieved",
		"count":   len(out),
		"data":    out,
	})
}

// GetUpcomingFestivals returns upcoming Hindu festivals.
func (h *PanchangaHandler) GetUpcomingFestivals(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}
	festivals, err := h.svc.GetUpcomingFestivals(r.Context(), limit)
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.InternalError(err.Error(), r.URL.Path))
		return
	}
	var out []FestivalResponse
	for _, f := range festivals {
		out = append(out, FestivalResponse{
			ID:          f.ID,
			Festival:    f.Festival,
			Date:        f.Date,
			Description: f.Description,
		})
	}
	response.WriteJSON(w, http.StatusOK, out)
}
