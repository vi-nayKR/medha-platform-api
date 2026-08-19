package middleware

import (
	"net/http"
	"strings"

	apierrors "github.com/medha/backend/pkg/errors"
)

const (
	maxRequestBody = int64(1 << 20)
	maxUploadBody  = int64(151 << 20)
)

// BodyLimit bounds request bodies before handlers decode or buffer them.
func BodyLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limit := maxRequestBody
		if strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "multipart/form-data") ||
			(r.Method == http.MethodPut && isLegacyStoragePath(r.URL.Path)) {
			limit = maxUploadBody
		}
		if r.ContentLength > limit {
			apierrors.WriteProblemDetail(w, apierrors.NewProblemDetail(
				http.StatusRequestEntityTooLarge,
				"https://medha.app/errors/request-too-large",
				"Request Too Large",
				"Request body exceeds the allowed size.",
				r.URL.Path,
			))
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, limit)
		next.ServeHTTP(w, r)
	})
}

func isLegacyStoragePath(path string) bool {
	for _, prefix := range []string{"/ceremony-logo/", "/festival-logo/", "/user-profile-pic/", "/user-feed/"} {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}
