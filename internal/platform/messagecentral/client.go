package messagecentral

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	redisAuthTokenKey = "mc:auth_token"
	authTokenTTL      = 23 * time.Hour
	otpRateLimitTTL   = time.Hour
	maxSendsPerHour   = 20
	otpVerifTTL       = 10 * time.Minute
)

// Client defines the interface for MessageCentral OTP operations.
type Client interface {
	// SendOTP sends a 6-digit OTP to the given phone number via SMS.
	// phone must be in E.164 format (e.g. "+919999999999").
	// countryCode is the numeric country prefix without the leading + (e.g. "91").
	// Returns the verificationId that must be passed to VerifyOTP.
	SendOTP(ctx context.Context, phone string, countryCode string) (verificationID string, err error)

	// VerifyOTP validates the supplied OTP code for the given verificationId.
	// Returns nil on success; a typed sentinel error otherwise.
	VerifyOTP(ctx context.Context, verificationID string, otp string) error
}

// messageCentralClient is the concrete implementation of Client.
type messageCentralClient struct {
	customerID string
	keyBase64  string
	email      string
	baseURL    string
	httpClient *http.Client
	redis      *redis.Client
	logger     *slog.Logger
}

// NewClient returns a new MessageCentral Client.
func NewClient(customerID, keyBase64, email, baseURL string, redis *redis.Client, logger *slog.Logger) Client {
	if baseURL == "" {
		baseURL = "https://cpaas.messagecentral.com"
	}
	return &messageCentralClient{
		customerID: customerID,
		keyBase64:  keyBase64,
		email:      email,
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		redis:      redis,
		logger:     logger,
	}
}

// --- auth token management ---

type mcAuthResponse struct {
	Status int    `json:"status"`
	Token  string `json:"token"`
}

func (c *messageCentralClient) getAuthToken(ctx context.Context) (string, error) {
	// 1. Try Redis cache
	if c.redis != nil {
		token, err := c.redis.Get(ctx, redisAuthTokenKey).Result()
		if err == nil && token != "" {
			return token, nil
		}
	}

	// 2. Fetch fresh token
	apiKey := c.keyBase64

	url := fmt.Sprintf(
		"%s/auth/v1/authentication/token?customerId=%s&key=%s&scope=NEW&country=91&email=%s",
		c.baseURL, c.customerID, apiKey, c.email,
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("build mc auth request: %w", err)
	}
	req.Header.Set("accept", "*/*")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("http get mc auth token: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	c.logger.Debug("mc auth raw response", "body", string(body))

	if resp.StatusCode >= 500 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("%w: mc auth http %d: %s", ErrProviderDown, resp.StatusCode, string(body))
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("mc auth failed (http %d): %s", resp.StatusCode, string(body))
	}

	var authResp mcAuthResponse
	if err := json.Unmarshal(body, &authResp); err != nil {
		return "", fmt.Errorf("decode mc auth response: %w (body: %s)", err, string(body))
	}

	if authResp.Status != 200 {
		return "", fmt.Errorf("mc auth error (status %d): %s", authResp.Status, string(body))
	}
	token := authResp.Token
	if token == "" {
		return "", fmt.Errorf("empty token in mc auth response")
	}

	// 3. Cache in Redis (best-effort)
	if c.redis != nil {
		if err := c.redis.Set(ctx, redisAuthTokenKey, token, authTokenTTL).Err(); err != nil {
			c.logger.Warn("failed to cache mc auth token in redis", "error", err)
		}
	}

	return token, nil
}

// --- SendOTP ---

type sendOTPResponse struct {
	ResponseCode int    `json:"responseCode"`
	Message      string `json:"message"`
	ErrorMessage string `json:"errorMessage"`
	Data         struct {
		VerificationID string `json:"verificationId"`
	} `json:"data"`
}

