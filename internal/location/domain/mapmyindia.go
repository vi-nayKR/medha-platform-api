package domain

import (
	"github.com/medha/backend/internal/platform/epoch"
)

// MapMyIndia API credentials and constants.
const (
	TokenEndpoint       = "https://outpost.mapmyindia.com/api/security/oauth/token"
	AutocompleteBaseURL = "https://atlas.mapmyindia.com/api/places/search/json"
	RouteBaseURL        = "https://apis.mappls.com/advancedmaps/v1"
	NearbyBaseURL       = "https://atlas.mapmyindia.com/api/places/nearby/json"
	RouteTypeShortest   = "1"
)

// TokenResponse is the OAuth2 token response from MapMyIndia.
type TokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"` // seconds
	Scope       string `json:"scope"`
}

// CachedToken holds a MapMyIndia access token with its expiry.
type CachedToken struct {
	AccessToken string
	ExpiresAt   int64
}

// IsExpired returns true if the token has expired or will expire within 60s.
func (t *CachedToken) IsExpired() bool {
	return epoch.Now() > (t.ExpiresAt - 60)
}

// AutocompleteResult is a single suggested location from the autocomplete API.
type AutocompleteResult struct {
	TypeX        int     `json:"typeX"`
	Type         string  `json:"type"`
	PlaceAddress string  `json:"placeAddress"`
	PlaceName    string  `json:"placeName"`
	ELoc         string  `json:"eLoc"`
	Latitude     float64 `json:"latitude"`
	Longitude    float64 `json:"longitude"`
	OrderIndex   int     `json:"orderIndex"`
	Score        float64 `json:"score"`
	Distance     int64   `json:"distance"`
}

// AutocompleteResponse wraps the suggestions returned to the client.
type AutocompleteResponse struct {
	Suggestions []AutocompleteResult `json:"suggestions"`
}

// RouteStep is a single navigation step in a route leg.
type RouteStep struct {
	Duration float64       `json:"duration"`
	Distance float64       `json:"distance"`
	Name     string        `json:"name"`
	Mode     string        `json:"mode"`
	Geometry string        `json:"geometry"`
	Maneuver RouteManeuver `json:"maneuver"`
}

// RouteManeuver describes the action at a step.
type RouteManeuver struct {
	Type     string    `json:"type"`
	Modifier string    `json:"modifier"`
	Location []float64 `json:"location"`
}

// RouteLeg is a segment between two waypoints.
type RouteLeg struct {
	Summary  string      `json:"summary"`
	Duration float64     `json:"duration"`
	Distance float64     `json:"distance"`
	Steps    []RouteStep `json:"steps"`
}

// Route is a full route option returned by the routing API.
type Route struct {
	Duration float64    `json:"duration"`
	Distance float64    `json:"distance"`
	Legs     []RouteLeg `json:"legs"`
}

// RouteResponse is what we return to the client.
type RouteResponse struct {
	Routes []Route `json:"routes"`
}

// NearbyResult is a single place returned by the nearby search API.
type NearbyResult struct {
	Distance     int64   `json:"distance"`
	ELoc         string  `json:"eLoc"`
	Email        string  `json:"email"`
	EntryType    string  `json:"entryType"`
	Mobile       string  `json:"mobile"`
	OrderIndex   int     `json:"orderIndex"`
	PlaceAddress string  `json:"placeAddress"`
	PlaceName    string  `json:"placeName"`
	Type         string  `json:"type"`
	Latitude     float64 `json:"latitude"`
	Longitude    float64 `json:"longitude"`
}

// NearbyResponse is the collection of nearby places.
type NearbyResponse struct {
	SuggestedLocations []NearbyResult `json:"suggestedLocations"`
}

// RevGeocodeResult is a single result returned by the reverse geocoding API.
type RevGeocodeResult struct {
	HouseNumber      string `json:"houseNumber"`
	HouseName        string `json:"houseName"`
	Poi              string `json:"poi"`
	PoiDist          string `json:"poi_dist"`
	Street           string `json:"street"`
	StreetDist       string `json:"street_dist"`
	SubSubLocality   string `json:"subSubLocality"`
	SubLocality      string `json:"subLocality"`
	Locality         string `json:"locality"`
	Village          string `json:"village"`
	District         string `json:"district"`
	SubDistrict      string `json:"subDistrict"`
	City             string `json:"city"`
	State            string `json:"state"`
	Pincode          string `json:"pincode"`
	Lat              string `json:"lat"`
	Lng              string `json:"lng"`
	Area             string `json:"area"`
	IsRooftop        bool   `json:"isRooftop"`
	IsVenue          bool   `json:"isVenue"`
	FormattedAddress string `json:"formatted_address"`
}

// RevGeocodeResponse wraps the response from the reverse geocoding API.
type RevGeocodeResponse struct {
	ResponseCode int                `json:"responseCode"`
	Version      string             `json:"version"`
	Results      []RevGeocodeResult `json:"results"`
}
