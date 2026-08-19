package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/medha/backend/internal/event/domain"
	"github.com/medha/backend/internal/platform/epoch"
)

type fakeEventRepo struct {
	domain.EventRepository
	events              map[uuid.UUID]*domain.Event
	lastExcludePanditID *uuid.UUID
}

func newFakeEventRepo() *fakeEventRepo {
	return &fakeEventRepo{events: make(map[uuid.UUID]*domain.Event)}
}

func (r *fakeEventRepo) ListNearby(ctx context.Context, lat, lng float64, radiusKM int, refLat, refLng *float64, excludePanditID *uuid.UUID, cursor string, limit int) ([]*domain.EventWithDistance, string, error) {
	r.lastExcludePanditID = excludePanditID
	return nil, "", nil
}

func (r *fakeEventRepo) ListInBoundingBox(ctx context.Context, minLat, maxLat, minLng, maxLng float64, refLat, refLng *float64, excludePanditID *uuid.UUID, cursor string, limit int) ([]*domain.EventWithDistance, string, error) {
	r.lastExcludePanditID = excludePanditID
	return nil, "", nil
}

func (r *fakeEventRepo) Create(_ context.Context, event *domain.Event) error {
	cp := *event
	if cp.CreatedAt == 0 {
		cp.CreatedAt = epoch.Now()
	}
	if cp.UpdatedAt == 0 {
		cp.UpdatedAt = cp.CreatedAt
	}
	r.events[event.ID] = &cp
	return nil
}

func (r *fakeEventRepo) GetByID(_ context.Context, id uuid.UUID) (*domain.Event, error) {
	event, ok := r.events[id]
	if !ok {
		return nil, domain.ErrEventNotFound
	}
	cp := *event
	return &cp, nil
}

func (r *fakeEventRepo) Update(_ context.Context, event *domain.Event) error {
	if _, ok := r.events[event.ID]; !ok {
		return domain.ErrEventNotFound
	}
	cp := *event
	r.events[event.ID] = &cp
	return nil
}

func (r *fakeEventRepo) ListCeremonies(_ context.Context) ([]*domain.Ceremony, error) {
	return []*domain.Ceremony{
		{ID: uuid.New(), Slug: "vivah", DisplayName: "Vivah", IsActive: true},
	}, nil
}

func (r *fakeEventRepo) SearchCeremonies(_ context.Context, query string) ([]*domain.Ceremony, error) {
	if query == "vivah" {
		return []*domain.Ceremony{{ID: uuid.New(), Slug: "vivah", DisplayName: "Vivah", IsActive: true}}, nil
	}
	return nil, nil
}

func TestEventService_CreateCustomEvent(t *testing.T) {
	repo := newFakeEventRepo()
	svc := NewEventService(nil, repo, slog.New(slog.NewTextHandler(io.Discard, nil)))

	event, err := svc.CreateEvent(context.Background(), CreateEventParams{
		YajmanID:                  uuid.New(),
		CeremonyType:              " custom ",
		CustomCeremonyName:        " Gudli Pooja ",
		CustomCeremonyDescription: "Regional ritual details and samagri notes.",
		EventDate:                 time.Now().Add(24 * time.Hour),
		Latitude:                  12.9716,
		Longitude:                 77.5946,
		Address:                   "Bengaluru",
		Description:               "Arrive by 7 AM.",
	})
	if err != nil {
		t.Fatalf("CreateEvent returned error: %v", err)
	}
	if event.CeremonyType != domain.CeremonyCustom {
		t.Fatalf("expected custom ceremony type, got %q", event.CeremonyType)
	}
	if event.CustomCeremonyName != "Gudli Pooja" {
		t.Fatalf("expected trimmed custom ceremony name, got %q", event.CustomCeremonyName)
	}
	if event.CustomCeremonyDescription == "" {
		t.Fatal("expected custom ceremony description")
	}
}

