package server

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/medha/backend/internal/config"
	"github.com/medha/backend/internal/infra/storage"
)

// containsPathTraversal returns true when the raw path contains sequences that
// could be used to escape the intended bucket prefix, including plain and
// URL-encoded variants of "../" and absolute-path starts.
func containsPathTraversal(rawPath string) bool {
	// Reject literal traversal sequences (forward-slash and backslash variants).
	if strings.Contains(rawPath, "../") || strings.Contains(rawPath, `..\\`) {
		return true
	}
	// Reject URL-encoded variants (%2e%2e%2f, %2e%2e/, etc.).
	lower := strings.ToLower(rawPath)
	if strings.Contains(lower, "%2e%2e%2f") ||
		strings.Contains(lower, "%2e%2e/") ||
		strings.Contains(lower, "..%2f") ||
		strings.Contains(lower, "%2e.") {
		return true
	}
	return false
}

// NewBucketProxy returns a handler that proxies requests to the S3-compatible
// storage backend. It maps the URL path to a bucket and object key.
func NewBucketProxy(cfg *config.Config) http.HandlerFunc {
	// Parse the internal S3 endpoint
	target, err := url.Parse("http://" + cfg.S3Endpoint)
	if err != nil || target.Host == "" {
		return func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "Storage service unavailable", http.StatusServiceUnavailable)
		}
	}

	proxy := httputil.NewSingleHostReverseProxy(target)

	// ModifyResponse cleans up duplicate CORS headers returned by the storage backend to prevent browser errors,
	// as the outer API CORS middleware already applies the correct CORS headers on the response.
	proxy.ModifyResponse = func(res *http.Response) error {
		res.Header.Del("Access-Control-Allow-Origin")
		res.Header.Del("Access-Control-Allow-Methods")
		res.Header.Del("Access-Control-Allow-Headers")
		res.Header.Del("Access-Control-Allow-Credentials")
		res.Header.Del("Access-Control-Expose-Headers")
		return nil
	}

	// Custom director to handle the path mapping
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)

		// Map legacy public path prefixes (kept for old client compatibility) to
		// their real per-content-type bucket.
		path := req.URL.Path
		if strings.HasPrefix(path, "/user-profile-pic/") {
			req.URL.Path = "/" + storage.BucketProfiles + "/" + strings.TrimPrefix(path, "/user-profile-pic/")
		} else if strings.HasPrefix(path, "/user-feed/") {
			req.URL.Path = "/" + storage.BucketFeeds + "/" + strings.TrimPrefix(path, "/user-feed/")
		} else if strings.HasPrefix(path, "/ceremony-logo/") {
			req.URL.Path = "/" + storage.BucketFestivalLogos + "/" + strings.TrimPrefix(path, "/ceremony-logo/")
		} else if strings.HasPrefix(path, "/festival-logo/") {
			req.URL.Path = "/" + storage.BucketFestivalLogos + "/" + strings.TrimPrefix(path, "/festival-logo/")
		}

		req.URL.Host = target.Host
		req.URL.Scheme = target.Scheme
	}

	return func(w http.ResponseWriter, r *http.Request) {
		// Allow GET, HEAD, and PUT requests for the public proxy (to support presigned PUT uploads)
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodPut {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Guard against path traversal attempts before forwarding to S3 storage.
		// Check both the parsed path and the raw URI to catch URL-encoded variants.
		if containsPathTraversal(r.URL.Path) || containsPathTraversal(r.URL.RawPath) || containsPathTraversal(r.RequestURI) {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		proxy.ServeHTTP(w, r)
	}
}

// MountBucketProxies registers routes for all managed public buckets.
func MountBucketProxies(r chi.Router, cfg *config.Config) {
	proxy := NewBucketProxy(cfg)

	// List of legacy path prefixes to expose publicly via the API, for old
	// clients that hardcoded api.medha.dev URLs before
	// s3.medha.dev became the dedicated public storage endpoint.
	publicBuckets := []string{
		"ceremony-logo",
		"festival-logo",
		"user-profile-pic",
		"user-feed",
	}

	for _, bucket := range publicBuckets {
		pattern := fmt.Sprintf("/%s/*", bucket)
		r.Get(pattern, proxy)
		r.Head(pattern, proxy)
		r.Put(pattern, proxy)
	}
}
