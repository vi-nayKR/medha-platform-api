package festivallogos

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"

	"github.com/medha/backend/internal/infra/storage"
)

const (
	pngMagic    = "\x89PNG\r\n\x1a\n"
	maxFileSize = 2 * 1024 * 1024 // 2 MB
)

// ValidationError represents a structured field-level validation failure.
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// SeedResult holds the outcome of seeding a single festival.
type SeedResult struct {
	Slug      string
	FoundMain bool
	FoundNoBg bool
	Error     string
}

// Service holds business logic for festival logos.
type Service struct {
	repo   *Repository
	s3     *storage.S3Client
	logger *slog.Logger
	bucket string
}

// NewService creates a new festival logos Service. The bucket is created and
// given a public-read policy centrally by storage.NewS3Client.
func NewService(ctx context.Context, repo *Repository, s3Client *storage.S3Client, logger *slog.Logger) (*Service, error) {
	return &Service{repo: repo, s3: s3Client, logger: logger, bucket: storage.BucketFestivalLogos}, nil
}

// ListFestivals returns all festival logo entries.
func (s *Service) ListFestivals(ctx context.Context) ([]*FestivalLogo, error) {
	festivals, err := s.repo.GetAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("festival service: list: %w", err)
	}
	for _, f := range festivals {
		if f.ImageURLNoBg != nil && *f.ImageURLNoBg != "" {
			resolved := s.s3.ResolveURLForBucket(s.bucket, *f.ImageURLNoBg)
			f.ImageURLNoBg = &resolved
		}
	}
	return festivals, nil
}

// CreateFestival inserts a new festival logo entry.
func (s *Service) CreateFestival(ctx context.Context, name, slug string, displayOrder int) (*FestivalLogo, error) {
	if err := validateName(name); err != nil {
		return nil, err
	}
	if err := validateSlug(slug); err != nil {
		return nil, err
	}
	f, err := s.repo.Create(ctx, CreateParams{Name: name, Slug: slug, DisplayOrder: displayOrder})
	if err != nil {
		return nil, fmt.Errorf("festival service: create: %w", err)
	}
	return f, nil
}

// UpdateFestival modifies name, is_active, and display_order of a festival logo.
func (s *Service) UpdateFestival(ctx context.Context, id string, p UpdateParams) (*FestivalLogo, error) {
	if err := validateName(p.Name); err != nil {
		return nil, err
	}
	f, err := s.repo.Update(ctx, id, p)
	if err != nil {
		return nil, fmt.Errorf("festival service: update: %w", err)
	}
	if f == nil {
		return nil, fmt.Errorf("festival service: update: not found")
	}
	return f, nil
}

// DeleteFestival removes a festival logo by UUID.
func (s *Service) DeleteFestival(ctx context.Context, id string) error {
	if err := s.repo.Delete(ctx, id); err != nil {
		return fmt.Errorf("festival service: delete: %w", err)
	}
	return nil
}

// UploadFestivalImage validates, uploads, and stores an image for a festival logo.
// variant must be "no_bg".
func (s *Service) UploadFestivalImage(ctx context.Context, festivalID, variant string, file multipart.File, _ *multipart.FileHeader) (*FestivalLogo, error) {
	if variant != "no_bg" {
		return nil, &ValidationError{Field: "variant", Message: `must be "no_bg"`}
	}

	// Read up to maxFileSize + 1 to detect oversize files without loading everything.
	limitedReader := io.LimitReader(file, maxFileSize+1)
	data, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, fmt.Errorf("festival service: read upload: %w", err)
	}
	if int64(len(data)) > maxFileSize {
		return nil, &ValidationError{Field: "image", Message: "file must be 2 MB or smaller"}
	}
	if !isPNG(data) {
		return nil, &ValidationError{Field: "image", Message: "only PNG files are accepted"}
	}

	// Fetch current record to derive the slug for the object key.
	current, err := s.repo.GetByID(ctx, festivalID)
	if err != nil {
		return nil, fmt.Errorf("festival service: fetch festival: %w", err)
	}
	if current == nil {
		return nil, fmt.Errorf("festival service: festival not found")
	}

	objectKey := objectKeyFor(current.Slug, variant)
	if err := s.s3.PutObjectInBucket(ctx, s.bucket, objectKey, bytes.NewReader(data), int64(len(data)), "image/png"); err != nil {
		return nil, fmt.Errorf("festival service: upload to s3: %w", err)
	}

	publicURL := s.s3.ResolveURLForBucket(s.bucket, objectKey)

	updated, err := s.repo.UpdateImageURL(ctx, festivalID, &publicURL)
	if err != nil {
		return nil, fmt.Errorf("festival service: persist image url: %w", err)
	}
	return updated, nil
}

