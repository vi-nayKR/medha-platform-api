package admin

import (
	"bufio"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/minio/minio-go/v7"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"

	"github.com/medha/backend/internal/admin/assignment"
	"github.com/medha/backend/internal/infra/storage"
	socialservice "github.com/medha/backend/internal/social/service"
	storyservice "github.com/medha/backend/internal/story/service"
	"github.com/medha/backend/pkg/crypto"
	apierrors "github.com/medha/backend/pkg/errors"
	"github.com/medha/backend/pkg/response"
)

type Handler struct {
	pool             *pgxpool.Pool
	s3Client         *storage.S3Client
	adminUsername    string
	adminPassword    string
	adminToken       string
	startedAt        time.Time
	logger           *slog.Logger
	assignmentEngine *assignment.Engine // optional — nil-safe
	encryptionKey    []byte
	resendAPIKey     string
	resendFromDomain string
	adminEmails      string
	redisClient      *redis.Client
	postService      *socialservice.PostService
	storyService     *storyservice.StoryService

	medhaSystemUserOnce sync.Once
	medhaSystemUserID   uuid.UUID
	medhaSystemUserErr  error
}

// SetPostService wires the post service.
func (h *Handler) SetPostService(svc *socialservice.PostService) {
	h.postService = svc
}

// SetStoryService wires the official story service.
func (h *Handler) SetStoryService(svc *storyservice.StoryService) {
	h.storyService = svc
}

type contextKey string

const (
	adminUsernameKey contextKey = "admin_username"
	adminRoleKey     contextKey = "admin_role"
)

