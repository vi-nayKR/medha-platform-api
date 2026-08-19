package response

import (
	"bytes"
	"encoding/json"
	"net/http"
	"sync"
)

// DataResponse wraps a single resource in {"data": ...} format.
type DataResponse struct {
	Data any `json:"data"`
}

// ListResponse wraps a list of resources with cursor-based pagination.
type ListResponse struct {
	Data   any     `json:"data"`
	Cursor *string `json:"cursor"`
}

var bufPool = sync.Pool{
	New: func() any {
		return new(bytes.Buffer)
	},
}

// WriteJSON writes a JSON response with the given status code and payload.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer func() {
		// Do not retain a large backing array after a one-off large response.
		if buf.Cap() <= 64<<10 {
			bufPool.Put(buf)
		}
	}()

	if err := json.NewEncoder(buf).Encode(v); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"failed to encode response"}`))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

// WriteData writes a single resource wrapped in {"data": ...}.
func WriteData(w http.ResponseWriter, status int, data any) {
	WriteJSON(w, status, DataResponse{Data: data})
}

// WriteList writes a list response with cursor-based pagination.
// Pass nil for cursor when there are no more pages.
func WriteList(w http.ResponseWriter, status int, data any, cursor *string) {
	WriteJSON(w, status, ListResponse{Data: data, Cursor: cursor})
}
