package service

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/medha/backend/internal/location/domain"
	"github.com/medha/backend/internal/platform/epoch"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newTestMapService(serverURL string) *MapMyIndiaService {
	svc := NewMapMyIndiaService(slog.New(slog.NewTextHandler(io.Discard, nil)))
	// These tests fully mock the MapMyIndia HTTP endpoints, so inject dummy
	// credentials to satisfy the service's "credentials configured" guard.
	// Without this the suite fails wherever MAPMYINDIA_CLIENT_ID/SECRET are
	// unset — notably in CI, which injects no secrets.
	svc.clientID = "test-client-id"
	svc.clientSecret = "test-client-secret"
	svc.httpClient = serverClient()
	svc.endpoints = mapMyIndiaEndpoints{
		token:        serverURL + "/token",
		autocomplete: serverURL + "/autocomplete",
		route:        serverURL + "/route",
		nearby:       serverURL + "/nearby",
	}
	return svc
}

func serverClient() *http.Client {
	return &http.Client{Timeout: 2 * time.Second}
}

func TestMapMyIndiaService_TokenIsCached(t *testing.T) {
	tokenCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/token" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		tokenCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"cached-token","token_type":"Bearer","expires_in":3600,"scope":"default"}`))
	}))
	defer server.Close()
	svc := newTestMapService(server.URL)

	first, err := svc.GetToken()
	if err != nil {
		t.Fatalf("GetToken returned error: %v", err)
	}
	second, err := svc.GetToken()
	if err != nil {
		t.Fatalf("GetToken returned error: %v", err)
	}
	if first != "cached-token" || second != "cached-token" {
		t.Fatalf("unexpected tokens %q %q", first, second)
	}
	if tokenCalls != 1 {
		t.Fatalf("expected one token request, got %d", tokenCalls)
	}
}