type AdminClaims struct {
	Username string `json:"username"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

func (h *Handler) generateAdminJWT(username, role string) (string, error) {
	claims := AdminClaims{
		Username: username,
		Role:     role,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "medha-admin",
			Subject:   username,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(h.adminToken))
}

func (h *Handler) parseAdminJWT(tokenStr string) (*AdminClaims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &AdminClaims{}, func(_ *jwt.Token) (interface{}, error) {
		return []byte(h.adminToken), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithIssuer("medha-admin"))
	if err != nil {
		return nil, err
	}
	if claims, ok := token.Claims.(*AdminClaims); ok && token.Valid {
		return claims, nil
	}
	return nil, fmt.Errorf("invalid token")
}

func (h *Handler) SeedAdminUsers(ctx context.Context) {
	raw := os.Getenv("SEED_ADMIN_OWNERS")
	if strings.TrimSpace(raw) == "" {
		h.logger.Info("SeedAdminUsers: SEED_ADMIN_OWNERS not set, skipping admin seeding")
		return
	}

	type owner struct {
		username string
		password string
	}
	var owners []owner
	for _, pair := range strings.Split(raw, ",") {
		if strings.TrimSpace(pair) == "" {
			continue
		}
		idx := strings.Index(pair, ":")
		if idx < 0 {
			h.logger.Warn("SeedAdminUsers: malformed SEED_ADMIN_OWNERS entry, missing ':', skipping")
			continue
		}
		username := strings.TrimSpace(pair[:idx])
		password := pair[idx+1:]
		if username == "" || password == "" {
			h.logger.Warn("SeedAdminUsers: malformed SEED_ADMIN_OWNERS entry, empty email or password, skipping")
			continue
		}
		owners = append(owners, owner{username: username, password: password})
	}

	for _, o := range owners {
		var exists bool
		err := h.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM admin_users WHERE username = $1)", o.username).Scan(&exists)
		if err != nil {
			h.logger.Error("failed to check existence of admin user", "username", o.username, "error", err)
			continue
		}
		if !exists {
			hashed, err := bcrypt.GenerateFromPassword([]byte(o.password), bcrypt.DefaultCost)
			if err != nil {
				h.logger.Error("failed to hash password for admin user", "username", o.username, "error", err)
				continue
			}
			_, err = h.pool.Exec(ctx, "INSERT INTO admin_users (username, password, role) VALUES ($1, $2, 'owner')", o.username, string(hashed))
			if err != nil {
				h.logger.Error("failed to insert seeded admin user", "username", o.username, "error", err)
			} else {
				h.logger.Info("successfully seeded admin user", "username", o.username)
			}
		}
	}
}

// SetAssignmentEngine wires the optional assignment engine for lead management.
func (h *Handler) SetAssignmentEngine(engine *assignment.Engine) {
	h.assignmentEngine = engine
}

func NewHandler(pool *pgxpool.Pool, s3Client *storage.S3Client, username, password, token string, logger *slog.Logger) *Handler {
	return &Handler{
		pool:          pool,
		s3Client:      s3Client,
		adminUsername: username,
		adminPassword: password,
		adminToken:    token,
		startedAt:     time.Now(),
		logger:        logger,
	}
}

// SetEncryptionKey sets and derives the credentials encryption key.
func (h *Handler) SetEncryptionKey(passphrase string) {
	h.encryptionKey = crypto.DeriveKey(passphrase)
}

func (h *Handler) SetEmailConfig(resendKey, fromDomain, adminEmails string, redisClient *redis.Client) {
	h.resendAPIKey = resendKey
	h.resendFromDomain = fromDomain
	h.adminEmails = adminEmails
	h.redisClient = redisClient
}

func (h *Handler) Routes(r chi.Router) {
	r.Post("/login", h.Login)
	h.ProtectedRoutes(r)
}

// ProtectedRoutes registers routes that require an authenticated administrator.
func (h *Handler) ProtectedRoutes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(h.requireAdmin)

		r.Post("/change-password", h.ChangePassword)

		r.Get("/summary", h.Summary)
		r.Get("/monitoring", h.Monitoring)
		r.Get("/server-stats", h.ServerStats)

		r.Get("/users", h.ListUsers)
		r.Post("/users", h.CreateUser)
		r.Put("/users/{id}", h.UpdateUser)
		r.Delete("/users/{id}", h.DeleteUser)
		r.Put("/users/{id}/pandit-profile", h.UpdateUserPanditProfile)
		r.Put("/users/{id}/service-cities", h.UpdateUserServiceCities)

		r.Get("/events", h.ListEvents)
		r.Post("/events", h.CreateEvent)
		r.Put("/events/{id}", h.UpdateEvent)
		r.Delete("/events/{id}", h.DeleteEvent)
		r.Get("/events/{id}/assignment-logs", h.GetEventAssignmentLogs)
		r.Get("/events/{eventId}/eligible-pandits", h.ListEligiblePandits)

		r.Get("/posts", h.ListPosts)
		r.Post("/posts", h.CreatePost)
		r.Put("/posts/{id}", h.UpdatePost)
		r.Delete("/posts/{id}", h.DeletePost)

		r.Get("/festivals", h.ListFestivals)
		r.Post("/festivals", h.CreateFestival)
		r.Put("/festivals/{id}", h.UpdateFestival)
		r.Delete("/festivals/{id}", h.DeleteFestival)

		r.Get("/panchanga", h.ListPanchanga)
		r.Post("/panchanga", h.CreatePanchanga)
		r.Put("/panchanga/{id}", h.UpdatePanchanga)
		r.Delete("/panchanga/{id}", h.DeletePanchanga)

		// Ceremony Logos / Festival Logos
		r.Get("/ceremony-logos", h.ListFestivalLogos)
		r.Post("/ceremony-logos", h.CreateFestivalLogo)
		r.Put("/ceremony-logos/{id}", h.UpdateFestivalLogo)
		r.Delete("/ceremony-logos/{id}", h.DeleteFestivalLogo)

		r.Post("/media/presign", h.PresignMedia)

		// Storage / Bucket Management
		r.Get("/buckets", h.ListBuckets)
		r.Get("/buckets/{bucket}/objects", h.ListBucketObjects)
		r.Post("/buckets/{bucket}/objects", h.UploadBucketObject)
		r.Delete("/buckets/{bucket}/objects/{key}", h.DeleteBucketObject)

		// Monetization
		r.Get("/monetization/summary", h.GetMonetizationSummary)
		r.Post("/monetization/users/{id}/premium", h.SetUserPremiumStatus)

		// Analytics
		r.Get("/analytics/overview", h.GetAnalyticsOverview)

		// Medha App Official Feed Posts
		r.Get("/feed", h.ListMedhaFeedPosts)
		r.Post("/feed", h.CreateMedhaFeedPost)
		r.Get("/feed/{id}", h.GetMedhaFeedPost)
		r.Put("/feed/{id}", h.UpdateMedhaFeedPost)
		r.Delete("/feed/{id}", h.DeleteMedhaFeedPost)

		// Official Stories (announcement campaigns)
		r.Get("/stories", h.ListStories)
		r.Post("/stories", h.CreateStory)
		r.Get("/stories/{id}", h.GetStory)
		r.Put("/stories/{id}", h.UpdateStory)
		r.Delete("/stories/{id}", h.DeleteStory)
		r.Post("/stories/{id}/publish", h.PublishStory)
		r.Post("/stories/{id}/pause", h.PauseStory)
		r.Post("/stories/{id}/archive", h.ArchiveStory)

		r.Get("/buckets/{bucket}/url", h.GetBucketObjectURL)
		r.Get("/buckets/{bucket}/stats", h.BucketStats)

		// Notifications (admin view)
		r.Get("/notifications", h.ListNotifications)
		r.Post("/notifications", h.CreateNotification)
		r.Put("/notifications/{id}", h.UpdateNotification)
		r.Delete("/notifications/{id}", h.DeleteNotification)

		// Messaging / Conversations (admin view)
		r.Get("/conversations", h.ListConversations)
		r.Post("/conversations", h.CreateConversation)
		r.Put("/conversations/{id}", h.UpdateConversation)
		r.Delete("/conversations/{id}", h.DeleteConversation)

		// Bookings / Matches (admin view)
		r.Get("/bookings", h.ListBookings)
		r.Post("/bookings", h.CreateBooking)
		r.Put("/bookings/{id}", h.UpdateBooking)
		r.Delete("/bookings/{id}", h.DeleteBooking)

		// Assignment Engine & Job Leads
		r.Get("/leads", h.ListJobLeads)
		r.Get("/leads/{id}", h.GetJobLead)
		r.Post("/leads/assign", h.ManualAssign)
		r.Post("/leads/finalize", h.FinalizeAssignment)
		r.Put("/leads/{id}/status", h.UpdateLeadStatus)
		r.Get("/leads/event/{eventId}", h.ListEventLeads)

		// Fee Configuration
		r.Get("/fees", h.ListFeeConfigs)
		r.Post("/fees", h.CreateFeeConfig)
		r.Put("/fees/{id}", h.UpdateFeeConfig)

		// Assignment Dashboard
		r.Get("/assignment/dashboard", h.AssignmentDashboard)

		// Owners only section
		r.Group(func(r chi.Router) {
			r.Use(h.requireOwner)

			// Console Admin User Management
			r.Get("/admin-users", h.ListAdminUsers)
			r.Post("/admin-users", h.CreateAdminUser)
			r.Delete("/admin-users/{id}", h.DeleteAdminUser)
		})
	})
}

func (h *Handler) Configured() bool {
	return h != nil && h.pool != nil && h.adminUsername != "" && h.adminPassword != "" && h.adminToken != ""
}

// Login handles admin authentication.
// @Summary Admin login
// @Description Authenticate admin user and return an access token.
// @Tags admin
// @Accept json
// @Produce json
// @Param body body object{username=string,password=string} true "Admin credentials"
// @Success 200 {object} response.DataResponse{data=object{access_token=string,token_type=string}} "Login successful"
// @Failure 400 {object} apierrors.ProblemDetail "Invalid request"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Failure 503 {object} apierrors.ProblemDetail "Service unavailable"
// @Router /api/v2/admin/login [post]
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	if !h.Configured() {
		missing := []string{}
		if h == nil {
			missing = append(missing, "handler_nil")
		} else {
			if h.pool == nil {
				missing = append(missing, "db_pool")
			}
			if h.adminUsername == "" {
				missing = append(missing, "username")
			}
			if h.adminPassword == "" {
				missing = append(missing, "password")
			}
			if h.adminToken == "" {
				missing = append(missing, "token")
			}
		}
		h.logger.Warn("admin login attempted but access is not configured", "missing", missing)
		apierrors.WriteProblemDetail(w, apierrors.ServiceUnavailable("Admin access is not configured.", r.URL.Path))
		return
	}

	// Extract client info for audit logging.
	remoteIP := r.Header.Get("X-Forwarded-For")
	if remoteIP == "" {
		remoteIP = r.RemoteAddr
	}
	userAgent := r.Header.Get("User-Agent")

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Invalid request body.", r.URL.Path))
		return
	}

	// The env-configured break-glass admin is authenticated first, before any DB
	// access, so it keeps working even when Postgres is unavailable — this panel
	// is used during incidents. DB-backed admin_users are handled below for all
	// other usernames. (If a DB admin shares this exact username, the configured
	// credentials take precedence for that name.)
	if constantTimeEqual(req.Username, h.adminUsername) {
		if !constantTimeEqual(req.Password, h.adminPassword) {
			h.logger.Warn("admin login failed: incorrect password (configured admin)", "username", req.Username)
			apierrors.WriteProblemDetail(w, apierrors.Unauthorized("Invalid admin credentials.", r.URL.Path))
			return
		}
		role := "owner"
		tokenString, err := h.generateAdminJWT(req.Username, role)
		if err != nil {
			h.internal(w, r, "generate admin jwt", err)
			return
		}
		h.logger.Info("admin login successful (configured admin)",
			"event", "admin_login_success",
			"remote_ip", remoteIP,
			"user_agent", userAgent,
			"username", req.Username,
			"role", role,
		)
		response.WriteData(w, http.StatusOK, map[string]any{
			"access_token": tokenString,
			"token_type":   "Bearer",
			"role":         role,
			"username":     req.Username,
		})
		return
	}

	var dbPassword, role string
	err := h.pool.QueryRow(r.Context(), "SELECT password, role FROM admin_users WHERE username = $1", req.Username).Scan(&dbPassword, &role)
	if err != nil {
		h.logger.Warn("admin login failed: user not found or bad credentials", "username", req.Username)
		apierrors.WriteProblemDetail(w, apierrors.Unauthorized("Invalid admin credentials.", r.URL.Path))
		return
	}

	err = bcrypt.CompareHashAndPassword([]byte(dbPassword), []byte(req.Password))
	if err != nil {
		h.logger.Warn("admin login failed: incorrect password", "username", req.Username)
		apierrors.WriteProblemDetail(w, apierrors.Unauthorized("Invalid admin credentials.", r.URL.Path))
		return
	}

	tokenString, err := h.generateAdminJWT(req.Username, role)
	if err != nil {
		h.internal(w, r, "generate jwt", err)
		return
	}

	// Audit log — successful login.
	h.logger.Info("admin login successful",
		"event", "admin_login_success",
		"remote_ip", remoteIP,
		"user_agent", userAgent,
		"username", req.Username,
		"role", role,
	)

	response.WriteData(w, http.StatusOK, map[string]any{
		"access_token": tokenString,
		"token_type":   "Bearer",
		"role":         role,
		"username":     req.Username,
	})
}

func (h *Handler) RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !h.Configured() {
			missing := []string{}
			if h == nil {
				missing = append(missing, "handler_nil")
			} else {
				if h.pool == nil {
					missing = append(missing, "db_pool")
				}
				if h.adminUsername == "" {
					missing = append(missing, "username")
				}
				if h.adminPassword == "" {
					missing = append(missing, "password")
				}
				if h.adminToken == "" {
					missing = append(missing, "token")
				}
			}
			h.logger.Warn("admin middleware check failed: access not configured", "missing", missing)
			apierrors.WriteProblemDetail(w, apierrors.ServiceUnavailable("Admin access is not configured.", r.URL.Path))
			return
		}

		auth := r.Header.Get("Authorization")
		token := strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
		if token == "" {
			apierrors.WriteProblemDetail(w, apierrors.Unauthorized("Admin token required.", r.URL.Path))
			return
		}

		var username, role string
		if token == h.adminToken {
			username = h.adminUsername
			role = "owner"
		} else {
			claims, err := h.parseAdminJWT(token)
			if err != nil {
				apierrors.WriteProblemDetail(w, apierrors.Unauthorized("Invalid or expired admin token.", r.URL.Path))
				return
			}
			username = claims.Username
			role = claims.Role
		}

		ctx := context.WithValue(r.Context(), adminUsernameKey, username)
		ctx = context.WithValue(ctx, adminRoleKey, role)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (h *Handler) requireAdmin(next http.Handler) http.Handler {
	return h.RequireAdmin(next)
}

func (h *Handler) requireOwner(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		role, _ := r.Context().Value(adminRoleKey).(string)
		if role != "owner" {
			apierrors.WriteProblemDetail(w, apierrors.Forbidden("Owners only resource.", r.URL.Path))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Summary returns high-level system statistics.
// @Summary Get system summary
// @Description Returns counts for users, events, festivals, panchanga, and logos.
// @Tags admin
// @Security AdminToken
// @Produce json
// @Success 200 {object} response.DataResponse{data=map[string]int} "Summary data"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router /api/v2/admin/summary [get]
func (h *Handler) Summary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	count := func(query string) int {
		var v int
		if err := h.pool.QueryRow(ctx, query).Scan(&v); err != nil {
			h.logger.Warn("admin summary count failed", "query", query, "error", err)
		}
		return v
	}

	response.WriteData(w, http.StatusOK, map[string]int{
		"users":     count("SELECT COUNT(*) FROM users WHERE deleted_at IS NULL"),
		"events":    count("SELECT COUNT(*) FROM events WHERE deleted_at IS NULL"),
		"festivals": count("SELECT COUNT(*) FROM festivals"),
		"panchanga": count("SELECT COUNT(*) FROM panchanga"),
		"logos":     count("SELECT COUNT(*) FROM festival_logos"),
	})
}

// Monitoring returns service health status and resource totals.
// @Summary System monitoring
// @Description Returns health status for API, PostgreSQL, S3 storage, and total resource counts.
// @Tags admin
// @Security AdminToken
// @Produce json
// @Success 200 {object} response.DataResponse{data=object{generated_at=int64,uptime_seconds=int64,services=[]map[string]any,totals=map[string]any}} "Monitoring data"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router /api/v2/admin/monitoring [get]
func (h *Handler) Monitoring(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	services := []map[string]any{
		h.serviceStatus("api", "API", "ok", "Admin API is responding", 0),
	}

	dbLatency := time.Duration(0)
	dbStarted := time.Now()
	dbStatus := "ok"
	dbMessage := "PostgreSQL ping succeeded"
	if err := h.pool.Ping(ctx); err != nil {
		dbStatus = "failing"
		dbMessage = err.Error()
		h.logger.Warn("admin monitoring postgres health failed", "error", err)
	}
	dbLatency = time.Since(dbStarted)
	services = append(services, h.serviceStatus("postgres", "PostgreSQL", dbStatus, dbMessage, dbLatency.Milliseconds()))

	s3Status := "not_configured"
	s3Message := "S3 client is not configured"
	s3Latency := int64(0)
	if h.s3Client != nil && h.s3Client.RawClient() != nil {
		started := time.Now()
		if _, err := h.s3Client.RawClient().ListBuckets(ctx); err != nil {
			s3Status = "failing"
			s3Message = err.Error()
			h.logger.Warn("admin monitoring s3 storage health failed", "error", err)
		} else {
			s3Status = "ok"
			s3Message = "S3 bucket listing succeeded"
		}
		s3Latency = time.Since(started).Milliseconds()
	}
	services = append(services, h.serviceStatus("s3", "S3 Object Store", s3Status, s3Message, s3Latency))

	totals := map[string]any{
		"users":           h.scalarInt(ctx, "SELECT COUNT(*) FROM users WHERE deleted_at IS NULL"),
		"events":          h.scalarInt(ctx, "SELECT COUNT(*) FROM events WHERE deleted_at IS NULL"),
		"notifications":   h.scalarInt(ctx, "SELECT COUNT(*) FROM notifications"),
		"conversations":   h.scalarInt(ctx, "SELECT COUNT(*) FROM conversations"),
		"bookings":        h.scalarInt(ctx, "SELECT COUNT(*) FROM matches"),
		"storage_bytes":   int64(0),
		"storage_objects": int64(0),
	}

	storageByBucket := h.storageStats(ctx)
	for _, item := range storageByBucket {
		if size, ok := item["total_size"].(int64); ok {
			totals["storage_bytes"] = totals["storage_bytes"].(int64) + size
		}
		if objects, ok := item["total_objects"].(int64); ok {
			totals["storage_objects"] = totals["storage_objects"].(int64) + objects
		}
	}

	response.WriteData(w, http.StatusOK, map[string]any{
		"generated_at":   time.Now().Unix(),
		"uptime_seconds": int64(time.Since(h.startedAt).Seconds()),
		"services":       services,
		"totals":         totals,
		"charts": map[string]any{
			"users_by_day":         h.querySeries(ctx, usersByDaySQL),
			"events_by_status":     h.querySeries(ctx, eventsByStatusSQL),
			"notifications_by_day": h.querySeries(ctx, notificationsByDaySQL),
			"storage_by_bucket":    storageByBucket,
		},
	})
}

// ServerStats returns runtime server metrics.
// @Summary Server statistics
// @Description Returns CPU, memory, goroutines, and OS information.
// @Tags admin
// @Security AdminToken
// @Produce json
// @Success 200 {object} response.DataResponse{data=map[string]any} "Server statistics"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router /api/v2/admin/server-stats [get]
func (h *Handler) ServerStats(w http.ResponseWriter, r *http.Request) {
	procRoot := hostProcRoot()
	sysRoot := hostSysRoot()
	rootPath := hostRootPath()

	stats := map[string]any{
		"collected_at": time.Now().Unix(),
		"metrics_source": map[string]any{
			"proc":      procRoot,
			"sys":       sysRoot,
			"root":      rootPath,
			"host_mode": procRoot != "/proc" || sysRoot != "/sys" || rootPath != "/",
		},
		"go_runtime": map[string]any{
			"version":    runtime.Version(),
			"goroutines": runtime.NumGoroutine(),
			"os":         runtime.GOOS,
			"arch":       runtime.GOARCH,
			"cpus":       runtime.NumCPU(),
		},
		"api_uptime_seconds": int64(time.Since(h.startedAt).Seconds()),
	}

	// ── OS Info ──
	stats["os_info"] = h.readOSInfo(rootPath)

	// ── System Uptime ──
	if data, err := os.ReadFile(filepath.Join(procRoot, "uptime")); err == nil {
		parts := strings.Fields(string(data))
		if len(parts) >= 1 {
			if uptime, err := strconv.ParseFloat(parts[0], 64); err == nil {
				stats["system_uptime_seconds"] = int64(uptime)
			}
		}
	}

	// ── Hostname ──
	if hostname, err := os.Hostname(); err == nil {
		stats["hostname"] = hostname
	}

	// ── Load Average ──
	if data, err := os.ReadFile(filepath.Join(procRoot, "loadavg")); err == nil {
		parts := strings.Fields(string(data))
		if len(parts) >= 3 {
			load1, _ := strconv.ParseFloat(parts[0], 64)
			load5, _ := strconv.ParseFloat(parts[1], 64)
			load15, _ := strconv.ParseFloat(parts[2], 64)
			stats["load_average"] = map[string]float64{
				"load_1m": load1, "load_5m": load5, "load_15m": load15,
			}
		}
	}

	// ── CPU Info ──
	stats["cpu"] = h.readCPUInfo(procRoot)

	// ── Memory Info ──
	memInfo := h.readMemoryInfo(procRoot)
	stats["memory"] = memInfo

	// ── Disk Usage ──
	stats["disks"] = h.readDiskUsage(rootPath)
	stats["disk_io"] = h.readDiskIO(procRoot)

	// ── Temperature ──
	stats["temperature"] = h.readTemperature(sysRoot)

	// ── Network Interfaces ──
	stats["network"] = h.readNetworkInterfaces(procRoot, sysRoot)

	// ── Processes ──
	if data, err := os.ReadFile(filepath.Join(procRoot, "stat")); err == nil {
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "processes ") {
				parts := strings.Fields(line)
				if len(parts) >= 2 {
					total, _ := strconv.ParseInt(parts[1], 10, 64)
					stats["total_processes_created"] = total
				}
			}
			if strings.HasPrefix(line, "procs_running ") {
				parts := strings.Fields(line)
				if len(parts) >= 2 {
					running, _ := strconv.ParseInt(parts[1], 10, 64)
					stats["processes_running"] = running
				}
			}
		}
	}

	// ── Process Count ──
	stats["process_count"] = h.countProcesses(procRoot)

	// ── Users Logged In ──
	if output, err := exec.Command("who").Output(); err == nil {
		lines := strings.Split(strings.TrimSpace(string(output)), "\n")
		users := []map[string]string{}
		for _, line := range lines {
			if line == "" {
				continue
			}
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				users = append(users, map[string]string{
					"user":     parts[0],
					"terminal": parts[1],
					"login_at": strings.Join(parts[2:], " "),
				})
			}
		}
		stats["logged_in_users"] = users
		stats["logged_in_user_count"] = len(users)
	}

	// ── Pending Updates ──
	if output, err := exec.Command("sh", "-c", "/usr/lib/update-notifier/apt-check 2>&1 || echo '0;0'").Output(); err == nil {
		raw := strings.TrimSpace(string(output))
		parts := strings.Split(raw, ";")
		if len(parts) >= 2 {
			regular, _ := strconv.ParseInt(parts[0], 10, 64)
			security, _ := strconv.ParseInt(parts[1], 10, 64)
			stats["pending_updates"] = map[string]int64{
				"regular":  regular,
				"security": security,
				"total":    regular + security,
			}
		}
	}

	// ── Swap Info ──
	stats["swap"] = h.readSwapInfo(procRoot)

	// ── Top Processes by CPU ──
	stats["top_processes"] = h.readTopProcesses()

	// ── Systemd Service Status ──
	stats["services"] = h.readSystemdServices()

	// ── Open Ports ──
	stats["open_ports"] = h.readOpenPorts()

	response.WriteData(w, http.StatusOK, stats)
}

func hostProcRoot() string {
	if v := strings.TrimSpace(os.Getenv("HOST_PROC_PATH")); v != "" {
		return strings.TrimRight(v, "/")
	}
	if _, err := os.Stat("/host/proc/stat"); err == nil {
		return "/host/proc"
	}
	return "/proc"
}

func hostSysRoot() string {
	if v := strings.TrimSpace(os.Getenv("HOST_SYS_PATH")); v != "" {
		return strings.TrimRight(v, "/")
	}
	if _, err := os.Stat("/host/sys/class"); err == nil {
		return "/host/sys"
	}
	return "/sys"
}

func hostRootPath() string {
	if v := strings.TrimSpace(os.Getenv("HOST_ROOT_PATH")); v != "" {
		return strings.TrimRight(v, "/")
	}
	if _, err := os.Stat("/host/root/etc/os-release"); err == nil {
		return "/host/root"
	}
	return "/"
}

// readOSInfo parses os-release for OS distribution details.
func (h *Handler) readOSInfo(rootPath string) map[string]string {
	info := map[string]string{}
	osReleasePath := filepath.Join(rootPath, "etc/os-release")
	if rootPath == "/" {
		osReleasePath = "/etc/os-release"
	}
	data, err := os.ReadFile(osReleasePath)
	if err != nil {
		return info
	}
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.ToLower(parts[0])
			val := strings.Trim(parts[1], "\"")
			switch key {
			case "pretty_name":
				info["distro"] = val
			case "version_id":
				info["version"] = val
			case "id":
				info["id"] = val
			}
		}
	}
	// Kernel version
	if output, err := exec.Command("uname", "-r").Output(); err == nil {
		info["kernel"] = strings.TrimSpace(string(output))
	}
	return info
}

type cpuSample struct {
	idle  uint64
	total uint64
}

func readCPUSample(procRoot string) (cpuSample, bool) {
	stat, err := os.ReadFile(filepath.Join(procRoot, "stat"))
	if err != nil {
		return cpuSample{}, false
	}
	for _, line := range strings.Split(string(stat), "\n") {
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 8 {
			return cpuSample{}, false
		}
		var values []uint64
		for _, field := range fields[1:] {
			value, _ := strconv.ParseUint(field, 10, 64)
			values = append(values, value)
		}
		var total uint64
		for _, value := range values {
			total += value
		}
		idle := values[3]
		if len(values) > 4 {
			idle += values[4]
		}
		return cpuSample{idle: idle, total: total}, true
	}
	return cpuSample{}, false
}

// readCPUInfo reads procfs for CPU model, core details, and live CPU usage.
func (h *Handler) readCPUInfo(procRoot string) map[string]any {
	result := map[string]any{}
	data, err := os.ReadFile(filepath.Join(procRoot, "cpuinfo"))
	if err != nil {
		return result
	}
	cores := 0
	modelName := ""
	mhz := ""
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		switch key {
		case "model name":
			if modelName == "" {
				modelName = val
			}
		case "cpu MHz":
			if mhz == "" {
				mhz = val
			}
		case "processor":
			cores++
		}
	}
	result["model"] = modelName
	result["cores"] = cores
	result["mhz"] = mhz

	// Live CPU usage sampled like top/btop instead of using all-time boot averages.
	if first, ok := readCPUSample(procRoot); ok {
		time.Sleep(250 * time.Millisecond)
		if second, ok := readCPUSample(procRoot); ok && second.total > first.total {
			totalDelta := second.total - first.total
			idleDelta := second.idle - first.idle
			usage := 100 * float64(totalDelta-idleDelta) / float64(totalDelta)
			result["usage_percent"] = fmt.Sprintf("%.1f", usage)
		}
	}

	return result
}

// readMemoryInfo reads procfs for RAM statistics.
func (h *Handler) readMemoryInfo(procRoot string) map[string]any {
	result := map[string]any{}
	data, err := os.ReadFile(filepath.Join(procRoot, "meminfo"))
	if err != nil {
		return result
	}
	memMap := map[string]int64{}
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		valStr := strings.TrimSpace(parts[1])
		valStr = strings.TrimSuffix(valStr, " kB")
		valStr = strings.TrimSpace(valStr)
		val, _ := strconv.ParseInt(valStr, 10, 64)
		memMap[key] = val
	}

	totalKB := memMap["MemTotal"]
	availKB := memMap["MemAvailable"]
	freeKB := memMap["MemFree"]
	buffersKB := memMap["Buffers"]
	cachedKB := memMap["Cached"]
	usedKB := totalKB - availKB

	result["total_bytes"] = totalKB * 1024
	result["available_bytes"] = availKB * 1024
	result["used_bytes"] = usedKB * 1024
	result["free_bytes"] = freeKB * 1024
	result["buffers_bytes"] = buffersKB * 1024
	result["cached_bytes"] = cachedKB * 1024
	if totalKB > 0 {
		result["usage_percent"] = fmt.Sprintf("%.1f", float64(usedKB)/float64(totalKB)*100)
	}
	return result
}

// readSwapInfo reads swap usage from procfs.
func (h *Handler) readSwapInfo(procRoot string) map[string]any {
	result := map[string]any{}
	data, err := os.ReadFile(filepath.Join(procRoot, "meminfo"))
	if err != nil {
		return result
	}
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		valStr := strings.TrimSuffix(strings.TrimSpace(parts[1]), " kB")
		val, _ := strconv.ParseInt(strings.TrimSpace(valStr), 10, 64)
		switch key {
		case "SwapTotal":
			result["total_bytes"] = val * 1024
		case "SwapFree":
			result["free_bytes"] = val * 1024
		}
	}
	if total, ok := result["total_bytes"].(int64); ok && total > 0 {
		free, _ := result["free_bytes"].(int64)
		used := total - free
		result["used_bytes"] = used
		result["usage_percent"] = fmt.Sprintf("%.1f", float64(used)/float64(total)*100)
	} else {
		result["used_bytes"] = int64(0)
		result["usage_percent"] = "0.0"
	}
	return result
}

// readDiskUsage runs `df -B1` for host disk usage statistics.
func (h *Handler) readDiskUsage(rootPath string) []map[string]any {
	disks := []map[string]any{}
	target := rootPath
	if target == "" {
		target = "/"
	}
	output, err := exec.Command("df", "-B1", "--output=source,fstype,size,used,avail,pcent,target", target).Output()
	if err != nil {
		return disks
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for i, line := range lines {
		if i == 0 { // skip header
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 7 {
			continue
		}
		// Skip tmpfs, devtmpfs, squashfs, overlay, etc.
		fs := fields[0]
		if !strings.HasPrefix(fs, "/dev/") {
			continue
		}
		size, _ := strconv.ParseInt(fields[2], 10, 64)
		used, _ := strconv.ParseInt(fields[3], 10, 64)
		avail, _ := strconv.ParseInt(fields[4], 10, 64)
		pct := strings.TrimSuffix(fields[5], "%")

		mountPoint := fields[6]
		if rootPath != "/" && mountPoint == rootPath {
			mountPoint = "/"
		}
		disks = append(disks, map[string]any{
			"filesystem":    fs,
			"type":          fields[1],
			"total_bytes":   size,
			"used_bytes":    used,
			"avail_bytes":   avail,
			"usage_percent": pct,
			"mount_point":   mountPoint,
		})
	}
	return disks
}

type diskIOSample struct {
	readSectors  uint64
	writeSectors uint64
	ioMs         uint64
}

func readDiskIOSample(procRoot string) map[string]diskIOSample {
	data, err := os.ReadFile(filepath.Join(procRoot, "diskstats"))
	if err != nil {
		return map[string]diskIOSample{}
	}
	samples := map[string]diskIOSample{}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 14 {
			continue
		}
		name := fields[2]
		if strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "ram") {
			continue
		}
		readSectors, _ := strconv.ParseUint(fields[5], 10, 64)
		writeSectors, _ := strconv.ParseUint(fields[9], 10, 64)
		ioMs, _ := strconv.ParseUint(fields[12], 10, 64)
		samples[name] = diskIOSample{readSectors: readSectors, writeSectors: writeSectors, ioMs: ioMs}
	}
	return samples
}

func (h *Handler) readDiskIO(procRoot string) []map[string]any {
	first := readDiskIOSample(procRoot)
	time.Sleep(250 * time.Millisecond)
	second := readDiskIOSample(procRoot)
	items := []map[string]any{}
	for name, after := range second {
		before, ok := first[name]
		if !ok {
			continue
		}
		seconds := 0.25
		readBytes := int64(after.readSectors-before.readSectors) * 512
		writeBytes := int64(after.writeSectors-before.writeSectors) * 512
		ioUtil := 100 * float64(after.ioMs-before.ioMs) / (seconds * 1000)
		if ioUtil > 100 {
			ioUtil = 100
		}
		items = append(items, map[string]any{
			"name":              name,
			"read_bps":          int64(float64(readBytes) / seconds),
			"write_bps":         int64(float64(writeBytes) / seconds),
			"util_percent":      fmt.Sprintf("%.1f", ioUtil),
			"read_bytes_delta":  readBytes,
			"write_bytes_delta": writeBytes,
		})
	}
	return items
}

// readTemperature reads thermal and hwmon temperatures from host sysfs.
func (h *Handler) readTemperature(sysRoot string) []map[string]any {
	temps := []map[string]any{}
	for i := 0; i < 10; i++ {
		basePath := filepath.Join(sysRoot, fmt.Sprintf("class/thermal/thermal_zone%d", i))
		tempFile := basePath + "/temp"
		typeFile := basePath + "/type"

		data, err := os.ReadFile(tempFile)
		if err != nil {
			break
		}
		millideg, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
		if err != nil {
			continue
		}
		zoneName := fmt.Sprintf("zone%d", i)
		if typeData, err := os.ReadFile(typeFile); err == nil {
			zoneName = strings.TrimSpace(string(typeData))
		}
		temps = append(temps, map[string]any{
			"zone":     zoneName,
			"temp_c":   millideg / 1000.0,
			"temp_f":   millideg/1000.0*9/5 + 32,
			"critical": millideg/1000.0 > 85.0,
		})
	}
	hwmonDirs, _ := filepath.Glob(filepath.Join(sysRoot, "class/hwmon/hwmon*"))
	for _, dir := range hwmonDirs {
		chip := filepath.Base(dir)
		if data, err := os.ReadFile(filepath.Join(dir, "name")); err == nil {
			chip = strings.TrimSpace(string(data))
		}
		inputs, _ := filepath.Glob(filepath.Join(dir, "temp*_input"))
		for _, input := range inputs {
			data, err := os.ReadFile(input)
			if err != nil {
				continue
			}
			millideg, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
			if err != nil || millideg == 0 {
				continue
			}
			label := strings.TrimSuffix(filepath.Base(input), "_input")
			if labelData, err := os.ReadFile(filepath.Join(dir, label+"_label")); err == nil {
				label = strings.TrimSpace(string(labelData))
			}
			zone := chip + " " + label
			temps = append(temps, map[string]any{
				"zone":     zone,
				"temp_c":   millideg / 1000.0,
				"temp_f":   millideg/1000.0*9/5 + 32,
				"critical": millideg/1000.0 > 85.0,
			})
		}
	}
	return temps
}

type netSample struct {
	rxBytes   int64
	txBytes   int64
	rxPackets int64
	txPackets int64
}

func readNetworkSample(procRoot string) map[string]netSample {
	data, err := os.ReadFile(filepath.Join(procRoot, "net/dev"))
	if err != nil {
		return map[string]netSample{}
	}
	samples := map[string]netSample{}
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		if i < 2 { // skip headers
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		if name == "lo" {
			continue
		}
		fields := strings.Fields(parts[1])
		if len(fields) < 10 {
			continue
		}
		rxBytes, _ := strconv.ParseInt(fields[0], 10, 64)
		rxPackets, _ := strconv.ParseInt(fields[1], 10, 64)
		txBytes, _ := strconv.ParseInt(fields[8], 10, 64)
		txPackets, _ := strconv.ParseInt(fields[9], 10, 64)
		samples[name] = netSample{rxBytes: rxBytes, txBytes: txBytes, rxPackets: rxPackets, txPackets: txPackets}
	}
	return samples
}

// readNetworkInterfaces parses procfs/sysfs for host network interface stats.
func (h *Handler) readNetworkInterfaces(procRoot, sysRoot string) []map[string]any {
	interfaces := []map[string]any{}
	first := readNetworkSample(procRoot)
	time.Sleep(250 * time.Millisecond)
	second := readNetworkSample(procRoot)
	for name, current := range second {
		if name == "lo" {
			continue
		}
		previous := first[name]

		iface := map[string]any{
			"name":       name,
			"rx_bytes":   current.rxBytes,
			"rx_packets": current.rxPackets,
			"tx_bytes":   current.txBytes,
			"tx_packets": current.txPackets,
			"rx_bps":     int64(float64(current.rxBytes-previous.rxBytes) / 0.25),
			"tx_bps":     int64(float64(current.txBytes-previous.txBytes) / 0.25),
		}

		// Get IP address from `ip addr show <name>`
		if ipOut, err := exec.Command("ip", "-4", "addr", "show", name).Output(); err == nil {
			re := regexp.MustCompile(`inet (\d+\.\d+\.\d+\.\d+)`)
			match := re.FindStringSubmatch(string(ipOut))
			if len(match) > 1 {
				iface["ipv4"] = match[1]
			}
		}
		// Get link status
		operFile := filepath.Join(sysRoot, "class/net", name, "operstate")
		if stateData, err := os.ReadFile(operFile); err == nil {
			iface["state"] = strings.TrimSpace(string(stateData))
		}
		// Get speed
		speedFile := filepath.Join(sysRoot, "class/net", name, "speed")
		if speedData, err := os.ReadFile(speedFile); err == nil {
			speed, _ := strconv.ParseInt(strings.TrimSpace(string(speedData)), 10, 64)
			if speed > 0 {
				iface["speed_mbps"] = speed
			}
		}

		interfaces = append(interfaces, iface)
	}
	return interfaces
}

// readTopProcesses returns the top 10 processes by CPU/MEM usage via `ps`.
func (h *Handler) readTopProcesses() []map[string]any {
	procs := []map[string]any{}
	output, err := exec.Command("ps", "aux", "--sort=-pcpu").Output()
	if err != nil {
		return procs
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for i, line := range lines {
		if i == 0 || i > 10 { // skip header, limit to 10
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 11 {
			continue
		}
		cpu, _ := strconv.ParseFloat(fields[2], 64)
		mem, _ := strconv.ParseFloat(fields[3], 64)
		pid, _ := strconv.ParseInt(fields[1], 10, 64)
		procs = append(procs, map[string]any{
			"user":        fields[0],
			"pid":         pid,
			"cpu_percent": cpu,
			"mem_percent": mem,
			"command":     strings.Join(fields[10:], " "),
		})
	}
	return procs
}

func (h *Handler) countProcesses(procRoot string) int64 {
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return 0
	}
	var count int64
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := strconv.ParseInt(entry.Name(), 10, 64); err == nil {
			count++
		}
	}
	return count
}

// readSystemdServices checks the status of key system services.
func (h *Handler) readSystemdServices() []map[string]any {
	serviceNames := []string{"medha-api", "nginx", "postgresql", "seaweedfs", "ssh", "ufw"}
	services := []map[string]any{}
	for _, svc := range serviceNames {
		svcInfo := map[string]any{"name": svc}

		// Check if active
		output, err := exec.Command("systemctl", "is-active", svc).Output()
		if err != nil {
			svcInfo["status"] = "unknown"
		} else {
			svcInfo["status"] = strings.TrimSpace(string(output))
		}

		// Check if enabled
		output2, err := exec.Command("systemctl", "is-enabled", svc).Output()
		if err != nil {
			svcInfo["enabled"] = "unknown"
		} else {
			svcInfo["enabled"] = strings.TrimSpace(string(output2))
		}

		services = append(services, svcInfo)
	}
	return services
}

// readOpenPorts reads listening TCP ports from /proc/net/tcp.
func (h *Handler) readOpenPorts() []map[string]any {
	ports := []map[string]any{}
	output, err := exec.Command("ss", "-tlnp").Output()
	if err != nil {
		return ports
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for i, line := range lines {
		if i == 0 { // skip header
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		localAddr := fields[3]
		// Extract port from address like *:8080 or 0.0.0.0:8080
		addrParts := strings.Split(localAddr, ":")
		port := addrParts[len(addrParts)-1]

		portEntry := map[string]any{
			"address": localAddr,
			"port":    port,
			"state":   fields[0],
		}
		if len(fields) >= 6 {
			portEntry["process"] = fields[5]
		}
		ports = append(ports, portEntry)
	}
	return ports
}

// ListUsers returns a list of users.
// @Summary List users
// @Description Returns up to 500 users from the database.
// @Tags admin
// @Security AdminToken
// @Produce json
// @Success 200 {object} response.DataResponse{data=[]map[string]any} "List of users"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router /api/v2/admin/users [get]
func (h *Handler) ListUsers(w http.ResponseWriter, r *http.Request) {
	rows, err := h.pool.Query(r.Context(), `
		SELECT id, COALESCE(auth_provider, ''), COALESCE(provider_uid, ''), COALESCE(role::text, ''),
		       first_name, last_name, COALESCE(username, ''), COALESCE(email, ''), COALESCE(phone, ''),
		       COALESCE(phone_verified, false), COALESCE(profile_photo_url, ''), COALESCE(profile_complete, false),
		       ST_Y(location::geometry), ST_X(location::geometry), created_at, updated_at
		FROM users
		WHERE deleted_at IS NULL
		ORDER BY updated_at DESC
		LIMIT 500
	`)
	if err != nil {
		h.internal(w, r, "list users", err)
		return
	}
	defer rows.Close()

	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, authProvider, providerUID, role, firstName, lastName, username, email, phone, photo string
		var phoneVerified, profileComplete bool
		var lat, lng *float64
		var createdAt, updatedAt int64
		if err := rows.Scan(&id, &authProvider, &providerUID, &role, &firstName, &lastName, &username, &email, &phone, &phoneVerified, &photo, &profileComplete, &lat, &lng, &createdAt, &updatedAt); err != nil {
			h.internal(w, r, "scan users", err)
			return
		}
		items = append(items, map[string]any{
			"id": id, "auth_provider": authProvider, "provider_uid": providerUID, "role": role,
			"first_name": firstName, "last_name": lastName, "username": username, "email": email,
			"phone": phone, "phone_verified": phoneVerified, "profile_photo_url": photo,
			"profile_complete": profileComplete, "latitude": lat, "longitude": lng,
			"created_at": createdAt, "updated_at": updatedAt,
		})
	}
	response.WriteData(w, http.StatusOK, items)
}

// CreateUser creates a new user.
// @Summary Create user
// @Description Manually create a new user record.
// @Tags admin
// @Security AdminToken
// @Accept json
// @Produce json
// @Param body body map[string]any true "User data"
// @Success 201 {object} response.DataResponse{data=object{id=string}} "User created"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router /api/v2/admin/users [post]
func (h *Handler) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req map[string]any
	if !decodeJSON(w, r, &req) {
		return
	}

	id := uuid.New()
	_, err := h.pool.Exec(r.Context(), `
		INSERT INTO users (
			id, first_name, last_name, username, email, phone, role, 
			phone_verified, profile_complete, profile_photo_url, location,
			auth_provider, provider_uid
		) VALUES (
			$1, $2, $3, NULLIF($4, ''), NULLIF($5, ''), NULLIF($6, ''), NULLIF($7, '')::user_role,
			$8, $9, NULLIF($10, ''), ST_SetSRID(ST_MakePoint($12, $11), 4326)::geography,
			'manual', $13
		)
	`, id, str(req, "first_name"), str(req, "last_name"), str(req, "username"), str(req, "email"), str(req, "phone"), str(req, "role"),
		boolVal(req, "phone_verified"), boolVal(req, "profile_complete"), str(req, "profile_photo_url"),
		floatPtr(req, "latitude"), floatPtr(req, "longitude"), id.String())

	if err != nil {
		h.internal(w, r, "create user", err)
		return
	}
	response.WriteData(w, http.StatusCreated, map[string]string{"id": id.String()})
}

// UpdateUser updates an existing user.
// @Summary Update user
// @Description Update user fields. Returns 404 if user not found.
// @Tags admin
// @Security AdminToken
// @Accept json
// @Produce json
// @Param id path string true "User ID"
// @Param body body map[string]any true "User data"
// @Success 200 {object} response.DataResponse{data=object{id=string}} "User updated"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Failure 404 {object} apierrors.ProblemDetail "User not found"
// @Router /api/v2/admin/users/{id} [put]
func (h *Handler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	var req map[string]any
	if !decodeJSON(w, r, &req) {
		return
	}

	tag, err := h.pool.Exec(r.Context(), `
		UPDATE users
		SET first_name = $2,
		    last_name = $3,
		    username = NULLIF($4, ''),
		    email = NULLIF($5, ''),
		    phone = NULLIF($6, ''),
		    role = NULLIF($7, '')::user_role,
		    phone_verified = $8,
		    profile_complete = $9,
		    profile_photo_url = NULLIF($10, ''),
		    location = CASE WHEN $11::DOUBLE PRECISION IS NULL OR $12::DOUBLE PRECISION IS NULL THEN location ELSE ST_SetSRID(ST_MakePoint($12, $11), 4326)::geography END
		WHERE id = $1 AND deleted_at IS NULL
	`, id, str(req, "first_name"), str(req, "last_name"), str(req, "username"), str(req, "email"), str(req, "phone"), str(req, "role"), boolVal(req, "phone_verified"), boolVal(req, "profile_complete"), str(req, "profile_photo_url"), floatPtr(req, "latitude"), floatPtr(req, "longitude"))
	if err != nil {
		h.internal(w, r, "update user", err)
		return
	}

	if tag.RowsAffected() == 0 {
		apierrors.WriteProblemDetail(w, apierrors.NotFound("User not found or already deleted.", r.URL.Path))
		return
	}

	response.WriteData(w, http.StatusOK, map[string]string{"id": id.String()})
}

// DeleteUser soft-deletes a user.
// @Summary Delete user
// @Description Soft-deletes a user. Returns 404 if user not found.
// @Tags admin
// @Security AdminToken
// @Param id path string true "User ID"
// @Success 204 "User deleted"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Failure 404 {object} apierrors.ProblemDetail "User not found"
// @Router /api/v2/admin/users/{id} [delete]
func (h *Handler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	tag, err := h.pool.Exec(r.Context(), `UPDATE users SET deleted_at = (EXTRACT(EPOCH FROM NOW()))::BIGINT WHERE id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		h.internal(w, r, "delete user", err)
		return
	}

	if tag.RowsAffected() == 0 {
		apierrors.WriteProblemDetail(w, apierrors.NotFound("User not found or already deleted.", r.URL.Path))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ListEvents returns a list of events.
// @Summary List events
// @Description Returns up to 500 events from the database.
// @Tags admin
// @Security AdminToken
// @Produce json
// @Success 200 {object} response.DataResponse{data=[]map[string]any} "List of events"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router /api/v2/admin/events [get]
func (h *Handler) ListEvents(w http.ResponseWriter, r *http.Request) {
	rows, err := h.pool.Query(r.Context(), `
		SELECT e.id, e.yajman_id, e.ceremony_type::text, e.event_date,
		       ST_Y(e.location::geometry), ST_X(e.location::geometry), e.address, COALESCE(e.description, ''),
		       COALESCE(e.custom_ceremony_name, ''), COALESCE(e.custom_ceremony_description, ''),
		       e.status::text,
		       e.created_at, e.updated_at,
		       e.platform_fee,
		       COALESCE(u.first_name, '') || ' ' || COALESCE(u.last_name, '') AS yajman_name,
		       COALESCE(
		           (SELECT COALESCE(up.first_name, '') || ' ' || COALESCE(up.last_name, '')
		            FROM matches m
		            JOIN users up ON up.id = m.pandit_id
		            WHERE m.event_id = e.id AND m.status = 'matched' LIMIT 1),
		           (SELECT string_agg(COALESCE(up.first_name, '') || ' ' || COALESCE(up.last_name, ''), ', ')
		            FROM job_leads jl
		            JOIN users up ON up.id = jl.pandit_id
		            WHERE jl.event_id = e.id AND jl.deleted_at IS NULL AND jl.status = 'sent'),
		           ''
		       ) AS pandit_name
		FROM events e
		LEFT JOIN users u ON u.id = e.yajman_id
		WHERE e.deleted_at IS NULL
		ORDER BY e.event_date DESC
		LIMIT 500
	`)
	if err != nil {
		h.internal(w, r, "list events", err)
		return
	}
	defer rows.Close()

	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, yajmanID, ceremonyType, address, description, customName, customDescription, status, yajmanName, panditName string
		var eventDate, createdAt, updatedAt int64
		var lat, lng *float64
		var platformFee *float64
		if err := rows.Scan(&id, &yajmanID, &ceremonyType, &eventDate, &lat, &lng, &address, &description, &customName, &customDescription, &status, &createdAt, &updatedAt, &platformFee, &yajmanName, &panditName); err != nil {
			h.internal(w, r, "scan events", err)
			return
		}
		items = append(items, map[string]any{
			"id": id, "yajman_id": yajmanID, "ceremony_type": ceremonyType, "event_date": eventDate,
			"latitude": lat, "longitude": lng, "address": address, "description": description,
			"custom_ceremony_name": customName, "custom_ceremony_description": customDescription,
			"status": status, "created_at": createdAt, "updated_at": updatedAt,
			"platform_fee": platformFee, "yajman_name": strings.TrimSpace(yajmanName),
			"pandit_name": strings.TrimSpace(panditName),
		})
	}
	response.WriteData(w, http.StatusOK, items)
}