// SendOTP implements Client.
func (c *messageCentralClient) SendOTP(ctx context.Context, phone string, countryCode string) (string, error) {
	// Rate limiting — gracefully skip if Redis is unavailable
	rateLimitKey := fmt.Sprintf("mc:rate:%s", phone)
	if c.redis != nil {
		count, err := c.redis.Get(ctx, rateLimitKey).Int()
		if err != nil && err != redis.Nil {
			c.logger.Warn("redis unavailable for rate limit check; skipping", "error", err)
		} else if count >= maxSendsPerHour {
			return "", ErrRateLimitExceeded
		}
	}

	token, err := c.getAuthToken(ctx)
	if err != nil {
		return "", err
	}

	// Strip leading + from phone for the mobileNumber param
	mobileNumber := phone
	if len(mobileNumber) > 0 && mobileNumber[0] == '+' {
		mobileNumber = mobileNumber[1:]
		// Remove the country code prefix to get local number
		if len(countryCode) < len(mobileNumber) {
			mobileNumber = mobileNumber[len(countryCode):]
		}
	}

	url := fmt.Sprintf(
		"%s/verification/v3/send?countryCode=%s&flowType=SMS&mobileNumber=%s&otpLength=6",
		c.baseURL, countryCode, mobileNumber,
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return "", fmt.Errorf("create send otp request: %w", err)
	}
	req.Header.Set("authtoken", token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("http post send otp: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 500 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("%w: send otp http %d: %s", ErrProviderDown, resp.StatusCode, string(body))
	}

	var sendResp sendOTPResponse
	if err := json.NewDecoder(resp.Body).Decode(&sendResp); err != nil {
		return "", fmt.Errorf("decode send otp response: %w", err)
	}

	if sendResp.ResponseCode != 200 {
		c.logger.Warn("messagecentral send otp rejected",
			"code", sendResp.ResponseCode,
			"message", sendResp.Message,
			"error_message", sendResp.ErrorMessage,
			"phone", phone,
		)
		if sendResp.ResponseCode == 400 || sendResp.ResponseCode == 401 || sendResp.ResponseCode == 829 ||
			strings.Contains(strings.ToLower(sendResp.Message), "invalid") ||
			strings.Contains(strings.ToLower(sendResp.ErrorMessage), "invalid") {
			return "", ErrInvalidPhone
		}
		return "", fmt.Errorf("%w: send otp mc code %d", ErrProviderDown, sendResp.ResponseCode)
	}

	verificationID := sendResp.Data.VerificationID

	// Store verificationId in Redis (best-effort; if Redis is down we still return the ID)
	otpKey := fmt.Sprintf("mc:otp:%s", phone)
	if c.redis != nil {
		if err := c.redis.Set(ctx, otpKey, verificationID, otpVerifTTL).Err(); err != nil {
			c.logger.Warn("failed to store verification id in redis", "error", err, "phone", phone)
		}
		// Increment rate limit counter
		newCount, incrErr := c.redis.Incr(ctx, rateLimitKey).Result()
		if incrErr != nil {
			c.logger.Warn("failed to increment otp rate limit counter", "error", incrErr)
		} else if newCount == 1 {
			_ = c.redis.Expire(ctx, rateLimitKey, otpRateLimitTTL).Err()
		}
	}

	return verificationID, nil
}

// --- VerifyOTP ---

type validateOTPResponse struct {
	ResponseCode int `json:"responseCode"`
	Data         struct {
		VerificationStatus string `json:"verificationStatus"`
	} `json:"data"`
}

// VerifyOTP implements Client.
func (c *messageCentralClient) VerifyOTP(ctx context.Context, verificationID string, otp string) error {
	token, err := c.getAuthToken(ctx)
	if err != nil {
		return err
	}

	url := fmt.Sprintf(
		"%s/verification/v3/validateOtp?verificationId=%s&code=%s&flowType=SMS",
		c.baseURL, verificationID, otp,
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create verify otp request: %w", err)
	}
	req.Header.Set("authtoken", token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http get verify otp: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 500 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%w: verify otp http %d: %s", ErrProviderDown, resp.StatusCode, string(body))
	}

	body, _ := io.ReadAll(resp.Body)
	c.logger.Debug("mc verify raw response", "body", string(body))

	var valResp validateOTPResponse
	if err := json.Unmarshal(body, &valResp); err != nil {
		return fmt.Errorf("decode verify otp response: %w", err)
	}

	statusUpper := strings.ToUpper(strings.TrimSpace(valResp.Data.VerificationStatus))

	switch valResp.ResponseCode {
	case 200:
		// Some provider responses can carry verification outcome in status text.
		switch statusUpper {
		case "", "VERIFICATION_COMPLETED", "ALREADY_VERIFIED", "SUCCESS":
			return nil
		case "VERIFICATION_FAILED", "FAILED", "INVALID_OTP":
			return ErrWrongOTP
		case "OTP_EXPIRED", "EXPIRED":
			return ErrOTPExpired
		}
		return nil
	case 702:
		return ErrWrongOTP
	case 703:
		return ErrAlreadyVerified
	case 705:
		return ErrOTPExpired
	case 800:
		return ErrMaxAttempts
	default:
		// Treat provider "business errors" as user-correctable instead of 5xx.
		if valResp.ResponseCode >= 700 && valResp.ResponseCode < 800 {
			return ErrWrongOTP
		}
		if valResp.ResponseCode == 400 || valResp.ResponseCode == 401 || valResp.ResponseCode == 404 {
			return ErrWrongOTP
		}
		return fmt.Errorf(
			"%w: verify otp mc code %d status=%s",
			ErrProviderDown,
			valResp.ResponseCode,
			statusUpper,
		)
	}
}
