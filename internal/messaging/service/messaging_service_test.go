package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"

	"github.com/medha/backend/internal/messaging/domain"
	"github.com/medha/backend/internal/platform/epoch"
)

type fakeMessagingRepo struct {
	domain.MessagingRepository
	conversations  map[uuid.UUID]*domain.Conversation
	messages       map[uuid.UUID]*domain.Message
	participants   map[uuid.UUID]map[uuid.UUID]bool
	existingDirect *domain.Conversation
}

func newFakeMessagingRepo() *fakeMessagingRepo {
	return &fakeMessagingRepo{
		conversations: make(map[uuid.UUID]*domain.Conversation),
		messages:      make(map[uuid.UUID]*domain.Message),
		participants:  make(map[uuid.UUID]map[uuid.UUID]bool),
	}
}

func (r *fakeMessagingRepo) CreateConversation(_ context.Context, conv *domain.Conversation) error {
	cp := *conv
	if cp.CreatedAt == 0 {
		cp.CreatedAt = epoch.Now()
	}
	r.conversations[conv.ID] = &cp
	r.participants[conv.ID] = make(map[uuid.UUID]bool, len(conv.Participants))
	for _, p := range conv.Participants {
		r.participants[conv.ID][p.UserID] = true
	}
	return nil
}

func (r *fakeMessagingRepo) GetConversation(_ context.Context, id uuid.UUID) (*domain.Conversation, error) {
	conv, ok := r.conversations[id]
	if !ok {
		return nil, domain.ErrConversationNotFound
	}
	cp := *conv
	cp.Participants = append([]domain.Participant(nil), conv.Participants...)
	return &cp, nil
}

func (r *fakeMessagingRepo) IsParticipant(_ context.Context, conversationID, userID uuid.UUID) (bool, error) {
	return r.participants[conversationID][userID], nil
}

func (r *fakeMessagingRepo) FindDirectConversation(_ context.Context, _, _ uuid.UUID, _ domain.ConversationType) (*domain.Conversation, error) {
	return r.existingDirect, nil
}

func (r *fakeMessagingRepo) CreateMessage(_ context.Context, msg *domain.Message) error {
	cp := *msg
	if cp.CreatedAt == 0 {
		cp.CreatedAt = epoch.Now()
	}
	r.messages[msg.ID] = &cp
	msg.CreatedAt = cp.CreatedAt
	return nil
}

func TestMessagingService_CreateConversationReturnsExistingDirect(t *testing.T) {
	callerID := uuid.New()
	otherID := uuid.New()
	existing := &domain.Conversation{ID: uuid.New(), Type: domain.ConvPanditYajman}
	repo := newFakeMessagingRepo()
	repo.existingDirect = existing
	svc := NewMessagingService(repo, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	conv, err := svc.CreateConversation(context.Background(), callerID, CreateConversationParams{
		Type:           domain.ConvPanditYajman,
		ParticipantIDs: []uuid.UUID{otherID},
	})
	if err != nil {
		t.Fatalf("CreateConversation returned error: %v", err)
	}
	if conv.ID != existing.ID {
		t.Fatalf("expected existing conversation %s, got %s", existing.ID, conv.ID)
	}
	if len(repo.conversations) != 0 {
		t.Fatal("expected existing conversation path not to create a new row")
	}
}

func TestMessagingService_CreateConversationValidatesDirectParticipants(t *testing.T) {
	svc := NewMessagingService(newFakeMessagingRepo(), nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	_, err := svc.CreateConversation(context.Background(), uuid.New(), CreateConversationParams{
		Type:           domain.ConvPanditYajman,
		ParticipantIDs: nil,
	})
	if err == nil {
		t.Fatal("expected error for missing direct participant")
	}
}

func TestMessagingService_SendMessage(t *testing.T) {
	callerID := uuid.New()
	otherID := uuid.New()
	convID := uuid.New()
	repo := newFakeMessagingRepo()
	repo.conversations[convID] = &domain.Conversation{
		ID:   convID,
		Type: domain.ConvPanditYajman,
		Participants: []domain.Participant{
			{UserID: callerID, FirstName: "Vinay", LastName: "KR"},
			{UserID: otherID, FirstName: "Ramesh", LastName: "Sharma"},
		},
	}
	repo.participants[convID] = map[uuid.UUID]bool{callerID: true, otherID: true}
	svc := NewMessagingService(repo, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	msg, err := svc.SendMessage(context.Background(), SendMessageParams{
		ConversationID: convID,
		SenderID:       callerID,
		ContentType:    domain.ContentImage,
		Content:        "  Namaste  ",
	})
	if err != nil {
		t.Fatalf("SendMessage returned error: %v", err)
	}
	if msg.Content != "Namaste" {
		t.Fatalf("expected trimmed content, got %q", msg.Content)
	}
	if msg.ContentType != domain.ContentText {
		t.Fatalf("expected unsupported content type to default to text, got %q", msg.ContentType)
	}
	if msg.SenderName != "Vinay KR" {
		t.Fatalf("expected sender name from participants, got %q", msg.SenderName)
	}
}

func TestMessagingService_SendMessageRejectsNonParticipant(t *testing.T) {
	convID := uuid.New()
	repo := newFakeMessagingRepo()
	repo.conversations[convID] = &domain.Conversation{ID: convID}
	repo.participants[convID] = map[uuid.UUID]bool{}
	svc := NewMessagingService(repo, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	_, err := svc.SendMessage(context.Background(), SendMessageParams{
		ConversationID: convID,
		SenderID:       uuid.New(),
		ContentType:    domain.ContentText,
		Content:        "Namaste",
	})
	if !errors.Is(err, domain.ErrNotParticipant) {
		t.Fatalf("expected ErrNotParticipant, got %v", err)
	}
}
