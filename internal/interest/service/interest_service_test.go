package service_test

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	authdomain "github.com/medha/backend/internal/auth/domain"
	eventdomain "github.com/medha/backend/internal/event/domain"
	"github.com/medha/backend/internal/infra/database"
	interestdomain "github.com/medha/backend/internal/interest/domain"
	interestsvc "github.com/medha/backend/internal/interest/service"
	matchdomain "github.com/medha/backend/internal/matching/domain"
	matchservice "github.com/medha/backend/internal/matching/service"
	userdomain "github.com/medha/backend/internal/user/domain"
)

// --- Mock Implementations ---

type mockInterestRepo struct {
	interests map[uuid.UUID]*interestdomain.Interest
}

type mockMatchRepo struct {
	matches map[uuid.UUID]*matchdomain.Match
}

func (m *mockMatchRepo) Create(ctx context.Context, match *matchdomain.Match) error {
	m.matches[match.ID] = match
	return nil
}

func (m *mockMatchRepo) GetByID(ctx context.Context, id uuid.UUID) (*matchdomain.Match, error) {
	match, ok := m.matches[id]
	if !ok {
		return nil, matchdomain.ErrMatchNotFound
	}
	return match, nil
}

func (m *mockMatchRepo) GetByIDWithDetails(ctx context.Context, id uuid.UUID) (*matchdomain.MatchWithDetails, error) {
	return nil, nil
}

func (m *mockMatchRepo) UpdateStatus(ctx context.Context, id uuid.UUID, status matchdomain.MatchStatus) error {
	match, ok := m.matches[id]
	if !ok {
		return matchdomain.ErrMatchNotFound
	}
	match.Status = status
	return nil
}

func (m *mockMatchRepo) ListByUserID(ctx context.Context, userID uuid.UUID, cursor string, limit int) ([]*matchdomain.MatchWithDetails, string, error) {
	return nil, "", nil
}

func (m *mockMatchRepo) ListByEventID(ctx context.Context, eventID uuid.UUID) ([]*matchdomain.MatchWithDetails, error) {
	return nil, nil
}

func (m *mockMatchRepo) CountByEventID(ctx context.Context, eventID uuid.UUID) (int, error) {
	return 0, nil
}

func (m *mockMatchRepo) GetByPanditAndEvent(ctx context.Context, panditID, eventID uuid.UUID) (*matchdomain.Match, error) {
	for _, match := range m.matches {
		if match.PanditID == panditID && match.EventID == eventID {
			return match, nil
		}
	}
	return nil, matchdomain.ErrMatchNotFound
}

type fakeTx struct {
	pgx.Tx
}

func (m *mockInterestRepo) Create(_ context.Context, interest *interestdomain.Interest) error {
	m.interests[interest.ID] = interest
	return nil
}

func (m *mockInterestRepo) GetByID(_ context.Context, id uuid.UUID) (*interestdomain.Interest, error) {
	interest, ok := m.interests[id]
	if !ok {
		return nil, interestdomain.ErrInterestNotFound
	}
	return interest, nil
}

func (m *mockInterestRepo) UpdateStatus(_ context.Context, id uuid.UUID, status interestdomain.InterestStatus) error {
	interest, ok := m.interests[id]
	if !ok {
		return interestdomain.ErrInterestNotFound
	}
	interest.Status = status
	return nil
}

func (m *mockInterestRepo) ListByEventID(_ context.Context, _ uuid.UUID, _ string, _ int) ([]*interestdomain.InterestWithDetails, string, error) {
	return nil, "", nil
}

func (m *mockInterestRepo) ListByPanditID(_ context.Context, _ uuid.UUID, _ string, _ int) ([]*interestdomain.InterestWithDetails, string, error) {
	return nil, "", nil
}

func (m *mockInterestRepo) GetByPanditAndEvent(_ context.Context, _, _ uuid.UUID) (*interestdomain.Interest, error) {
	return nil, nil
}

