package main

// @title Medha API V2
// @version 2.0.0
// @description Backend API for the Medha platform. V2 Authentication (OTP-based), User Profiles, Events, Matching, and more.
// @termsOfService https://swagger.io/terms/

// @contact.name Medha Backend Team
// @contact.url https://medha.app
// @contact.email support@medha.app

// @license.name Apache 2.0
// @license.url https://www.apache.org/licenses/LICENSE-2.0.html

// @BasePath /

// @tag.name auth-v2
// @tag.name user-v2
// @tag.name event-v2
// @tag.name feed-v2
// @tag.name interest-v2
// @tag.name location-v2
// @tag.name match-v2
// @tag.name messaging
// @tag.name notification-v2
// @tag.name panchanga-v2
// @tag.name pandit-v2
// @tag.name storage-v2
// @tag.name trust-v2
// @tag.name Connections
// @tag.name Events
// @tag.name Metadata
// @tag.name cities
// @tag.name admin

// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description Type "Bearer" followed by a space and JWT token.

// @securityDefinitions.apikey AdminToken
// @in header
// @name Authorization
// @description Type "Bearer" followed by a space and an admin JWT or break-glass token.

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata"

	"github.com/medha/backend/internal/server"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	adminhandler "github.com/medha/backend/internal/admin"
	"github.com/medha/backend/internal/admin/assignment"

	authdomain "github.com/medha/backend/internal/auth/domain"
	authhandler "github.com/medha/backend/internal/auth/handler"
	authrepo "github.com/medha/backend/internal/auth/repository"
	authservice "github.com/medha/backend/internal/auth/service"
	citydomain "github.com/medha/backend/internal/city/domain"
	cityhandler "github.com/medha/backend/internal/city/handler"
	cityrepo "github.com/medha/backend/internal/city/repository"
	cityservice "github.com/medha/backend/internal/city/service"
	"github.com/medha/backend/internal/config"
	eventhandler "github.com/medha/backend/internal/event/handler"
	eventrepo "github.com/medha/backend/internal/event/repository"
	eventservice "github.com/medha/backend/internal/event/service"
	festivallogos "github.com/medha/backend/internal/festivallogos"
	"github.com/medha/backend/internal/infra/cache"
	"github.com/medha/backend/internal/infra/database"
	"github.com/medha/backend/internal/infra/storage"
	interesthandler "github.com/medha/backend/internal/interest/handler"
	interestrepo "github.com/medha/backend/internal/interest/repository"
	interestservice "github.com/medha/backend/internal/interest/service"
	locationhandler "github.com/medha/backend/internal/location/handler"
	locationservice "github.com/medha/backend/internal/location/service"
	matchhandler "github.com/medha/backend/internal/matching/handler"
	matchrepo "github.com/medha/backend/internal/matching/repository"
	matchservice "github.com/medha/backend/internal/matching/service"
	messaginghandler "github.com/medha/backend/internal/messaging/handler"
	messagingrepo "github.com/medha/backend/internal/messaging/repository"
	messagingservice "github.com/medha/backend/internal/messaging/service"
	notifhandler "github.com/medha/backend/internal/notification/handler"
	notifrepo "github.com/medha/backend/internal/notification/repository"
	notifservice "github.com/medha/backend/internal/notification/service"
	panchangahandler "github.com/medha/backend/internal/panchanga/handler"
	panchangarepo "github.com/medha/backend/internal/panchanga/repository"
	panchangaservice "github.com/medha/backend/internal/panchanga/service"
	msgcentral "github.com/medha/backend/internal/platform/messagecentral"
	"github.com/medha/backend/internal/platform/worker"
	"github.com/medha/backend/internal/platform/ws"
	pushservice "github.com/medha/backend/internal/push/service"
	socialhandler "github.com/medha/backend/internal/social/handler"
	socialrepo "github.com/medha/backend/internal/social/repository"
	socialservice "github.com/medha/backend/internal/social/service"
	storagehandler "github.com/medha/backend/internal/storage/handler"
	storyhandler "github.com/medha/backend/internal/story/handler"
	storyrepo "github.com/medha/backend/internal/story/repository"
	storyservice "github.com/medha/backend/internal/story/service"
	systempublisherrepo "github.com/medha/backend/internal/systempublisher/repository"
	systempublisherservice "github.com/medha/backend/internal/systempublisher/service"
	userdomain "github.com/medha/backend/internal/user/domain"
	userhandler "github.com/medha/backend/internal/user/handler"
	userrepo "github.com/medha/backend/internal/user/repository"
	userservice "github.com/medha/backend/internal/user/service"
	"github.com/medha/backend/migrations"
)

