package domain

import "github.com/google/uuid"

// UserInfo is the user sub-object embedded in every V2 auth response.
type UserInfo struct {
	ID              uuid.UUID `json:"id"                         example:"550e8400-e29b-41d4-a716-446655440000"`
	Phone           string    `json:"phone,omitempty"             example:"+919999999999"`
	Email           string    `json:"email,omitempty"             example:"user@example.com"`
	FirstName       string    `json:"first_name"                  example:"Synthetic"`
	LastName        string    `json:"last_name"                   example:"User"`
	Role            string    `json:"role,omitempty"              example:"yajman"`
	ProfilePhotoURL string    `json:"profile_photo_url,omitempty" example:"https://cdn.medha.app/photos/abc.jpg"`
}

// AuthResponse is the unified response returned by ALL V2 auth endpoints that issue tokens.
// The frontend uses the flags (IsNewUser, IsProfileComplete, IsPhoneVerified) to decide
// which screen to show next.
type AuthResponse struct {
	AccessToken       string   `json:"access_token"        example:"eyJhbGciOiJSUzI1NiJ9..."`
	RefreshToken      string   `json:"refresh_token"       example:"a1b2c3d4e5f6..."`
	TokenType         string   `json:"token_type"          example:"Bearer"`
	ExpiresIn         int      `json:"expires_in"          example:"900"`
	IsNewUser         bool     `json:"is_new_user"         example:"false"`
	IsProfileComplete bool     `json:"is_profile_complete" example:"true"`
	IsPhoneVerified   bool     `json:"is_phone_verified"   example:"true"`
	User              UserInfo `json:"user"`
}

// SendOTPResponse is returned by POST /api/v2/auth/send-otp.
type SendOTPResponse struct {
	Message        string `json:"message"         example:"OTP sent successfully"`
	VerificationID string `json:"verification_id" example:"mc_verif_id_stored_in_redis"`
	ExpiresIn      int    `json:"expires_in_seconds" example:"600"`
}