// CreateEvent creates a new event.
// @Summary Create event
// @Description Manually create a new event record.
// @Tags admin
// @Security AdminToken
// @Accept json
// @Produce json
// @Param body body map[string]any true "Event data"
// @Success 201 {object} response.DataResponse{data=object{id=string}} "Event created"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router /api/v2/admin/events [post]
func (h *Handler) CreateEvent(w http.ResponseWriter, r *http.Request) {
	var req map[string]any
	if !decodeJSON(w, r, &req) {
		return
	}

	var fee float64
	var hasFee bool
	if pf, ok := req["platform_fee"].(float64); ok {
		fee = pf
		hasFee = true
	}
	if !hasFee {
		err := h.pool.QueryRow(r.Context(), `
			SELECT fee_amount FROM platform_fee_config
			WHERE (ceremony_type = $1 OR ceremony_type = '*') AND is_active = true
			ORDER BY CASE WHEN ceremony_type = '*' THEN 1 ELSE 0 END, created_at DESC
			LIMIT 1
		`, str(req, "ceremony_type")).Scan(&fee)
		if err != nil {
			fee = 49.00
		}
	}

	id := uuid.New()
	_, err := h.pool.Exec(r.Context(), `
		INSERT INTO events (
			id, yajman_id, ceremony_type, custom_ceremony_name, custom_ceremony_description,
			event_date, location, address, description, status, platform_fee
		)
		VALUES (
			$1, $2, $3::ceremony_type,
			CASE WHEN $3 = 'custom' THEN NULLIF($4, '') ELSE NULL END,
			CASE WHEN $3 = 'custom' THEN NULLIF($5, '') ELSE NULL END,
			$6, ST_SetSRID(ST_MakePoint($8, $7), 4326)::geography, $9, $10, $11::event_status,
			$12
		)
	`, id, str(req, "yajman_id"), str(req, "ceremony_type"), str(req, "custom_ceremony_name"), str(req, "custom_ceremony_description"), int64Val(req, "event_date"), floatVal(req, "latitude"), floatVal(req, "longitude"), str(req, "address"), str(req, "description"), defaultString(str(req, "status"), "Created"), fee)
	if err != nil {
		h.internal(w, r, "create event", err)
		return
	}
	response.WriteData(w, http.StatusCreated, map[string]string{"id": id.String()})
}

