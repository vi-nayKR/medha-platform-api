package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"

	"github.com/medha/backend/internal/server/middleware"
	userdomain "github.com/medha/backend/internal/user/domain"
	userservice "github.com/medha/backend/internal/user/service"
	apierrors "github.com/medha/backend/pkg/errors"
	"github.com/medha/backend/pkg/response"
)

type ServiceCitiesHandler struct {
	panditSvc *userservice.PanditService
	validate  *validator.Validate
	logger    *slog.Logger
}

func NewServiceCitiesHandler(panditSvc *userservice.PanditService, logger *slog.Logger) *ServiceCitiesHandler {
	return &ServiceCitiesHandler{
		panditSvc: panditSvc,
		validate:  validator.New(),
		logger:    logger,
	}
}

type SetServiceCitiesRequest struct {
	HomeCityID        uuid.UUID   `json:"homeCityId" validate:"required"`
	AdditionalCityIDs []uuid.UUID `json:"additionalCityIds" validate:"dive"`
}

func (s *SetServiceCitiesRequest) UnmarshalJSON(data []byte) error {
	type Alias SetServiceCitiesRequest
	aux := &struct {
		HomeCityIDCamel        *uuid.UUID   `json:"homeCityId"`
		HomeCityIDSnake        *uuid.UUID   `json:"home_city_id"`
		AdditionalCityIDsCamel []uuid.UUID  `json:"additionalCityIds"`
		AdditionalCityIDsSnake []uuid.UUID  `json:"additional_city_ids"`
		*Alias
	}{
		Alias: (*Alias)(s),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if aux.HomeCityIDSnake != nil {
		s.HomeCityID = *aux.HomeCityIDSnake
	} else if aux.HomeCityIDCamel != nil {
		s.HomeCityID = *aux.HomeCityIDCamel
	}
	if aux.AdditionalCityIDsSnake != nil {
		s.AdditionalCityIDs = aux.AdditionalCityIDsSnake
	} else if aux.AdditionalCityIDsCamel != nil {
		s.AdditionalCityIDs = aux.AdditionalCityIDsCamel
	}
	return nil
}

type ServiceCitiesResponse struct {
	ServiceCities []userdomain.ServiceCityDetail `json:"serviceCities"`
	TotalCities   int                            `json:"totalCities"`
}

// SetServiceCities handles PUT /api/v2/pandit/service-cities
//
// @Summary      Set pandit service cities
// @Description  Replaces the pandit's home and additional serviceable cities.
// @Tags         pandit-v2
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body body SetServiceCitiesRequest true "Service cities payload"
// @Success      200 {object} response.DataResponse{data=ServiceCitiesResponse} "Enriched service cities"
// @Failure      400 {object} apierrors.ProblemDetail "Validation/limit/distance errors"
// @Failure      401 {object} apierrors.ProblemDetail "Unauthorized"
// @Failure      404 {object} apierrors.ProblemDetail "Profile or city not found"
// @Router       /api/v2/pandit/service-cities [put]
func (h *ServiceCitiesHandler) SetServiceCities(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.UserIDFromContext(r.Context())
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.Unauthorized("authentication required", r.URL.Path))
		return
	}

	var req SetServiceCitiesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Invalid request body.", r.URL.Path))
		return
	}

	if err := h.validate.Struct(req); err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Validation failed for parameters.", r.URL.Path))
		return
	}

	list, err := h.panditSvc.SetServiceCities(r.Context(), userID, req.HomeCityID, req.AdditionalCityIDs)
	if err != nil {
		if errors.Is(err, userdomain.ErrPanditProfileNotFound) {
			apierrors.WriteProblemDetail(w, apierrors.NotFound("pandit profile not found", r.URL.Path))
			return
		}
		if errors.Is(err, userdomain.ErrTooManyServiceCities) {
			apierrors.WriteProblemDetail(w, apierrors.BadRequest("maximum 2 additional cities allowed", r.URL.Path))
			return
		}
		if errors.Is(err, userdomain.ErrDuplicateServiceCity) {
			apierrors.WriteProblemDetail(w, apierrors.BadRequest("duplicate city in request", r.URL.Path))
			return
		}
		if errors.Is(err, userdomain.ErrCityTooFar) {
			apierrors.WriteProblemDetail(w, apierrors.BadRequest("city is too far from home city (max 250km)", r.URL.Path))
			return
		}
		if strings.Contains(err.Error(), "not found") {
			apierrors.WriteProblemDetail(w, apierrors.NotFound("one or more cities not found", r.URL.Path))
			return
		}
		h.logger.Error("set service cities failed", "error", err, "user_id", userID)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Failed to update service cities.", r.URL.Path))
		return
	}

	response.WriteData(w, http.StatusOK, ServiceCitiesResponse{
		ServiceCities: list,
		TotalCities:   len(list),
	})
}

// GetServiceCities handles GET /api/v2/pandit/service-cities
//
// @Summary      Get pandit service cities
// @Description  Returns the list of serviceable cities configured by the authenticated pandit.
// @Tags         pandit-v2
// @Produce      json
// @Security     BearerAuth
// @Success      200 {object} response.DataResponse{data=ServiceCitiesResponse} "Enriched service cities"
// @Failure      401 {object} apierrors.ProblemDetail "Unauthorized"
// @Failure      404 {object} apierrors.ProblemDetail "Profile not found"
// @Router       /api/v2/pandit/service-cities [get]
func (h *ServiceCitiesHandler) GetServiceCities(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.UserIDFromContext(r.Context())
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.Unauthorized("authentication required", r.URL.Path))
		return
	}

	list, err := h.panditSvc.GetServiceCities(r.Context(), userID)
	if err != nil {
		if errors.Is(err, userdomain.ErrPanditProfileNotFound) {
			apierrors.WriteProblemDetail(w, apierrors.NotFound("pandit profile not found", r.URL.Path))
			return
		}
		h.logger.Error("get service cities failed", "error", err, "user_id", userID)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Failed to retrieve service cities.", r.URL.Path))
		return
	}

	response.WriteData(w, http.StatusOK, ServiceCitiesResponse{
		ServiceCities: list,
		TotalCities:   len(list),
	})
}
