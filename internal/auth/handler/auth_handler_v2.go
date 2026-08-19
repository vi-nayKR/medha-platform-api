package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/redis/go-redis/v9"

	authdomain "github.com/medha/backend/internal/auth/domain"
	authsvc "github.com/medha/backend/internal/auth/service"
	mc "github.com/medha/backend/internal/platform/messagecentral"
	apierrors "github.com/medha/backend/pkg/errors"
	"github.com/medha/backend/pkg/response"
)

// AuthHandlerV2 handles V2 authentication HTTP endpoints.
type AuthHandlerV2 struct {
	authService *authsvc.AuthServiceV2
	redis       *redis.Client
	validate    *validator.Validate
	logger      *slog.Logger
	panditPhone string // DEV-ONLY: phone of the hardcoded pandit dev account
	yajmanPhone string // DEV-ONLY: phone of the hardcoded yajman dev account
}

// NewAuthHandlerV2 creates a new AuthHandlerV2.
func NewAuthHandlerV2(
	authService *authsvc.AuthServiceV2,
	redis *redis.Client,
	logger *slog.Logger,
	panditPhone string,
	yajmanPhone string,
) *AuthHandlerV2 {
	return &AuthHandlerV2{
		authService: authService,
		redis:       redis,
		validate:    validator.New(),
		logger:      logger,
		panditPhone: panditPhone,
		yajmanPhone: yajmanPhone,
	}
}

// --- Request / Response DTOs ---

var phoneE164Pattern = regexp.MustCompile(`^\+[1-9]\d{7,14}$`)

// SendOTPRequest is the request body for POST /api/v2/auth/send-otp.
type SendOTPRequest struct {
	Phone string `json:"phone" validate:"required" example:"+919999999999"`
}

// RefreshTokenRequest is the request body for POST /api/v2/auth/refresh.
type RefreshTokenRequest struct {
	RefreshToken string `json:"refresh_token" validate:"required" example:"a1b2c3d4e5f6..."`
}

// RefreshTokenResponse is the response body for POST /api/v2/auth/refresh.
type RefreshTokenResponse struct {
	AccessToken  string `json:"access_token"  example:"eyJhbGciOiJSUzI1NiJ9..."`
	RefreshToken string `json:"refresh_token" example:"x9y8z7w6v5u4..."`
	TokenType    string `json:"token_type"    example:"Bearer"`
	ExpiresIn    int    `json:"expires_in"    example:"900"`
}

// LogoutRequestV2 is the request body for POST /api/v2/auth/logout.
type LogoutRequestV2 struct {
	RefreshToken string `json:"refresh_token" validate:"required" example:"a1b2c3d4e5f6..."`
}

// --- Handlers ---

// SendOTP handles POST /api/v2/auth/send-otp.
//
// @Summary      Send OTP to phone number
// @Description  Sends a 6-digit OTP via SMS to the given phone number. Rate limited to 3 requests per hour per number.
// @Tags         auth-v2
// @Accept       json
// @Produce      json
// @Param body body SendOTPRequest true "Phone number in E.164 format"
// @Success      200 {object} response.DataResponse{data=authdomain.SendOTPResponse} "OTP sent"
// @Failure      400 {object} apierrors.ProblemDetail "Invalid phone number"
// @Failure      429 {object} apierrors.ProblemDetail "Rate limit exceeded"
// @Failure      503 {object} apierrors.ProblemDetail "OTP service unavailable"
// @Router       /api/v2/auth/send-otp [post]
func (h *AuthHandlerV2) SendOTP(w http.ResponseWriter, r *http.Request) {
	var req SendOTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Please provide a valid request.", r.URL.Path))
		return
	}

	if !phoneE164Pattern.MatchString(req.Phone) {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest(
			"Please enter a correct phone number (e.g. +919999999999).",
			r.URL.Path,
		))
		return
	}

	// Proactive validation: Indian phone numbers starting with +91 must have exactly 10 digits after +91.
	if strings.HasPrefix(req.Phone, "+91") {
		local := req.Phone[3:]
		if len(local) != 10 {
			apierrors.WriteProblemDetail(w, apierrors.BadRequest(
				"Indian phone numbers must be exactly 10 digits.",
				r.URL.Path,
			))
			return
		}
	}

	result, err := h.authService.SendOTP(r.Context(), req.Phone)
	if err != nil {
		h.mapSendOTPError(w, r, err)
		return
	}

	h.deleteRecentlyVerified(r.Context(), req.Phone)

	response.WriteData(w, http.StatusOK, authdomain.SendOTPResponse{
		Message:        "OTP sent successfully",
		VerificationID: result.VerificationID,
		ExpiresIn:      result.ExpiresInSeconds,
	})
}

