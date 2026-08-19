package domain

import (
	"encoding/json"
	"errors"

	"github.com/google/uuid"
)

// ConversationType represents the kind of conversation.
type ConversationType string

const (
	ConvPanditPandit ConversationType = "pandit_pandit"
	ConvPanditYajman ConversationType = "pandit_yajman"
	ConvHelp         ConversationType = "help"
)

func (c ConversationType) String() string { return string(c) }

// IsValid returns true if the conversation type is one of the known values.
func (c ConversationType) IsValid() bool {
	switch c {
	case ConvPanditPandit, ConvPanditYajman, ConvHelp:
		return true
	}
	return false
}

// MessageContentType represents the content type of a message.
type MessageContentType string

const (
	ContentText     MessageContentType = "text"
	ContentImage    MessageContentType = "image"
	ContentLocation MessageContentType = "location"
	ContentSystem   MessageContentType = "system"
)

func (m MessageContentType) String() string { return string(m) }

// IsValid returns true if the content type is one of the known values.
func (m MessageContentType) IsValid() bool {
	switch m {
	case ContentText, ContentImage, ContentLocation, ContentSystem:
		return true
	}
	return false
}

// Conversation represents a chat thread between participants.
type Conversation struct {
	ID            uuid.UUID        `json:"id"`
	Type          ConversationType `json:"type"`
	EventID       *uuid.UUID       `json:"event_id,omitempty"`
	MatchID       *uuid.UUID       `json:"match_id,omitempty"`
	Title         string           `json:"title"`
	IsActive      bool             `json:"is_active"`
	LastMessageAt *int64           `json:"last_message_at,omitempty"`
	Participants  []Participant    `json:"participants,omitempty"`
	CreatedAt     int64            `json:"created_at"`
	UpdatedAt     int64            `json:"updated_at"`
}

// Participant represents a user in a conversation.
type Participant struct {
	UserID     uuid.UUID `json:"user_id"`
	IsAdmin    bool      `json:"is_admin"`
	LastReadAt int64     `json:"last_read_at"`
	Muted      bool      `json:"muted"`
	JoinedAt   int64     `json:"joined_at"`
	// Joined from users table for API responses
	FirstName string `json:"first_name,omitempty"`
	LastName  string `json:"last_name,omitempty"`
	Role      string `json:"role,omitempty"`
	PhotoURL  string `json:"profile_photo_url,omitempty"`
}

// Message represents a single chat message within a conversation.
type Message struct {
	ID             uuid.UUID          `json:"id"`
	ConversationID uuid.UUID          `json:"conversation_id"`
	SenderID       uuid.UUID          `json:"sender_id"`
	ContentType    MessageContentType `json:"content_type"`
	Content        string             `json:"content"`
	Metadata       json.RawMessage    `json:"metadata,omitempty"`
	CreatedAt      int64              `json:"created_at"`
	// Joined from users table for API responses
	SenderName string `json:"sender_name,omitempty"`
}

// Sentinel errors for messaging operations.
var (
	ErrConversationNotFound = errors.New("conversation not found")
	ErrNotParticipant       = errors.New("user is not a participant in this conversation")
	ErrConversationInactive = errors.New("conversation is no longer active")
	ErrConversationExists   = errors.New("a conversation between these users already exists")
)
