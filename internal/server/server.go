package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	httpSwagger "github.com/swaggo/http-swagger/v2"

	swagger "github.com/medha/backend/api/openapi"
	adminhandler "github.com/medha/backend/internal/admin"
	authhandler "github.com/medha/backend/internal/auth/handler"
	authservice "github.com/medha/backend/internal/auth/service"
	cityhandler "github.com/medha/backend/internal/city/handler"
	"github.com/medha/backend/internal/config"
	eventhandler "github.com/medha/backend/internal/event/handler"
	festivallogos "github.com/medha/backend/internal/festivallogos"
	interesthandler "github.com/medha/backend/internal/interest/handler"
	locationhandler "github.com/medha/backend/internal/location/handler"
	matchhandler "github.com/medha/backend/internal/matching/handler"
	messaginghandler "github.com/medha/backend/internal/messaging/handler"
	notifhandler "github.com/medha/backend/internal/notification/handler"
	panchangahandler "github.com/medha/backend/internal/panchanga/handler"
	"github.com/medha/backend/internal/platform/ws"
	"github.com/medha/backend/internal/server/middleware"
	socialhandler "github.com/medha/backend/internal/social/handler"
	storagehandler "github.com/medha/backend/internal/storage/handler"
	storyhandler "github.com/medha/backend/internal/story/handler"
	userhandler "github.com/medha/backend/internal/user/handler"
	apierrors "github.com/medha/backend/pkg/errors"
)