func (m *mockInterestRepo) CountByEventID(_ context.Context, _ uuid.UUID) (int, error) {
	return 0, nil
}

func (m *mockInterestRepo) BulkRejectByEventID(_ context.Context, eventID uuid.UUID, excludePanditID uuid.UUID) error {
	for _, interest := range m.interests {
		if interest.EventID == eventID && interest.PanditID != excludePanditID && (interest.Status == interestdomain.InterestStatusPending || interest.Status == interestdomain.InterestStatusConnected) {
			interest.Status = interestdomain.InterestStatusRejected
		}
	}
	return nil
}

func (m *mockInterestRepo) BulkCancelByEventID(_ context.Context, eventID uuid.UUID) error {
	for _, interest := range m.interests {
		if interest.EventID == eventID && (interest.Status == interestdomain.InterestStatusPending || interest.Status == interestdomain.InterestStatusConnected || interest.Status == interestdomain.InterestStatusAccepted) {
			interest.Status = interestdomain.InterestStatusEventCancelled
		}
	}
	return nil
}

func (m *mockInterestRepo) BulkCompleteByEventID(_ context.Context, eventID uuid.UUID) error {
	for _, interest := range m.interests {
		if interest.EventID == eventID && interest.Status == interestdomain.InterestStatusAccepted {
			interest.Status = interestdomain.InterestStatusCompleted
		}
	}
	return nil
}

func (m *mockInterestRepo) CountActiveByEventID(_ context.Context, eventID uuid.UUID) (int, error) {
	count := 0
	for _, interest := range m.interests {
		if interest.EventID == eventID && (interest.Status == interestdomain.InterestStatusPending || interest.Status == interestdomain.InterestStatusConnected) {
			count++
		}
	}
	return count, nil
}

type mockEventRepo struct {
	events           map[uuid.UUID]*eventdomain.Event
	failUpdateStatus bool
}

func (m *mockEventRepo) Create(_ context.Context, event *eventdomain.Event) error {
	m.events[event.ID] = event
	return nil
}

func (m *mockEventRepo) GetByID(_ context.Context, id uuid.UUID) (*eventdomain.Event, error) {
	event, ok := m.events[id]
	if !ok {
		return nil, eventdomain.ErrEventNotFound
	}
	return event, nil
}

func (m *mockEventRepo) Update(_ context.Context, _ *eventdomain.Event) error {
	return nil
}

func (m *mockEventRepo) UpdateStatus(_ context.Context, id uuid.UUID, status eventdomain.EventStatus) error {
	if m.failUpdateStatus {
		return errors.New("simulated database failure during event status update")
	}
	if event, ok := m.events[id]; ok {
		event.Status = status
	}
	return nil
}

func (m *mockEventRepo) SoftDelete(_ context.Context, _ uuid.UUID) error {
	return nil
}

func (m *mockEventRepo) ListByYajmanID(_ context.Context, _ uuid.UUID, _ string, _ int) ([]*eventdomain.Event, string, error) {
	return nil, "", nil
}

func (m *mockEventRepo) ListNearby(_ context.Context, _, _ float64, _ int, _, _ *float64, _ *uuid.UUID, _ string, _ int) ([]*eventdomain.EventWithDistance, string, error) {
	return nil, "", nil
}

func (m *mockEventRepo) ListInBoundingBox(_ context.Context, _, _, _, _ float64, _, _ *float64, _ *uuid.UUID, _ string, _ int) ([]*eventdomain.EventWithDistance, string, error) {
	return nil, "", nil
}

func (m *mockEventRepo) ListCeremonies(_ context.Context) ([]*eventdomain.Ceremony, error) {
	return nil, nil
}

func (m *mockEventRepo) SearchCeremonies(_ context.Context, _ string) ([]*eventdomain.Ceremony, error) {
	return nil, nil
}