// UpdateEvent updates an existing event.
// @Summary Update event
// @Description Update event fields. Returns 404 if event not found.
// @Tags admin
// @Security AdminToken
// @Accept json
// @Produce json
// @Param id path string true "Event ID"
// @Param body body map[string]any true "Event data"
// @Success 200 {object} response.DataResponse{data=object{id=string}} "Event updated"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Failure 404 {object} apierrors.ProblemDetail "Event not found"
// @Router /api/v2/admin/events/{id} [put]
func (h *Handler) UpdateEvent(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	var req map[string]any
	if !decodeJSON(w, r, &req) {
		return
	}
	tag, err := h.pool.Exec(r.Context(), `
		UPDATE events
		SET yajman_id = $2,
		    ceremony_type = $3::ceremony_type,
		    custom_ceremony_name = CASE WHEN $3 = 'custom' THEN NULLIF($4, '') ELSE NULL END,
		    custom_ceremony_description = CASE WHEN $3 = 'custom' THEN NULLIF($5, '') ELSE NULL END,
		    event_date = $6,
		    location = CASE WHEN $7::DOUBLE PRECISION IS NULL OR $8::DOUBLE PRECISION IS NULL THEN location ELSE ST_SetSRID(ST_MakePoint($8, $7), 4326)::geography END,
		    address = $9,
		    description = $10,
		    status = $11::event_status,
		    platform_fee = $12,
		    updated_at = (EXTRACT(EPOCH FROM NOW()))::BIGINT
		WHERE id = $1 AND deleted_at IS NULL
	`, id, str(req, "yajman_id"), str(req, "ceremony_type"), str(req, "custom_ceremony_name"), str(req, "custom_ceremony_description"), int64Val(req, "event_date"), floatPtr(req, "latitude"), floatPtr(req, "longitude"), str(req, "address"), str(req, "description"), str(req, "status"), floatPtr(req, "platform_fee"))
	if err != nil {
		h.internal(w, r, "update event", err)
		return
	}

	if tag.RowsAffected() == 0 {
		apierrors.WriteProblemDetail(w, apierrors.NotFound("Event not found or already deleted.", r.URL.Path))
		return
	}

	response.WriteData(w, http.StatusOK, map[string]string{"id": id.String()})
}

