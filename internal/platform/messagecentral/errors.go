package messagecentral

import "errors"

// Sentinel errors returned by the MessageCentral client.
// Handlers map these to appropriate RFC 7807 responses.
var (
	// ErrWrongOTP is returned when the OTP code is incorrect (MC response code 702).
	ErrWrongOTP = errors.New("wrong OTP provided")

	// ErrAlreadyVerified is returned when the OTP session was already used (MC response code 703).
	ErrAlreadyVerified = errors.New("OTP already verified")

	// ErrOTPExpired is returned when the OTP has expired (MC response code 705).
	ErrOTPExpired = errors.New("OTP expired, please request a new one")

	// ErrMaxAttempts is returned when max verification attempts exceeded (MC response code 800).
	ErrMaxAttempts = errors.New("too many OTP attempts")

	// ErrRateLimitExceeded is returned when the send rate limit has been hit.
	ErrRateLimitExceeded = errors.New("OTP send rate limit exceeded")

	// ErrInvalidPhone is returned when the phone number is invalid or rejected by the provider.
	ErrInvalidPhone = errors.New("invalid phone number")

	// ErrProviderDown is returned on 5xx responses from MessageCentral.
	ErrProviderDown = errors.New("OTP service temporarily unavailable")
)
