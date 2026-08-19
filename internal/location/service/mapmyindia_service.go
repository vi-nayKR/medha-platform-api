package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/medha/backend/internal/location/domain"
	"github.com/medha/backend/internal/platform/epoch"
)

var errMapMyIndiaAuth = errors.New("mapmyindia authentication failed")

// MapMyIndiaService manages the MapMyIndia OAuth token and provides
// autocomplete + routing via the MapMyIndia Atlas APIs.
type MapMyIndiaService struct {
	clientID     string
	clientSecret string
	logger       *slog.Logger
	httpClient   *http.Client
	endpoints    mapMyIndiaEndpoints

	mu          sync.Mutex
	cachedToken *domain.CachedToken
}

type mapMyIndiaEndpoints struct {
	token        string
	autocomplete string
	route        string
	nearby       string
}

// NewMapMyIndiaService creates a new MapMyIndiaService using environment variables.
// MAPMYINDIA_CLIENT_ID and MAPMYINDIA_CLIENT_SECRET must be set; if missing, the
// service is constructed but API calls will fail with a clear error at token fetch.
func NewMapMyIndiaService(logger *slog.Logger) *MapMyIndiaService {
	clientID := os.Getenv("MAPMYINDIA_CLIENT_ID")
	clientSecret := os.Getenv("MAPMYINDIA_CLIENT_SECRET")
	if clientID == "" || clientSecret == "" {
		logger.Warn("MAPMYINDIA_CLIENT_ID / MAPMYINDIA_CLIENT_SECRET not set; MapMyIndia API calls will fail")
	}
	return &MapMyIndiaService{
		clientID:     clientID,
		clientSecret: clientSecret,
		logger:       logger,
		httpClient:   &http.Client{Timeout: 10 * time.Second},
		endpoints: mapMyIndiaEndpoints{
			token:        domain.TokenEndpoint,
			autocomplete: domain.AutocompleteBaseURL,
			route:        domain.RouteBaseURL,
			nearby:       domain.NearbyBaseURL,
		},
	}
}

// token returns a valid access token, fetching a new one if expired.
func (s *MapMyIndiaService) token() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cachedToken != nil && !s.cachedToken.IsExpired() {
		return s.cachedToken.AccessToken, nil
	}

	if s.clientID == "" || s.clientSecret == "" {
		return "", fmt.Errorf("mapmyindia credentials not configured (set MAPMYINDIA_CLIENT_ID and MAPMYINDIA_CLIENT_SECRET)")
	}

	s.logger.Info("fetching new MapMyIndia access token")

	data := url.Values{}
	data.Set("grant_type", "client_credentials")
	data.Set("client_id", s.clientID)
	data.Set("client_secret", s.clientSecret)

	resp, err := s.httpClient.Post(
		s.endpoints.token,
		"application/x-www-form-urlencoded",
		strings.NewReader(data.Encode()),
	)
	if err != nil {
		return "", fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		s.logger.Error("MapMyIndia token endpoint error", "status", resp.StatusCode, "body", string(body))
		return "", fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, body)
	}

	var tr domain.TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return "", fmt.Errorf("parse token response: %w", err)
	}

	s.cachedToken = &domain.CachedToken{
		AccessToken: tr.AccessToken,
		ExpiresAt:   epoch.Now() + int64(tr.ExpiresIn),
	}
	s.logger.Info("MapMyIndia token refreshed", "expires_in_seconds", tr.ExpiresIn)
	return s.cachedToken.AccessToken, nil
}

// Autocomplete queries the MapMyIndia places autocomplete API.
// It retries once with a fresh token if a 401/403 is returned.
func (s *MapMyIndiaService) Autocomplete(query, region string) (*domain.AutocompleteResponse, error) {
	result, err := s.doAutocomplete(query, region)
	if errors.Is(err, errMapMyIndiaAuth) {
		// Force token refresh on auth error and retry once
		s.invalidateToken()
		result, err = s.doAutocomplete(query, region)
	}
	return result, err
}