// GetJWKS handles GET /.well-known/jwks.json
//
// @Summary      Get JWKS
// @Description  Returns the public keys in JWKS format for verifying JWTs issued by Medha.
// @Tags         auth-v2
// @Produce      json
// @Success      200 {object} map[string]interface{}
// @Router       /.well-known/jwks.json [get]
func (h *AuthHandlerV2) GetJWKS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(h.authService.GetJWKS())
}

func (h *AuthHandlerV2) mapSendOTPError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, mc.ErrInvalidPhone) {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest(
			"Please enter a valid phone number.",
			r.URL.Path,
		))
		return
	}
	if errors.Is(err, mc.ErrRateLimitExceeded) {
		apierrors.WriteProblemDetail(w, apierrors.NewProblemDetail(
			http.StatusTooManyRequests,
			"https://medha.app/errors/rate-limit-exceeded",
			"Too Many Requests",
			"OTP send limit exceeded. Please try again in an hour.",
			r.URL.Path,
		))
		return
	}
	if errors.Is(err, mc.ErrProviderDown) {
		apierrors.WriteProblemDetail(w, apierrors.NewProblemDetail(
			http.StatusServiceUnavailable,
			"https://medha.app/errors/otp-service-unavailable",
			"OTP Service Unavailable",
			"The OTP service is temporarily unavailable. Please try again later.",
			r.URL.Path,
		))
		return
	}
	h.logger.Error("send otp failed", "error", err)
	apierrors.WriteProblemDetail(w, apierrors.InternalError("Unable to send OTP. Please try again later.", r.URL.Path))
}