func SetupRouter(
	logger *slog.Logger,
	cfg *config.Config,
	pool *pgxpool.Pool,
	authSvc *authservice.AuthService,
	panditHdlr *userhandler.PanditHandler,
	eventHdlr *eventhandler.EventHandler,
	interestHdlr *interesthandler.InterestHandler,
	matchHdlr *matchhandler.MatchHandler,
	trustHdlr *socialhandler.TrustHandler,
	notifHdlr *notifhandler.NotificationHandler,
	locHdlr *locationhandler.LocationHandler,
	storageHdlr *storagehandler.StorageHandler,
	authHandlerV2 *authhandler.AuthHandlerV2,
	profileHandlerV2 *userhandler.ProfileHandlerV2,
	locationHandlerV2 *userhandler.LocationHandlerV2,
	feedHandlerV2 *socialhandler.FeedHandlerV2,
	cityHdlr *cityhandler.CityHandler,
	serviceCitiesHdlr *userhandler.ServiceCitiesHandler,
	riskHdlr *notifhandler.RiskHandler,
	fcmHdlr *notifhandler.FCMPlaceholderHandler,
	panchangaHdlr *panchangahandler.PanchangaHandler,
	msgHdlr *messaginghandler.MessagingHandler,
	adminHdlr *adminhandler.Handler,
	festivalLogosHdlr *festivallogos.Handler,
	leadHdlr *matchhandler.LeadHandler,
	storyHdlr *storyhandler.Handler,
	hub *ws.Hub,
	dbConnected bool,
	isDev bool,
) *chi.Mux {
	r := chi.NewRouter()

	// Middleware chain: Recovery -> RequestID -> Logging -> CORS -> RateLimit
	r.Use(middleware.Recovery(logger))

	r.Use(middleware.RequestID)
	r.Use(middleware.BodyLimit)
	r.Use(chimiddleware.Compress(5, "application/json", "text/plain"))
	r.Use(middleware.Logging(logger, pool))
	r.Use(middleware.CORS(isDev))
	r.Use(middleware.RateLimit)

	// Swagger UI and OpenAPI spec — gated behind SwaggerEnabled so the raw
	// OpenAPI spec is not publicly exposed in production.
	if cfg.SwaggerEnabled {
		r.Get("/api/docs", swagger.UIHandler())
		r.Get("/api/docs/swagger.yaml", swagger.YamlHandler())
		r.Get("/api/docs/swagger.json", swagger.JSONHandler())
		r.Get("/api/docs/custom.css", swagger.CustomStylesHandler())

		r.Get("/swagger", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/swagger/index.html", http.StatusFound)
		})
		r.Mount("/swagger", httpSwagger.Handler(
			httpSwagger.URL("/api/docs/swagger.json"), // Point to our reliable embedded JSON
			httpSwagger.DefaultModelsExpandDepth(-1),
		))
	}

	// Health check — includes DB status
	// Registered for both GET and HEAD: Cloudflare (and other load balancers)
	// send HEAD probes every 15s; Chi does NOT auto-handle HEAD for GET routes,
	// so without this explicit registration HEAD returns 405 → origin marked unhealthy.
	healthHandler := func(w http.ResponseWriter, r *http.Request) {
		status := "ok"
		statusCode := http.StatusOK

		if pool != nil {
			if err := pool.Ping(r.Context()); err != nil {
				status = "failing (db ping failed)"
				statusCode = http.StatusServiceUnavailable
				logger.Error("health check failed", "error", err)
			}
		} else if !isDev {
			// In production, missing DB is a fatal health failure
			status = "failing (no db connection)"
			statusCode = http.StatusServiceUnavailable
		}

		w.WriteHeader(statusCode)
		if r.Method != http.MethodHead {
			_, _ = w.Write([]byte(fmt.Sprintf("%s-v2 (%s)", status, cfg.Version)))
		}
	}
	r.Get("/health", healthHandler)
	r.Head("/health", healthHandler)

	// WebSocket endpoint
	if hub != nil && authSvc != nil {
		r.Get("/ws", ws.ServeWS(hub, authSvc, logger, isDev))
	}

	// Internal routes
	if riskHdlr != nil {
		r.Post("/internal/notifications/risk-check", riskHdlr.CheckRisk)
	}

	// Public JWKS route
	if authHandlerV2 != nil {
		r.Get("/.well-known/jwks.json", authHandlerV2.GetJWKS)
	}

	// Android App Links
	r.Get("/.well-known/assetlinks.json", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		type androidTarget struct {
			Namespace              string   `json:"namespace"`
			PackageName            string   `json:"package_name"`
			Sha256CertFingerprints []string `json:"sha256_cert_fingerprints"`
		}
		type androidLink struct {
			Relation []string      `json:"relation"`
			Target   androidTarget `json:"target"`
		}
		data := []androidLink{
			{
				Relation: []string{"delegate_permission/common.handle_all_urls"},
				Target: androidTarget{
					Namespace:              "android_app",
					PackageName:            cfg.AndroidPackageName,
					Sha256CertFingerprints: []string{cfg.AndroidSHA256Fingerprint},
				},
			},
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(data)
	})

	// iOS Universal Links
	r.Get("/.well-known/apple-app-site-association", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		type iosDetails struct {
			AppID string   `json:"appID"`
			Paths []string `json:"paths"`
		}
		type iosApplinks struct {
			Apps    []string     `json:"apps"`
			Details []iosDetails `json:"details"`
		}
		type iosAssociation struct {
			Applinks iosApplinks `json:"applinks"`
		}
		data := iosAssociation{
			Applinks: iosApplinks{
				Apps: []string{},
				Details: []iosDetails{
					{
						AppID: fmt.Sprintf("%s.%s", cfg.IOSAppIDPrefix, cfg.IOSBundleID),
						Paths: []string{"/p/*"},
					},
				},
			},
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(data)
	})

	// Public shareable profile HTML landing page (Universal Links / App Links fallbacks)
	if profileHandlerV2 != nil {
		r.Get("/p/{username}", profileHandlerV2.RenderPublicProfilePage)
		r.Head("/p/{username}", profileHandlerV2.RenderPublicProfilePage)
	}

	// V2 routes
	r.Route("/api/v2", func(r chi.Router) {
		r.Get("/", func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			body := map[string]string{"status": "ok", "message": "Medha API v2 is active"}
			if cfg.SwaggerEnabled {
				body["docs"] = "/api/docs"
			}
			_ = json.NewEncoder(w).Encode(body)
		})

		// WebSocket endpoint (handles its own authentication via query param token validation)
		if hub != nil && authSvc != nil {
			r.Get("/ws", ws.ServeWS(hub, authSvc, logger, isDev))
		}

		// Auth — public (no JWT required), strictly rate limited to prevent
		// OTP brute force and SMS-cost abuse.
		if authHandlerV2 != nil {
			r.Group(func(r chi.Router) {
				r.Use(middleware.AuthRateLimit)
				r.Post("/auth/send-otp", authHandlerV2.SendOTP)
				r.Get("/auth/verify-otp", authHandlerV2.VerifyOTP)
				r.Head("/auth/verify-otp", authHandlerV2.VerifyOTP)
				r.Post("/auth/refresh", authHandlerV2.RefreshToken)
				r.Post("/auth/logout", authHandlerV2.Logout)
				r.Get("/auth/dev-users", authHandlerV2.DevUsers)
				r.Post("/auth/dev-login", authHandlerV2.DevLogin)
			})
		}

		// User — public
		if profileHandlerV2 != nil {
			r.Get("/user/check-username", profileHandlerV2.CheckUsername)
			r.Head("/user/check-username", profileHandlerV2.CheckUsername)
			r.Get("/public/profile/{username}", profileHandlerV2.GetPublicProfile)
		}

		// Ceremony catalog — public (supports ?q= fuzzy search)
		if eventHdlr != nil {
			r.Get("/ceremony", eventHdlr.ListCeremoniesV2)
		}

		// Panchanga calendar & festivals — public (no auth required)
		if panchangaHdlr != nil {
			r.Get("/panchanga/today", panchangaHdlr.GetTodayPanchangaV2)
			r.Get("/panchanga/range", panchangaHdlr.GetPanchangaRangeV2)
			r.Get("/panchanga/festivals", panchangaHdlr.GetUpcomingFestivalsV2)
			r.Get("/panchanga", panchangaHdlr.GetPanchangaByDateV2)
		}

		// Cities — public (no auth required)
		if cityHdlr != nil {
			r.Get("/cities/nearby", cityHdlr.ListNearbyCities)
			r.Get("/cities/search", cityHdlr.SearchCities)
		}

		if adminHdlr != nil {
			r.Route("/admin", func(r chi.Router) {
				r.With(middleware.AuthRateLimit).Post("/login", adminHdlr.Login)
				adminHdlr.ProtectedRoutes(r)
			})
		}

		if festivalLogosHdlr != nil && adminHdlr != nil {
			r.Route("/admin/festival-logos", func(r chi.Router) {
				r.Use(adminHdlr.RequireAdmin)
				festivalLogosHdlr.Routes(r)
			})
		}

		// Protected V2 routes (JWT required)
		r.Group(func(r chi.Router) {
			if authSvc != nil {
				r.Use(middleware.Auth(authSvc))
			} else {
				r.Use(func(http.Handler) http.Handler {
					return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						apierrors.WriteProblemDetail(w, apierrors.ServiceUnavailable("Authentication is not configured.", r.URL.Path))
					})
				})
			}

			// ==========================================
			// 1. Yajman-Specific Routes
			// ==========================================
			r.Group(func(r chi.Router) {
				r.Use(middleware.RequireYajman)

				// Events
				if eventHdlr != nil {
					r.Post("/event", eventHdlr.CreateEventV2)
					r.Get("/event/mine", eventHdlr.ListMyEventsV2)
					r.Put("/event/{id}", eventHdlr.UpdateEventV2)
					r.Delete("/event/{id}", eventHdlr.CancelEventV2)
				}

				// Interests (Yajman responses)
				if interestHdlr != nil {
					r.Put("/interest/{id}/accept", interestHdlr.AcceptInterestV2)
					r.Put("/interest/{id}/book", interestHdlr.ConfirmBookingV2)
					r.Get("/event/{id}/interest", interestHdlr.ListEventInterestsV2)
				}

				// Trust & Feedback (Yajman submits feedback on Pandit)
				if trustHdlr != nil {
					r.Post("/match/{id}/feedback", trustHdlr.SubmitFeedbackV2)
				}

				// Pandit discovery (Yajman searching for pandits)
				if panditHdlr != nil {
					r.Get("/pandit/nearby", panditHdlr.ListNearbyPanditsV2)
				}

				// FCM connection placeholders (Yajman initiating)
				if fcmHdlr != nil {
					r.Post("/connection/request", fcmHdlr.RequestConnection)
					r.Post("/connection", fcmHdlr.ConfirmConnection)
				}
			})

			// ==========================================
			// 2. Pandit-Specific Routes
			// ==========================================
			r.Group(func(r chi.Router) {
				r.Use(middleware.RequirePandit)

				// Pandit profile
				if profileHandlerV2 != nil {
					r.Get("/pandit/profile", profileHandlerV2.GetPanditProfile)
					r.Put("/pandit/profile", profileHandlerV2.SetupPanditProfile)
				}

				// Pandit service cities
				if serviceCitiesHdlr != nil {
					r.Get("/pandit/service-cities", serviceCitiesHdlr.GetServiceCities)
					r.Put("/pandit/service-cities", serviceCitiesHdlr.SetServiceCities)
				}

				// Interests (Pandit actions)
				if interestHdlr != nil {
					r.Post("/interest", interestHdlr.ExpressInterestV2)
					r.Put("/interest/{id}/withdraw", interestHdlr.WithdrawInterestV2)
					r.Put("/interest/{id}/respond", interestHdlr.RespondToInterestV2)
					r.Get("/interest/mine", interestHdlr.ListMyInterestsV2)
				}

				// Job Leads (Pandit side)
				if leadHdlr != nil {
					r.Get("/leads/mine", leadHdlr.ListMyLeads)
					r.Put("/leads/{id}/accept", leadHdlr.AcceptLead)
					r.Put("/leads/{id}/decline", leadHdlr.DeclineLead)
				}
			})

			// ==========================================
			// 3. Common Protected Routes (Both Roles)
			// ==========================================
			r.Group(func(r chi.Router) {
				// User profile
				if profileHandlerV2 != nil {
					r.Post("/user/profile", profileHandlerV2.SetupProfile)
					r.Post("/user/me/profile", profileHandlerV2.SetupProfile)
					r.Get("/user/profile", profileHandlerV2.GetProfile)
					r.Get("/user/me/profile", profileHandlerV2.GetProfile)
					r.Head("/user/profile", profileHandlerV2.GetProfile)
					r.Head("/user/me/profile", profileHandlerV2.GetProfile)
					r.Put("/user/location", profileHandlerV2.UpdateLocation)
					r.Put("/user/me/location", profileHandlerV2.UpdateLocation)
					r.Delete("/user/me", profileHandlerV2.DeleteAccount)
				}

				// Storage routes
				if storageHdlr != nil {
					r.Route("/storage", func(r chi.Router) {
						r.Post("/presign", storageHdlr.GeneratePresignedURL)
						r.Post("/confirm", storageHdlr.ConfirmUpload)
					})
				}

				// User location
				if locationHandlerV2 != nil {
					r.Get("/user/location", locationHandlerV2.GetLocation)
					r.Get("/user/me/location", locationHandlerV2.GetLocation)
				}

				// Pandit profile lookup by ID
				if panditHdlr != nil {
					r.Get("/pandit/{id}", panditHdlr.GetPanditByIDV2)
				}

				// Event details lookup
				if eventHdlr != nil {
					r.Get("/event/{id}", eventHdlr.GetEventV2)
				}

				// Community feed
				if feedHandlerV2 != nil {
					r.Get("/feed", feedHandlerV2.ListFeed)
					r.Post("/feed", feedHandlerV2.CreatePost)
					r.Get("/feed/{id}", feedHandlerV2.GetPost)
					r.Delete("/feed/{id}", feedHandlerV2.DeletePost)
					r.Get("/feed/user/{authorId}", feedHandlerV2.ListUserPosts)
					r.Post("/feed/{id}/like", feedHandlerV2.LikePost)
					r.Delete("/feed/{id}/like", feedHandlerV2.UnlikePost)
				}

				// Official stories
				if storyHdlr != nil {
					r.Get("/stories", storyHdlr.ListFeed)
					r.Post("/stories/{id}/view", storyHdlr.RecordView)
				}

				// Matching
				if matchHdlr != nil {
					r.Get("/match/mine", matchHdlr.ListMyMatchesV2)
					r.Get("/match/{id}", matchHdlr.GetMatchV2)
					r.Put("/match/{id}/accept", matchHdlr.AcceptMatchV2)
					r.Put("/match/{id}/activate", matchHdlr.ActivateMatchV2)
					r.Put("/match/{id}/complete", matchHdlr.CompleteMatchV2)
				}

				// Trust badges (Common view)
				if trustHdlr != nil {
					r.Get("/pandit/{id}/badge", trustHdlr.GetBadgeV2)
				}

				// Notifications
				if notifHdlr != nil {
					r.Get("/notification", notifHdlr.ListNotificationsV2)
					r.Put("/notification/read-all", notifHdlr.MarkAllReadV2)
					r.Put("/notification/{id}/read", notifHdlr.MarkReadV2)
					r.Post("/notification/device-token", notifHdlr.RegisterDeviceTokenV2)
					r.Delete("/notification/device-token", notifHdlr.UnregisterDeviceTokenV2)
				}

				// Messaging (chat/fcm)
				if fcmHdlr != nil {
					r.Post("/chat/message", fcmHdlr.SendMessage)
				}
				if msgHdlr != nil {
					r.Post("/conversation", msgHdlr.CreateConversation)
					r.Get("/conversation", msgHdlr.ListConversations)
					r.Get("/conversation/{id}", msgHdlr.GetConversation)
					r.Get("/conversation/{id}/message", msgHdlr.ListMessages)
					r.Post("/conversation/{id}/message", msgHdlr.SendMessage)
					r.Put("/conversation/{id}/read", msgHdlr.MarkRead)
					r.Post("/help", msgHdlr.StartHelpConversation)
				}

				// Location utility
				if locHdlr != nil {
					r.Get("/location/suggest", locHdlr.AutocompleteV2)
					r.Get("/location/reverse", locHdlr.ReverseGeocodeV2)
					r.Get("/location/route", locHdlr.RouteV2)
					r.Get("/location/token", locHdlr.TokenV2)
					r.Get("/location/nearby", locHdlr.NearbyV2)
				}
			})
		})
	})

	// Mount public storage proxies (allows serving logos directly via the API domain)
	MountBucketProxies(r, cfg)

	return r
}
