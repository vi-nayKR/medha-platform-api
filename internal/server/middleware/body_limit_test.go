package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBodyLimitRejectsOversizedJSONBeforeHandler(t *testing.T) {
	called := false
	handler := BodyLimit(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	req := httptest.NewRequest(http.MethodPost, "/api/v2/auth/send-otp", bytes.NewReader(make([]byte, maxRequestBody+1)))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusRequestEntityTooLarge || called {
		t.Fatalf("status = %d, called = %v", res.Code, called)
	}
}

func TestBodyLimitAllowsBoundedMultipartUpload(t *testing.T) {
	called := false
	handler := BodyLimit(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	req := httptest.NewRequest(http.MethodPost, "/api/v2/admin/buckets/test/objects", bytes.NewReader(make([]byte, maxRequestBody+1)))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=test")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if !called {
		t.Fatalf("bounded multipart upload was rejected with status %d", res.Code)
	}
}