// VerifyOTP handles GET /api/v2/auth/verify-otp.
//
// @Summary      Verify OTP code
// @Description  Validates the 6-digit OTP. Creates a new user if not found, or logs in the existing user. Returns the unified auth response. Note: This is an HTTP GET request, so phone and otp must be sent as query parameters.
// @Tags         auth-v2
// @Accept       json
// @Produce      json
// @Param        phone  query     string  true  "Phone number in E.164 format"  example("+919999999999")
// @Param        otp    query     string  true  "6-digit OTP"                   example("123456")
// @Success      200 {object} response.DataResponse{data=authdomain.AuthResponse} "Authentication successful"
// @Failure      400 {object} apierrors.ProblemDetail "Invalid OTP or phone"
// @Failure      429 {object} apierrors.ProblemDetail "Too many attempts"
// @Failure      503 {object} apierrors.ProblemDetail "OTP service unavailable"
// @Router       /api/v2/auth/verify-otp [get]
func (h *AuthHandlerV2) VerifyOTP(w http.ResponseWriter, r *http.Request) {
	// 1. Try query parameters
	phone := r.URL.Query().Get("phone")
	otp := r.URL.Query().Get("otp")
	verificationID := r.URL.Query().Get("verification_id")
	if verificationID == "" {
		verificationID = r.URL.Query().Get("verificationId")
	}

	// 2. Fallback to JSON body (even for GET)
	var bodyReq struct {
		Phone          string `json:"phone"`
		OTP            string `json:"otp"`
		Code           string `json:"code"` // Support 'code' alias
		VerificationID string `json:"verification_id"`
	}
	// Best-effort decode; request is still valid if params are in query.
	_ = json.NewDecoder(r.Body).Decode(&bodyReq)

	if phone == "" {
		phone = bodyReq.Phone
	}
	if otp == "" {
		if bodyReq.OTP != "" {
			otp = bodyReq.OTP
		} else {
			otp = bodyReq.Code
		}
	}
	if verificationID == "" {
		verificationID = bodyReq.VerificationID
	}

	if phone == "" || otp == "" {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest(
			"Phone number and OTP are required.",
			r.URL.Path,
		))
		return
	}

	if !phoneE164Pattern.MatchString(phone) {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest(
			"Please enter a valid phone number.",
			r.URL.Path,
		))
		return
	}

	// Proactive validation: Indian phone numbers starting with +91 must have exactly 10 digits after +91.
	if strings.HasPrefix(phone, "+91") {
		local := phone[3:]
		if len(local) != 10 {
			apierrors.WriteProblemDetail(w, apierrors.BadRequest(
				"Indian phone numbers must be exactly 10 digits.",
				r.URL.Path,
			))
			return
		}
	}
	if len(otp) != 6 {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest(
			"OTP must be exactly 6 digits.",
			r.URL.Path,
		))
		return
	}

	// Prefer caller-provided verification_id, fallback to Redis for backward compatibility.
	clientProvidedVerificationID := verificationID
	if verificationID == "" {
		var err error
		verificationID, err = h.getVerificationID(r.Context(), phone)
		if err != nil {
			if h.isRecentlyVerified(r.Context(), phone) {
				apierrors.WriteProblemDetail(w, apierrors.BadRequest(
					"This OTP has already been verified and used. Please request a new OTP to log in.",
					r.URL.Path,
				))
				return
			}
			apierrors.WriteProblemDetail(w, apierrors.BadRequest(
				"OTP has not been sent to this number. Please request a new OTP.",
				r.URL.Path,
			))
			return
		}
	}

	authResp, err := h.authService.VerifyOTPWithID(r.Context(), phone, verificationID, otp)
	// Safety fallback: if client-provided verification_id is stale, retry once with latest Redis id.
	if err != nil && clientProvidedVerificationID != "" {
		latestID, latestErr := h.getVerificationID(r.Context(), phone)
		if latestErr == nil && latestID != "" && latestID != clientProvidedVerificationID {
			h.logger.Warn(
				"verify otp retrying with latest redis verification id",
				"phone", phone,
				"provided_verification_id", clientProvidedVerificationID,
				"latest_verification_id", latestID,
			)
			authResp, err = h.authService.VerifyOTPWithID(r.Context(), phone, latestID, otp)
		}
	}
	if err != nil {
		h.mapVerifyOTPError(w, r, err)
		return
	}

	// Delete the verificationId from Redis after successful verification
	h.deleteVerificationID(r.Context(), phone)
	h.markAsRecentlyVerified(r.Context(), phone)

	response.WriteData(w, http.StatusOK, authResp)
}

// getVerificationID looks up the verificationId stored by SendOTP in Redis.
func (h *AuthHandlerV2) getVerificationID(ctx context.Context, phone string) (string, error) {
	if phone == "+919999999999" || phone == "+918888888888" {
		return "reviewer-test-verification-id", nil
	}
	if h.redis == nil {
		return "", fmt.Errorf("redis unavailable")
	}
	otpKey := fmt.Sprintf("mc:otp:%s", phone)
	// Use a short timeout so we don't block the request if Redis is slow
	ctx2, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return h.redis.Get(ctx2, otpKey).Result()
}

// deleteVerificationID removes the verificationId from Redis after successful verification.
func (h *AuthHandlerV2) deleteVerificationID(ctx context.Context, phone string) {
	if h.redis == nil {
		return
	}
	otpKey := fmt.Sprintf("mc:otp:%s", phone)
	ctx2, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()
	_ = h.redis.Del(ctx2, otpKey).Err()
}