func TestMapMyIndiaService_AutocompleteMapsSuggestions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			_, _ = w.Write([]byte(`{"access_token":"token","expires_in":3600}`))
		case "/autocomplete":
			if got := r.URL.Query().Get("query"); got != "Bangalore" {
				t.Fatalf("expected query Bangalore, got %q", got)
			}
			_, _ = w.Write([]byte(`{"suggestedLocations":[{"type":"CITY","typeX":1,"placeAddress":"Karnataka","placeName":"Bangalore","eLoc":"ABC123","latitude":12.9716,"longitude":77.5946,"orderIndex":1,"score":0.98,"distance":0}]}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	svc := newTestMapService(server.URL)

	resp, err := svc.Autocomplete("Bangalore", "IND")
	if err != nil {
		t.Fatalf("Autocomplete returned error: %v", err)
	}
	if len(resp.Suggestions) != 1 || resp.Suggestions[0].PlaceName != "Bangalore" {
		t.Fatalf("unexpected suggestions: %#v", resp.Suggestions)
	}
}

func TestMapMyIndiaService_DoesNotRetryNonAuthErrors(t *testing.T) {
	var autocompleteCalls int
	svc := NewMapMyIndiaService(slog.New(slog.NewTextHandler(io.Discard, nil)))
	svc.cachedToken = &domain.CachedToken{AccessToken: "cached-token", ExpiresAt: epoch.Now() + 3600}
	svc.endpoints.autocomplete = "https://example.test/autocomplete"
	svc.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		autocompleteCalls++
		return &http.Response{
			StatusCode: http.StatusBadGateway,
			Body:       io.NopCloser(strings.NewReader("upstream unavailable")),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})}

	_, err := svc.Autocomplete("Bangalore", "IND")
	if err == nil || autocompleteCalls != 1 {
		t.Fatalf("expected one non-retried upstream request, calls=%d err=%v", autocompleteCalls, err)
	}
	if !strings.Contains(err.Error(), "502") {
		t.Fatalf("expected upstream status in error, got %v", err)
	}
}

func TestMapMyIndiaService_RetriesAuthErrorsOnce(t *testing.T) {
	var tokenCalls, autocompleteCalls int
	svc := NewMapMyIndiaService(slog.New(slog.NewTextHandler(io.Discard, nil)))
	svc.clientID = "test-client-id"
	svc.clientSecret = "test-client-secret"
	svc.cachedToken = &domain.CachedToken{AccessToken: "expired-token", ExpiresAt: epoch.Now() + 3600}
	svc.endpoints = mapMyIndiaEndpoints{
		token:        "https://example.test/token",
		autocomplete: "https://example.test/autocomplete",
	}
	svc.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/token":
			tokenCalls++
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"access_token":"fresh-token","expires_in":3600}`)),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		case "/autocomplete":
			autocompleteCalls++
			if autocompleteCalls == 1 {
				return &http.Response{StatusCode: http.StatusUnauthorized, Body: io.NopCloser(strings.NewReader("expired")), Header: make(http.Header), Request: req}, nil
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"suggestedLocations":[]}`)), Header: make(http.Header), Request: req}, nil
		default:
			return nil, fmt.Errorf("unexpected request path %q", req.URL.Path)
		}
	})}

	if _, err := svc.Autocomplete("Bangalore", "IND"); err != nil {
		t.Fatalf("Autocomplete returned error: %v", err)
	}
	if tokenCalls != 1 || autocompleteCalls != 2 {
		t.Fatalf("expected one token refresh and two autocomplete requests, token=%d autocomplete=%d", tokenCalls, autocompleteCalls)
	}
}

func TestMapMyIndiaService_RouteParsesJSONP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			_, _ = w.Write([]byte(`{"access_token":"token","expires_in":3600}`))
		case strings.Contains(r.URL.Path, "/route_adv/driving/"):
			_, _ = w.Write([]byte(`route_api_result({"routes":[{"duration":120,"distance":1500,"legs":[{"summary":"MG Road","duration":120,"distance":1500,"steps":[{"mode":"driving","duration":60,"distance":750,"name":"Step one","geometry":"abc","maneuver":{"type":"turn","modifier":"left","location":[77.59,12.97]}}]}]}]})`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	svc := newTestMapService(server.URL)

	resp, err := svc.Route("77.59,12.97;77.60,12.98", "0")
	if err != nil {
		t.Fatalf("Route returned error: %v", err)
	}
	if len(resp.Routes) != 1 || len(resp.Routes[0].Legs) != 1 || resp.Routes[0].Legs[0].Steps[0].Name != "Step one" {
		t.Fatalf("unexpected route response: %#v", resp)
	}
}

func TestMapMyIndiaService_NearbyAndReverseGeocode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			_, _ = w.Write([]byte(`{"access_token":"token","expires_in":3600}`))
		case r.URL.Path == "/nearby":
			_, _ = w.Write([]byte(`{"suggestedLocations":[{"placeName":"Temple","placeAddress":"Bengaluru","eLoc":"TMP001","latitude":12.97,"longitude":77.59,"distance":100}]}`))
		case strings.HasSuffix(r.URL.Path, "/rev_geocode"):
			_, _ = w.Write([]byte(`{"responseCode":200,"version":"1.0","results":[{"formatted_address":"MG Road, Bengaluru","city":"Bengaluru","lat":"12.97","lng":"77.59"}]}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	svc := newTestMapService(server.URL)
	svc.cachedToken = nil

	nearby, err := svc.Nearby("temple", 12.9716, 77.5946, 5000)
	if err != nil {
		t.Fatalf("Nearby returned error: %v", err)
	}
	if len(nearby.SuggestedLocations) != 1 || nearby.SuggestedLocations[0].PlaceName != "Temple" {
		t.Fatalf("unexpected nearby response: %#v", nearby)
	}

	svc.cachedToken = nil
	reverse, err := svc.ReverseGeocode(12.9716, 77.5946)
	if err != nil {
		t.Fatalf("ReverseGeocode returned error: %v", err)
	}
	if reverse.ResponseCode != 200 || reverse.Results[0].FormattedAddress == "" {
		t.Fatalf("unexpected reverse response: %#v", reverse)
	}
}

func TestCachedToken_IsExpired(t *testing.T) {
	token := &domain.CachedToken{ExpiresAt: epoch.Now() + 30}
	if !token.IsExpired() {
		t.Fatal("expected token expiring within 60s to be treated as expired")
	}
}
