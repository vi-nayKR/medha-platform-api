package config

import (
	"fmt"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

var Version = "dev-local"

// Config holds all application configuration loaded from environment variables.
type Config struct {
	Version     string
	ServerPort  int
	Environment string

	DatabaseURL string
	RedisURL    string

	LogLevel string

	// S3-compatible object storage (SeaweedFS in prod/dev)
	S3Endpoint       string
	S3PublicEndpoint string
	S3AccessKey      string
	S3SecretKey      string

	// JWT (auth - used by Story 1.4)
	JWTPrivateKeyPath string
	JWTPublicKeyPath  string
	JWTExpiryMinutes  int

	// MessageCentral OTP
	MessageCentralCustomerID string // MESSAGECENTRAL_CUSTOMER_ID
	MessageCentralKeyBase64  string // MESSAGECENTRAL_KEY_BASE64
	MessageCentralEmail      string // MESSAGECENTRAL_EMAIL
	MessageCentralBaseURL    string // MESSAGECENTRAL_BASE_URL (default: https://cpaas.messagecentral.com)

	// Push Notifications (FCM + APNs) — optional; feature disabled if empty
	FirebaseServiceAccountJSON string // FIREBASE_SERVICE_ACCOUNT_JSON (base64-encoded JSON)
	FirebaseCredentialsPath    string // FIREBASE_CREDENTIALS_PATH (path to service account JSON file)
	APNSKeyID                  string // APNS_KEY_ID
	APNSTeamID                 string // APNS_TEAM_ID
	APNSPrivateKeyBase64       string // APNS_PRIVATE_KEY_BASE64
	APNSBundleID               string // APNS_BUNDLE_ID (e.g. "com.medha.app")

	// Geospatial
	NearbyEventRadiusKM int // NEARBY_EVENT_RADIUS_KM — radius for notifying pandits of new events (default 10)

	// Background jobs
	RiskCheckEnabled bool // RISK_CHECK_ENABLED — run risk-tier notification background job (default true)

	// Swagger UI
	SwaggerEnabled bool // SWAGGER_ENABLED (default true for dev, false for prod)

	// Dev Login Shortcuts (ONLY USED IN DEV)
	DevPanditPhone string // DEV_PANDIT_PHONE
	DevYajmanPhone string // DEV_YAJMAN_PHONE

	// Admin panel access
	AdminUsername    string // ADMIN_USERNAME
	AdminPassword    string // ADMIN_PASSWORD
	AdminAPIToken    string // ADMIN_API_TOKEN
	ResendAPIKey     string // RESEND_API_KEY
	ResendFromDomain string // RESEND_FROM_DOMAIN
	AdminEmails      string // ADMIN_EMAILS
	// App Links & Universal Links / Profile Sharing Configuration
	ShareBaseURL             string // SHARE_BASE_URL
	AndroidPackageName       string // ANDROID_PACKAGE_NAME
	AndroidSHA256Fingerprint string // ANDROID_SHA256_FINGERPRINT
	IOSAppIDPrefix           string // IOS_APP_ID_PREFIX
	IOSBundleID              string // IOS_BUNDLE_ID
	CredentialsEncryptionKey string // CREDENTIALS_ENCRYPTION_KEY
}

// MigrationConfig contains only the resource a one-shot migration is allowed
// to access. Migration jobs must not need storage, cache, signing, or admin
// credentials merely to update the database schema.
type MigrationConfig struct {
	DatabaseURL string
}

// LoadMigration reads the minimal configuration required by the migration
// command. DATABASE_URL is explicit and namespace-scoped by the deployment.
func LoadMigration() (*MigrationConfig, error) {
	_ = godotenv.Load()
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		return nil, fmt.Errorf("refusing to migrate: DATABASE_URL is required")
	}
	return &MigrationConfig{DatabaseURL: databaseURL}, nil
}

