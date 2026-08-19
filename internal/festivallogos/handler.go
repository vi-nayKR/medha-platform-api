package festivallogos

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	apierrors "github.com/medha/backend/pkg/errors"
	"github.com/medha/backend/pkg/response"
)

// Handler exposes the festival logos endpoints under /api/v2/admin/festival-logos.
type Handler struct {
	svc    *Service
	logger *slog.Logger
}

// NewHandler creates a new Handler.
func NewHandler(svc *Service, logger *slog.Logger) *Handler {
	return &Handler{svc: svc, logger: logger}
}

// Routes registers all festival logo routes.
// Call as: r.Route("/festival-logos", festivalLogosHandler.Routes)
func (h *Handler) Routes(r chi.Router) {
	r.Get("/", h.List)
	r.Post("/", h.Create)
	r.Post("/seed", h.Seed)
	r.Put("/{id}", h.Update)
	r.Delete("/{id}", h.Delete)
	r.Post("/{id}/image", h.UploadImage)
}

// List returns all festival logo entries.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	festivals, err := h.svc.ListFestivals(r.Context())
	if err != nil {
		h.internal(w, r, "list festival logos", err)
		return
	}
	if festivals == nil {
		festivals = []*FestivalLogo{}
	}
	response.WriteData(w, http.StatusOK, festivals)
}

// Create adds a new festival logo entry.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name         string `json:"name"`
		Slug         string `json:"slug"`
		DisplayOrder int    `json:"display_order"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Invalid request body.", r.URL.Path))
		return
	}

	f, err := h.svc.CreateFestival(r.Context(), req.Name, req.Slug, req.DisplayOrder)
	if err != nil {
		var ve *ValidationError
		if errors.As(err, &ve) {
			apierrors.WriteProblemDetail(w, apierrors.BadRequest(ve.Error(), r.URL.Path))
			return
		}
		h.internal(w, r, "create festival logo", err)
		return
	}
	response.WriteData(w, http.StatusCreated, f)
}

// Update modifies name, is_active, and display_order for a festival logo.
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req struct {
		Name         string `json:"name"`
		IsActive     bool   `json:"is_active"`
		DisplayOrder int    `json:"display_order"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Invalid request body.", r.URL.Path))
		return
	}

	f, err := h.svc.UpdateFestival(r.Context(), id, UpdateParams{
		Name:         req.Name,
		IsActive:     req.IsActive,
		DisplayOrder: req.DisplayOrder,
	})
	if err != nil {
		var ve *ValidationError
		if errors.As(err, &ve) {
			apierrors.WriteProblemDetail(w, apierrors.BadRequest(ve.Error(), r.URL.Path))
			return
		}
		h.internal(w, r, "update festival logo", err)
		return
	}
	if f == nil {
		apierrors.WriteProblemDetail(w, apierrors.NotFound("Festival logo not found.", r.URL.Path))
		return
	}
	response.WriteData(w, http.StatusOK, f)
}

// Delete removes a festival logo.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.svc.DeleteFestival(r.Context(), id); err != nil {
		h.internal(w, r, "delete festival logo", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// UploadImage accepts a multipart PNG upload and stores it in S3-compatible storage.
// Query param: ?variant=default or ?variant=no_bg
func (h *Handler) UploadImage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	variant := r.URL.Query().Get("variant")
	if variant == "" {
		variant = "default"
	}

	if err := r.ParseMultipartForm(2 << 20); err != nil { // 2 MB memory limit
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Could not parse multipart form: "+err.Error(), r.URL.Path))
		return
	}

	file, header, err := r.FormFile("image")
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Missing image field in form.", r.URL.Path))
		return
	}
	defer file.Close()

	f, err := h.svc.UploadFestivalImage(r.Context(), id, variant, file, header)
	if err != nil {
		var ve *ValidationError
		if errors.As(err, &ve) {
			apierrors.WriteProblemDetail(w, apierrors.BadRequest(ve.Error(), r.URL.Path))
			return
		}
		h.internal(w, r, "upload festival image", err)
		return
	}
	response.WriteData(w, http.StatusOK, f)
}

// Seed triggers the seeding process from a local directory.
// Body: {"dir_path": "/path/to/pngs"}
func (h *Handler) Seed(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DirPath string `json:"dir_path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Invalid request body.", r.URL.Path))
		return
	}
	if req.DirPath == "" {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("dir_path is required.", r.URL.Path))
		return
	}

	seeded, results, err := h.svc.SeedFestivals(r.Context(), req.DirPath)
	if err != nil {
		h.internal(w, r, "seed festival logos", err)
		return
	}

	response.WriteData(w, http.StatusOK, map[string]any{
		"seeded":  seeded,
		"results": results,
	})
}

func (h *Handler) internal(w http.ResponseWriter, r *http.Request, action string, err error) {
	h.logger.Error("festival logos handler error", "action", action, "error", err)
	apierrors.WriteProblemDetail(w, apierrors.InternalError("An internal error occurred.", r.URL.Path))
}
