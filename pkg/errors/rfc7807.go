package errors

import (
	"bytes"
	"encoding/json"
	"net/http"
	"sync"
)

// ProblemDetail implements RFC 7807 (Problem Details for HTTP APIs).
type ProblemDetail struct {
	Type     string `json:"type"`
	Title    string `json:"title"`
	Status   int    `json:"status"`
	Detail   string `json:"detail"`
	Instance string `json:"instance,omitempty"`
}

var errBufPool = sync.Pool{
	New: func() any {
		return new(bytes.Buffer)
	},
}

// WriteProblemDetail writes an RFC 7807 problem detail response.
func WriteProblemDetail(w http.ResponseWriter, pd ProblemDetail) {
	buf := errBufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer func() {
		if buf.Cap() <= 16<<10 {
			errBufPool.Put(buf)
		}
	}()

	if err := json.NewEncoder(buf).Encode(pd); err != nil {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"title":"Internal Server Error","status":500,"detail":"failed to encode error response"}`))
		return
	}

	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(pd.Status)
	_, _ = w.Write(buf.Bytes())
}

// NewProblemDetail creates a new ProblemDetail with the given parameters.
func NewProblemDetail(status int, problemType, title, detail, instance string) ProblemDetail {
	return ProblemDetail{
		Type:     problemType,
		Title:    title,
		Status:   status,
		Detail:   detail,
		Instance: instance,
	}
}

// NotFound creates a 404 Not Found problem detail.
func NotFound(detail, instance string) ProblemDetail {
	return NewProblemDetail(
		http.StatusNotFound,
		"https://medha.app/errors/not-found",
		"Not Found",
		detail,
		instance,
	)
}

// BadRequest creates a 400 Bad Request problem detail.
func BadRequest(detail, instance string) ProblemDetail {
	return NewProblemDetail(
		http.StatusBadRequest,
		"https://medha.app/errors/bad-request",
		"Bad Request",
		detail,
		instance,
	)
}

// Unauthorized creates a 401 Unauthorized problem detail.
func Unauthorized(detail, instance string) ProblemDetail {
	return NewProblemDetail(
		http.StatusUnauthorized,
		"https://medha.app/errors/unauthorized",
		"Unauthorized",
		detail,
		instance,
	)
}

// Forbidden creates a 403 Forbidden problem detail.
func Forbidden(detail, instance string) ProblemDetail {
	return NewProblemDetail(
		http.StatusForbidden,
		"https://medha.app/errors/forbidden",
		"Forbidden",
		detail,
		instance,
	)
}

// Conflict creates a 409 Conflict problem detail.
func Conflict(detail, instance string) ProblemDetail {
	return NewProblemDetail(
		http.StatusConflict,
		"https://medha.app/errors/conflict",
		"Conflict",
		detail,
		instance,
	)
}

// InternalError creates a 500 Internal Server Error problem detail.
func InternalError(detail, instance string) ProblemDetail {
	return NewProblemDetail(
		http.StatusInternalServerError,
		"https://medha.app/errors/internal-error",
		"Internal Server Error",
		detail,
		instance,
	)
}

// ServiceUnavailable creates a 503 Service Unavailable problem detail.
func ServiceUnavailable(detail, instance string) ProblemDetail {
	return NewProblemDetail(
		http.StatusServiceUnavailable,
		"https://medha.app/errors/service-unavailable",
		"Service Unavailable",
		detail,
		instance,
	)
}