// Load reads configuration from environment variables with sensible defaults.
func Load() (*Config, error) {
	// Load .env file if present (silently ignored in production where env vars are set externally)
	_ = godotenv.Load()

	port, err := getEnvInt("SERVER_PORT", 8080)
	if err != nil {
		return nil, fmt.Errorf("parse SERVER_PORT: %w", err)
	}

	jwtExpiry, err := getEnvInt("JWT_EXPIRY_MINUTES", 15)
	if err != nil {
		return nil, fmt.Errorf("parse JWT_EXPIRY_MINUTES: %w", err)
	}

	environment := strings.ToLower(strings.TrimSpace(os.Getenv("ENVIRONMENT")))
	if environment != "development" && environment != "dev" && environment != "production" && environment != "prod" {
		return nil, fmt.Errorf("ENVIRONMENT must be explicitly set to development, dev, production, or prod")
	}
	isProd := environment == "production" || environment == "prod"

	// Resolve infra connection targets up front so we can assert they match the
	// declared environment before anything connects to them.
	databaseURL := os.Getenv("DATABASE_URL")
	redisURL := os.Getenv("REDIS_URL")
	s3Endpoint := os.Getenv("S3_ENDPOINT")
	s3PublicEndpoint := os.Getenv("S3_PUBLIC_ENDPOINT")
	s3AccessKey := os.Getenv("S3_ACCESS_KEY")
	s3SecretKey := os.Getenv("S3_SECRET_KEY")
	jwtPrivateKeyPath := os.Getenv("JWT_PRIVATE_KEY_PATH")
	jwtPublicKeyPath := os.Getenv("JWT_PUBLIC_KEY_PATH")
	shareBaseURL := os.Getenv("SHARE_BASE_URL")
	adminPassword := os.Getenv("ADMIN_PASSWORD")
	adminAPIToken := os.Getenv("ADMIN_API_TOKEN")
	credsEncKey := os.Getenv("CREDENTIALS_ENCRYPTION_KEY")

	required := map[string]string{
		"ADMIN_API_TOKEN":      adminAPIToken,
		"ADMIN_PASSWORD":       adminPassword,
		"DATABASE_URL":         databaseURL,
		"JWT_PRIVATE_KEY_PATH": jwtPrivateKeyPath,
		"JWT_PUBLIC_KEY_PATH":  jwtPublicKeyPath,
		"S3_ACCESS_KEY":        s3AccessKey,
		"S3_ENDPOINT":          s3Endpoint,
		"S3_PUBLIC_ENDPOINT":   s3PublicEndpoint,
		"S3_SECRET_KEY":        s3SecretKey,
		"REDIS_URL":            redisURL,
		"SHARE_BASE_URL":       shareBaseURL,
	}
	if isProd {
		required["CREDENTIALS_ENCRYPTION_KEY"] = credsEncKey
	}
	var missing []string
	for key, value := range required {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		slices.Sort(missing)
		return nil, fmt.Errorf("refusing to start: required environment variables are missing: %s", strings.Join(missing, ", "))
	}

	// Safety guard: dev code must never reach prod infra, and a prod deploy must
	// never point at dev/local infra. In Kubernetes the short names (postgres,
	// redis, seaweedfs) are intentionally identical; namespace-scoped DNS provides
	// the hard isolation. The guard still rejects legacy suffixed and local targets.
	if err := checkEnvInfraConsistency(isProd, databaseURL, redisURL, s3Endpoint); err != nil {
		return nil, err
	}

	// Swagger: fail-closed — disabled unless explicitly enabled, and default off in prod.
	swaggerEnabled, err := getEnvBool("SWAGGER_ENABLED", !isProd)
	if err != nil {
		return nil, fmt.Errorf("parse SWAGGER_ENABLED: %w", err)
	}

	nearbyRadiusKM, err := getEnvInt("NEARBY_EVENT_RADIUS_KM", 25)
	if err != nil {
		return nil, fmt.Errorf("parse NEARBY_EVENT_RADIUS_KM: %w", err)
	}

	riskCheckEnabled, err := getEnvBool("RISK_CHECK_ENABLED", true)
	if err != nil {
		return nil, fmt.Errorf("parse RISK_CHECK_ENABLED: %w", err)
	}

	return &Config{
		Version:     Version,
		ServerPort:  port,
		Environment: environment,

		DatabaseURL: databaseURL,
		RedisURL:    redisURL,

		LogLevel: getEnv("LOG_LEVEL", "debug"),

		S3Endpoint:       s3Endpoint,
		S3PublicEndpoint: s3PublicEndpoint,
		S3AccessKey:      s3AccessKey,
		S3SecretKey:      s3SecretKey,

		JWTPrivateKeyPath: jwtPrivateKeyPath,
		JWTPublicKeyPath:  jwtPublicKeyPath,
		JWTExpiryMinutes:  jwtExpiry,

		MessageCentralCustomerID: getEnv("MESSAGECENTRAL_CUSTOMER_ID", ""),
		MessageCentralKeyBase64:  getEnv("MESSAGECENTRAL_KEY_BASE64", ""),
		MessageCentralEmail:      getEnv("MESSAGECENTRAL_EMAIL", ""),
		MessageCentralBaseURL:    getEnv("MESSAGECENTRAL_BASE_URL", "https://cpaas.messagecentral.com"),

		FirebaseServiceAccountJSON: getEnv("FIREBASE_SERVICE_ACCOUNT_JSON", ""),
		FirebaseCredentialsPath:    getEnv("FIREBASE_CREDENTIALS_PATH", ""),
		APNSKeyID:                  getEnv("APNS_KEY_ID", ""),
		APNSTeamID:                 getEnv("APNS_TEAM_ID", ""),
		APNSPrivateKeyBase64:       getEnv("APNS_PRIVATE_KEY_BASE64", ""),
		APNSBundleID:               getEnv("APNS_BUNDLE_ID", "com.medha.app"),

		NearbyEventRadiusKM: nearbyRadiusKM,
		RiskCheckEnabled:    riskCheckEnabled,

		SwaggerEnabled: swaggerEnabled,

		DevPanditPhone: getEnv("DEV_PANDIT_PHONE", ""),
		DevYajmanPhone: getEnv("DEV_YAJMAN_PHONE", ""),

		AdminUsername:    getEnv("ADMIN_USERNAME", "admin"),
		AdminPassword:    adminPassword,
		AdminAPIToken:    adminAPIToken,
		ResendAPIKey:     getEnv("RESEND_API_KEY", ""),
		ResendFromDomain: getEnv("RESEND_FROM_DOMAIN", "support.medha.dev"),
		AdminEmails:      getEnv("ADMIN_EMAILS", ""),

		ShareBaseURL:             shareBaseURL,
		AndroidPackageName:       getEnv("ANDROID_PACKAGE_NAME", "com.medhainnovations.medha"),
		AndroidSHA256Fingerprint: getEnv("ANDROID_SHA256_FINGERPRINT", "FA:C6:17:45:DC:09:03:78:6C:B9:ED:E6:2A:96:2B:39:9F:73:48:F0:BB:6F:89:9B:83:32:66:75:91:03:3B:9C"),
		IOSAppIDPrefix:           getEnv("IOS_APP_ID_PREFIX", "9JA89QQL4R"),
		IOSBundleID:              getEnv("IOS_BUNDLE_ID", "com.medhainnovations.medha"),
		CredentialsEncryptionKey: credsEncKey,
	}, nil
}

