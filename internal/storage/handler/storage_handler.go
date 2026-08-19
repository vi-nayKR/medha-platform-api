package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"

	pkgstorage "github.com/medha/backend/internal/infra/storage"
	"github.com/medha/backend/internal/server/middleware"
	"github.com/medha/backend/internal/user/service"
	apierrors "github.com/medha/backend/pkg/errors"
	"github.com/medha/backend/pkg/response"
)

// StorageHandler handles pre-signed uploads for S3-compatible storage.
type StorageHandler struct {
	s3Client    *pkgstorage.S3Client
	userService *service.UserService
	validate    *validator.Validate
	logger      *slog.Logger
}

// NewStorageHandler creates a new StorageHandler.
func NewStorageHandler(s3Client *pkgstorage.S3Client, userService *service.UserService, logger *slog.Logger) *StorageHandler {
	return &StorageHandler{
		s3Client:    s3Client,
		userService: userService,
		validate:    validator.New(),
		logger:      logger,
	}
}

// PresignRequestDTO is the request body for POST /api/v2/storage/presign.
type PresignRequestDTO struct {
	FileName    string `json:"file_name"    validate:"required,max=255"`
	ContentType string `json:"content_type" validate:"required"`
	UploadType  string `json:"upload_type"  validate:"required,oneof=profile_photo post_media"`
}

// PresignResponseDTO is the response body for POST /api/v2/storage/presign.
type PresignResponseDTO struct {
	UploadURL string `json:"upload_url"`
	ObjectKey string `json:"object_key"`
	ExpiresIn int    `json:"expires_in"` // in seconds
}

// ConfirmRequestDTO is the request body for POST /api/v2/storage/confirm.
type ConfirmRequestDTO struct {
	ObjectKey  string `json:"object_key"  validate:"required"`
	UploadType string `json:"upload_type" validate:"required,oneof=profile_photo post_media"`
}

// ConfirmResponseDTO is the response body for POST /api/v2/storage/confirm.
type ConfirmResponseDTO struct {
	PublicURL string `json:"public_url"`
}

var allowedContentTypes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/webp": true,
	"video/mp4":  true,
}

// GeneratePresignedURL handles POST /api/v2/storage/presign.
//
// @Summary      Get pre-signed upload URL
// @Description  Generates a pre-signed PUT URL for uploading media to S3-compatible storage directly.
// @Tags         storage-v2
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param body body PresignRequestDTO true "Presign request info"
// @Success      200 {object} response.DataResponse{data=PresignResponseDTO} "Presigned URL details"
// @Failure      400 {object} apierrors.ProblemDetail "Validation error"
// @Failure      401 {object} apierrors.ProblemDetail "Unauthorized"
// @Failure      422 {object} apierrors.ProblemDetail "Unprocessable entity (invalid content type)"
// @Router       /api/v2/storage/presign [post]
func (h *StorageHandler) GeneratePresignedURL(w http.ResponseWriter, r *http.Request) {
	if h.s3Client == nil {
		apierrors.WriteProblemDetail(w, apierrors.ServiceUnavailable("Storage service is not configured.", r.URL.Path))
		return
	}

	userID, err := middleware.UserIDFromContext(r.Context())
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.Unauthorized("authentication required", r.URL.Path))
		return
	}

	var req PresignRequestDTO
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Invalid request body.", r.URL.Path))
		return
	}

	if err := h.validate.Struct(req); err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Please provide valid file_name, content_type, and upload_type.", r.URL.Path))
		return
	}

	// Validate content type
	if !allowedContentTypes[req.ContentType] {
		apierrors.WriteProblemDetail(w, apierrors.NewProblemDetail(
			http.StatusUnprocessableEntity,
			"https://medha.app/errors/invalid-content-type",
			"Unprocessable Entity",
			"Invalid content type. Allowed types: image/jpeg, image/png, image/webp, video/mp4",
			r.URL.Path,
		))
		return
	}

	// Strip directory traversal characters
	safeFileName := filepath.Base(req.FileName)
	ext := strings.TrimPrefix(filepath.Ext(safeFileName), ".")
	if ext == "" {
		ext = "bin" // fallback if no extension provided
	}

	var bucket, objectKey string
	switch req.UploadType {
	case "profile_photo":
		bucket = pkgstorage.BucketProfiles
		objectKey = pkgstorage.GenerateProfilePhotoKey(userID, ext)
	case "post_media":
		bucket = pkgstorage.BucketFeeds
		objectKey = pkgstorage.GenerateFeedPhotoKey(userID, ext)
	default:
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Invalid upload_type.", r.URL.Path))
		return
	}

	expiryDuration := 15 * time.Minute
	uploadURL, err := h.s3Client.GeneratePresignedPutURL(r.Context(), bucket, objectKey, expiryDuration)
	if err != nil {
		h.logger.Error("failed to generate presigned upload url", "error", err, "user_id", userID)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Unable to generate upload URL.", r.URL.Path))
		return
	}

	response.WriteData(w, http.StatusOK, PresignResponseDTO{
		UploadURL: uploadURL,
		ObjectKey: objectKey,
		ExpiresIn: int(expiryDuration.Seconds()),
	})
}