func (m *mockEventRepo) FindNearbyPandits(_ context.Context, _, _ float64, _ int) ([]eventdomain.NearbyPandit, error) {
	return nil, nil
}

type mockUserRepo struct {
	users map[uuid.UUID]*userdomain.User
}

func (m *mockUserRepo) GetByID(_ context.Context, id uuid.UUID) (*userdomain.User, error) {
	user, ok := m.users[id]
	if !ok {
		return nil, userdomain.ErrUserNotFound
	}
	return user, nil
}

func (m *mockUserRepo) Create(_ context.Context, _ *userdomain.User) error { return nil }
func (m *mockUserRepo) FindByPhone(_ context.Context, _ string) (*userdomain.User, error) {
	return nil, nil
}
func (m *mockUserRepo) Update(_ context.Context, _ *userdomain.User) error { return nil }
func (m *mockUserRepo) SoftDelete(_ context.Context, _ uuid.UUID) error    { return nil }
func (m *mockUserRepo) Delete(_ context.Context, _ uuid.UUID) error        { return nil }
func (m *mockUserRepo) GetByPhone(_ context.Context, _ string) (*userdomain.User, error) {
	return nil, nil
}
func (m *mockUserRepo) UpdatePhoneVerified(_ context.Context, _ uuid.UUID, _ bool) error {
	return nil
}
func (m *mockUserRepo) UpdateProfileComplete(_ context.Context, _ uuid.UUID, _ bool) error {
	return nil
}
func (m *mockUserRepo) SetupProfile(_ context.Context, _ uuid.UUID, _, _, _, _, _ string) error {
	return nil
}
func (m *mockUserRepo) CheckUsernameExists(_ context.Context, _ string) (bool, error) {
	return false, nil
}
func (m *mockUserRepo) UpdateLocation(_ context.Context, _ uuid.UUID, _, _ float64, _, _, _ string) error {
	return nil
}
func (m *mockUserRepo) ListAll(_ context.Context) ([]*userdomain.User, error) { return nil, nil }
func (m *mockUserRepo) CanViewContactInfo(_ context.Context, _, _ uuid.UUID) (bool, error) {
	return false, nil
}
func (m *mockUserRepo) UpdatePremium(_ context.Context, _ uuid.UUID, _ bool, _ *int64, _ string) error {
	return nil
}

func (m *mockUserRepo) GetByUsername(_ context.Context, _ string) (*userdomain.User, error) {
	return nil, nil
}

func (m *mockUserRepo) GetBadgeTier(_ context.Context, _ uuid.UUID) (string, error) {
	return "puja_praveen", nil
}

// --- Tests ---