func TestEventService_CustomEventValidation(t *testing.T) {
	repo := newFakeEventRepo()
	svc := NewEventService(nil, repo, slog.New(slog.NewTextHandler(io.Discard, nil)))

	_, err := svc.CreateEvent(context.Background(), CreateEventParams{
		YajmanID:     uuid.New(),
		CeremonyType: domain.CeremonyCustom.String(),
		EventDate:    time.Now().Add(24 * time.Hour),
		Address:      "Bengaluru",
	})
	if !errors.Is(err, domain.ErrInvalidCustomCeremony) {
		t.Fatalf("expected ErrInvalidCustomCeremony, got %v", err)
	}
}

func TestEventService_UpdateClearsCustomFieldsForCatalogCeremony(t *testing.T) {
	repo := newFakeEventRepo()
	svc := NewEventService(nil, repo, slog.New(slog.NewTextHandler(io.Discard, nil)))
	yajmanID := uuid.New()
	existing := &domain.Event{
		ID:                        uuid.New(),
		YajmanID:                  yajmanID,
		CeremonyType:              domain.CeremonyCustom,
		CustomCeremonyName:        "Gudli Pooja",
		CustomCeremonyDescription: "Regional ritual details.",
		EventDate:                 epoch.FromTime(time.Now().Add(24 * time.Hour)),
		Address:                   "Bengaluru",
		Status:                    domain.EventStatusCreated,
	}
	repo.events[existing.ID] = existing

	updated, err := svc.UpdateEvent(context.Background(), UpdateEventParams{
		EventID:      existing.ID,
		YajmanID:     yajmanID,
		CeremonyType: ptr(domain.CeremonyVivah.String()),
		Description:  ptr("Catalog ceremony now."),
	})
	if err != nil {
		t.Fatalf("UpdateEvent returned error: %v", err)
	}
	if updated.CeremonyType != domain.CeremonyVivah {
		t.Fatalf("expected vivah, got %q", updated.CeremonyType)
	}
	if updated.CustomCeremonyName != "" || updated.CustomCeremonyDescription != "" {
		t.Fatalf("expected custom fields cleared, got name=%q description=%q", updated.CustomCeremonyName, updated.CustomCeremonyDescription)
	}
}

func ptr[T any](v T) *T {
	return &v
}

func TestEventService_CeremonyCatalogIncludesCustomOption(t *testing.T) {
	svc := NewEventService(nil, newFakeEventRepo(), slog.New(slog.NewTextHandler(io.Discard, nil)))

	ceremonies, err := svc.ListCeremonies(context.Background())
	if err != nil {
		t.Fatalf("ListCeremonies returned error: %v", err)
	}
	if ceremonies[len(ceremonies)-1].Slug != domain.CeremonyCustom.String() {
		t.Fatalf("expected custom ceremony option, got %#v", ceremonies)
	}

	results, err := svc.SearchCeremonies(context.Background(), "gudli")
	if err != nil {
		t.Fatalf("SearchCeremonies returned error: %v", err)
	}
	if len(results) != 1 || results[0].Slug != domain.CeremonyCustom.String() {
		t.Fatalf("expected custom option for unmatched ritual query, got %#v", results)
	}
}

func TestEventService_ListNearbyAndInBoundingBox_PassesExcludePanditID(t *testing.T) {
	repo := newFakeEventRepo()
	svc := NewEventService(nil, repo, slog.New(slog.NewTextHandler(io.Discard, nil)))

	panditID := uuid.New()

	_, _, err := svc.ListNearbyEvents(context.Background(), 12.97, 77.59, 20, &panditID, "", 10)
	if err != nil {
		t.Fatalf("ListNearbyEvents failed: %v", err)
	}
	if repo.lastExcludePanditID == nil || *repo.lastExcludePanditID != panditID {
		t.Errorf("expected excludePanditID %v, got %v", panditID, repo.lastExcludePanditID)
	}

	repo.lastExcludePanditID = nil
	_, _, err = svc.ListInBoundingBox(context.Background(), 12.0, 13.0, 77.0, 78.0, &panditID, "", 10)
	if err != nil {
		t.Fatalf("ListInBoundingBox failed: %v", err)
	}
	if repo.lastExcludePanditID == nil || *repo.lastExcludePanditID != panditID {
		t.Errorf("expected excludePanditID %v in bounding box call, got %v", panditID, repo.lastExcludePanditID)
	}
}