func (h *AuthHandlerV2) mapVerifyOTPError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, mc.ErrWrongOTP):
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("The OTP entered is incorrect. Please check the code and try again.", r.URL.Path))
	case errors.Is(err, mc.ErrOTPExpired):
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("The OTP is incorrect or has expired. Please check the code or request a new one.", r.URL.Path))
	case errors.Is(err, mc.ErrAlreadyVerified):
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("This OTP has already been verified and used. Please request a new OTP to log in.", r.URL.Path))
	case errors.Is(err, mc.ErrMaxAttempts):
		apierrors.WriteProblemDetail(w, apierrors.NewProblemDetail(
			http.StatusTooManyRequests,
			"https://medha.app/errors/max-attempts",
			"Too Many Requests",
			"Too many incorrect OTP attempts. For security, please request a new OTP and try again.",
			r.URL.Path,
		))
	case errors.Is(err, mc.ErrProviderDown):
		apierrors.WriteProblemDetail(w, apierrors.NewProblemDetail(
			http.StatusServiceUnavailable,
			"https://medha.app/errors/otp-service-unavailable",
			"OTP Service Unavailable",
			"The OTP service is temporarily unavailable. Please try again later.",
			r.URL.Path,
		))
	case errors.Is(err, authdomain.ErrAccountCannotAuthenticate):
		apierrors.WriteProblemDetail(w, apierrors.Forbidden("This account cannot sign in.", r.URL.Path))
	default:
		h.logger.Error("verify otp failed", "error", err)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("OTP verification failed. Please request a new OTP and try again.", r.URL.Path))
	}
}

// RefreshToken handles POST /api/v2/auth/refresh.
//
// @Summary      Refresh access token
// @Description  Uses a valid refresh token to obtain a new access + refresh token pair. The old refresh token is revoked (rotation).
// @Tags         auth-v2
// @Accept       json
// @Produce      json
// @Param body body RefreshTokenRequest true "Refresh token"
// @Success      200 {object} response.DataResponse{data=RefreshTokenResponse} "Token refreshed"
// @Failure      400 {object} apierrors.ProblemDetail "Bad request"
// @Failure      401 {object} apierrors.ProblemDetail "Invalid or expired refresh token"
// @Router       /api/v2/auth/refresh [post]
func (h *AuthHandlerV2) RefreshToken(w http.ResponseWriter, r *http.Request) {
	var req RefreshTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Please provide a valid request.", r.URL.Path))
		return
	}
	if req.RefreshToken == "" {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("refresh_token is required", r.URL.Path))
		return
	}

	tokenPair, err := h.authService.RefreshToken(r.Context(), req.RefreshToken)
	if err != nil {
		if errors.Is(err, authdomain.ErrInvalidRefreshToken) {
			apierrors.WriteProblemDetail(w, apierrors.NewProblemDetail(
				http.StatusUnauthorized,
				"https://medha.app/errors/invalid-refresh-token",
				"Refresh Token Invalid",
				"Your session has expired or the refresh token is invalid. Please log in again.",
				r.URL.Path,
			))
			return
		}
		if errors.Is(err, authdomain.ErrUserDeactivated) {
			apierrors.WriteProblemDetail(w, apierrors.NewProblemDetail(
				http.StatusForbidden,
				"https://medha.app/errors/account-deactivated",
				"Account Deactivated",
				"Your account has been deactivated. Please contact support for assistance.",
				r.URL.Path,
			))
			return
		}
		if errors.Is(err, authdomain.ErrAccountCannotAuthenticate) {
			apierrors.WriteProblemDetail(w, apierrors.Forbidden("This account cannot sign in.", r.URL.Path))
			return
		}
		h.logger.Error("token refresh failed", "error", err)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Unable to refresh your session. Please log in again.", r.URL.Path))
		return
	}

	response.WriteData(w, http.StatusOK, RefreshTokenResponse{
		AccessToken:  tokenPair.AccessToken,
		RefreshToken: tokenPair.RefreshToken,
		TokenType:    "Bearer",
		ExpiresIn:    tokenPair.ExpiresIn,
	})
}