// checkEnvInfraConsistency refuses to start when the declared environment does not
// match the infrastructure it is pointed at, so dev builds can never write to prod
// data and prod deploys can never silently run against dev/local infra.
//
// Legacy suffixed service names remain detectable. The production manifests use
// the same short service names as dev; Kubernetes namespace DNS is the isolation
// boundary for those targets. localhost/127.0.0.1 count as dev.
//
// ponytail: substring match on connection targets. It cannot see through a local
// port-forward tunnel (e.g. prod DB mapped to localhost:5433 via connect_prod_db.bat);
// that deliberate break-glass path is out of scope here.
func checkEnvInfraConsistency(isProd bool, targets ...string) error {
	const devMarker = "dev"   // -dev-service, medha_dev, localhost/127.0.0.1 handled below
	const prodMarker = "prod" // -prod-service

	for index, t := range targets {
		if t == "" {
			continue
		}
		lower := nonSecretTarget(t)
		looksProd := strings.Contains(lower, "-"+prodMarker+"-") ||
			strings.Contains(lower, "_"+prodMarker) ||
			strings.Contains(lower, "."+prodMarker+".")
		looksDev := strings.Contains(lower, "-"+devMarker+"-") ||
			strings.Contains(lower, "_"+devMarker) ||
			strings.Contains(lower, "."+devMarker+".") ||
			strings.Contains(lower, "localhost") ||
			strings.Contains(lower, "127.0.0.1")

		name := []string{"DATABASE_URL", "REDIS_URL", "S3_ENDPOINT"}[index]
		if isProd && looksDev {
			return fmt.Errorf("refusing to start: ENVIRONMENT is production but %s looks like development or local infrastructure", name)
		}
		if !isProd && looksProd {
			return fmt.Errorf("refusing to start: ENVIRONMENT is development but %s looks like production infrastructure", name)
		}
	}
	return nil
}

func nonSecretTarget(target string) string {
	parsed, err := url.Parse(target)
	if err == nil && parsed.Host != "" {
		return strings.ToLower(parsed.Hostname() + parsed.Path)
	}
	if parsed, err = url.Parse("scheme://" + target); err == nil {
		return strings.ToLower(parsed.Hostname() + parsed.Path)
	}
	return "invalid-target"
}

func getEnvBool(key string, fallback bool) (bool, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("invalid boolean value %q for %s: %w", v, key, err)
	}
	return parsed, nil
}

// IsDevelopment returns true if the environment is development.
func (c *Config) IsDevelopment() bool {
	return c.Environment == "development" || c.Environment == "dev"
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("invalid integer value %q for %s: %w", v, key, err)
	}
	return parsed, nil
}
