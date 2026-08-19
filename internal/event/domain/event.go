package domain

import (
	"errors"

	"github.com/google/uuid"

	authdomain "github.com/medha/backend/internal/auth/domain"
)

// Ceremony represents an entry in the ceremony catalog (mapped from festival_logos).
type Ceremony struct {
	ID           uuid.UUID `json:"id"`
	Slug         string    `json:"slug"`
	DisplayName  string    `json:"display_name"`
	ImageURL     string    `json:"image_url"`
	LogoURL      string    `json:"logo_url"` // Keep for backward-compatibility JSON tag
	Category     string    `json:"category"`
	Description  string    `json:"description,omitempty"`
	ImageURLNoBg string    `json:"image_url_no_bg,omitempty"`
	DisplayOrder int       `json:"display_order"`
	IsActive     bool      `json:"is_active"`
	CreatedAt    int64     `json:"created_at"`
}

// CeremonyType represents the type of ceremony for an event.
type CeremonyType string

const (
	CeremonyShraadh         CeremonyType = "shraadh"
	CeremonyGrihapravesh    CeremonyType = "grihapravesh"
	CeremonyVivah           CeremonyType = "vivah"
	CeremonySatyanarayan    CeremonyType = "satyanarayan"
	CeremonyMundan          CeremonyType = "mundan"
	CeremonyAntimSanskar    CeremonyType = "antim_sanskar"
	CeremonyVastuShanti     CeremonyType = "vastu_shanti"
	CeremonyNaamkaran       CeremonyType = "naamkaran"
	CeremonyUpanayana       CeremonyType = "upanayana"
	CeremonyAkshayaTritiya  CeremonyType = "akshaya_tritiya"
	CeremonyAnnaprashan     CeremonyType = "annaprashan"
	CeremonyBhagwatKatha    CeremonyType = "bhagwat_katha"
	CeremonyBhoomiPuja      CeremonyType = "bhoomi_puja"
	CeremonyChandiHoma      CeremonyType = "chandi_homa"
	CeremonyDasara          CeremonyType = "dasara"
	CeremonyDiwali          CeremonyType = "diwali"
	CeremonyDurgaPuja       CeremonyType = "durga_puja"
	CeremonyEngagement      CeremonyType = "engagement"
	CeremonyGaneshChaturthi CeremonyType = "ganesh_chaturthi"
	CeremonyHanumanJayanti  CeremonyType = "hanuman_jayanti"
	CeremonyKarthikPournami CeremonyType = "karthik_pournami"
	CeremonyKarvaChauth     CeremonyType = "karva_chauth"
	CeremonyKrishnaJanma    CeremonyType = "krishna_janmashtami"
	CeremonyMahaShivaratri  CeremonyType = "maha_shivaratri"
	CeremonyNavratri        CeremonyType = "navratri"
	CeremonyOnam            CeremonyType = "onam"
	CeremonyRamNavami       CeremonyType = "ram_navami"
	CeremonyRudraHoma       CeremonyType = "rudra_homa"
	CeremonyCustom          CeremonyType = "custom"
	CeremonyVaramahalakshmiVrata CeremonyType = "varamahalakshmi_vrata"
	CeremonyGowriGaneshaPuja     CeremonyType = "gowri_ganesha_puja"
	CeremonyAyudhaPuja           CeremonyType = "ayudha_puja"
	CeremonyKarthikaSomavaraPuja CeremonyType = "karthika_somavara_puja"
	CeremonyGanapathiHoma        CeremonyType = "ganapathi_homa"
	CeremonyNavagrahaHoma        CeremonyType = "navagraha_homa"
	CeremonySudarshanaHoma       CeremonyType = "sudarshana_homa"
	CeremonyAyushyaHoma          CeremonyType = "ayushya_homa"
	CeremonyMrityunjayaHoma       CeremonyType = "mrityunjaya_homa"
	CeremonySeemantha            CeremonyType = "seemantha"
)

