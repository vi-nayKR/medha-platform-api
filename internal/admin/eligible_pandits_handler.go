package admin

import (
	"math"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	apierrors "github.com/medha/backend/pkg/errors"
	"github.com/medha/backend/pkg/response"
)

// eligiblePanditRow maps the raw SQL result for an eligible Pandit candidate.
type eligiblePanditRow struct {
	UserID                  uuid.UUID
	FirstName               string
	LastName                string
	Phone                   string
	Email                   string
	ProfilePhotoURL         string
	DistanceKM              float64
	CeremonySpecializations []string
	Languages               []string
	ServiceRadiusKM         int
	AvailabilityStatus      string
	BadgeTier               string
	State                   string
	ServiceCities           string // aggregated comma-separated city names
	IsSpecializationMatch   bool
	IsCityMatch             bool
}

// ListEligiblePandits returns pandits eligible for a specific event, filtered by
// ceremony specializations, service cities, and geographic proximity.
// @Summary List eligible pandits for event
// @Description Returns pandits matching the event's ceremony type and location, excluding already-pushed pandits.
// @Tags admin
// @Security AdminToken
// @Produce json
// @Param eventId path string true "Event ID"
// @Success 200 {object} response.DataResponse "Eligible pandits list"
// @Failure 400 {object} apierrors.ProblemDetail "Bad request"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Failure 404 {object} apierrors.ProblemDetail "Event not found"
// @Router /api/v2/admin/events/{eventId}/eligible-pandits [get]
func (h *Handler) ListEligiblePandits(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	eventIDStr := chi.URLParam(r, "eventId")
	eventID, err := uuid.Parse(eventIDStr)
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("invalid event_id", r.URL.Path))
		return
	}

	// 1. Fetch event details (ceremony_type, custom_ceremony_name, location, yajman state, yajman city)
	var ceremonyType string
	var customCeremonyName string
	var eventLat, eventLng *float64
	var yajmanState string
	var yajmanCity string

	err = h.pool.QueryRow(ctx, `
		SELECT e.ceremony_type::text,
		       COALESCE(e.custom_ceremony_name, '') AS custom_ceremony_name,
		       ST_Y(e.location::geometry), ST_X(e.location::geometry),
		       COALESCE(u.state, '') AS yajman_state,
		       COALESCE(u.city, '') AS yajman_city
		FROM events e
		LEFT JOIN users u ON u.id = e.yajman_id
		WHERE e.id = $1 AND e.deleted_at IS NULL
	`, eventID).Scan(&ceremonyType, &customCeremonyName, &eventLat, &eventLng, &yajmanState, &yajmanCity)
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.NotFound("event not found", r.URL.Path))
		return
	}

	// 2. Guard: event must have a location for geographic filtering
	if eventLat == nil || eventLng == nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("event has no location set", r.URL.Path))
		return
	}
	lat := *eventLat
	lng := *eventLng

	specializationToMatch := ceremonyType
	if ceremonyType == "custom" && customCeremonyName != "" {
		specializationToMatch = customCeremonyName
	}

	// 3. Query eligible pandits
	// The query uses a two-pronged approach matching the assignment engine:
	// - Pandits within 100km of the event location (ST_DWithin)
	// - Pandits who registered to serve the city nearest to the event (pandit_service_cities)
	// It also annotates each pandit with:
	// - is_specialization_match: whether their specializations contain the event's ceremony type
	// - is_city_match: whether they serve the nearest city
	// - Aggregated service city names for display
	// Already-pushed pandits (existing job_leads for this event) are excluded.
	rows, err := h.pool.Query(ctx, `
		WITH nearest_city AS (
			SELECT id FROM cities
			WHERE is_active = true
			ORDER BY 
				CASE WHEN name ILIKE $6::text THEN 0 ELSE 1 END ASC,
				ST_Distance(location, ST_SetSRID(ST_MakePoint($2::double precision, $1::double precision), 4326)::geography) ASC
			LIMIT 1
		),
		pushed_pandits AS (
			SELECT pandit_id FROM job_leads
			WHERE event_id = $4::uuid AND deleted_at IS NULL
		)
		SELECT
			u.id,
			COALESCE(u.first_name, '') AS first_name,
			COALESCE(u.last_name, '') AS last_name,
			COALESCE(u.phone, '') AS phone,
			COALESCE(u.email, '') AS email,
			COALESCE(u.profile_photo_url, '') AS profile_photo_url,
			ST_Distance(u.location, ST_SetSRID(ST_MakePoint($2::double precision, $1::double precision), 4326)::geography) / 1000.0 AS distance_km,
			COALESCE(pp.ceremony_specializations, '{}') AS ceremony_specializations,
			COALESCE(pp.languages, '{}') AS languages,
			COALESCE(pp.service_radius_km, 25) AS service_radius_km,
			COALESCE(pp.availability_status::text, 'available') AS availability_status,
			COALESCE(pb.badge_tier::text, 'pandit_ji') AS badge_tier,
			COALESCE(u.state, '') AS pandit_state,
			COALESCE(
				(SELECT string_agg(c.name || ' (' || c.state || ')', ', ' ORDER BY c.name)
				 FROM pandit_service_cities psc
				 JOIN cities c ON c.id = psc.city_id
				 WHERE psc.user_id = u.id),
				''
			) AS service_cities,
			CASE WHEN EXISTS (
				SELECT 1 FROM unnest(pp.ceremony_specializations) s 
				WHERE s ILIKE $5::text OR replace(s, '_', '-') ILIKE replace($5::text, '_', '-')
			) THEN true ELSE false END AS is_specialization_match,
			CASE WHEN EXISTS (
				SELECT 1 FROM pandit_service_cities psc
				WHERE psc.user_id = u.id AND psc.city_id = (SELECT id FROM nearest_city)
			) THEN true ELSE false END AS is_city_match
		FROM users u
		JOIN pandit_profiles pp ON pp.user_id = u.id AND pp.deleted_at IS NULL
		LEFT JOIN pandit_badges pb ON pb.pandit_id = u.id
		WHERE u.role = 'pandit'
			AND u.deleted_at IS NULL
			AND u.location IS NOT NULL
			AND u.id NOT IN (SELECT pandit_id FROM pushed_pandits)
			AND (
				-- Geographic proximity: within 100km
				ST_DWithin(u.location, ST_SetSRID(ST_MakePoint($2::double precision, $1::double precision), 4326)::geography, $3::double precision)
				-- OR serves the nearest city
				OR EXISTS (
					SELECT 1 FROM pandit_service_cities psc
					WHERE psc.user_id = u.id
					  AND psc.city_id = (SELECT id FROM nearest_city)
				)
			)
		ORDER BY
			-- Prioritize: specialization match first, then city match, then distance
			CASE WHEN EXISTS (
				SELECT 1 FROM unnest(pp.ceremony_specializations) s 
				WHERE s ILIKE $5::text OR replace(s, '_', '-') ILIKE replace($5::text, '_', '-')
			) THEN 0 ELSE 1 END ASC,
			CASE WHEN EXISTS (
				SELECT 1 FROM pandit_service_cities psc
				WHERE psc.user_id = u.id AND psc.city_id = (SELECT id FROM nearest_city)
			) THEN 0 ELSE 1 END ASC,
			distance_km ASC
		LIMIT 100
	`, lat, lng, float64(100*1000), eventID, specializationToMatch, yajmanCity) // $3 = 100km in meters (float64)
	if err != nil {
		h.internal(w, r, "list eligible pandits", err)
		return
	}
	defer rows.Close()

	results := make([]map[string]any, 0)
	for rows.Next() {
		var ep eligiblePanditRow
		if err := rows.Scan(
			&ep.UserID, &ep.FirstName, &ep.LastName,
			&ep.Phone, &ep.Email, &ep.ProfilePhotoURL,
			&ep.DistanceKM,
			&ep.CeremonySpecializations, &ep.Languages,
			&ep.ServiceRadiusKM, &ep.AvailabilityStatus,
			&ep.BadgeTier, &ep.State, &ep.ServiceCities,
			&ep.IsSpecializationMatch, &ep.IsCityMatch,
		); err != nil {
			h.internal(w, r, "scan eligible pandit", err)
			return
		}

		// Compute a relevance score using the same logic as the assignment engine
		score := computeRelevanceScore(ep, ceremonyType, yajmanState)

		results = append(results, map[string]any{
			"user_id":                  ep.UserID,
			"first_name":              ep.FirstName,
			"last_name":               ep.LastName,
			"phone":                   ep.Phone,
			"email":                   ep.Email,
			"profile_photo_url":       ep.ProfilePhotoURL,
			"distance_km":             math.Round(ep.DistanceKM*10) / 10,
			"ceremony_specializations": ep.CeremonySpecializations,
			"languages":               ep.Languages,
			"service_radius_km":       ep.ServiceRadiusKM,
			"availability_status":     ep.AvailabilityStatus,
			"badge_tier":              ep.BadgeTier,
			"service_cities":          ep.ServiceCities,
			"is_specialization_match": ep.IsSpecializationMatch,
			"is_city_match":           ep.IsCityMatch,
			"score":                   score,
		})
	}

	response.WriteData(w, http.StatusOK, results)
}