// ConfirmUpload handles POST /api/v2/storage/confirm.
//
// @Summary      Confirm media upload
// @Description  Confirms that the client successfully uploaded a file via a pre-signed URL and links it in the database if applicable.
// @Tags         storage-v2
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param body body ConfirmRequestDTO true "Confirm request info"
// @Success      200 {object} response.DataResponse{data=ConfirmResponseDTO} "Upload confirmed"
// @Failure      400 {object} apierrors.ProblemDetail "Bad request (upload not found)"
// @Failure      401 {object} apierrors.ProblemDetail "Unauthorized"
// @Failure      403 {object} apierrors.ProblemDetail "Forbidden (key does not belong to user)"
// @Router       /api/v2/storage/confirm [post]
func (h *StorageHandler) ConfirmUpload(w http.ResponseWriter, r *http.Request) {
	if h.s3Client == nil {
		apierrors.WriteProblemDetail(w, apierrors.ServiceUnavailable("Storage service is not configured.", r.URL.Path))
		return
	}

	userID, err := middleware.UserIDFromContext(r.Context())
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.Unauthorized("authentication required", r.URL.Path))
		return
	}

	var req ConfirmRequestDTO
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Invalid request body.", r.URL.Path))
		return
	}

	if err := h.validate.Struct(req); err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Please provide a valid object_key and upload_type.", r.URL.Path))
		return
	}

	// Make sure the key belongs to the user
	if !strings.Contains(req.ObjectKey, userID.String()) {
		apierrors.WriteProblemDetail(w, apierrors.NewProblemDetail(
			http.StatusForbidden,
			"https://medha.app/errors/forbidden",
			"Forbidden",
			"You do not have permission to confirm this upload.",
			r.URL.Path,
		))
		return
	}

	var bucket string
	switch req.UploadType {
	case "profile_photo":
		bucket = pkgstorage.BucketProfiles
	case "post_media":
		bucket = pkgstorage.BucketFeeds
	}

	// Verify object exists in S3 storage
	exists, err := h.s3Client.ObjectExists(r.Context(), bucket, req.ObjectKey)
	if err != nil {
		h.logger.Error("failed to check if object exists during confirm", "error", err, "object_key", req.ObjectKey)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Failed to verify upload at this time.", r.URL.Path))
		return
	}
	if !exists {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("File not found. Please upload using the pre-signed URL before confirming.", r.URL.Path))
		return
	}

	// Resolving full URL
	publicURL := h.s3Client.ResolveURLForBucket(bucket, req.ObjectKey)

	// Perform backend integrations based on upload type
	switch req.UploadType {
	case "profile_photo":
		if h.userService != nil {
			err = h.userService.UpdateProfilePhoto(r.Context(), userID, publicURL)
			if err != nil {
				h.logger.Error("failed to update profile photo url", "error", err, "user_id", userID)
				apierrors.WriteProblemDetail(w, apierrors.InternalError("Upload verified, but failed to link to your profile. Please try again.", r.URL.Path))
				return
			}
		}
	case "post_media":
		// No direct DB action here, post media is fully linked when the user creates a Post.
		h.logger.Info("post_media confirmed", "object_key", req.ObjectKey, "user_id", userID)
	}

	response.WriteData(w, http.StatusOK, ConfirmResponseDTO{
		PublicURL: publicURL,
	})
}
