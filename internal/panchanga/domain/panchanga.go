package domain

// Panchanga represents a single day's Hindu calendar details.
type Panchanga struct {
	ID              string `json:"id"`
	Date            int64  `json:"date"`
	Samvatsara      string `json:"samvatsara"`
	Ayana           string `json:"ayana"`
	Rutu            string `json:"rutu"`
	Masa            string `json:"masa"`
	Paksha          string `json:"paksha"`
	Tithi           string `json:"tithi"`
	Nakshatra       string `json:"nakshatra"`
	Yoga            string `json:"yoga"`
	Karana          string `json:"karana"`
	Vasara          string `json:"vasara"`
	ShraddhaThithi  string `json:"shraddha_tithi"`
	MasaNiyamaka    string `json:"masa_niyamaka"`
	FestivalsEvents string `json:"festivals_events"`
	Sunrise         string `json:"sunrise"`
	Sunset          string `json:"sunset"`
	Rahukala        string `json:"rahukala"`
	Gulikala        string `json:"gulikala"`
	Yamaganda       string `json:"yamaganda"`
}

// Festival represents a named Hindu festival with its Gregorian date.
type Festival struct {
	ID          string `json:"id"`
	Festival    string `json:"festival"`
	Date        int64  `json:"date"`
	Description string `json:"description"`
}