func TestInterestService_AcceptAndBookFlow(t *testing.T) {
	interestRepo := &mockInterestRepo{interests: make(map[uuid.UUID]*interestdomain.Interest)}
	eventRepo := &mockEventRepo{events: make(map[uuid.UUID]*eventdomain.Event)}
	userRepo := &mockUserRepo{users: make(map[uuid.UUID]*userdomain.User)}
	matchRepo := &mockMatchRepo{matches: make(map[uuid.UUID]*matchdomain.Match)}

	matchSvc := matchservice.NewMatchService(nil, matchRepo, eventRepo, nil, slog.Default())
	svc := interestsvc.NewInterestService(nil, interestRepo, eventRepo, matchSvc, userRepo, slog.Default())

	yajmanID := uuid.New()
	panditID := uuid.New()
	eventID := uuid.New()

	// Seed Yajman, Pandit, and Event
	userRepo.users[yajmanID] = &userdomain.User{
		ID:        yajmanID,
		FirstName: "Yajman",
		LastName:  "User",
		Role:      authdomain.RoleYajman,
	}
	userRepo.users[panditID] = &userdomain.User{
		ID:        panditID,
		FirstName: "Pandit",
		LastName:  "User",
		Role:      authdomain.RolePandit,
	}
	eventRepo.events[eventID] = &eventdomain.Event{
		ID:           eventID,
		YajmanID:     yajmanID,
		CeremonyType: eventdomain.CeremonySatyanarayan,
		Status:       eventdomain.EventStatusActive,
	}

	// Express interest
	interest := &interestdomain.Interest{
		ID:       uuid.New(),
		PanditID: panditID,
		EventID:  eventID,
		Status:   interestdomain.InterestStatusPending,
	}
	interestRepo.interests[interest.ID] = interest

	// Create match record representing interest
	match := &matchdomain.Match{
		ID:       uuid.New(),
		YajmanID: yajmanID,
		PanditID: panditID,
		EventID:  eventID,
		Status:   matchdomain.MatchStatusCreated,
	}
	matchRepo.matches[match.ID] = match

	// 1. Yajman accepts Pandit's interest (moves to connected status)
	txCtx := database.InjectTx(context.Background(), &fakeTx{})
	updatedInterest, err := svc.AcceptInterest(txCtx, interest.ID, yajmanID)
	if err != nil {
		t.Fatalf("AcceptInterest failed: %v", err)
	}

	// Assert interest status is now connected
	if updatedInterest.Status != interestdomain.InterestStatusConnected {
		t.Errorf("expected interest status connected, got %v", updatedInterest.Status)
	}

	// Assert match status is matched
	updatedMatch, err := matchRepo.GetByID(txCtx, match.ID)
	if err != nil {
		t.Fatalf("failed to get match: %v", err)
	}
	if updatedMatch.Status != matchdomain.MatchStatusMatched {
		t.Errorf("expected match status matched, got %v", updatedMatch.Status)
	}

	// Assert event status remains unchanged (Active, NOT Booked yet!)
	event, _ := eventRepo.GetByID(txCtx, eventID)
	if event.Status != eventdomain.EventStatusActive {
		t.Errorf("expected event status to remain Active on AcceptInterest, got %v", event.Status)
	}

	// 2. Yajman confirms booking (moves to accepted status)
	bookedInterest, err := svc.ConfirmBooking(txCtx, interest.ID, yajmanID)
	if err != nil {
		t.Fatalf("ConfirmBooking failed: %v", err)
	}

	// Assert interest status is accepted
	if bookedInterest.Status != interestdomain.InterestStatusAccepted {
		t.Errorf("expected interest status accepted, got %v", bookedInterest.Status)
	}

	// Assert match status is active (booked)
	updatedMatch, _ = matchRepo.GetByID(txCtx, match.ID)
	if updatedMatch.Status != matchdomain.MatchStatusActive {
		t.Errorf("expected match status active (booked) on ConfirmBooking, got %v", updatedMatch.Status)
	}

	// Assert event status is now Booked
	event, _ = eventRepo.GetByID(txCtx, eventID)
	if event.Status != eventdomain.EventStatusBooked {
		t.Errorf("expected event status to be Booked on ConfirmBooking, got %v", event.Status)
	}
}

func TestInterestService_ConfirmBooking_InterestNotFound(t *testing.T) {
	interestRepo := &mockInterestRepo{interests: make(map[uuid.UUID]*interestdomain.Interest)}
	eventRepo := &mockEventRepo{events: make(map[uuid.UUID]*eventdomain.Event)}
	userRepo := &mockUserRepo{users: make(map[uuid.UUID]*userdomain.User)}
	matchRepo := &mockMatchRepo{matches: make(map[uuid.UUID]*matchdomain.Match)}

	matchSvc := matchservice.NewMatchService(nil, matchRepo, eventRepo, nil, slog.Default())
	svc := interestsvc.NewInterestService(nil, interestRepo, eventRepo, matchSvc, userRepo, slog.Default())

	_, err := svc.ConfirmBooking(context.Background(), uuid.New(), uuid.New())
	if !errors.Is(err, interestdomain.ErrInterestNotFound) {
		t.Fatalf("expected ErrInterestNotFound, got %v", err)
	}
}