// SeedFestivals iterates over all 30 festivals, upserts DB rows, and uploads PNG files found at dirPath.
func (s *Service) SeedFestivals(ctx context.Context, dirPath string) (seeded int, results []SeedResult, err error) {
	type entry struct {
		name         string
		slug         string
		displayOrder int
	}
	all := []entry{
		{"Akshaya Tritiya", "akshaya-tritiya", 1},
		{"Annaprashan", "annaprashan", 2},
		{"Bhagwat Katha", "bhagwat-katha", 3},
		{"Bhoomi Puja", "bhoomi-puja", 4},
		{"Chandi Homa", "chandi-homa", 5},
		{"Dasara", "dasara", 6},
		{"Diwali", "diwali", 7},
		{"Durga Puja", "durga-puja", 8},
		{"Engagement", "engagement", 9},
		{"Ganesh Chaturthi", "ganesh-chaturthi", 10},
		{"Gau Seva", "gau-seva", 11},
		{"Griha Pravesh", "griha-pravesh", 12},
		{"Hanuman Jayanti", "hanuman-jayanti", 13},
		{"Karthik Pournami", "karthik-pournami", 14},
		{"Karva Chauth", "karva-chauth", 15},
		{"Krishna Janmashtami", "krishna-janmashtami", 16},
		{"Maha Mrityunjaya Jaap", "maha-mrityunjaya-jaap", 17},
		{"Maha Shivaratri", "maha-shivaratri", 18},
		{"Namakarana", "namakarana", 19},
		{"Navratri", "navratri", 20},
		{"Onam", "onam", 21},
		{"Ram Navami", "ram-navami", 22},
		{"Rudra Homa", "rudra-homa", 23},
		{"Satyanarayan Puja", "satyanarayan-puja", 24},
		{"Shraddha", "shraddha", 25},
		{"Sundarkand Path", "sundarkand-path", 26},
		{"Ugadi", "ugadi", 27},
		{"Upanayana", "upanayana", 28},
		{"Vastu Shanti Puja", "vastu-shanti-puja", 29},
		{"Vivah", "vivah", 30},
	}

	for _, e := range all {
		r := SeedResult{Slug: e.slug}

		f, upsertErr := s.repo.Upsert(ctx, CreateParams{Name: e.name, Slug: e.slug, DisplayOrder: e.displayOrder})
		if upsertErr != nil {
			r.Error = upsertErr.Error()
			results = append(results, r)
			continue
		}

		underscored := strings.ReplaceAll(e.slug, "-", "_")

		// Upload no-bg variant.
		noBgPath := filepath.Join(dirPath, underscored+"_without_bg.png")
		if uploaded, uploadErr := s.uploadSeedFile(ctx, f.ID, f.Slug, noBgPath, "no_bg"); uploadErr != nil {
			r.Error = uploadErr.Error()
		} else if uploaded {
			r.FoundNoBg = true
		}

		seeded++
		results = append(results, r)
	}
	return seeded, results, nil
}

// uploadSeedFile uploads a single PNG file from disk; returns (uploaded, error).
// Returns (false, nil) when the file simply doesn't exist.
func (s *Service) uploadSeedFile(ctx context.Context, festivalID, slug, path, variant string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil // silently skip missing files
		}
		return false, fmt.Errorf("read %s: %w", path, err)
	}
	if !isPNG(data) {
		return false, fmt.Errorf("file %s is not a valid PNG", filepath.Base(path))
	}

	objectKey := objectKeyFor(slug, variant)
	if err := s.s3.PutObjectInBucket(ctx, s.bucket, objectKey, bytes.NewReader(data), int64(len(data)), "image/png"); err != nil {
		return false, fmt.Errorf("upload %s: %w", objectKey, err)
	}

	publicURL := s.s3.ResolveURLForBucket(s.bucket, objectKey)

	if _, err := s.repo.UpdateImageURL(ctx, festivalID, &publicURL); err != nil {
		return false, fmt.Errorf("persist url for %s: %w", slug, err)
	}
	return true, nil
}

// --- helpers ---

func isPNG(data []byte) bool {
	return len(data) >= 8 && string(data[:8]) == pngMagic
}

func objectKeyFor(slug, variant string) string {
	if variant == "no_bg" {
		return slug + "-no-bg.png"
	}
	return slug + ".png"
}

// resolvePublicURL is deprecated, use storage.S3Client methods directly.
func resolvePublicURL(m *storage.S3Client, bucketName, objectKey string) string {
	return m.ResolveURLForBucket(bucketName, objectKey)
}

func validateName(name string) error {
	if strings.TrimSpace(name) == "" || len(name) > 100 {
		return &ValidationError{Field: "name", Message: "must be between 1 and 100 characters"}
	}
	return nil
}

func validateSlug(slug string) error {
	if slug == "" {
		return &ValidationError{Field: "slug", Message: "must not be empty"}
	}
	for _, c := range slug {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
			return &ValidationError{Field: "slug", Message: "must contain only lowercase letters, numbers, and hyphens"}
		}
	}
	return nil
}