func main() {
	// Parse CLI flags
	migrateFlag := flag.Bool("migrate", false, "run database migrations and exit")
	flag.Parse()

	// Initialize structured logging
	logger := setupLogger()

	// Migration is a least-privilege one-shot mode. It intentionally loads only
	// DATABASE_URL so the init container does not need runtime application secrets.
	if *migrateFlag {
		migrationCfg, err := config.LoadMigration()
		if err != nil {
			logger.Error("failed to load migration configuration", "error", err)
			os.Exit(1)
		}
		logger.Info("running database migrations...")
		if err := migrations.Run("postgres", migrationCfg.DatabaseURL); err != nil {
			logger.Error("migration failed", "error", err)
			os.Exit(1)
		}
		logger.Info("migrations completed successfully")
		return
	}

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		logger.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}

	logger.Info("configuration loaded",
		"environment", cfg.Environment,
		"port", cfg.ServerPort,
	)

	ctx := context.Background()

	// Initialize database pool with retry logic (resilience against startup delays)
	var pool *pgxpool.Pool
	for i := 0; i < 5; i++ {
		pool, err = database.NewPool(ctx, cfg.DatabaseURL, logger)
		if err == nil {
			break
		}
		if cfg.IsDevelopment() {
			logger.Warn("database connection failed — running without DB (dev mode)",
				"attempt", i+1,
				"error", err,
			)
			break // Don't block in dev mode if DB is truly missing
		}
		logger.Error("failed to connect to database, retrying in 2s...", "attempt", i+1, "error", err)
		time.Sleep(2 * time.Second)
	}

	if err != nil && !cfg.IsDevelopment() {
		logger.Error("terminal failure: could not connect to database after 5 attempts")
		os.Exit(1)
	}

	if pool != nil {
		defer pool.Close()
	}

	// Initialize Redis with retry logic
	var redisClient *redis.Client
	for i := 0; i < 3; i++ {
		redisClient, err = cache.NewRedisClient(ctx, cfg.RedisURL, logger)
		if err == nil {
			break
		}
		if cfg.IsDevelopment() {
			logger.Warn("redis connection failed (dev mode)", "attempt", i+1, "error", err)
			redisClient = nil
			break
		}
		logger.Error("failed to connect to redis, retrying in 2s...", "attempt", i+1, "error", err)
		time.Sleep(2 * time.Second)
	}

	if err != nil && !cfg.IsDevelopment() {
		logger.Error("terminal failure: could not connect to redis after 3 attempts")
		os.Exit(1)
	}

	// Initialize WebSocket components
	hub := ws.NewHub(logger)
	go hub.Run()

	// Initialize shared worker pool for async background tasks
	var workerPool *worker.Pool
	if pool != nil {
		workerPool = worker.NewPool(20, 1000, logger) // 20 workers, 1000 queue depth
		defer workerPool.Stop()
	}

	var notifPublisher notifservice.NotificationPublisher
	if redisClient != nil {
		notifPublisher = ws.NewRedisPublisher(redisClient)

		subscriber := ws.NewRedisSubscriber(redisClient, hub, logger)
		if err := subscriber.StartGlobalSubscriber(ctx); err != nil {
			logger.Error("failed to start global redis subscriber", "error", err)
		}
	}

	// Initialize repositories (nil-safe — will be nil if pool is nil)
	var userRepository userdomain.UserRepository
	var panditRepository userdomain.PanditProfileRepository
	var tokenRepository authdomain.TokenRepository
	var cityRepository citydomain.CityRepository
	if pool != nil {
		userRepository = userrepo.NewPostgresUserRepository(pool)
		panditRepository = userrepo.NewPostgresPanditProfileRepository(pool)
		tokenRepository = authrepo.NewPostgresTokenRepository(pool)
		cityRepository = cityrepo.NewPostgresCityRepository(pool)
	}

	// JWT configuration
	jwtPrivateKeyPath := cfg.JWTPrivateKeyPath
	jwtPublicKeyPath := cfg.JWTPublicKeyPath
	jwtExpiryMinutes := cfg.JWTExpiryMinutes

	// Initialize auth service (requires DB + JWT keys — provides ValidateJWT for middleware)
	var authSvc *authservice.AuthService
	if pool != nil && jwtPrivateKeyPath != "" && jwtPublicKeyPath != "" {
		authSvc, err = authservice.NewAuthService(
			userRepository,
			tokenRepository,
			jwtPrivateKeyPath,
			jwtPublicKeyPath,
			jwtExpiryMinutes,
			logger,
		)
		if err != nil {
			logger.Error("failed to initialize auth service", "error", err)
			os.Exit(1)
		}
	} else {
		if pool == nil {
			logger.Warn("auth service disabled — no database connection")
		} else {
			logger.Warn("auth service disabled — JWT key paths not configured")
		}
	}

	// Initialize location service (MapMyIndia — always available, no DB needed)
	mapSvc := locationservice.NewMapMyIndiaService(
		logger,
	)

	// Initialize services (nil-safe)
	var userSvc *userservice.UserService
	if userRepository != nil {
		userSvc = userservice.NewUserService(userRepository, panditRepository, mapSvc, logger)
	}

	var citySvc *cityservice.CityService
	if cityRepository != nil {
		citySvc = cityservice.NewCityService(cityRepository, mapSvc, logger)
	}

	// Initialize S3-compatible object storage client (optional in development)
	var s3Client *storage.S3Client
	if cfg.S3AccessKey != "" && cfg.S3SecretKey != "" {
		s3Client, err = storage.NewS3Client(
			ctx,
			cfg.S3Endpoint,
			cfg.S3PublicEndpoint,
			cfg.S3AccessKey,
			cfg.S3SecretKey,
			false, // useSSL — false for dev
			logger,
		)
		if err != nil {
			if cfg.IsDevelopment() {
				logger.Warn("s3 client initialization failed — photo upload disabled (dev mode)", "error", err)
			} else {
				logger.Error("failed to initialize s3 client", "error", err)
				os.Exit(1)
			}
		}
	} else {
		logger.Warn("s3 storage not configured — photo upload disabled")
	}

	if s3Client != nil && userSvc != nil {
		userSvc.SetS3Client(s3Client)
	}

	// Initialize handlers
	storageHdlr := storagehandler.NewStorageHandler(s3Client, userSvc, logger)

	// Initialize pandit bounded context
	var panditHdlr *userhandler.PanditHandler
	var serviceCitiesHdlr *userhandler.ServiceCitiesHandler
	if panditRepository != nil && userRepository != nil && cityRepository != nil && pool != nil {
		panditSvc := userservice.NewPanditService(panditRepository, userRepository, cityRepository, pool, redisClient, logger)
		panditHdlr = userhandler.NewPanditHandler(panditSvc, logger)
		serviceCitiesHdlr = userhandler.NewServiceCitiesHandler(panditSvc, logger)
	}

	var cityHdlr *cityhandler.CityHandler
	if citySvc != nil {
		cityHdlr = cityhandler.NewCityHandler(citySvc, logger)
	}

	// Initialize event bounded context
	var eventHdlr *eventhandler.EventHandler
	var eventRepository *eventrepo.PostgresEventRepository
	var eventSvc *eventservice.EventService
	if pool != nil {
		eventRepository = eventrepo.NewPostgresEventRepository(pool)
		eventSvc = eventservice.NewEventService(pool, eventRepository, logger)
		eventHdlr = eventhandler.NewEventHandler(eventSvc, s3Client, logger)
	}

	// Initialize matching bounded context
	var matchHdlr *matchhandler.MatchHandler
	var matchSvc *matchservice.MatchService
	if pool != nil && eventRepository != nil {
		matchRepo := matchrepo.NewPostgresMatchRepository(pool)
		matchSvc = matchservice.NewMatchService(pool, matchRepo, eventRepository, workerPool, logger)
		matchHdlr = matchhandler.NewMatchHandler(matchSvc, logger)
	}

	// Initialize interest bounded context (depends on matching for auto-match on accept)
	var interestHdlr *interesthandler.InterestHandler
	var interestSvc *interestservice.InterestService
	var interestRepo *interestrepo.PostgresInterestRepository
	if pool != nil && eventRepository != nil {
		interestRepo = interestrepo.NewPostgresInterestRepository(pool)
		interestSvc = interestservice.NewInterestService(pool, interestRepo, eventRepository, matchSvc, userRepository, logger)
		interestHdlr = interesthandler.NewInterestHandler(interestSvc, logger)
		if eventSvc != nil {
			eventSvc.SetInterestRepository(interestRepo)
		}
	}

	// Start event auto-completion background worker (runs every hour)
	// Marks past-date events (Active/Pending/Booked) → Completed and cascades to matches.
	if pool != nil && eventRepository != nil && matchSvc != nil {
		matchCompletionRepo := matchrepo.NewPostgresMatchRepository(pool)
		completionWorker := worker.NewEventCompletionWorker(
			eventRepository,
			matchCompletionRepo,
			1*time.Hour,
			logger,
		)
		if interestRepo != nil {
			completionWorker.SetInterestRepository(interestRepo)
		}
		go completionWorker.Start(context.Background())
		logger.Info("event completion worker started")
	}

	// Initialize social bounded context (trust only — feed uses FeedHandlerV2)
	var trustHdlr *socialhandler.TrustHandler
	if pool != nil {
		// Trust (feedback + badges)
		trustRepo := socialrepo.NewPostgresTrustRepository(pool)
		matchRepository := matchrepo.NewPostgresMatchRepository(pool)
		trustSvc := socialservice.NewTrustService(trustRepo, matchRepository, logger)
		trustHdlr = socialhandler.NewTrustHandler(trustSvc, logger)
	}

	// Initialize V2 auth service + handlers
	var authHandlerV2 *authhandler.AuthHandlerV2
	var profileHandlerV2 *userhandler.ProfileHandlerV2
	var locationHandlerV2 *userhandler.LocationHandlerV2
	if authSvc != nil && userRepository != nil && tokenRepository != nil {
		v2mcClient := msgcentral.NewClient(
			cfg.MessageCentralCustomerID,
			cfg.MessageCentralKeyBase64,
			cfg.MessageCentralEmail,
			cfg.MessageCentralBaseURL,
			redisClient,
			logger,
		)
		// Pass the RSA key paths dynamically to authSvcV2
		if cfg.JWTPrivateKeyPath != "" && cfg.JWTPublicKeyPath != "" {
			authSvcV2 := authservice.NewAuthServiceV2(
				userRepository,
				tokenRepository,
				v2mcClient,
				cfg.JWTPrivateKeyPath,
				cfg.JWTPublicKeyPath,
				cfg.JWTExpiryMinutes,
				logger,
			)
			authHandlerV2 = authhandler.NewAuthHandlerV2(authSvcV2, redisClient, logger, cfg.DevPanditPhone, cfg.DevYajmanPhone)
			if userSvc != nil {
				profileHandlerV2 = userhandler.NewProfileHandlerV2(userSvc, cfg, logger)
				locationHandlerV2 = userhandler.NewLocationHandlerV2(userSvc, logger)
			}
		}
	}

	// Initialize V2 feed handler (reuses existing PostService, no new dependencies)
	var feedHandlerV2 *socialhandler.FeedHandlerV2
	if pool != nil {
		postRepo := socialrepo.NewPostgresPostRepository(pool)
		postSvcV2 := socialservice.NewPostService(postRepo, logger)
		feedHandlerV2 = socialhandler.NewFeedHandlerV2(postSvcV2, s3Client, logger)
	}

	// Initialize system publishers + official stories bounded context
	var storyHdlr *storyhandler.Handler
	var storySvc *storyservice.StoryService
	if pool != nil && userRepository != nil {
		publisherRepo := systempublisherrepo.NewPostgresSystemPublisherRepository(pool)
		publisherSvc := systempublisherservice.NewSystemPublisherService(publisherRepo)
		storyRepo := storyrepo.NewPostgresStoryRepository(pool)
		storyViewRepo := storyrepo.NewPostgresStoryViewRepository(pool)
		var mediaURLPrefixes []string
		if s3Client != nil {
			mediaURLPrefixes = []string{s3Client.PublicBaseURL() + "/"}
		}
		storySvc = storyservice.NewStoryService(storyRepo, storyViewRepo, userRepository, publisherSvc, mediaURLPrefixes, logger)
		storyHdlr = storyhandler.NewHandler(storySvc, logger)
	}

	// Initialize notification bounded context
	var notifHdlr *notifhandler.NotificationHandler
	var riskHdlr *notifhandler.RiskHandler
	var fcmHdlr *notifhandler.FCMPlaceholderHandler
	var notifSvc *notifservice.NotificationService
	if pool != nil {
		notifRepo := notifrepo.NewPostgresNotificationRepository(pool)
		pushSvc := pushservice.NewPushService(notifRepo, cfg, logger)
		notifSvc = notifservice.NewNotificationService(notifRepo, notifPublisher, pushSvc, workerPool, logger)
		notifHdlr = notifhandler.NewNotificationHandler(notifSvc, logger)
		fcmHdlr = notifhandler.NewFCMPlaceholderHandler(notifSvc)

		// Wire notification service into other contexts
		if eventSvc != nil && userRepository != nil {
			eventSvc.SetNotificationDependencies(notifSvc, userRepository, cfg.NearbyEventRadiusKM)
		}
		if matchSvc != nil {
			matchSvc.SetNotificationService(notifSvc)
		}
		if interestSvc != nil {
			interestSvc.SetNotificationService(notifSvc)
		}

		// Initialize Risk Checker background job
		if cfg.RiskCheckEnabled {
			riskChecker := notifservice.NewRiskChecker(notifRepo, notifSvc, logger)
			riskHdlr = notifhandler.NewRiskHandler(riskChecker, cfg.AdminAPIToken)

			go func() {
				ticker := time.NewTicker(1 * time.Hour) // Run hourly
				defer ticker.Stop()

				// Optional: Run immediately on startup
				if err := riskChecker.Run(context.Background()); err != nil {
					logger.Error("initial risk check failed", "error", err)
				}

				for range ticker.C {
					if err := riskChecker.Run(context.Background()); err != nil {
						logger.Error("scheduled risk check failed", "error", err)
					}
				}
			}()
		}
	}

	// Initialize panchanga bounded context (public, read-only)
	var panchangaHdlr *panchangahandler.PanchangaHandler
	if pool != nil {
		panchangaRepo := panchangarepo.NewPostgresPanchangaRepository(pool)
		panchangaSvc := panchangaservice.NewPanchangaService(panchangaRepo)
		panchangaHdlr = panchangahandler.NewPanchangaHandler(panchangaSvc)
	}

	locHdlr := locationhandler.NewLocationHandler(mapSvc, logger)

	// Initialize messaging bounded context (conversations + chat)
	var msgHdlr *messaginghandler.MessagingHandler
	var msgSvc *messagingservice.MessagingService
	if pool != nil {
		msgRepo := messagingrepo.NewPostgresMessagingRepository(pool)

		// Build the chat publisher (uses existing Redis publisher)
		var chatPub messagingservice.ChatPublisher
		if redisPub, ok := notifPublisher.(*ws.RedisPublisher); ok {
			chatPub = redisPub
		}

		msgSvc = messagingservice.NewMessagingService(msgRepo, chatPub, notifSvc, logger)
		msgHdlr = messaginghandler.NewMessagingHandler(msgSvc, logger)

		// Wire the WS adapter so the Hub can handle inbound chat messages
		adapter := ws.NewMessagingAdapter(msgSvc)
		hub.SetMessageHandler(adapter)

		// Wire messaging into match service for auto-creating conversations on match accept
		if matchSvc != nil {
			matchSvc.SetMessagingService(msgSvc)
		}

		// Wire messaging into interest service for auto-creating conversations on First Flow
		// (Pandit expresses interest → conversation workspace created immediately)
		if interestSvc != nil {
			interestSvc.SetMessagingService(msgSvc)
		}

		logger.Info("messaging bounded context initialized")
	}

	// Initialize direct connections bounded context (REMOVED)

	adminHdlr := adminhandler.NewHandler(pool, s3Client, cfg.AdminUsername, cfg.AdminPassword, cfg.AdminAPIToken, logger)
	adminHdlr.SetEncryptionKey(cfg.CredentialsEncryptionKey)
	adminHdlr.SetEmailConfig(cfg.ResendAPIKey, cfg.ResendFromDomain, cfg.AdminEmails, redisClient)
	if pool != nil {
		adminHdlr.SeedAdminUsers(context.Background())
		postRepo := socialrepo.NewPostgresPostRepository(pool)
		postSvc := socialservice.NewPostService(postRepo, logger)
		adminHdlr.SetPostService(postSvc)
	}
	if storySvc != nil {
		adminHdlr.SetStoryService(storySvc)
	}

	// Initialize assignment engine (auto-assigns Pandits on event creation)
	var assignEngine *assignment.Engine
	var leadHdlr *matchhandler.LeadHandler
	if pool != nil {
		assignEngine = assignment.NewEngine(pool, logger)
		if notifSvc != nil {
			assignEngine.SetNotificationService(notifSvc)
		}
		if eventSvc != nil {
			eventSvc.SetAssignmentEngine(assignEngine)
		}
		adminHdlr.SetAssignmentEngine(assignEngine)
		leadHdlr = matchhandler.NewLeadHandler(pool, assignEngine, logger)
		logger.Info("assignment engine initialized")

		// Start lead expiry background worker (sweeps every hour, expires leads older than 24 hours)
		expiryWorker := worker.NewLeadExpiryWorker(assignEngine, 1*time.Hour, 24*time.Hour, logger)
		go expiryWorker.Start(context.Background())
		logger.Info("lead expiry worker started")
	}

	// Festival logos bounded context
	var festivalLogosHdlr *festivallogos.Handler
	if pool != nil && s3Client != nil {
		festivalLogosRepo := festivallogos.NewRepository(pool)
		festivalLogosSvc, err := festivallogos.NewService(ctx, festivalLogosRepo, s3Client, logger)
		if err != nil {
			logger.Error("failed to initialize festival logos service", "error", err)
			os.Exit(1)
		}
		festivalLogosHdlr = festivallogos.NewHandler(festivalLogosSvc, logger)
		logger.Info("festival logos bounded context initialized")
	}

	// Create router (V2 only)
	r := server.SetupRouter(logger, cfg, pool, authSvc,
		panditHdlr, eventHdlr, interestHdlr, matchHdlr,
		trustHdlr, notifHdlr, locHdlr, storageHdlr,
		authHandlerV2,
		profileHandlerV2,
		locationHandlerV2,
		feedHandlerV2,
		cityHdlr,
		serviceCitiesHdlr,
		riskHdlr,
		fcmHdlr,
		panchangaHdlr,
		msgHdlr,
		adminHdlr,
		festivalLogosHdlr,
		leadHdlr,
		storyHdlr,
		hub,
		pool != nil,
		cfg.IsDevelopment(),
	)

	// Start server
	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.ServerPort),
		Handler:           r,
		ReadHeaderTimeout: 15 * time.Second,
		ReadTimeout:       5 * time.Minute,
		WriteTimeout:      5 * time.Minute,
		IdleTimeout:       60 * time.Second,
	}

	// Graceful shutdown
	go func() {
		logger.Info("server starting", "addr", srv.Addr,
			"swagger_ui", fmt.Sprintf("http://localhost:%d/api/docs", cfg.ServerPort),
			"health", fmt.Sprintf("http://localhost:%d/health", cfg.ServerPort),
		)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	logger.Info("shutdown signal received", "signal", sig.String())

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("server forced to shutdown", "error", err)
		os.Exit(1)
	}

	logger.Info("server stopped gracefully")
}

func setupLogger() *slog.Logger {
	env := os.Getenv("ENVIRONMENT")
	var handler slog.Handler
	if env == "production" {
		handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
	} else {
		handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})
	}
	logger := slog.New(handler)
	slog.SetDefault(logger)
	return logger
}