// validCeremonyTypes is the internal list (unexported to prevent mutation).
var validCeremonyTypes = []CeremonyType{
	CeremonyShraadh, CeremonyGrihapravesh, CeremonyVivah,
	CeremonySatyanarayan, CeremonyMundan, CeremonyAntimSanskar,
	CeremonyVastuShanti, CeremonyNaamkaran, CeremonyUpanayana,
	CeremonyAkshayaTritiya, CeremonyAnnaprashan, CeremonyBhagwatKatha,
	CeremonyBhoomiPuja, CeremonyChandiHoma, CeremonyDasara,
	CeremonyDiwali, CeremonyDurgaPuja, CeremonyEngagement,
	CeremonyGaneshChaturthi, CeremonyHanumanJayanti, CeremonyKarthikPournami,
	CeremonyKarvaChauth, CeremonyKrishnaJanma, CeremonyMahaShivaratri,
	CeremonyNavratri, CeremonyOnam, CeremonyRamNavami, CeremonyRudraHoma,
	CeremonyCustom, CeremonyVaramahalakshmiVrata, CeremonyGowriGaneshaPuja,
	CeremonyAyudhaPuja, CeremonyKarthikaSomavaraPuja, CeremonyGanapathiHoma,
	CeremonyNavagrahaHoma, CeremonySudarshanaHoma, CeremonyAyushyaHoma,
	CeremonyMrityunjayaHoma, CeremonySeemantha,
}

// ValidCeremonyTypes returns a copy of valid ceremony types.
func ValidCeremonyTypes() []CeremonyType {
	result := make([]CeremonyType, len(validCeremonyTypes))
	copy(result, validCeremonyTypes)
	return result
}

// IsValidCeremonyType checks if the given string is a valid ceremony type.
func IsValidCeremonyType(ct string) bool {
	for _, v := range validCeremonyTypes {
		if string(v) == ct {
			return true
		}
	}
	return false
}

func (ct CeremonyType) String() string {
	return string(ct)
}

// EventStatus represents the status of an event.
type EventStatus string

const (
	EventStatusCreated   EventStatus = "Created"
	EventStatusActive    EventStatus = "Active"
	EventStatusPending   EventStatus = "Pending"
	EventStatusBooked    EventStatus = "Booked"
	EventStatusCompleted EventStatus = "Completed"
	EventStatusCancelled EventStatus = "Cancelled"
	EventStatusPushed    EventStatus = "Pushed"
)

func (s EventStatus) IsValid() bool {
	switch s {
	case EventStatusCreated, EventStatusActive, EventStatusPending, EventStatusBooked, EventStatusCompleted, EventStatusCancelled, EventStatusPushed:
		return true
	}
	return false
}

func (s EventStatus) String() string {
	return string(s)
}

// Event represents a ceremony event posted by a Yajman.
type Event struct {
	ID                        uuid.UUID
	YajmanID                  uuid.UUID
	CeremonyType              CeremonyType
	EventDate                 int64
	Location                  *authdomain.GeoPoint
	Address                   string
	Description               string
	CustomCeremonyName        string
	CustomCeremonyDescription string
	Status                    EventStatus
	CeremonyLogoURL           *string   // Added for response inclusion
	PanditPhoneNumber         *string   // Added for Booked event details
	PanditName                *string   // Added for Booked event details
	ConversationID            *uuid.UUID // Added for Booked event — Yajman opens chat
	YajmanPhoneNumber         *string   // Added for event details
	CreatedAt                 int64
	UpdatedAt                 int64
	DeletedAt                 *int64
	PlatformFee               *float64
}

// IsDeleted returns true if the event has been soft-deleted.
func (e *Event) IsDeleted() bool {
	return e.DeletedAt != nil
}

// IsActive returns true if the event status is open for interest (Created, Active, Pending, or Pushed).
func (e *Event) IsActive() bool {
	return e.Status == EventStatusCreated || e.Status == EventStatusActive || e.Status == EventStatusPending || e.Status == EventStatusPushed
}

// EventWithDistance contains an event plus distance from a reference point,
// along with the Yajman's display name and photo for list views.
type EventWithDistance struct {
	Event
	DistanceKM      float64
	YajmanFirstName string
	YajmanLastName  string
	YajmanPhotoURL  *string
}

// Sentinel errors for event operations.
var (
	ErrEventNotFound         = errors.New("event not found")
	ErrEventAlreadyExists    = errors.New("event already exists")
	ErrInvalidCeremonyType   = errors.New("invalid ceremony type")
	ErrInvalidCustomCeremony = errors.New("invalid custom ceremony")
	ErrInvalidEventStatus    = errors.New("invalid event status")
	ErrEventNotOwned         = errors.New("event does not belong to this user")
	ErrEventNotActive        = errors.New("event is not open for interactions")
	ErrEventDateInPast       = errors.New("event date must be set in the future")
)