func (s *MapMyIndiaService) doAutocomplete(query, region string) (*domain.AutocompleteResponse, error) {
	tok, err := s.token()
	if err != nil {
		return nil, err
	}

	// Build request URL
	params := url.Values{}
	params.Set("access_token", tok)
	params.Set("query", query)
	if region != "" {
		params.Set("region", region)
	} else {
		params.Set("region", "IND")
	}

	reqURL := s.endpoints.autocomplete + "?" + params.Encode()
	resp, err := s.httpClient.Get(reqURL)
	if err != nil {
		return nil, fmt.Errorf("autocomplete request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("%w: %d", errMapMyIndiaAuth, resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		s.logger.Error("MapMyIndia autocomplete error", "status", resp.StatusCode, "body", string(body))
		return nil, fmt.Errorf("autocomplete API returned %d: %s", resp.StatusCode, body)
	}

	// MapMyIndia returns the full suggestedLocations structure
	var raw struct {
		SuggestedLocations []struct {
			Type         string  `json:"type"`
			TypeX        int     `json:"typeX"`
			PlaceAddress string  `json:"placeAddress"`
			PlaceName    string  `json:"placeName"`
			ELoc         string  `json:"eLoc"`
			Latitude     float64 `json:"latitude"`
			Longitude    float64 `json:"longitude"`
			OrderIndex   int     `json:"orderIndex"`
			Score        float64 `json:"score"`
			Distance     int64   `json:"distance"`
		} `json:"suggestedLocations"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("parse autocomplete response: %w", err)
	}

	results := make([]domain.AutocompleteResult, 0, len(raw.SuggestedLocations))
	for _, loc := range raw.SuggestedLocations {
		results = append(results, domain.AutocompleteResult{
			Type:         loc.Type,
			TypeX:        loc.TypeX,
			PlaceAddress: loc.PlaceAddress,
			PlaceName:    loc.PlaceName,
			ELoc:         loc.ELoc,
			Latitude:     loc.Latitude,
			Longitude:    loc.Longitude,
			OrderIndex:   loc.OrderIndex,
			Score:        loc.Score,
			Distance:     loc.Distance,
		})
	}

	return &domain.AutocompleteResponse{Suggestions: results}, nil
}

// Route returns driving directions between waypoints (semicolon-separated lng,lat pairs).
// It retries once with a fresh token on auth errors.
func (s *MapMyIndiaService) Route(waypoints, alternatives string) (*domain.RouteResponse, error) {
	result, err := s.doRoute(waypoints, alternatives)
	if errors.Is(err, errMapMyIndiaAuth) {
		s.invalidateToken()
		result, err = s.doRoute(waypoints, alternatives)
	}
	return result, err
}

func (s *MapMyIndiaService) doRoute(waypoints, alternatives string) (*domain.RouteResponse, error) {
	tok, err := s.token()
	if err != nil {
		return nil, err
	}

	// Build URL: https://apis.mapmyindia.com/advancedmaps/v1/{token}/route_adv/driving/{waypoints}
	reqURL := fmt.Sprintf("%s/%s/route_adv/driving/%s", s.endpoints.route, tok, waypoints)

	params := url.Values{}
	if alternatives != "" {
		params.Set("alternatives", alternatives)
	}
	params.Set("geometries", "polyline")
	params.Set("overview", "full")
	params.Set("steps", "true")
	params.Set("region", "ind")
	params.Set("rtype", domain.RouteTypeShortest)
	reqURL = reqURL + "?" + params.Encode()

	resp, err := s.httpClient.Get(reqURL)
	if err != nil {
		return nil, fmt.Errorf("route request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("%w: %d", errMapMyIndiaAuth, resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("route API returned %d: %s", resp.StatusCode, body)
	}

	body, _ := io.ReadAll(resp.Body)

	// The API returns a JSONP callback: route_api_result({...})
	// Strip the callback wrapper if present
	cleaned := strings.TrimSpace(string(body))
	if idx := strings.Index(cleaned, "({"); idx != -1 {
		cleaned = cleaned[idx+1:]
		cleaned = strings.TrimSuffix(cleaned, ")")
	}

	// Parse the routes
	var raw struct {
		Routes []struct {
			Duration float64 `json:"duration"`
			Distance float64 `json:"distance"`
			Legs     []struct {
				Summary  string  `json:"summary"`
				Duration float64 `json:"duration"`
				Distance float64 `json:"distance"`
				Steps    []struct {
					Mode     string  `json:"mode"`
					Duration float64 `json:"duration"`
					Distance float64 `json:"distance"`
					Name     string  `json:"name"`
					Geometry string  `json:"geometry"`
					Maneuver struct {
						Type     string    `json:"type"`
						Modifier string    `json:"modifier"`
						Location []float64 `json:"location"`
					} `json:"maneuver"`
				} `json:"steps"`
			} `json:"legs"`
		} `json:"routes"`
	}

	if err := json.Unmarshal([]byte(cleaned), &raw); err != nil {
		return nil, fmt.Errorf("parse route response: %w", err)
	}

	routes := make([]domain.Route, 0, len(raw.Routes))
	for _, r := range raw.Routes {
		legs := make([]domain.RouteLeg, 0, len(r.Legs))
		for _, l := range r.Legs {
			steps := make([]domain.RouteStep, 0, len(l.Steps))
			for _, st := range l.Steps {
				steps = append(steps, domain.RouteStep{
					Duration: st.Duration,
					Distance: st.Distance,
					Name:     st.Name,
					Mode:     st.Mode,
					Geometry: st.Geometry,
					Maneuver: domain.RouteManeuver{
						Type:     st.Maneuver.Type,
						Modifier: st.Maneuver.Modifier,
						Location: st.Maneuver.Location,
					},
				})
			}
			legs = append(legs, domain.RouteLeg{
				Summary:  l.Summary,
				Duration: l.Duration,
				Distance: l.Distance,
				Steps:    steps,
			})
		}
		routes = append(routes, domain.Route{
			Duration: r.Duration,
			Distance: r.Distance,
			Legs:     legs,
		})
	}

	return &domain.RouteResponse{Routes: routes}, nil
}

// GetToken returns a valid fresh access token.
func (s *MapMyIndiaService) GetToken() (string, error) {
	return s.token()
}

// Nearby searches for nearby POIs based on keywords and reference location.
func (s *MapMyIndiaService) Nearby(keywords string, lat, lng float64, radius int) (*domain.NearbyResponse, error) {
	result, err := s.doNearby(keywords, lat, lng, radius)
	if errors.Is(err, errMapMyIndiaAuth) {
		s.invalidateToken()
		result, err = s.doNearby(keywords, lat, lng, radius)
	}
	return result, err
}

func (s *MapMyIndiaService) doNearby(keywords string, lat, lng float64, radius int) (*domain.NearbyResponse, error) {
	tok, err := s.token()
	if err != nil {
		return nil, err
	}

	params := url.Values{}
	params.Set("access_token", tok)
	params.Set("keywords", keywords)
	params.Set("refLocation", fmt.Sprintf("%f,%f", lat, lng))
	if radius > 0 {
		params.Set("radius", fmt.Sprintf("%d", radius))
	}

	reqURL := s.endpoints.nearby + "?" + params.Encode()
	resp, err := s.httpClient.Get(reqURL)
	if err != nil {
		return nil, fmt.Errorf("nearby request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("%w: %d", errMapMyIndiaAuth, resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		s.logger.Error("MapMyIndia nearby error", "status", resp.StatusCode, "body", string(body))
		return nil, fmt.Errorf("nearby API returned %d: %s", resp.StatusCode, body)
	}

	var results domain.NearbyResponse
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil, fmt.Errorf("parse nearby response: %w", err)
	}

	return &results, nil
}

// ReverseGeocode fetches the address for given coordinates.
func (s *MapMyIndiaService) ReverseGeocode(lat, lng float64) (*domain.RevGeocodeResponse, error) {
	result, err := s.doReverseGeocode(lat, lng)
	if errors.Is(err, errMapMyIndiaAuth) {
		s.logger.Warn("MapMyIndia ReverseGeocode authentication failed, refreshing token", "error", err)
		s.invalidateToken()
		result, err = s.doReverseGeocode(lat, lng)
	}
	return result, err
}

func (s *MapMyIndiaService) doReverseGeocode(lat, lng float64) (*domain.RevGeocodeResponse, error) {
	tok, err := s.token()
	if err != nil {
		return nil, err
	}

	// Build URL: https://apis.mappls.com/advancedmaps/v1/{token}/rev_geocode
	reqURL := fmt.Sprintf("%s/%s/rev_geocode", s.endpoints.route, tok)

	params := url.Values{}
	params.Set("lat", fmt.Sprintf("%f", lat))
	params.Set("lng", fmt.Sprintf("%f", lng))
	params.Set("region", "IND")
	reqURL = reqURL + "?" + params.Encode()

	resp, err := s.httpClient.Get(reqURL)
	if err != nil {
		return nil, fmt.Errorf("reverse geocode request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("%w: %d", errMapMyIndiaAuth, resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		s.logger.Error("MapMyIndia reverse geocode error", "status", resp.StatusCode, "body", string(body))
		return nil, fmt.Errorf("reverse geocode API returned %d: %s", resp.StatusCode, body)
	}

	var results domain.RevGeocodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil, fmt.Errorf("parse reverse geocode response: %w", err)
	}

	return &results, nil
}

func (s *MapMyIndiaService) invalidateToken() {
	s.mu.Lock()
	s.cachedToken = nil
	s.mu.Unlock()
}
