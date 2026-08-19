package admin

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	apierrors "github.com/medha/backend/pkg/errors"
	"github.com/medha/backend/pkg/response"
)

type AdminUpdatePanditProfileRequest struct {
	Parampara               string   `json:"parampara"`
	VedaAffiliation         string   `json:"veda_affiliation"`
	CeremonySpecializations []string `json:"ceremony_specializations"`
	Languages               []string `json:"languages"`
	ServiceRadiusKM         int      `json:"service_radius_km"`
	AvailabilityStatus      string   `json:"availability_status"`
	About                   string   `json:"about"`
}

type AdminUpdateServiceCitiesRequest struct {
	HomeCityID        string   `json:"home_city_id"`
	AdditionalCityIDs []string `json:"additional_city_ids"`
}

func (h *Handler) UpdateUserPanditProfile(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userIDStr := chi.URLParam(r, "id")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("invalid user_id", r.URL.Path))
		return
	}

	var req AdminUpdatePanditProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("invalid request body", r.URL.Path))
		return
	}

	// 1. Ensure user exists and has role 'pandit'
	var role string
	err = h.pool.QueryRow(ctx, `SELECT role::text FROM users WHERE id = $1 AND deleted_at IS NULL`, userID).Scan(&role)
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.NotFound("user not found", r.URL.Path))
		return
	}
	if role != "pandit" {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("user is not a pandit", r.URL.Path))
		return
	}

	// 2. Insert/Update pandit_profile
	query := `
		INSERT INTO pandit_profiles (
			id, user_id, parampara, veda_affiliation, ceremony_specializations,
			languages, service_radius_km, availability_status, about,
			created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, EXTRACT(EPOCH FROM NOW())::BIGINT, EXTRACT(EPOCH FROM NOW())::BIGINT)
		ON CONFLICT (user_id) DO UPDATE
		SET parampara = EXCLUDED.parampara,
		    veda_affiliation = EXCLUDED.veda_affiliation,
		    ceremony_specializations = EXCLUDED.ceremony_specializations,
		    languages = EXCLUDED.languages,
		    service_radius_km = EXCLUDED.service_radius_km,
		    availability_status = EXCLUDED.availability_status,
		    about = EXCLUDED.about,
		    updated_at = EXTRACT(EPOCH FROM NOW())::BIGINT
	`
	profileID := uuid.New()
	_, err = h.pool.Exec(ctx, query,
		profileID,
		userID,
		req.Parampara,
		req.VedaAffiliation,
		req.CeremonySpecializations,
		req.Languages,
		req.ServiceRadiusKM,
		req.AvailabilityStatus,
		req.About,
	)
	if err != nil {
		h.internal(w, r, "upsert pandit profile", err)
		return
	}

	response.WriteData(w, http.StatusOK, map[string]any{"status": "updated"})
}

func (h *Handler) UpdateUserServiceCities(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userIDStr := chi.URLParam(r, "id")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("invalid user_id", r.URL.Path))
		return
	}

	var req AdminUpdateServiceCitiesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("invalid request body", r.URL.Path))
		return
	}

	homeCityID, err := uuid.Parse(req.HomeCityID)
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("invalid home_city_id", r.URL.Path))
		return
	}

	var additionalCityIDs []uuid.UUID
	for _, idStr := range req.AdditionalCityIDs {
		id, err := uuid.Parse(idStr)
		if err != nil {
			apierrors.WriteProblemDetail(w, apierrors.BadRequest("invalid additional_city_id: "+idStr, r.URL.Path))
			return
		}
		additionalCityIDs = append(additionalCityIDs, id)
	}

	// 1. Ensure user exists and has role 'pandit'
	var role string
	err = h.pool.QueryRow(ctx, `SELECT role::text FROM users WHERE id = $1 AND deleted_at IS NULL`, userID).Scan(&role)
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.NotFound("user not found", r.URL.Path))
		return
	}
	if role != "pandit" {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("user is not a pandit", r.URL.Path))
		return
	}

	// Use transaction to replace
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		h.internal(w, r, "begin transaction", err)
		return
	}
	defer tx.Rollback(ctx)

	// Delete existing
	_, err = tx.Exec(ctx, `DELETE FROM pandit_service_cities WHERE user_id = $1`, userID)
	if err != nil {
		h.internal(w, r, "delete existing service cities", err)
		return
	}

	// Insert home city
	_, err = tx.Exec(ctx, `
		INSERT INTO pandit_service_cities (id, user_id, city_id, is_home, created_at)
		VALUES ($1, $2, $3, true, EXTRACT(EPOCH FROM NOW())::BIGINT)
	`, uuid.New(), userID, homeCityID)
	if err != nil {
		h.internal(w, r, "insert home city", err)
		return
	}

	// Insert additional cities
	for _, cityID := range additionalCityIDs {
		_, err = tx.Exec(ctx, `
			INSERT INTO pandit_service_cities (id, user_id, city_id, is_home, created_at)
			VALUES ($1, $2, $3, false, EXTRACT(EPOCH FROM NOW())::BIGINT)
		`, uuid.New(), userID, cityID)
		if err != nil {
			h.internal(w, r, "insert additional city", err)
			return
		}
	}

	if err := tx.Commit(ctx); err != nil {
		h.internal(w, r, "commit transaction", err)
		return
	}

	response.WriteData(w, http.StatusOK, map[string]any{"status": "updated"})
}