// Logout handles POST /api/v2/auth/logout.
//
// @Summary      Logout (revoke refresh token)
// @Description  Revokes the given refresh token, effectively logging the user out of that session. Idempotent — calling with an already-revoked token returns 200.
// @Tags         auth-v2
// @Accept       json
// @Produce      json
// @Param body body LogoutRequestV2 true "Refresh token to revoke"
// @Success      200 {object} response.DataResponse{data=interface{}} "Logout successful"
// @Failure      400 {object} apierrors.ProblemDetail "Bad request"
// @Router       /api/v2/auth/logout [post]
func (h *AuthHandlerV2) Logout(w http.ResponseWriter, r *http.Request) {
	var req LogoutRequestV2
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Please provide a valid request.", r.URL.Path))
		return
	}
	if req.RefreshToken == "" {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("refresh_token is required", r.URL.Path))
		return
	}

	if err := h.authService.Logout(r.Context(), req.RefreshToken); err != nil {
		h.logger.Error("logout failed", "error", err)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Logout failed. Please try again.", r.URL.Path))
		return
	}

	response.WriteData(w, http.StatusOK, map[string]string{"message": "logged out successfully"})
}

// markAsRecentlyVerified stores a temporary marker in Redis indicating that the OTP was successfully verified.
func (h *AuthHandlerV2) markAsRecentlyVerified(ctx context.Context, phone string) {
	if h.redis == nil {
		return
	}
	verifiedKey := fmt.Sprintf("mc:verified:%s", phone)
	ctx2, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()
	_ = h.redis.Set(ctx2, verifiedKey, "true", 5*time.Minute).Err()
}

// isRecentlyVerified checks if there is a recently verified marker in Redis.
func (h *AuthHandlerV2) isRecentlyVerified(ctx context.Context, phone string) bool {
	if h.redis == nil {
		return false
	}
	verifiedKey := fmt.Sprintf("mc:verified:%s", phone)
	ctx2, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()
	val, err := h.redis.Get(ctx2, verifiedKey).Result()
	return err == nil && val == "true"
}

// deleteRecentlyVerified removes the recently verified marker from Redis.
func (h *AuthHandlerV2) deleteRecentlyVerified(ctx context.Context, phone string) {
	if h.redis == nil {
		return
	}
	verifiedKey := fmt.Sprintf("mc:verified:%s", phone)
	ctx2, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()
	_ = h.redis.Del(ctx2, verifiedKey).Err()
}

// DevLoginRequestV2 is the request body for POST /api/v2/auth/dev-login.
type DevLoginRequestV2 struct {
	Role  string `json:"role"  example:"pandit"`        // "pandit" or "yajman" (backward compatible)
	Phone string `json:"phone" example:"+919999999999"` // alternative: login by phone directly
}

// DevUserDTO represents a dev account in the dev-users list response.
type DevUserDTO struct {
	Role      string `json:"role"  example:"pandit"`
	Phone     string `json:"phone" example:"+919999999999"`
	FirstName string `json:"first_name" example:"Koushik"`
}

// devEnvGuard blocks dev-only endpoints outside development environments.
// Fail closed: these endpoints bypass OTP / expose user data, so they are only
// available when ENVIRONMENT is explicitly "development" or "dev".
// Returns false (after writing a 403) if the request must not proceed.
func devEnvGuard(w http.ResponseWriter, r *http.Request) bool {
	env := os.Getenv("ENVIRONMENT")
	if env == "development" || env == "dev" {
		return true
	}
	apierrors.WriteProblemDetail(w, apierrors.NewProblemDetail(
		http.StatusForbidden,
		"https://medha.app/errors/forbidden",
		"Forbidden",
		"This endpoint is disabled outside development.",
		r.URL.Path,
	))
	return false
}