func TestInterestService_ConfirmBooking_NotEventOwner(t *testing.T) {
	interestRepo := &mockInterestRepo{interests: make(map[uuid.UUID]*interestdomain.Interest)}
	eventRepo := &mockEventRepo{events: make(map[uuid.UUID]*eventdomain.Event)}
	userRepo := &mockUserRepo{users: make(map[uuid.UUID]*userdomain.User)}
	matchRepo := &mockMatchRepo{matches: make(map[uuid.UUID]*matchdomain.Match)}

	matchSvc := matchservice.NewMatchService(nil, matchRepo, eventRepo, nil, slog.Default())
	svc := interestsvc.NewInterestService(nil, interestRepo, eventRepo, matchSvc, userRepo, slog.Default())

	yajmanID := uuid.New()
	otherYajmanID := uuid.New()
	panditID := uuid.New()
	eventID := uuid.New()

	eventRepo.events[eventID] = &eventdomain.Event{
		ID:           eventID,
		YajmanID:     yajmanID,
		CeremonyType: eventdomain.CeremonySatyanarayan,
		Status:       eventdomain.EventStatusActive,
	}
	interest := &interestdomain.Interest{
		ID:       uuid.New(),
		PanditID: panditID,
		EventID:  eventID,
		Status:   interestdomain.InterestStatusConnected,
	}
	interestRepo.interests[interest.ID] = interest

	_, err := svc.ConfirmBooking(context.Background(), interest.ID, otherYajmanID)
	if !errors.Is(err, interestdomain.ErrNotEventOwner) {
		t.Fatalf("expected ErrNotEventOwner, got %v", err)
	}
}

func TestInterestService_ConfirmBooking_NotConnected(t *testing.T) {
	statuses := []interestdomain.InterestStatus{
		interestdomain.InterestStatusPending,
		interestdomain.InterestStatusAccepted,
		interestdomain.InterestStatusRejected,
	}

	for _, status := range statuses {
		t.Run(string(status), func(t *testing.T) {
			interestRepo := &mockInterestRepo{interests: make(map[uuid.UUID]*interestdomain.Interest)}
			eventRepo := &mockEventRepo{events: make(map[uuid.UUID]*eventdomain.Event)}
			userRepo := &mockUserRepo{users: make(map[uuid.UUID]*userdomain.User)}
			matchRepo := &mockMatchRepo{matches: make(map[uuid.UUID]*matchdomain.Match)}

			matchSvc := matchservice.NewMatchService(nil, matchRepo, eventRepo, nil, slog.Default())
			svc := interestsvc.NewInterestService(nil, interestRepo, eventRepo, matchSvc, userRepo, slog.Default())

			yajmanID := uuid.New()
			panditID := uuid.New()
			eventID := uuid.New()

			eventRepo.events[eventID] = &eventdomain.Event{
				ID:           eventID,
				YajmanID:     yajmanID,
				CeremonyType: eventdomain.CeremonySatyanarayan,
				Status:       eventdomain.EventStatusActive,
			}
			interest := &interestdomain.Interest{
				ID:       uuid.New(),
				PanditID: panditID,
				EventID:  eventID,
				Status:   status,
			}
			interestRepo.interests[interest.ID] = interest

			_, err := svc.ConfirmBooking(context.Background(), interest.ID, yajmanID)
			if !errors.Is(err, interestdomain.ErrInterestNotConnected) {
				t.Fatalf("expected ErrInterestNotConnected for status %v, got %v", status, err)
			}
		})
	}
}