// computeRelevanceScore calculates a 0–100 relevance score for a pandit candidate,
// using the same weighting factors as the assignment engine (engine.go).
func computeRelevanceScore(ep eligiblePanditRow, ceremonyType, yajmanState string) float64 {
	const (
		wDistance  = 0.30
		wExpertise = 0.25
		wAvailable = 0.15
		wLanguage  = 0.10
		wRadius    = 0.10
		wBadge     = 0.10
	)

	// Distance score: closer = higher. Max 100 at 0km, linearly decays to 0 at 100km.
	distScore := math.Max(0, 100.0-ep.DistanceKM) * wDistance

	// Expertise: binary — does Pandit specialize in this ceremony?
	expertiseScore := 0.0
	if ceremonyType != "" && ceremonyType != "custom" {
		for _, spec := range ep.CeremonySpecializations {
			if strings.EqualFold(spec, ceremonyType) {
				expertiseScore = 100.0
				break
			}
		}
	}
	expertiseScore *= wExpertise

	// Availability: binary
	availScore := 0.0
	if ep.AvailabilityStatus == "available" {
		availScore = 100.0
	}
	availScore *= wAvailable

	// Language match heuristic: state-based (matches assignment engine)
	langScore := 0.0
	if yajmanState != "" && strings.EqualFold(ep.State, yajmanState) {
		langScore = 100.0
	}
	langScore *= wLanguage

	// Service radius fit: does configured radius cover the actual distance?
	radiusScore := 0.0
	if ep.ServiceRadiusKM > 0 && ep.DistanceKM <= float64(ep.ServiceRadiusKM) {
		radiusScore = 100.0
	}
	radiusScore *= wRadius

	// Badge tier score
	badgeScore := badgeTierScoreLocal(ep.BadgeTier) * wBadge

	total := distScore + expertiseScore + availScore + langScore + radiusScore + badgeScore
	return math.Round(total*100) / 100
}

// badgeTierScoreLocal mirrors the assignment engine's badge scoring.
func badgeTierScoreLocal(tier string) float64 {
	switch tier {
	case "dharma_ratna":
		return 100
	case "yajna_maharshi":
		return 80
	case "karma_kandi":
		return 60
	case "puja_praveen":
		return 40
	case "pandit_ji":
		return 20
	default:
		return 10
	}
}
