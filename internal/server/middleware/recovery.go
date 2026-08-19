package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/medha/backend/pkg/response"
)

// Recovery recovers from panics, logs the stack trace, and returns a JSON 500 response.
func Recovery(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					reqID := GetRequestID(r.Context())
					logger.Error("panic recovered",
						"error", err,
						"stack", string(debug.Stack()),
						"path", r.URL.Path,
						"method", r.Method,
						"request_id", reqID,
					)

					// Write JSON response
					response.WriteJSON(w, http.StatusInternalServerError, map[string]string{
						"error": "Internal Server Error",
					})
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}