func TestInterestService_ConfirmBooking_BulkRejectsOtherInterests(t *testing.T) {
	interestRepo := &mockInterestRepo{interests: make(map[uuid.UUID]*interestdomain.Interest)}
	eventRepo := &mockEventRepo{events: make(map[uuid.UUID]*eventdomain.Event)}
	userRepo := &mockUserRepo{users: make(map[uuid.UUID]*userdomain.User)}
	matchRepo := &mockMatchRepo{matches: make(map[uuid.UUID]*matchdomain.Match)}

	matchSvc := matchservice.NewMatchService(nil, matchRepo, eventRepo, nil, slog.Default())
	svc := interestsvc.NewInterestService(nil, interestRepo, eventRepo, matchSvc, userRepo, slog.Default())

	yajmanID := uuid.New()
	panditA := uuid.New()
	panditB := uuid.New()
	eventID := uuid.New()

	eventRepo.events[eventID] = &eventdomain.Event{
		ID:           eventID,
		YajmanID:     yajmanID,
		CeremonyType: eventdomain.CeremonySatyanarayan,
		Status:       eventdomain.EventStatusActive,
	}

	interestA := &interestdomain.Interest{
		ID:       uuid.New(),
		PanditID: panditA,
		EventID:  eventID,
		Status:   interestdomain.InterestStatusConnected,
	}
	interestB := &interestdomain.Interest{
		ID:       uuid.New(),
		PanditID: panditB,
		EventID:  eventID,
		Status:   interestdomain.InterestStatusConnected,
	}
	interestRepo.interests[interestA.ID] = interestA
	interestRepo.interests[interestB.ID] = interestB

	txCtx := database.InjectTx(context.Background(), &fakeTx{})
	booked, err := svc.ConfirmBooking(txCtx, interestA.ID, yajmanID)
	if err != nil {
		t.Fatalf("ConfirmBooking failed: %v", err)
	}

	if booked.Status != interestdomain.InterestStatusAccepted {
		t.Errorf("expected booked interest status accepted, got %v", booked.Status)
	}

	other, err := interestRepo.GetByID(txCtx, interestB.ID)
	if err != nil {
		t.Fatalf("failed to get other interest: %v", err)
	}
	if other.Status != interestdomain.InterestStatusRejected {
		t.Errorf("expected other interest status rejected, got %v", other.Status)
	}
}

func TestInterestService_ConfirmBooking_AtomicRollbackOnEventUpdateFailure(t *testing.T) {
	interestRepo := &mockInterestRepo{interests: make(map[uuid.UUID]*interestdomain.Interest)}
	eventRepo := &mockEventRepo{events: make(map[uuid.UUID]*eventdomain.Event)}
	userRepo := &mockUserRepo{users: make(map[uuid.UUID]*userdomain.User)}
	matchRepo := &mockMatchRepo{matches: make(map[uuid.UUID]*matchdomain.Match)}

	matchSvc := matchservice.NewMatchService(nil, matchRepo, eventRepo, nil, slog.Default())
	svc := interestsvc.NewInterestService(nil, interestRepo, eventRepo, matchSvc, userRepo, slog.Default())

	yajmanID := uuid.New()
	panditID := uuid.New()
	eventID := uuid.New()

	eventRepo.events[eventID] = &eventdomain.Event{
		ID:           eventID,
		YajmanID:     yajmanID,
		CeremonyType: eventdomain.CeremonySatyanarayan,
		Status:       eventdomain.EventStatusActive,
	}

	interest := &interestdomain.Interest{
		ID:       uuid.New(),
		PanditID: panditID,
		EventID:  eventID,
		Status:   interestdomain.InterestStatusConnected,
	}
	interestRepo.interests[interest.ID] = interest

	// Simulate database transaction failure during event status update
	eventRepo.failUpdateStatus = true

	txCtx := database.InjectTx(context.Background(), &fakeTx{})
	_, err := svc.ConfirmBooking(txCtx, interest.ID, yajmanID)
	if err == nil {
		t.Fatal("expected ConfirmBooking to fail when event update fails, got nil")
	}

	if !strings.Contains(err.Error(), "simulated database failure") {
		t.Fatalf("expected simulated db failure error, got: %v", err)
	}
}