// DeleteEvent soft-deletes an event.
// @Summary Delete event
// @Description Soft-deletes an event. Returns 404 if event not found.
// @Tags admin
// @Security AdminToken
// @Param id path string true "Event ID"
// @Success 204 "Event deleted"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Failure 404 {object} apierrors.ProblemDetail "Event not found"
// @Router /api/v2/admin/events/{id} [delete]
func (h *Handler) DeleteEvent(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	tag, err := h.pool.Exec(r.Context(), `UPDATE events SET deleted_at = (EXTRACT(EPOCH FROM NOW()))::BIGINT WHERE id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		h.internal(w, r, "delete event", err)
		return
	}

	if tag.RowsAffected() == 0 {
		apierrors.WriteProblemDetail(w, apierrors.NotFound("Event not found or already deleted.", r.URL.Path))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ListPosts returns a list of social posts.
// @Summary List posts
// @Description Returns up to 500 non-deleted social posts with author metadata.
// @Tags admin
// @Security AdminToken
// @Produce json
// @Success 200 {object} response.DataResponse{data=[]map[string]any} "List of posts"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router /api/v2/admin/posts [get]
func (h *Handler) ListPosts(w http.ResponseWriter, r *http.Request) {
	rows, err := h.pool.Query(r.Context(), `
		SELECT p.id, p.author_id, p.content, COALESCE(p.image_urls, '{}'), COALESCE(p.tags, '{}'),
		       p.like_count, p.comment_count, p.is_pinned, p.post_type, p.visibility, p.feed_score,
		       COALESCE(p.specialty, '{}'), p.created_at, p.updated_at,
		       COALESCE(u.first_name, ''), COALESCE(u.last_name, ''), COALESCE(u.role::text, '')
		FROM posts p
		LEFT JOIN users u ON u.id = p.author_id
		WHERE p.deleted_at IS NULL
		ORDER BY p.created_at DESC, p.id DESC
		LIMIT 500
	`)
	if err != nil {
		h.internal(w, r, "list posts", err)
		return
	}
	defer rows.Close()

	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, authorID, content, postType, visibility, firstName, lastName, authorRole string
		var imageURLs, tags, specialty []string
		var likeCount, commentCount int
		var isPinned bool
		var feedScore float64
		var createdAt, updatedAt int64
		if err := rows.Scan(&id, &authorID, &content, &imageURLs, &tags, &likeCount, &commentCount, &isPinned, &postType, &visibility, &feedScore, &specialty, &createdAt, &updatedAt, &firstName, &lastName, &authorRole); err != nil {
			h.internal(w, r, "scan posts", err)
			return
		}
		items = append(items, map[string]any{
			"id": id, "author_id": authorID, "author_name": strings.TrimSpace(firstName + " " + lastName),
			"author_role": authorRole, "content": content, "image_urls": strings.Join(imageURLs, ", "),
			"tags": strings.Join(tags, ", "), "like_count": likeCount, "comment_count": commentCount,
			"is_pinned": isPinned, "post_type": postType, "visibility": visibility, "feed_score": feedScore,
			"specialty": strings.Join(specialty, ", "), "created_at": createdAt, "updated_at": updatedAt,
		})
	}
	response.WriteData(w, http.StatusOK, items)
}

// CreatePost creates a social post.
// @Summary Create post
// @Description Manually create a social feed post.
// @Tags admin
// @Security AdminToken
// @Accept json
// @Produce json
// @Param body body map[string]any true "Post data"
// @Success 201 {object} response.DataResponse{data=object{id=string}} "Post created"
// @Failure 400 {object} apierrors.ProblemDetail "Invalid request"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router /api/v2/admin/posts [post]
func (h *Handler) CreatePost(w http.ResponseWriter, r *http.Request) {
	var req map[string]any
	if !decodeJSON(w, r, &req) {
		return
	}
	authorID, err := uuid.Parse(str(req, "author_id"))
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("A valid author_id is required.", r.URL.Path))
		return
	}
	imageURLs := stringSlice(req, "image_urls")
	if strings.TrimSpace(str(req, "content")) == "" && len(imageURLs) == 0 {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Post content or at least one image URL is required.", r.URL.Path))
		return
	}

	id := uuid.New()
	now := time.Now().Unix()
	_, err = h.pool.Exec(r.Context(), `
		INSERT INTO posts (
			id, author_id, content, image_urls, tags, like_count, comment_count, is_pinned,
			post_type, visibility, feed_score, specialty, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $13)
	`, id, authorID, str(req, "content"), imageURLs, stringSlice(req, "tags"),
		int64Val(req, "like_count"), int64Val(req, "comment_count"), boolVal(req, "is_pinned"),
		defaultString(str(req, "post_type"), "post"), defaultString(str(req, "visibility"), "public"),
		floatVal(req, "feed_score"), stringSlice(req, "specialty"), now)
	if err != nil {
		h.internal(w, r, "create post", err)
		return
	}
	response.WriteData(w, http.StatusCreated, map[string]string{"id": id.String()})
}

// UpdatePost updates a social post.
// @Summary Update post
// @Description Update social feed post fields. Returns 404 if post is not found.
// @Tags admin
// @Security AdminToken
// @Accept json
// @Produce json
// @Param id path string true "Post ID"
// @Param body body map[string]any true "Post data"
// @Success 200 {object} response.DataResponse{data=object{id=string}} "Post updated"
// @Failure 400 {object} apierrors.ProblemDetail "Invalid request"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Failure 404 {object} apierrors.ProblemDetail "Post not found"
// @Router /api/v2/admin/posts/{id} [put]
func (h *Handler) UpdatePost(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	var req map[string]any
	if !decodeJSON(w, r, &req) {
		return
	}
	authorID, err := uuid.Parse(str(req, "author_id"))
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("A valid author_id is required.", r.URL.Path))
		return
	}
	imageURLs := stringSlice(req, "image_urls")
	if strings.TrimSpace(str(req, "content")) == "" && len(imageURLs) == 0 {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Post content or at least one image URL is required.", r.URL.Path))
		return
	}

	tag, err := h.pool.Exec(r.Context(), `
		UPDATE posts
		SET author_id = $2,
		    content = $3,
		    image_urls = $4,
		    tags = $5,
		    like_count = $6,
		    comment_count = $7,
		    is_pinned = $8,
		    post_type = $9,
		    visibility = $10,
		    feed_score = $11,
		    specialty = $12,
		    updated_at = (EXTRACT(EPOCH FROM NOW()))::BIGINT
		WHERE id = $1 AND deleted_at IS NULL
	`, id, authorID, str(req, "content"), imageURLs, stringSlice(req, "tags"),
		int64Val(req, "like_count"), int64Val(req, "comment_count"), boolVal(req, "is_pinned"),
		defaultString(str(req, "post_type"), "post"), defaultString(str(req, "visibility"), "public"),
		floatVal(req, "feed_score"), stringSlice(req, "specialty"))
	if err != nil {
		h.internal(w, r, "update post", err)
		return
	}
	if tag.RowsAffected() == 0 {
		apierrors.WriteProblemDetail(w, apierrors.NotFound("Post not found or already deleted.", r.URL.Path))
		return
	}

	response.WriteData(w, http.StatusOK, map[string]string{"id": id.String()})
}

// DeletePost soft-deletes a social post.
// @Summary Delete post
// @Description Soft-deletes a social feed post. Returns 404 if post is not found.
// @Tags admin
// @Security AdminToken
// @Param id path string true "Post ID"
// @Success 204 "Post deleted"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Failure 404 {object} apierrors.ProblemDetail "Post not found"
// @Router /api/v2/admin/posts/{id} [delete]
func (h *Handler) DeletePost(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	tag, err := h.pool.Exec(r.Context(), `UPDATE posts SET deleted_at = (EXTRACT(EPOCH FROM NOW()))::BIGINT WHERE id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		h.internal(w, r, "delete post", err)
		return
	}
	if tag.RowsAffected() == 0 {
		apierrors.WriteProblemDetail(w, apierrors.NotFound("Post not found or already deleted.", r.URL.Path))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ListFestivals returns a list of festivals.
// @Summary List festivals
// @Description Returns all festivals from the database.
// @Tags admin
// @Security AdminToken
// @Produce json
// @Success 200 {object} response.DataResponse{data=[]map[string]any} "List of festivals with id, festival, date"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router /api/v2/admin/festivals [get]
func (h *Handler) ListFestivals(w http.ResponseWriter, r *http.Request) {
	rows, err := h.pool.Query(r.Context(), `
		SELECT f.id, f.festival, f.date, f.created_at, COALESCE(fl.image_url_no_bg, '') AS image_url, COALESCE(f.description, '') AS description
		FROM festivals f
		LEFT JOIN festival_logos fl ON LOWER(TRIM(f.festival)) = LOWER(TRIM(fl.name))
		ORDER BY f.date ASC LIMIT 1000
	`)
	if err != nil {
		h.internal(w, r, "list festivals", err)
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, festival, imageURL, description string
		var date, createdAt int64
		if err := rows.Scan(&id, &festival, &date, &createdAt, &imageURL, &description); err != nil {
			h.internal(w, r, "scan festivals", err)
			return
		}
		resolvedImageURL := imageURL
		if h.s3Client != nil && imageURL != "" {
			resolvedImageURL = h.s3Client.ResolveURLForBucket(storage.BucketFestivalLogos, imageURL)
		}
		items = append(items, map[string]any{
			"id":          id,
			"festival":    festival,
			"date":        date,
			"image_url":   resolvedImageURL,
			"description": description,
			"created_at":  createdAt,
		})
	}
	response.WriteData(w, http.StatusOK, items)
}

// CreateFestival creates a new festival.
// @Summary Create festival
// @Description Manually create a new festival record.
// @Tags admin
// @Security AdminToken
// @Accept json
// @Produce json
// @Param body body map[string]any{festival=string,date=int64} true "Festival data"
// @Success 201 {object} response.DataResponse{data=object{id=string}} "Festival created"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router /api/v2/admin/festivals [post]
func (h *Handler) CreateFestival(w http.ResponseWriter, r *http.Request) {
	var req map[string]any
	if !decodeJSON(w, r, &req) {
		return
	}
	id := uuid.New()
	_, err := h.pool.Exec(r.Context(), `INSERT INTO festivals (id, festival, date, description) VALUES ($1, $2, $3, $4)`, id, str(req, "festival"), int64Val(req, "date"), str(req, "description"))
	if err != nil {
		h.internal(w, r, "create festival", err)
		return
	}
	response.WriteData(w, http.StatusCreated, map[string]string{"id": id.String()})
}

// UpdateFestival updates an existing festival.
// @Summary Update festival
// @Description Update festival fields. Returns 404 if festival not found.
// @Tags admin
// @Security AdminToken
// @Accept json
// @Produce json
// @Param id path string true "Festival ID"
// @Param body body map[string]any{festival=string,date=int64} true "Festival data"
// @Success 200 {object} response.DataResponse{data=object{id=string}} "Festival updated"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Failure 404 {object} apierrors.ProblemDetail "Festival not found"
// @Router /api/v2/admin/festivals/{id} [put]
func (h *Handler) UpdateFestival(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req map[string]any
	if !decodeJSON(w, r, &req) {
		return
	}
	tag, err := h.pool.Exec(r.Context(), `UPDATE festivals SET festival = $2, date = $3, description = $4 WHERE id = $1`, id, str(req, "festival"), int64Val(req, "date"), str(req, "description"))
	if err != nil {
		h.internal(w, r, "update festival", err)
		return
	}

	if tag.RowsAffected() == 0 {
		apierrors.WriteProblemDetail(w, apierrors.NotFound("Festival not found.", r.URL.Path))
		return
	}

	response.WriteData(w, http.StatusOK, map[string]string{"id": id})
}

// DeleteFestival deletes a festival.
// @Summary Delete festival
// @Description Deletes a festival record. Returns 404 if festival not found.
// @Tags admin
// @Security AdminToken
// @Param id path string true "Festival ID"
// @Success 204 "Festival deleted"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Failure 404 {object} apierrors.ProblemDetail "Festival not found"
// @Router /api/v2/admin/festivals/{id} [delete]
func (h *Handler) DeleteFestival(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	tag, err := h.pool.Exec(r.Context(), `DELETE FROM festivals WHERE id = $1`, id)
	if err != nil {
		h.internal(w, r, "delete festival", err)
		return
	}

	if tag.RowsAffected() == 0 {
		apierrors.WriteProblemDetail(w, apierrors.NotFound("Festival not found.", r.URL.Path))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ListPanchanga returns a list of panchanga records.
// @Summary List panchanga
// @Description Returns panchanga records within a date range.
// @Tags admin
// @Security AdminToken
// @Param from query string false "From date (Unix timestamp)"
// @Param to query string false "To date (Unix timestamp)"
// @Produce json
// @Success 200 {object} response.DataResponse{data=[]map[string]any} "List of panchanga records"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router /api/v2/admin/panchanga [get]
func (h *Handler) ListPanchanga(w http.ResponseWriter, r *http.Request) {
	from := queryInt64(r, "from", 0)
	to := queryInt64(r, "to", time.Now().AddDate(1, 0, 0).Unix())
	if from == 0 {
		from = time.Now().AddDate(0, -1, 0).Unix()
	}
	rows, err := h.pool.Query(r.Context(), `
		SELECT id, date, samvatsara, ayana, rutu, masa, paksha, tithi, nakshatra, yoga, karana,
		       vasara, shraddha_tithi, masa_niyamaka, festivals_events, sunrise, sunset, rahukala, gulikala, yamaganda
		FROM panchanga
		WHERE date BETWEEN $1 AND $2
		ORDER BY date ASC
		LIMIT 500
	`, from, to)
	if err != nil {
		h.internal(w, r, "list panchanga", err)
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		item := map[string]any{}
		var id string
		var date int64
		var fields [18]string
		if err := rows.Scan(&id, &date, &fields[0], &fields[1], &fields[2], &fields[3], &fields[4], &fields[5], &fields[6], &fields[7], &fields[8], &fields[9], &fields[10], &fields[11], &fields[12], &fields[13], &fields[14], &fields[15], &fields[16], &fields[17]); err != nil {
			h.internal(w, r, "scan panchanga", err)
			return
		}
		names := []string{"samvatsara", "ayana", "rutu", "masa", "paksha", "tithi", "nakshatra", "yoga", "karana", "vasara", "shraddha_tithi", "masa_niyamaka", "festivals_events", "sunrise", "sunset", "rahukala", "gulikala", "yamaganda"}
		item["id"], item["date"] = id, date
		for i, name := range names {
			item[name] = fields[i]
		}
		items = append(items, item)
	}
	response.WriteData(w, http.StatusOK, items)
}

// UpdatePanchanga updates an existing panchanga record.
// @Summary Update panchanga
// @Description Update panchanga fields.
// @Tags admin
// @Security AdminToken
// @Accept json
// @Produce json
// @Param id path string true "Panchanga ID"
// @Param body body map[string]any true "Panchanga data"
// @Success 200 {object} response.DataResponse{data=object{id=string}} "Panchanga updated"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router /api/v2/admin/panchanga/{id} [put]
func (h *Handler) UpdatePanchanga(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	var req map[string]any
	if !decodeJSON(w, r, &req) {
		return
	}
	tag, err := h.pool.Exec(r.Context(), `
		UPDATE panchanga
		SET samvatsara=$2, ayana=$3, rutu=$4, masa=$5, paksha=$6, tithi=$7, nakshatra=$8, yoga=$9,
		    karana=$10, vasara=$11, shraddha_tithi=$12, masa_niyamaka=$13, festivals_events=$14,
		    sunrise=$15, sunset=$16, rahukala=$17, gulikala=$18, yamaganda=$19
		WHERE id=$1
	`, id, str(req, "samvatsara"), str(req, "ayana"), str(req, "rutu"), str(req, "masa"), str(req, "paksha"), str(req, "tithi"), str(req, "nakshatra"), str(req, "yoga"), str(req, "karana"), str(req, "vasara"), str(req, "shraddha_tithi"), str(req, "masa_niyamaka"), str(req, "festivals_events"), str(req, "sunrise"), str(req, "sunset"), str(req, "rahukala"), str(req, "gulikala"), str(req, "yamaganda"))
	if err != nil {
		h.internal(w, r, "update panchanga", err)
		return
	}
	if tag.RowsAffected() == 0 {
		apierrors.WriteProblemDetail(w, apierrors.NotFound("Panchanga record not found.", r.URL.Path))
		return
	}
	response.WriteData(w, http.StatusOK, map[string]string{"id": id.String()})
}

// CreatePanchanga creates a new panchanga record.
// @Summary Create panchanga
// @Description Manually create a new panchanga record.
// @Tags admin
// @Security AdminToken
// @Accept json
// @Produce json
// @Param body body map[string]any true "Panchanga data"
// @Success 201 {object} response.DataResponse{data=object{id=string}} "Panchanga created"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router /api/v2/admin/panchanga [post]
func (h *Handler) CreatePanchanga(w http.ResponseWriter, r *http.Request) {
	var req map[string]any
	if !decodeJSON(w, r, &req) {
		return
	}
	id := uuid.New()
	_, err := h.pool.Exec(r.Context(), `
		INSERT INTO panchanga (
			id, date, samvatsara, ayana, rutu, masa, paksha, tithi, nakshatra, yoga, karana,
			vasara, shraddha_tithi, masa_niyamaka, festivals_events, sunrise, sunset, rahukala, gulikala, yamaganda
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20
		)
	`, id, int64Val(req, "date"), str(req, "samvatsara"), str(req, "ayana"), str(req, "rutu"), str(req, "masa"), str(req, "paksha"), str(req, "tithi"), str(req, "nakshatra"), str(req, "yoga"), str(req, "karana"), str(req, "vasara"), str(req, "shraddha_tithi"), str(req, "masa_niyamaka"), str(req, "festivals_events"), str(req, "sunrise"), str(req, "sunset"), str(req, "rahukala"), str(req, "gulikala"), str(req, "yamaganda"))
	if err != nil {
		h.internal(w, r, "create panchanga", err)
		return
	}
	response.WriteData(w, http.StatusCreated, map[string]string{"id": id.String()})
}

// DeletePanchanga soft-deletes a panchanga record.
// @Summary Delete panchanga
// @Description Soft-deletes a panchanga record by setting deleted_at.
// @Tags admin
// @Security AdminToken
// @Param id path string true "Panchanga ID"
// @Success 200 {object} response.DataResponse{data=object{status=string}} "Panchanga deleted"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router /api/v2/admin/panchanga/{id} [delete]
func (h *Handler) DeletePanchanga(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	tag, err := h.pool.Exec(r.Context(), `DELETE FROM panchanga WHERE id = $1`, id)
	if err != nil {
		h.internal(w, r, "delete panchanga", err)
		return
	}
	if tag.RowsAffected() == 0 {
		apierrors.WriteProblemDetail(w, apierrors.NotFound("Panchanga record not found.", r.URL.Path))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListFestivalLogos returns a list of festival logos.
// @Summary List festival logos
// @Description Returns all festival logos from the database.
// @Tags admin
// @Security AdminToken
// @Produce json
// @Success 200 {object} response.DataResponse{data=[]map[string]any} "List of festival logos"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router /api/v2/admin/ceremony-logos [get]
func (h *Handler) ListFestivalLogos(w http.ResponseWriter, r *http.Request) {
	rows, err := h.pool.Query(r.Context(), `
		SELECT id, name, slug, COALESCE(image_url_no_bg, ''), 
		       COALESCE(category, ''), COALESCE(description, ''),
		       is_active, display_order, created_at 
		FROM festival_logos 
		ORDER BY category ASC, display_order ASC, name ASC
	`)
	if err != nil {
		h.internal(w, r, "list festival logos", err)
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, name, slug, imageURLNoBg, category, description string
		var isActive bool
		var displayOrder int
		var createdAt time.Time
		if err := rows.Scan(&id, &name, &slug, &imageURLNoBg, &category, &description, &isActive, &displayOrder, &createdAt); err != nil {
			h.internal(w, r, "scan festival logos", err)
			return
		}
		resolvedImageURLNoBg := imageURLNoBg
		if h.s3Client != nil && imageURLNoBg != "" {
			resolvedImageURLNoBg = h.s3Client.ResolveURLForBucket(storage.BucketFestivalLogos, imageURLNoBg)
		}
		items = append(items, map[string]any{
			"id":              id,
			"name":            name,
			"slug":            slug,
			"image_url":       resolvedImageURLNoBg, // Admin panel compatibility
			"image_url_no_bg": resolvedImageURLNoBg, // Android compatibility
			"category":        category,
			"description":     description,
			"is_active":       isActive,
			"display_order":   displayOrder,
			"created_at":      createdAt.Unix(),
		})
	}
	response.WriteData(w, http.StatusOK, items)
}

// CreateFestivalLogo creates a new festival logo.
// @Summary Create festival logo
// @Description Manually create a new festival logo record.
// @Tags admin
// @Security AdminToken
// @Accept json
// @Produce json
// @Param body body map[string]any true "Logo data"
// @Success 201 {object} response.DataResponse{data=object{id=string}} "Logo created"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router /api/v2/admin/ceremony-logos [post]
func (h *Handler) CreateFestivalLogo(w http.ResponseWriter, r *http.Request) {
	var req map[string]any
	if !decodeJSON(w, r, &req) {
		return
	}
	logoURL := str(req, "image_url")
	if logoURL == "" {
		logoURL = str(req, "image_url_no_bg")
	}
	id := uuid.New()
	_, err := h.pool.Exec(r.Context(), `
		INSERT INTO festival_logos (id, name, slug, image_url_no_bg, category, description, is_active, display_order)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, id, str(req, "name"), str(req, "slug"), logoURL, str(req, "category"), str(req, "description"), boolVal(req, "is_active"), int64Val(req, "display_order"))
	if err != nil {
		h.internal(w, r, "create festival logo", err)
		return
	}
	response.WriteData(w, http.StatusCreated, map[string]string{"id": id.String()})
}

// UpdateFestivalLogo updates an existing festival logo.
// @Summary Update festival logo
// @Description Update festival logo fields.
// @Tags admin
// @Security AdminToken
// @Accept json
// @Produce json
// @Param id path string true "Logo ID or slug"
// @Param body body map[string]any true "Logo data"
// @Success 200 {object} response.DataResponse{data=object{id=string}} "Logo updated"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router /api/v2/admin/ceremony-logos/{id} [put]
func (h *Handler) UpdateFestivalLogo(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req map[string]any
	if !decodeJSON(w, r, &req) {
		return
	}
	logoURL := str(req, "image_url")
	if logoURL == "" {
		logoURL = str(req, "image_url_no_bg")
	}
	tag, err := h.pool.Exec(r.Context(), `
		UPDATE festival_logos
		SET name = $2, slug = $3, image_url_no_bg = $4,
		    category = $5, description = $6, is_active = $7, display_order = $8, updated_at = now()
		WHERE id::text = $1 OR slug = $1
	`, id, str(req, "name"), str(req, "slug"), logoURL, str(req, "category"), str(req, "description"), boolVal(req, "is_active"), int64Val(req, "display_order"))
	if err != nil {
		h.internal(w, r, "update festival logo", err)
		return
	}
	if tag.RowsAffected() == 0 {
		apierrors.WriteProblemDetail(w, apierrors.NotFound("Festival logo not found.", r.URL.Path))
		return
	}
	response.WriteData(w, http.StatusOK, map[string]string{"id": id})
}

// DeleteFestivalLogo deletes a festival logo.
// @Summary Delete festival logo
// @Description Deletes a festival logo from the database.
// @Tags admin
// @Security AdminToken
// @Param id path string true "Logo ID or slug"
// @Success 200 {object} response.DataResponse{data=object{status=string}} "Logo deleted"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router /api/v2/admin/ceremony-logos/{id} [delete]
func (h *Handler) DeleteFestivalLogo(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	tag, err := h.pool.Exec(r.Context(), `DELETE FROM festival_logos WHERE id::text = $1 OR slug = $1`, id)
	if err != nil {
		h.internal(w, r, "delete festival logo", err)
		return
	}
	if tag.RowsAffected() == 0 {
		apierrors.WriteProblemDetail(w, apierrors.NotFound("Festival logo not found.", r.URL.Path))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// PresignLogo generates a presigned URL for festival logo upload.
// @Summary Presign logo upload
// @Description Generates a presigned URL for uploading a festival logo.
// @Tags admin
// @Security AdminToken
// @Accept json
// @Produce json
// @Param body body object{file_name=string,slug=string} true "Presign request"
// @Success 200 {object} response.DataResponse{data=object{upload_url=string,object_key=string,public_url=string}} "Presigned URL"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router /api/v2/admin/ceremony-logos/presign [post]
func (h *Handler) PresignLogo(w http.ResponseWriter, r *http.Request) {
	h.presignMedia(w, r, "festival_logo")
}

// PresignMedia generates a presigned URL for media upload.
// @Summary Presign media upload
// @Description Generates a presigned URL for uploading media to S3-compatible storage.
// @Tags admin
// @Security AdminToken
// @Accept json
// @Produce json
// @Param body body object{filename=string,bucket=string} true "Presign request"
// @Success 200 {object} response.DataResponse{data=object{upload_url=string,object_key=string,public_url=string,expires_in=int}} "Presigned URL"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router /api/v2/admin/media/presign [post]
func (h *Handler) PresignMedia(w http.ResponseWriter, r *http.Request) {
	h.presignMedia(w, r, "")
}

func (h *Handler) presignMedia(w http.ResponseWriter, r *http.Request, defaultUploadType string) {
	if h.s3Client == nil {
		apierrors.WriteProblemDetail(w, apierrors.ServiceUnavailable("Storage service is not configured.", r.URL.Path))
		return
	}
	var req struct {
		FileName    string `json:"file_name"`
		ContentType string `json:"content_type"`
		Slug        string `json:"slug"`
		UploadType  string `json:"upload_type"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	uploadType := strings.TrimSpace(req.UploadType)
	if uploadType == "" {
		uploadType = defaultUploadType
	}
	if uploadType == "" {
		uploadType = "admin_media"
	}
	ext := strings.TrimPrefix(filepath.Ext(filepath.Base(req.FileName)), ".")
	if ext == "" {
		ext = "png"
	}
	slug := strings.Trim(strings.ToLower(req.Slug), "/ ")
	if slug == "" {
		slug = strings.ReplaceAll(uploadType, "_", "-")
	}
	bucket := storage.BucketAdminMedia
	switch uploadType {
	case "festival_logo", "ceremony_logo":
		bucket = storage.BucketFestivalLogos
	case "profile_photo":
		bucket = storage.BucketProfiles
	}
	objectKey := fmt.Sprintf("%s-%s.%s", slug, uuid.NewString(), ext)
	uploadURL, err := h.s3Client.GeneratePresignedPutURL(r.Context(), bucket, objectKey, 15*time.Minute)
	if err != nil {
		h.internal(w, r, "presign media", err)
		return
	}
	response.WriteData(w, http.StatusOK, map[string]any{
		"upload_url": uploadURL,
		"object_key": objectKey,
		"public_url": h.s3Client.ResolveURLForBucket(bucket, objectKey),
		"expires_in": 900,
	})
}

// -- Storage / Bucket Management Endpoints --

var managedBuckets = storage.ManagedBuckets()

// ListBuckets returns a list of managed buckets.
// @Summary List buckets
// @Description Returns a list of buckets managed by the system.
// @Tags admin
// @Security AdminToken
// @Produce json
// @Success 200 {object} response.DataResponse{data=[]string} "List of buckets"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router /api/v2/admin/buckets [get]
func (h *Handler) ListBuckets(w http.ResponseWriter, r *http.Request) {
	response.WriteData(w, http.StatusOK, managedBuckets)
}

func (h *Handler) isManagedBucket(b string) bool {
	for _, mb := range managedBuckets {
		if mb == b {
			return true
		}
	}
	return false
}

// ListBucketObjects returns objects in a bucket.
// @Summary List bucket objects
// @Description Returns a list of objects in the specified bucket.
// @Tags admin
// @Security AdminToken
// @Param bucket path string true "Bucket name"
// @Produce json
// @Success 200 {object} response.DataResponse{data=[]map[string]any} "List of objects"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router /api/v2/admin/buckets/{bucket}/objects [get]
func (h *Handler) ListBucketObjects(w http.ResponseWriter, r *http.Request) {
	bucket := chi.URLParam(r, "bucket")
	if !h.isManagedBucket(bucket) {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Invalid bucket.", r.URL.Path))
		return
	}
	if h.s3Client == nil || h.s3Client.RawClient() == nil {
		apierrors.WriteProblemDetail(w, apierrors.ServiceUnavailable("Storage service is not configured.", r.URL.Path))
		return
	}

	ctx := r.Context()
	client := h.s3Client.RawClient()
	objectsCh := client.ListObjects(ctx, bucket, minio.ListObjectsOptions{Recursive: true})

	var items []map[string]any
	for object := range objectsCh {
		if object.Err != nil {
			h.internal(w, r, "list bucket objects", object.Err)
			return
		}
		items = append(items, map[string]any{
			"name":          object.Key,
			"size":          object.Size,
			"last_modified": object.LastModified.Unix(),
			"url":           fmt.Sprintf("%s/%s/%s", h.s3Client.PublicBaseURL(), bucket, object.Key),
		})
	}
	if items == nil {
		items = []map[string]any{}
	}
	response.WriteData(w, http.StatusOK, items)
}

// UploadBucketObject uploads an object to a bucket.
// @Summary Upload bucket object
// @Description Uploads a file to the specified bucket.
// @Tags admin
// @Security AdminToken
// @Param bucket path string true "Bucket name"
// @Param file formData file true "File to upload"
// @Param preserve_name query boolean false "Preserve original filename"
// @Accept multipart/form-data
// @Produce json
// @Success 200 {object} response.DataResponse{data=object{key=string,url=string}} "Upload successful"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router /api/v2/admin/buckets/{bucket}/objects [post]
func (h *Handler) UploadBucketObject(w http.ResponseWriter, r *http.Request) {
	bucket := chi.URLParam(r, "bucket")
	if !h.isManagedBucket(bucket) {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Invalid bucket.", r.URL.Path))
		return
	}
	if h.s3Client == nil || h.s3Client.RawClient() == nil {
		apierrors.WriteProblemDetail(w, apierrors.ServiceUnavailable("Storage service is not configured.", r.URL.Path))
		return
	}

	err := r.ParseMultipartForm(150 << 20) // 150 MB limit
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Could not parse multipart form.", r.URL.Path))
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("No file provided.", r.URL.Path))
		return
	}
	defer file.Close()

	ctx := r.Context()
	client := h.s3Client.RawClient()

	// Ensure the bucket exists
	exists, err := client.BucketExists(ctx, bucket)
	if err != nil {
		h.internal(w, r, "check bucket exists", err)
		return
	}
	if !exists {
		err = client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{})
		if err != nil {
			h.internal(w, r, "create bucket", err)
			return
		}
		// Apply public read policy so uploaded files are downloadable
		policy := fmt.Sprintf(`{
			"Version": "2012-10-17",
			"Statement": [
				{
					"Effect": "Allow",
					"Principal": {"AWS": ["*"]},
					"Action": ["s3:GetObject"],
					"Resource": ["arn:aws:s3:::%s/*"]
				}
			]
		}`, bucket)
		_ = client.SetBucketPolicy(ctx, bucket, policy)
	}

	objectName := header.Filename
	// If preserve_name is set, use the original filename as-is (useful for seeding logos with clean names).
	// Otherwise, append a UUID to prevent collisions.
	preserveName := r.URL.Query().Get("preserve_name") == "true"
	var objectKey string
	if preserveName {
		objectKey = objectName
	} else {
		ext := filepath.Ext(objectName)
		base := strings.TrimSuffix(filepath.Base(objectName), ext)
		objectKey = fmt.Sprintf("%s-%s%s", base, uuid.NewString(), ext)
	}

	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	_, err = client.PutObject(ctx, bucket, objectKey, file, header.Size, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		h.internal(w, r, "upload bucket object", err)
		return
	}

	response.WriteData(w, http.StatusCreated, map[string]any{
		"name": objectKey,
		"size": header.Size,
		"url":  fmt.Sprintf("%s/%s/%s", h.s3Client.PublicBaseURL(), bucket, objectKey),
	})
}

// DeleteBucketObject deletes an object from a bucket.
// @Summary Delete bucket object
// @Description Deletes a specific object from a managed bucket.
// @Tags admin
// @Security AdminToken
// @Param bucket path string true "Bucket name"
// @Param key query string true "Object key"
// @Success 204 "No Content"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router /api/v2/admin/buckets/{bucket}/objects [delete]
func (h *Handler) DeleteBucketObject(w http.ResponseWriter, r *http.Request) {
	bucket := chi.URLParam(r, "bucket")
	key := r.URL.Query().Get("key")
	if !h.isManagedBucket(bucket) {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Invalid bucket.", r.URL.Path))
		return
	}
	if key == "" {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Key is required.", r.URL.Path))
		return
	}
	if h.s3Client == nil || h.s3Client.RawClient() == nil {
		apierrors.WriteProblemDetail(w, apierrors.ServiceUnavailable("Storage service is not configured.", r.URL.Path))
		return
	}

	ctx := r.Context()
	client := h.s3Client.RawClient()
	err := client.RemoveObject(ctx, bucket, key, minio.RemoveObjectOptions{})
	if err != nil {
		h.internal(w, r, "delete bucket object", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// GetBucketObjectURL returns the public URL for an object.
// @Summary Get bucket object URL
// @Description Returns the public accessibility URL for a specific object key.
// @Tags admin
// @Security AdminToken
// @Param bucket path string true "Bucket name"
// @Param key query string true "Object key"
// @Produce json
// @Success 200 {object} response.DataResponse{data=object{url=string}} "Object URL"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router /api/v2/admin/buckets/{bucket}/url [get]
func (h *Handler) GetBucketObjectURL(w http.ResponseWriter, r *http.Request) {
	bucket := chi.URLParam(r, "bucket")
	key := r.URL.Query().Get("key")
	if !h.isManagedBucket(bucket) {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Invalid bucket.", r.URL.Path))
		return
	}
	if key == "" {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Key is required.", r.URL.Path))
		return
	}
	if h.s3Client == nil {
		apierrors.WriteProblemDetail(w, apierrors.ServiceUnavailable("Storage service is not configured.", r.URL.Path))
		return
	}

	urlStr := fmt.Sprintf("%s/%s/%s", h.s3Client.PublicBaseURL(), bucket, key)
	response.WriteData(w, http.StatusOK, map[string]string{"url": urlStr})
}

// BucketStats returns statistics for a specific bucket.
// @Summary Bucket statistics
// @Description Returns object count and total size for a managed bucket.
// @Tags admin
// @Security AdminToken
// @Param bucket path string true "Bucket name"
// @Produce json
// @Success 200 {object} response.DataResponse{data=object{bucket=string,total_objects=int,total_size=int}} "Bucket stats"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router /api/v2/admin/buckets/{bucket}/stats [get]
func (h *Handler) BucketStats(w http.ResponseWriter, r *http.Request) {
	bucket := chi.URLParam(r, "bucket")
	if !h.isManagedBucket(bucket) {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Invalid bucket.", r.URL.Path))
		return
	}
	if h.s3Client == nil || h.s3Client.RawClient() == nil {
		apierrors.WriteProblemDetail(w, apierrors.ServiceUnavailable("Storage service is not configured.", r.URL.Path))
		return
	}

	ctx := r.Context()
	client := h.s3Client.RawClient()
	objectsCh := client.ListObjects(ctx, bucket, minio.ListObjectsOptions{Recursive: true})

	var totalObjects int64
	var totalSize int64
	for object := range objectsCh {
		if object.Err != nil {
			h.internal(w, r, "bucket stats", object.Err)
			return
		}
		totalObjects++
		totalSize += object.Size
	}

	response.WriteData(w, http.StatusOK, map[string]any{
		"bucket":        bucket,
		"total_objects": totalObjects,
		"total_size":    totalSize,
	})
}

// -- Notifications (admin view — all notifications across all users) --

// ListNotifications returns all notifications.
// @Summary List notifications
// @Description Returns all notifications across all users.
// @Tags admin
// @Security AdminToken
// @Produce json
// @Param limit query int false "Limit"
// @Success 200 {object} response.DataResponse{data=[]map[string]any} "List of notifications"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router /api/v2/admin/notifications [get]
func (h *Handler) ListNotifications(w http.ResponseWriter, r *http.Request) {
	limit := queryInt64(r, "limit", 200)
	rows, err := h.pool.Query(r.Context(), `
		SELECT n.id, n.user_id, n.type::text, n.title, n.body,
		       COALESCE(n.read, false), n.created_at,
		       COALESCE(u.username, '')
		FROM notifications n
		LEFT JOIN users u ON u.id = n.user_id
		ORDER BY n.created_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		h.internal(w, r, "list notifications", err)
		return
	}
	defer rows.Close()

	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, userID, nType, title, body, username string
		var read bool
		var createdAt int64
		if err := rows.Scan(&id, &userID, &nType, &title, &body, &read, &createdAt, &username); err != nil {
			h.internal(w, r, "scan notifications", err)
			return
		}
		items = append(items, map[string]any{
			"id": id, "user_id": userID, "type": nType,
			"title": title, "body": body, "read": read,
			"created_at": createdAt,
			"username":   username,
		})
	}
	response.WriteData(w, http.StatusOK, items)
}

// CreateNotification creates a new notification for a user.
// @Summary Create notification
// @Description Manually create a notification for a specific user.
// @Tags admin
// @Security AdminToken
// @Accept json
// @Produce json
// @Param body body map[string]any true "Notification data"
// @Success 201 {object} response.DataResponse{data=object{id=string}} "Notification created"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router /api/v2/admin/notifications [post]
func (h *Handler) CreateNotification(w http.ResponseWriter, r *http.Request) {
	var req map[string]any
	if !decodeJSON(w, r, &req) {
		return
	}

	userID, err := uuid.Parse(str(req, "user_id"))
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("A valid user_id is required.", r.URL.Path))
		return
	}

	nType := str(req, "type")
	validTypes := map[string]bool{
		"event_nearby":         true,
		"interest_received":    true,
		"match_confirmed":      true,
		"social_like":          true,
		"event_expiring":       true,
		"badge_earned":         true,
		"match_completed":      true,
		"interest_accepted":    true,
		"connection_request":   true,
		"connection_confirmed": true,
		"chat_message":         true,
		"booking_confirmed":    true,
		"timeline_risk":        true,
	}
	if !validTypes[nType] {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Invalid notification type.", r.URL.Path))
		return
	}

	id := uuid.New()
	_, err = h.pool.Exec(r.Context(), `
		INSERT INTO notifications (id, user_id, type, title, body, read)
		VALUES ($1, $2, $3::notification_type, $4, $5, $6)
	`, id, userID, nType, str(req, "title"), str(req, "body"), boolVal(req, "read"))
	if err != nil {
		h.internal(w, r, "create notification", err)
		return
	}
	response.WriteData(w, http.StatusCreated, map[string]string{"id": id.String()})
}

// UpdateNotification updates an existing notification.
// @Summary Update notification
// @Description Update fields of a notification.
// @Tags admin
// @Security AdminToken
// @Accept json
// @Produce json
// @Param id path string true "Notification ID"
// @Param body body map[string]any true "Notification data"
// @Success 200 {object} response.DataResponse{data=object{id=string}} "Notification updated"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router /api/v2/admin/notifications/{id} [put]
func (h *Handler) UpdateNotification(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	var req map[string]any
	if !decodeJSON(w, r, &req) {
		return
	}

	userID, err := uuid.Parse(str(req, "user_id"))
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("A valid user_id is required.", r.URL.Path))
		return
	}

	nType := str(req, "type")
	validTypes := map[string]bool{
		"event_nearby":         true,
		"interest_received":    true,
		"match_confirmed":      true,
		"social_like":          true,
		"event_expiring":       true,
		"badge_earned":         true,
		"match_completed":      true,
		"interest_accepted":    true,
		"connection_request":   true,
		"connection_confirmed": true,
		"chat_message":         true,
		"booking_confirmed":    true,
		"timeline_risk":        true,
	}
	if !validTypes[nType] {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Invalid notification type.", r.URL.Path))
		return
	}

	tag, err := h.pool.Exec(r.Context(), `
		UPDATE notifications
		SET user_id = $2, type = $3::notification_type, title = $4, body = $5, read = $6
		WHERE id = $1
	`, id, userID, nType, str(req, "title"), str(req, "body"), boolVal(req, "read"))
	if err != nil {
		h.internal(w, r, "update notification", err)
		return
	}
	if tag.RowsAffected() == 0 {
		apierrors.WriteProblemDetail(w, apierrors.NotFound("Notification not found.", r.URL.Path))
		return
	}
	response.WriteData(w, http.StatusOK, map[string]string{"id": id.String()})
}

// DeleteNotification deletes a notification.
// @Summary Delete notification
// @Description Deletes a notification from the database.
// @Tags admin
// @Security AdminToken
// @Param id path string true "Notification ID"
// @Success 204 "No Content"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router /api/v2/admin/notifications/{id} [delete]
func (h *Handler) DeleteNotification(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	tag, err := h.pool.Exec(r.Context(), `DELETE FROM notifications WHERE id = $1`, id)
	if err != nil {
		h.internal(w, r, "delete notification", err)
		return
	}
	if tag.RowsAffected() == 0 {
		apierrors.WriteProblemDetail(w, apierrors.NotFound("Notification not found.", r.URL.Path))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// -- Conversations (admin view — all chat conversations) --

// ListConversations returns all conversations.
// @Summary List conversations
// @Description Returns all chat conversations with metadata.
// @Tags admin
// @Security AdminToken
// @Produce json
// @Param limit query int false "Limit"
// @Success 200 {object} response.DataResponse{data=[]map[string]any} "List of conversations"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router /api/v2/admin/conversations [get]
func (h *Handler) ListConversations(w http.ResponseWriter, r *http.Request) {
	limit := queryInt64(r, "limit", 200)
	rows, err := h.pool.Query(r.Context(), `
		SELECT c.id, c.type::text, c.created_at, c.updated_at,
		       COALESCE(last_msg.content, ''),
		       COALESCE(c.last_message_at, 0),
		       (SELECT COUNT(*) FROM messages m WHERE m.conversation_id = c.id),
		       COALESCE(NULLIF(CONCAT_WS(', ',
		           NULLIF(BTRIM(CONCAT_WS(' ', user_one.first_name, user_one.last_name)), ''),
		           NULLIF(BTRIM(CONCAT_WS(' ', user_two.first_name, user_two.last_name)), '')
		       ), ''), '')
		FROM conversations c
		LEFT JOIN LATERAL (
			SELECT m.content
			FROM messages m
			WHERE m.conversation_id = c.id
			ORDER BY m.created_at DESC, m.id DESC
			LIMIT 1
		) last_msg ON TRUE
		LEFT JOIN users user_one ON user_one.id = c.user_one_id
		LEFT JOIN users user_two ON user_two.id = c.user_two_id
		ORDER BY c.updated_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		h.internal(w, r, "list conversations", err)
		return
	}
	defer rows.Close()

	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, cType, lastMsg, participants string
		var createdAt, updatedAt, lastMsgAt, msgCount int64
		if err := rows.Scan(&id, &cType, &createdAt, &updatedAt, &lastMsg, &lastMsgAt, &msgCount, &participants); err != nil {
			h.internal(w, r, "scan conversations", err)
			return
		}
		items = append(items, map[string]any{
			"id": id, "type": cType,
			"created_at": createdAt, "updated_at": updatedAt,
			"last_message": lastMsg, "last_message_at": lastMsgAt,
			"message_count": msgCount,
			"participants":  participants,
		})
	}
	response.WriteData(w, http.StatusOK, items)
}

// CreateConversation creates a new conversation.
// @Summary Create conversation
// @Description Manually create a new chat conversation.
// @Tags admin
// @Security AdminToken
// @Accept json
// @Produce json
// @Param body body map[string]any true "Conversation data"
// @Success 201 {object} response.DataResponse{data=object{id=string}} "Conversation created"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router /api/v2/admin/conversations [post]
func (h *Handler) CreateConversation(w http.ResponseWriter, r *http.Request) {
	var req map[string]any
	if !decodeJSON(w, r, &req) {
		return
	}
	id := uuid.New()
	_, err := h.pool.Exec(r.Context(), `
		INSERT INTO conversations (id, type)
		VALUES ($1, $2::conversation_type)
	`, id, str(req, "type"))
	if err != nil {
		h.internal(w, r, "create conversation", err)
		return
	}
	response.WriteData(w, http.StatusCreated, map[string]string{"id": id.String()})
}

// UpdateConversation updates an existing conversation.
// @Summary Update conversation
// @Description Update fields of a conversation.
// @Tags admin
// @Security AdminToken
// @Accept json
// @Produce json
// @Param id path string true "Conversation ID"
// @Param body body map[string]any true "Conversation data"
// @Success 200 {object} response.DataResponse{data=object{id=string}} "Conversation updated"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router /api/v2/admin/conversations/{id} [put]
func (h *Handler) UpdateConversation(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	var req map[string]any
	if !decodeJSON(w, r, &req) {
		return
	}
	tag, err := h.pool.Exec(r.Context(), `
		UPDATE conversations
		SET type = $2::conversation_type, updated_at = (EXTRACT(EPOCH FROM NOW()))::BIGINT
		WHERE id = $1
	`, id, str(req, "type"))
	if err != nil {
		h.internal(w, r, "update conversation", err)
		return
	}
	if tag.RowsAffected() == 0 {
		apierrors.WriteProblemDetail(w, apierrors.NotFound("Conversation not found.", r.URL.Path))
		return
	}
	response.WriteData(w, http.StatusOK, map[string]string{"id": id.String()})
}

// DeleteConversation deletes a conversation.
// @Summary Delete conversation
// @Description Deletes a conversation from the database.
// @Tags admin
// @Security AdminToken
// @Param id path string true "Conversation ID"
// @Success 204 "No Content"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router /api/v2/admin/conversations/{id} [delete]
func (h *Handler) DeleteConversation(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	tag, err := h.pool.Exec(r.Context(), `DELETE FROM conversations WHERE id = $1`, id)
	if err != nil {
		h.internal(w, r, "delete conversation", err)
		return
	}
	if tag.RowsAffected() == 0 {
		apierrors.WriteProblemDetail(w, apierrors.NotFound("Conversation not found.", r.URL.Path))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// -- Bookings / Matches (admin view — all matches across all users) --

// ListBookings returns all bookings (matches).
// @Summary List bookings
// @Description Returns all event-pandit matches with details.
// @Tags admin
// @Security AdminToken
// @Produce json
// @Param limit query int false "Limit"
// @Success 200 {object} response.DataResponse{data=[]map[string]any} "List of bookings"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router /api/v2/admin/bookings [get]
func (h *Handler) ListBookings(w http.ResponseWriter, r *http.Request) {
	limit := queryInt64(r, "limit", 500)
	rows, err := h.pool.Query(r.Context(), `
		SELECT m.id, m.event_id, m.pandit_id, m.status::text,
		       m.created_at, m.updated_at,
		       COALESCE(e.ceremony_type::text, ''),
		       COALESCE(e.address, ''),
		       COALESCE(e.event_date, 0),
		       COALESCE(u_y.first_name || ' ' || u_y.last_name, '') AS yajman_name,
		       COALESCE(u_p.first_name || ' ' || u_p.last_name, '') AS pandit_name
		FROM matches m
		LEFT JOIN events e ON e.id = m.event_id
		LEFT JOIN users u_y ON u_y.id = e.yajman_id
		LEFT JOIN users u_p ON u_p.id = m.pandit_id
		ORDER BY m.updated_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		h.internal(w, r, "list bookings", err)
		return
	}
	defer rows.Close()

	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, eventID, panditID, status, ceremony, address, yajmanName, panditName string
		var createdAt, updatedAt, eventDate int64
		if err := rows.Scan(&id, &eventID, &panditID, &status,
			&createdAt, &updatedAt,
			&ceremony, &address, &eventDate,
			&yajmanName, &panditName); err != nil {
			h.internal(w, r, "scan bookings", err)
			return
		}
		items = append(items, map[string]any{
			"id": id, "event_id": eventID, "pandit_id": panditID,
			"status": status, "ceremony": ceremony, "address": address,
			"event_date":  eventDate,
			"yajman_name": yajmanName, "pandit_name": panditName,
			"created_at": createdAt, "updated_at": updatedAt,
		})
	}
	response.WriteData(w, http.StatusOK, items)
}

// UpdateBooking updates an existing booking.
// @Summary Update booking
// @Description Update fields of a booking (match).
// @Tags admin
// @Security AdminToken
// @Accept json
// @Produce json
// @Param id path string true "Booking ID"
// @Param body body map[string]any true "Booking data"
// @Success 200 {object} response.DataResponse{data=object{id=string,status=string}} "Booking updated"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router /api/v2/admin/bookings/{id} [put]
func (h *Handler) UpdateBooking(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	var req map[string]any
	if !decodeJSON(w, r, &req) {
		return
	}

	eventID := str(req, "event_id")
	panditID := str(req, "pandit_id")
	status := str(req, "status")

	if status != "" {
		switch status {
		case "created", "matched", "active", "completed", "cancelled":
		default:
			apierrors.WriteProblemDetail(w, apierrors.BadRequest("Invalid booking status.", r.URL.Path))
			return
		}
	}

	tag, err := h.pool.Exec(r.Context(), `
		UPDATE matches
		SET event_id = COALESCE(NULLIF($2, '')::UUID, event_id),
		    pandit_id = COALESCE(NULLIF($3, '')::UUID, pandit_id),
		    status = COALESCE(NULLIF($4, '')::match_status, status),
		    updated_at = (EXTRACT(EPOCH FROM NOW()))::BIGINT
		WHERE id = $1
	`, id, eventID, panditID, status)
	if err != nil {
		h.internal(w, r, "update booking", err)
		return
	}
	if tag.RowsAffected() == 0 {
		apierrors.WriteProblemDetail(w, apierrors.NotFound("Booking not found.", r.URL.Path))
		return
	}

	eventStatus := map[string]string{
		"created":   "Pending",
		"matched":   "Booked",
		"active":    "Active",
		"completed": "Completed",
		"cancelled": "Cancelled",
	}[status]
	if eventStatus != "" {
		_, err = h.pool.Exec(r.Context(), `
			UPDATE events
			SET status = $2::event_status, updated_at = (EXTRACT(EPOCH FROM NOW()))::BIGINT
			WHERE id = (SELECT event_id FROM matches WHERE id = $1)
		`, id, eventStatus)
		if err != nil {
			h.internal(w, r, "sync booking event status", err)
			return
		}
	}

	response.WriteData(w, http.StatusOK, map[string]string{"id": id.String(), "status": status})
}

// CreateBooking creates a new booking.
// @Summary Create booking
// @Description Manually create a new booking (match).
// @Tags admin
// @Security AdminToken
// @Accept json
// @Produce json
// @Param body body map[string]any true "Booking data"
// @Success 201 {object} response.DataResponse{data=object{id=string}} "Booking created"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router /api/v2/admin/bookings [post]
func (h *Handler) CreateBooking(w http.ResponseWriter, r *http.Request) {
	var req map[string]any
	if !decodeJSON(w, r, &req) {
		return
	}
	id := uuid.New()
	_, err := h.pool.Exec(r.Context(), `
		INSERT INTO matches (id, event_id, pandit_id, status)
		VALUES ($1, $2, $3, $4::match_status)
	`, id, str(req, "event_id"), str(req, "pandit_id"), defaultString(str(req, "status"), "created"))
	if err != nil {
		h.internal(w, r, "create booking", err)
		return
	}
	response.WriteData(w, http.StatusCreated, map[string]string{"id": id.String()})
}

// DeleteBooking deletes a booking.
// @Summary Delete booking
// @Description Deletes a booking from the database.
// @Tags admin
// @Security AdminToken
// @Param id path string true "Booking ID"
// @Success 204 "No Content"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router /api/v2/admin/bookings/{id} [delete]
func (h *Handler) DeleteBooking(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	tag, err := h.pool.Exec(r.Context(), `DELETE FROM matches WHERE id = $1`, id)
	if err != nil {
		h.internal(w, r, "delete booking", err)
		return
	}
	if tag.RowsAffected() == 0 {
		apierrors.WriteProblemDetail(w, apierrors.NotFound("Booking not found.", r.URL.Path))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

const usersByDaySQL = `
	SELECT to_char(to_timestamp(created_at)::date, 'Mon DD') AS label, COUNT(*)::DOUBLE PRECISION AS value
	FROM users
	WHERE deleted_at IS NULL AND created_at >= EXTRACT(EPOCH FROM NOW() - INTERVAL '14 DAYS')::BIGINT
	GROUP BY to_timestamp(created_at)::date
	ORDER BY to_timestamp(created_at)::date ASC
`

const eventsByStatusSQL = `
	SELECT status::text AS label, COUNT(*)::DOUBLE PRECISION AS value
	FROM events
	WHERE deleted_at IS NULL
	GROUP BY status::text
	ORDER BY COUNT(*) DESC
`

const notificationsByDaySQL = `
	SELECT to_char(to_timestamp(created_at)::date, 'Mon DD') AS label, COUNT(*)::DOUBLE PRECISION AS value
	FROM notifications
	WHERE created_at >= EXTRACT(EPOCH FROM NOW() - INTERVAL '14 DAYS')::BIGINT
	GROUP BY to_timestamp(created_at)::date
	ORDER BY to_timestamp(created_at)::date ASC
`

func (h *Handler) serviceStatus(id, name, status, message string, latencyMS int64) map[string]any {
	return map[string]any{
		"id":         id,
		"name":       name,
		"status":     status,
		"message":    message,
		"latency_ms": latencyMS,
		"checked_at": time.Now().Unix(),
	}
}

func (h *Handler) scalarInt(ctx context.Context, query string, args ...any) int64 {
	var value int64
	if err := h.pool.QueryRow(ctx, query, args...).Scan(&value); err != nil {
		h.logger.Warn("admin monitoring scalar query failed", "query", query, "error", err)
		return 0
	}
	return value
}

func (h *Handler) querySeries(ctx context.Context, query string, args ...any) []map[string]any {
	rows, err := h.pool.Query(ctx, query, args...)
	if err != nil {
		h.logger.Warn("admin monitoring series query failed", "query", query, "error", err)
		return []map[string]any{}
	}
	defer rows.Close()

	items := make([]map[string]any, 0)
	for rows.Next() {
		var label string
		var value float64
		if err := rows.Scan(&label, &value); err != nil {
			h.logger.Warn("admin monitoring series scan failed", "error", err)
			return items
		}
		items = append(items, map[string]any{"label": label, "value": value})
	}
	return items
}

func (h *Handler) storageStats(ctx context.Context) []map[string]any {
	items := make([]map[string]any, 0, len(managedBuckets))
	if h.s3Client == nil || h.s3Client.RawClient() == nil {
		return items
	}

	client := h.s3Client.RawClient()
	for _, bucket := range managedBuckets {
		var totalObjects int64
		var totalSize int64
		objectsCh := client.ListObjects(ctx, bucket, minio.ListObjectsOptions{Recursive: true})
		for object := range objectsCh {
			if object.Err != nil {
				h.logger.Warn("admin monitoring bucket stats failed", "bucket", bucket, "error", object.Err)
				totalObjects = 0
				totalSize = 0
				break
			}
			totalObjects++
			totalSize += object.Size
		}
		items = append(items, map[string]any{
			"label":         bucket,
			"value":         totalSize,
			"bucket":        bucket,
			"total_objects": totalObjects,
			"total_size":    totalSize,
		})
	}
	return items
}

func constantTimeEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dest any) bool {
	if err := json.NewDecoder(r.Body).Decode(dest); err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Invalid request body.", r.URL.Path))
		return false
	}
	return true
}

func parseUUIDParam(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Invalid id.", r.URL.Path))
		return uuid.Nil, false
	}
	return id, true
}

func (h *Handler) internal(w http.ResponseWriter, r *http.Request, action string, err error) {
	h.logger.Error("admin action failed", "action", action, "error", err)
	apierrors.WriteProblemDetail(w, apierrors.InternalError("Admin operation failed.", r.URL.Path))
}

func str(values map[string]any, key string) string {
	if v, ok := values[key].(string); ok {
		return v
	}
	return ""
}

func boolVal(values map[string]any, key string) bool {
	if v, ok := values[key].(bool); ok {
		return v
	}
	return false
}

func stringSlice(values map[string]any, key string) []string {
	switch v := values[key].(type) {
	case []string:
		return v
	case []any:
		items := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				items = append(items, strings.TrimSpace(s))
			}
		}
		return items
	case string:
		if strings.TrimSpace(v) == "" {
			return []string{}
		}
		parts := strings.Split(v, ",")
		items := make([]string, 0, len(parts))
		for _, part := range parts {
			if s := strings.TrimSpace(part); s != "" {
				items = append(items, s)
			}
		}
		return items
	default:
		return []string{}
	}
}

func int64Val(values map[string]any, key string) int64 {
	switch v := values[key].(type) {
	case float64:
		return int64(v)
	case string:
		n, _ := strconv.ParseInt(v, 10, 64)
		return n
	default:
		return 0
	}
}

func floatVal(values map[string]any, key string) float64 {
	if v := floatPtr(values, key); v != nil {
		return *v
	}
	return 0
}

func floatPtr(values map[string]any, key string) *float64 {
	switch v := values[key].(type) {
	case float64:
		return &v
	case string:
		if v == "" {
			return nil
		}
		n, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return nil
		}
		return &n
	default:
		return nil
	}
}

func queryInt64(r *http.Request, key string, fallback int64) int64 {
	v := r.URL.Query().Get(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return fallback
	}
	return n
}

func defaultString(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

var _ = pgx.ErrNoRows

// ============================================================
// Docker Container Management Endpoints
// ============================================================

// ListDockerContainers returns all Docker containers with their status, image, and resource usage.
// @Summary List Docker containers
// @Description Returns all running and stopped Docker containers with metadata.
// @Tags admin
// @Security AdminToken
// @Produce json
// @Success 200 {object} response.DataResponse{data=[]map[string]any}
// @Failure 401 {object} apierrors.ProblemDetail
func (h *Handler) ListDockerContainers(w http.ResponseWriter, r *http.Request) {
	// Use docker ps with custom format for structured output
	output, err := exec.Command("docker", "ps", "-a", "--format", "{{.ID}}\t{{.Names}}\t{{.Image}}\t{{.Status}}\t{{.State}}\t{{.Ports}}\t{{.CreatedAt}}\t{{.Size}}").Output()
	if err != nil {
		h.logger.Warn("docker ps failed", "error", err)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Failed to list Docker containers: "+err.Error(), r.URL.Path))
		return
	}

	containers := []map[string]any{}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 8)
		if len(parts) < 5 {
			continue
		}

		container := map[string]any{
			"id":         parts[0],
			"name":       parts[1],
			"image":      parts[2],
			"status":     parts[3],
			"state":      parts[4],
			"ports":      "",
			"created_at": "",
			"size":       "",
		}
		if len(parts) > 5 {
			container["ports"] = parts[5]
		}
		if len(parts) > 6 {
			container["created_at"] = parts[6]
		}
		if len(parts) > 7 {
			container["size"] = parts[7]
		}

		// Fetch resource stats (CPU, Memory) for running containers
		if parts[4] == "running" {
			statsOut, err := exec.Command("docker", "stats", "--no-stream", "--format", "{{.CPUPerc}}\t{{.MemUsage}}\t{{.MemPerc}}\t{{.NetIO}}\t{{.BlockIO}}", parts[0]).Output()
			if err == nil {
				statsParts := strings.SplitN(strings.TrimSpace(string(statsOut)), "\t", 5)
				if len(statsParts) >= 3 {
					container["cpu_percent"] = statsParts[0]
					container["mem_usage"] = statsParts[1]
					container["mem_percent"] = statsParts[2]
				}
				if len(statsParts) >= 5 {
					container["net_io"] = statsParts[3]
					container["block_io"] = statsParts[4]
				}
			}
		}

		containers = append(containers, container)
	}

	response.WriteData(w, http.StatusOK, containers)
}

// GetDockerContainerLogs returns recent logs for a specific Docker container.
// @Summary Get Docker container logs
// @Description Returns the last N lines of logs for a container.
// @Tags admin
// @Security AdminToken
// @Produce json
// @Param id path string true "Container ID or name"
// @Param tail query int false "Number of tail lines" default(200)
// @Param since query string false "Show logs since timestamp (e.g. 1h, 30m)" default("")
// @Success 200 {object} response.DataResponse{data=map[string]any}
// @Failure 400 {object} apierrors.ProblemDetail
// @Failure 401 {object} apierrors.ProblemDetail
func (h *Handler) GetDockerContainerLogs(w http.ResponseWriter, r *http.Request) {
	containerID := chi.URLParam(r, "id")
	if containerID == "" {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Container ID is required.", r.URL.Path))
		return
	}

	// Sanitize container ID to prevent command injection
	if !isValidDockerID(containerID) {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Invalid container ID format.", r.URL.Path))
		return
	}

	tail := r.URL.Query().Get("tail")
	if tail == "" {
		tail = "200"
	}
	since := r.URL.Query().Get("since")

	args := []string{"logs", "--tail", tail, "--timestamps"}
	if since != "" {
		args = append(args, "--since", since)
	}
	args = append(args, containerID)

	output, err := exec.Command("docker", args...).CombinedOutput()
	if err != nil {
		h.logger.Warn("docker logs failed", "container", containerID, "error", err)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Failed to fetch container logs: "+err.Error(), r.URL.Path))
		return
	}

	logLines := strings.Split(string(output), "\n")

	response.WriteData(w, http.StatusOK, map[string]any{
		"container_id": containerID,
		"tail":         tail,
		"since":        since,
		"lines":        logLines,
		"line_count":   len(logLines),
		"fetched_at":   time.Now().Unix(),
	})
}

// RestartDockerContainer restarts a specific Docker container.
// @Summary Restart Docker container
// @Description Restarts a container by its ID or name.
// @Tags admin
// @Security AdminToken
// @Produce json
// @Param id path string true "Container ID or name"
// @Success 200 {object} response.DataResponse{data=map[string]any}
// @Failure 400 {object} apierrors.ProblemDetail
// @Failure 401 {object} apierrors.ProblemDetail
func (h *Handler) RestartDockerContainer(w http.ResponseWriter, r *http.Request) {
	containerID := chi.URLParam(r, "id")
	if containerID == "" {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Container ID is required.", r.URL.Path))
		return
	}

	if !isValidDockerID(containerID) {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Invalid container ID format.", r.URL.Path))
		return
	}

	h.logger.Info("admin restarting docker container", "container", containerID)

	output, err := exec.Command("docker", "restart", containerID).CombinedOutput()
	if err != nil {
		h.logger.Error("docker restart failed", "container", containerID, "error", err, "output", string(output))
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Failed to restart container: "+err.Error(), r.URL.Path))
		return
	}

	response.WriteData(w, http.StatusOK, map[string]any{
		"container_id": containerID,
		"message":      "Container restarted successfully",
		"restarted_at": time.Now().Unix(),
	})
}

// isValidDockerID validates that a Docker container ID/name is safe for command execution.
func isValidDockerID(id string) bool {
	if len(id) == 0 || len(id) > 128 {
		return false
	}
	for _, c := range id {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-' || c == '.') {
			return false
		}
	}
	return true
}

func (h *Handler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	username, _ := r.Context().Value(adminUsernameKey).(string)
	if username == "" {
		apierrors.WriteProblemDetail(w, apierrors.Unauthorized("Unauthorized.", r.URL.Path))
		return
	}

	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Invalid request body.", r.URL.Path))
		return
	}

	if req.NewPassword == "" {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("New password cannot be empty.", r.URL.Path))
		return
	}

	// Verify old password
	var dbPassword string
	err := h.pool.QueryRow(r.Context(), "SELECT password FROM admin_users WHERE username = $1", username).Scan(&dbPassword)
	if err != nil {
		// Fallback for legacy admin
		if username == h.adminUsername {
			if req.OldPassword != h.adminPassword {
				apierrors.WriteProblemDetail(w, apierrors.Unauthorized("Incorrect old password.", r.URL.Path))
				return
			}
			hashed, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
			if err != nil {
				h.internal(w, r, "hash new password", err)
				return
			}
			_, err = h.pool.Exec(r.Context(), "INSERT INTO admin_users (username, password, role) VALUES ($1, $2, 'owner') ON CONFLICT (username) DO UPDATE SET password = EXCLUDED.password", username, string(hashed))
			if err != nil {
				h.internal(w, r, "insert admin user", err)
				return
			}
			response.WriteData(w, http.StatusOK, map[string]string{"status": "success"})
			return
		}
		apierrors.WriteProblemDetail(w, apierrors.NotFound("Admin user not found.", r.URL.Path))
		return
	}

	err = bcrypt.CompareHashAndPassword([]byte(dbPassword), []byte(req.OldPassword))
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.Unauthorized("Incorrect old password.", r.URL.Path))
		return
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		h.internal(w, r, "hash new password", err)
		return
	}

	_, err = h.pool.Exec(r.Context(), "UPDATE admin_users SET password = $1, updated_at = (EXTRACT(EPOCH FROM NOW()))::BIGINT WHERE username = $2", string(hashed), username)
	if err != nil {
		h.internal(w, r, "update password", err)
		return
	}

	response.WriteData(w, http.StatusOK, map[string]string{"status": "success"})
}

func (h *Handler) ListAdminUsers(w http.ResponseWriter, r *http.Request) {
	rows, err := h.pool.Query(r.Context(), "SELECT id, username, role, created_at, updated_at FROM admin_users ORDER BY username ASC")
	if err != nil {
		h.internal(w, r, "list admin users", err)
		return
	}
	defer rows.Close()

	users := make([]map[string]any, 0)
	for rows.Next() {
		var id string
		var username, role string
		var createdAt, updatedAt int64
		if err := rows.Scan(&id, &username, &role, &createdAt, &updatedAt); err != nil {
			h.internal(w, r, "scan admin user", err)
			return
		}
		users = append(users, map[string]any{
			"id":         id,
			"username":   username,
			"role":       role,
			"created_at": createdAt,
			"updated_at": updatedAt,
		})
	}
	response.WriteData(w, http.StatusOK, users)
}

func (h *Handler) CreateAdminUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Invalid request body.", r.URL.Path))
		return
	}

	if req.Username == "" || req.Password == "" {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Username and password cannot be empty.", r.URL.Path))
		return
	}

	role := req.Role
	if role != "owner" && role != "admin" {
		role = "admin"
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		h.internal(w, r, "hash password", err)
		return
	}

	id := uuid.New()
	_, err = h.pool.Exec(r.Context(), "INSERT INTO admin_users (id, username, password, role) VALUES ($1, $2, $3, $4)", id, req.Username, string(hashed), role)
	if err != nil {
		h.internal(w, r, "create admin user", err)
		return
	}

	response.WriteData(w, http.StatusCreated, map[string]string{
		"id":       id.String(),
		"username": req.Username,
		"role":     role,
	})
}

func (h *Handler) DeleteAdminUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Admin user ID required.", r.URL.Path))
		return
	}

	currentUsername, _ := r.Context().Value(adminUsernameKey).(string)
	var targetUsername string
	err := h.pool.QueryRow(r.Context(), "SELECT username FROM admin_users WHERE id = $1", id).Scan(&targetUsername)
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.NotFound("Admin user not found.", r.URL.Path))
		return
	}

	if targetUsername == currentUsername {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Cannot delete your own account.", r.URL.Path))
		return
	}

	_, err = h.pool.Exec(r.Context(), "DELETE FROM admin_users WHERE id = $1", id)
	if err != nil {
		h.internal(w, r, "delete admin user", err)
		return
	}

	response.WriteData(w, http.StatusOK, map[string]string{"status": "success"})
}
