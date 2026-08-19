package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/medha/backend/internal/event/domain"
	"github.com/medha/backend/internal/infra/storage"
	"github.com/medha/backend/internal/platform/epoch"
	"github.com/medha/backend/internal/server/middleware"
	apierrors "github.com/medha/backend/pkg/errors"
	"github.com/medha/backend/pkg/response"
)

// LocationDTO represents a latitude/longitude pair in API payloads.
type LocationDTO struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// EventResponse represents an event in API responses.
type EventResponse struct {
	ID                        uuid.UUID    `json:"id"`
	YajmanID                  uuid.UUID    `json:"yajman_id"`
	CeremonyType              string       `json:"ceremony_type"`
	CustomCeremonyName        string       `json:"custom_ceremony_name,omitempty"`
	CustomCeremonyDescription string       `json:"custom_ceremony_description,omitempty"`
	EventDate                 string       `json:"event_date"`
	Location                  *LocationDTO `json:"location"`
	Address                   string       `json:"address"`
	Description               string       `json:"description"`
	Status                    string       `json:"status"`
	CeremonyLogoURL           *string      `json:"ceremony_logo_url,omitempty"`
	PanditPhoneNumber         *string      `json:"pandit_phone_number,omitempty"`
	PanditName                *string      `json:"pandit_name,omitempty"`
	ConversationID            *uuid.UUID   `json:"conversation_id,omitempty"` // Present only when Booked
	YajmanPhoneNumber         *string      `json:"yajman_phone_number,omitempty"`
	CreatedAt                 string       `json:"created_at"`
	UpdatedAt                 string       `json:"updated_at"`
}

// CeremonyDTO represents a ceremony in catalog responses.
type CeremonyDTO struct {
	ID           string `json:"id"`
	Slug         string `json:"slug"`
	DisplayName  string `json:"display_name"`
	ImageURL     string `json:"image_url"`
	LogoURL      string `json:"logo_url"`
	Category     string `json:"category"`
	Description  string `json:"description"`
	ImageURLNoBg string `json:"image_url_no_bg"`
	DisplayOrder int    `json:"display_order"`
	IsActive     bool   `json:"is_active"`
}

// GetEvent handles GET /api/v2/event/{id}.
func (h *EventHandler) GetEvent(w http.ResponseWriter, r *http.Request) {
	eventID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("The event ID provided is not valid.", r.URL.Path))
		return
	}

	event, err := h.eventService.GetByID(r.Context(), eventID)
	if err != nil {
		if errors.Is(err, domain.ErrEventNotFound) {
			apierrors.WriteProblemDetail(w, apierrors.NotFound("This event could not be found.", r.URL.Path))
			return
		}
		h.logger.Error("get event failed", "error", err, "event_id", eventID)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Unable to load event details. Please try again later.", r.URL.Path))
		return
	}

	role, _ := middleware.UserRoleFromContext(r.Context())
	userID, _ := middleware.UserIDFromContext(r.Context())
	if (role == "pandit" || (role == "yajman" && event.YajmanID != userID)) && event.Status == domain.EventStatusCreated {
		apierrors.WriteProblemDetail(w, apierrors.NotFound("This event could not be found.", r.URL.Path))
		return
	}

	response.WriteData(w, http.StatusOK, h.toEventResponse(r.Context(), event))
}

// ListCeremonies handles GET /api/v2/ceremony.
func (h *EventHandler) ListCeremonies(w http.ResponseWriter, r *http.Request) {
	ceremonies, err := h.eventService.ListCeremonies(r.Context())
	if err != nil {
		h.logger.Error("list ceremonies failed", "error", err)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Unable to load ceremony types. Please try again later.", r.URL.Path))
		return
	}

	dtos := make([]CeremonyDTO, len(ceremonies))
	for i, c := range ceremonies {
		imageURL, logoURL, imageURLNoBg := h.resolveCeremonyURLs(c)
		dtos[i] = CeremonyDTO{
			ID:           c.ID.String(),
			Slug:         c.Slug,
			DisplayName:  c.DisplayName,
			ImageURL:     imageURL,
			LogoURL:      logoURL,
			Category:     c.Category,
			Description:  c.Description,
			ImageURLNoBg: imageURLNoBg,
			DisplayOrder: c.DisplayOrder,
			IsActive:     c.IsActive,
		}
	}

	response.WriteData(w, http.StatusOK, dtos)
}

func (h *EventHandler) resolveCeremonyURLs(c *domain.Ceremony) (imageURL, logoURL, noBgURL string) {
	bucket := storage.BucketFestivalLogos

	targetKey := c.ImageURL
	if targetKey == "" {
		targetKey = c.Slug + ".png"
	}

	targetNoBgKey := c.ImageURLNoBg
	if targetNoBgKey == "" {
		targetNoBgKey = c.Slug + "-no-bg.png"
	}

	if h.s3Client == nil {
		// Fallback for local development if S3 storage is not configured
		imageURL = "http://localhost:8333/" + bucket + "/" + targetKey
		noBgURL = "http://localhost:8333/" + bucket + "/" + targetNoBgKey
		return imageURL, imageURL, noBgURL
	}

	imageURL = h.s3Client.ResolveURLForBucket(bucket, targetKey)
	logoURL = imageURL
	noBgURL = h.s3Client.ResolveURLForBucket(bucket, targetNoBgKey)

	return imageURL, logoURL, noBgURL
}

