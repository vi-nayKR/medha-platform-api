package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRiskHandlerFailsClosedWithoutConfiguration(t *testing.T) {
	handler := NewRiskHandler(nil, "")
	req := httptest.NewRequest(http.MethodPost, "/internal/notifications/risk-check", nil)
	res := httptest.NewRecorder()
	handler.CheckRisk(res, req)
	if res.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusServiceUnavailable)
	}
}