// DevUsers handles GET /api/v2/auth/dev-users.
//
// @Summary      List available dev accounts
// @Description  Returns all users in the database that have a phone number, sorted by role then first name. Only available in non-production environments.
// @Tags         auth-v2
// @Produce      json
// @Success      200 {object} response.DataResponse{data=[]DevUserDTO} "Dev accounts list"
// @Failure      403 {object} apierrors.ProblemDetail "Disabled in production"
// @Failure      500 {object} apierrors.ProblemDetail "Internal server error"
// @Router       /api/v2/auth/dev-users [get]
func (h *AuthHandlerV2) DevUsers(w http.ResponseWriter, r *http.Request) {
	if !devEnvGuard(w, r) {
		return
	}

	devUsers, err := h.authService.ListDevUsers(r.Context(), h.panditPhone, h.yajmanPhone)
	if err != nil {
		h.logger.Error("failed to list dev users", "error", err)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Failed to list dev accounts.", r.URL.Path))
		return
	}

	users := make([]DevUserDTO, 0, len(devUsers))
	for _, u := range devUsers {
		users = append(users, DevUserDTO{
			Role:      u.Role,
			Phone:     u.Phone,
			FirstName: u.FirstName,
		})
	}

	response.WriteData(w, http.StatusOK, users)
}

// DevLogin handles POST /api/v2/auth/dev-login.
//
// @Summary      Dev-only login bypass
// @Description  Bypasses OTP verification for development. Accepts either a role ("pandit"/"yajman") or a direct phone number from the dev account list. Returns a real JWT + refresh token pair at zero cost.
// @Tags         auth-v2
// @Accept       json
// @Produce      json
// @Param body body DevLoginRequestV2 true "Login by role or phone"
// @Success      200 {object} response.DataResponse{data=authdomain.AuthResponse} "Authentication successful"
// @Failure      400 {object} apierrors.ProblemDetail "Bad request"
// @Failure      403 {object} apierrors.ProblemDetail "Disabled in production or phone not allowed"
// @Router       /api/v2/auth/dev-login [post]
func (h *AuthHandlerV2) DevLogin(w http.ResponseWriter, r *http.Request) {
	if !devEnvGuard(w, r) {
		return
	}

	var req DevLoginRequestV2
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Invalid JSON body", r.URL.Path))
		return
	}

	var phone string

	// Option 1: Login by direct phone number
	if req.Phone != "" {
		phone = req.Phone
	} else if req.Role != "" {
		// Option 2: Login by role (backward compatible — uses the first DB user with that role)
		switch req.Role {
		case "pandit":
			phone = h.panditPhone
		case "yajman":
			phone = h.yajmanPhone
		default:
			apierrors.WriteProblemDetail(w, apierrors.BadRequest(
				"Valid role is required (pandit or yajman), or provide a phone number.",
				r.URL.Path,
			))
			return
		}
	} else {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest(
			"Either 'role' (pandit/yajman) or 'phone' is required.",
			r.URL.Path,
		))
		return
	}

	if phone == "" || (phone != h.panditPhone && phone != h.yajmanPhone) {
		apierrors.WriteProblemDetail(w, apierrors.NewProblemDetail(
			http.StatusForbidden,
			"https://medha.app/errors/forbidden",
			"Forbidden",
			"Dev login is only allowed for configured developer accounts.",
			r.URL.Path,
		))
		return
	}

	authResp, err := h.authService.DevLoginByRole(r.Context(), phone)
	if err != nil {
		h.logger.Error("dev login failed", "error", err, "role", req.Role)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Dev login failed", r.URL.Path))
		return
	}

	response.WriteData(w, http.StatusOK, authResp)
}