// --- Helpers ---

func getDisplayStatus(event *domain.Event) string {
	if event.Status == domain.EventStatusCancelled {
		return event.Status.String()
	}

	eventTime := epoch.ToTime(event.EventDate).UTC()
	today := time.Now().UTC().Truncate(24 * time.Hour)
	eventDay := eventTime.Truncate(24 * time.Hour)

	if eventDay.Before(today) {
		return domain.EventStatusCompleted.String()
	}
	if eventDay.Equal(today) {
		if event.Status == domain.EventStatusBooked {
			return domain.EventStatusActive.String()
		}
	}

	return event.Status.String()
}

func (h *EventHandler) toEventResponse(ctx context.Context, event *domain.Event) EventResponse {
	displayStatus := getDisplayStatus(event)
	role, _ := middleware.UserRoleFromContext(ctx)

	if role == "yajman" {
		// Yajaman sees 'Pushed'/'Pending' as 'Active'
		if event.Status == domain.EventStatusPushed || event.Status == domain.EventStatusPending {
			displayStatus = "Active"
		}
	} else if role == "pandit" {
		// Pandit sees 'Pushed'/'Pending' as 'Active' (or 'Interested' if they have expressed interest)
		if event.Status == domain.EventStatusPushed || event.Status == domain.EventStatusPending {
			panditID, err := middleware.UserIDFromContext(ctx)
			if err == nil {
				var leadStatus string
				err = h.eventService.Pool.QueryRow(ctx, `
					SELECT status FROM job_leads 
					WHERE event_id = $1 AND pandit_id = $2 AND deleted_at IS NULL
				`, event.ID, panditID).Scan(&leadStatus)
				if err == nil && leadStatus == "interested" {
					displayStatus = "Interested"
				} else {
					displayStatus = "Active"
				}
			} else {
				displayStatus = "Active"
			}
		}
	}

	userID, _ := middleware.UserIDFromContext(ctx)
	var yajmanPhone *string = nil
	if userID == event.YajmanID {
		yajmanPhone = event.YajmanPhoneNumber
	} else if role == "pandit" {
		var matchExists bool
		err := h.eventService.Pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM matches
				WHERE event_id = $1 AND pandit_id = $2 AND status IN ('matched', 'active', 'completed')
			)
		`, event.ID, userID).Scan(&matchExists)
		if err == nil && matchExists {
			yajmanPhone = event.YajmanPhoneNumber
		}
	}

	resp := EventResponse{
		ID:                        event.ID,
		YajmanID:                  event.YajmanID,
		CeremonyType:              event.CeremonyType.String(),
		CustomCeremonyName:        event.CustomCeremonyName,
		CustomCeremonyDescription: event.CustomCeremonyDescription,
		EventDate:                 epoch.ToTime(event.EventDate).Format("2006-01-02"),
		Address:                   event.Address,
		Description:               event.Description,
		Status:                    displayStatus,
		PanditPhoneNumber:         event.PanditPhoneNumber,
		PanditName:                event.PanditName,
		YajmanPhoneNumber:         yajmanPhone,
		CreatedAt:                 epoch.ToTime(event.CreatedAt).Format(time.RFC3339),
		UpdatedAt:                 epoch.ToTime(event.UpdatedAt).Format(time.RFC3339),
	}

	if event.CeremonyLogoURL != nil && *event.CeremonyLogoURL != "" {
		var resolvedLogo string
		if h.s3Client != nil {
			resolvedLogo = h.s3Client.ResolveURLForBucket(storage.BucketFestivalLogos, *event.CeremonyLogoURL)
		} else {
			resolvedLogo = *event.CeremonyLogoURL
		}
		resp.CeremonyLogoURL = &resolvedLogo
	}

	if event.Location != nil {
		resp.Location = &LocationDTO{
			Latitude:  event.Location.Latitude,
			Longitude: event.Location.Longitude,
		}
	}

	if event.Status != domain.EventStatusBooked {
		resp.PanditPhoneNumber = nil
		resp.PanditName = nil
		resp.ConversationID = nil
	} else {
		resp.ConversationID = event.ConversationID
	}

	return resp
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

func NormalizeCeremonyType(slug string) string {
	switch slug {
	case "shraddha":
		return "shraadh"
	case "griha-pravesh":
		return "grihapravesh"
	case "satyanarayan-puja":
		return "satyanarayan"
	case "vastu-shanti-puja":
		return "vastu_shanti"
	case "namakarana":
		return "naamkaran"
	default:
		return strings.ReplaceAll(slug, "-", "_")
	}
}
