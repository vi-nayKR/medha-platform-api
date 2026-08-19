package middleware

import (
	"net/http"

	"github.com/go-chi/cors"
)

// CORS returns a configured CORS middleware.
// In development mode, localhost origins with any port are allowed so that
// local frontends and admin panels work without additional configuration.
// In production, only the real medha.app and admin domains are permitted.
func CORS(isDev bool) func(http.Handler) http.Handler {
	allowedOrigins := []string{
		"https://*.medha.app",
		"https://admin.medha.dev",
		"https://medha.dev",
		"https://www.medha.dev",
	}

	if isDev {
		allowedOrigins = []string{
			"https://admin-dev.medha.dev",
			"https://dev.medha.dev",
		}
		// Allow localhost with any port (both hostname variants for Windows/Linux/macOS).
		// 127.0.0.1 is commonly used on Windows where localhost may resolve differently.
		// These origins are intentionally excluded in production to prevent a locally-running
		// malicious page from making credentialed requests to the API.
		allowedOrigins = append(allowedOrigins,
			"http://localhost:*",
			"http://127.0.0.1:*",
		)
	}

	return cors.Handler(cors.Options{
		AllowedOrigins:   allowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Request-ID", "X-Requested-With"},
		ExposedHeaders:   []string{"X-Request-ID"},
		AllowCredentials: true,
		MaxAge:           300,
	})
}
