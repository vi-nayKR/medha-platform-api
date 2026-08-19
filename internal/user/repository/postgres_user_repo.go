package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	authdomain "github.com/medha/backend/internal/auth/domain"
	"github.com/medha/backend/internal/infra/database"
	"github.com/medha/backend/internal/user/domain"
)

// PostgresUserRepository implements domain.UserRepository using pgxpool.
type PostgresUserRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresUserRepository creates a new PostgresUserRepository.
func NewPostgresUserRepository(pool *pgxpool.Pool) *PostgresUserRepository {
	return &PostgresUserRepository{pool: pool}
}

// Create inserts a new user into the database.
func (r *PostgresUserRepository) Create(ctx context.Context, user *domain.User) error {
	query := `
		INSERT INTO users (id, auth_provider, provider_uid, role, first_name, last_name, username,
		                   email, profile_photo_url,
		                   location, phone, phone_verified, google_id, apple_id, profile_complete, city, state, pincode)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, ST_SetSRID(ST_MakePoint($10, $11), 4326)::geography, $12, $13, $14, $15, $16, $17, $18, $19)
	`

	if user.ID == uuid.Nil {
		user.ID = uuid.New()
	}

	var lng, lat *float64
	if user.Location != nil {
		lng = &user.Location.Longitude
		lat = &user.Location.Latitude
	}

	var roleVal *string
	if user.Role != "" {
		s := user.Role.String()
		roleVal = &s
	}

	_, err := database.GetExecutor(ctx, r.pool).Exec(ctx, query,
		user.ID,
		user.AuthProvider,
		user.ProviderUID,
		roleVal,
		user.FirstName,
		user.LastName,
		nullableString(user.Username),
		nullableString(user.Email),
		nullableString(user.ProfilePhotoURL),
		lng,
		lat,
		nullableString(user.Phone), // new phone column
		user.PhoneVerified,
		nullableString(user.GoogleID),
		nullableString(user.AppleID),
		user.ProfileComplete,
		nullableString(user.City),
		nullableString(user.State),
		nullableString(user.Pincode),
	)
	if err != nil {
		if isDuplicateKeyError(err) {
			if isUsernameConflict(err) {
				return domain.ErrUsernameTaken
			}
			return domain.ErrUserAlreadyExists
		}
		return fmt.Errorf("insert user: %w", err)
	}

	return nil
}

// GetByID returns a non-deleted user by primary key.
func (r *PostgresUserRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	query := `
		SELECT id, COALESCE(auth_provider, ''), COALESCE(provider_uid, ''), role, first_name, last_name, username,
		       email, profile_photo_url,
		       ST_Y(location::geometry) AS latitude, ST_X(location::geometry) AS longitude,
		       COALESCE(created_at, 0), COALESCE(updated_at, 0), deleted_at, phone, COALESCE(phone_verified, false),
		       COALESCE(google_id, ''), COALESCE(apple_id, ''), COALESCE(profile_complete, false),
		       COALESCE(city, ''), COALESCE(state, ''), COALESCE(pincode, ''),
		       is_premium, premium_until, COALESCE(premium_badge, ''), COALESCE(premium_features, '{}'::jsonb),
		       can_authenticate
		FROM users
		WHERE id = $1 AND deleted_at IS NULL
	`

	return r.scanUser(ctx, query, id)
}

func (r *PostgresUserRepository) FindByPhone(ctx context.Context, phone string) (*domain.User, error) {
	query := `
		SELECT id, COALESCE(auth_provider, ''), COALESCE(provider_uid, ''), role, first_name, last_name, username,
		       email, profile_photo_url,
		       ST_Y(location::geometry) AS latitude, ST_X(location::geometry) AS longitude,
		       COALESCE(created_at, 0), COALESCE(updated_at, 0), deleted_at, phone, COALESCE(phone_verified, false),
		       COALESCE(google_id, ''), COALESCE(apple_id, ''), COALESCE(profile_complete, false),
		       COALESCE(city, ''), COALESCE(state, ''), COALESCE(pincode, ''),
		       is_premium, premium_until, COALESCE(premium_badge, ''), COALESCE(premium_features, '{}'::jsonb),
		       can_authenticate
		FROM users
		WHERE phone = $1 AND deleted_at IS NULL
	`

	return r.scanUser(ctx, query, phone)
}

// Update modifies an existing non-deleted user.
func (r *PostgresUserRepository) Update(ctx context.Context, user *domain.User) error {
	query := `
		UPDATE users
		SET first_name = $2,
		    last_name = $3,
		    username = $4,
		    email = $5,
		    profile_photo_url = $6,
		    location = ST_SetSRID(ST_MakePoint($7, $8), 4326)::geography,
		    role = $9,
		    phone = $10,
		    phone_verified = $11,
		    city = $12,
		    state = $13,
		    pincode = $14,
		    is_premium = $15,
		    premium_until = $16,
		    premium_badge = $17,
		    premium_features = $18
		WHERE id = $1 AND deleted_at IS NULL
	`

	var lng, lat *float64
	if user.Location != nil {
		lng = &user.Location.Longitude
		lat = &user.Location.Latitude
	}

	var roleVal *string
	if user.Role != "" {
		s := user.Role.String()
		roleVal = &s
	}

	featuresVal := user.PremiumFeatures
	if len(featuresVal) == 0 {
		featuresVal = []byte("{}")
	}

	tag, err := database.GetExecutor(ctx, r.pool).Exec(ctx, query,
		user.ID,
		user.FirstName,
		user.LastName,
		nullableString(user.Username),
		nullableString(user.Email),
		nullableString(user.ProfilePhotoURL),
		lng,
		lat,
		roleVal,
		nullableString(user.Phone),
		user.PhoneVerified,
		nullableString(user.City),
		nullableString(user.State),
		nullableString(user.Pincode),
		user.IsPremium,
		user.PremiumUntil,
		user.PremiumBadge,
		featuresVal,
	)
	if err != nil {
		if isDuplicateKeyError(err) && isUsernameConflict(err) {
			return domain.ErrUsernameTaken
		}
		return fmt.Errorf("update user: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrUserNotFound
	}

	return nil
}

// SoftDelete marks a user as deleted by setting deleted_at.
func (r *PostgresUserRepository) SoftDelete(ctx context.Context, id uuid.UUID) error {
	query := `UPDATE users SET deleted_at = (EXTRACT(EPOCH FROM NOW()))::BIGINT WHERE id = $1 AND deleted_at IS NULL`

	tag, err := database.GetExecutor(ctx, r.pool).Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("soft delete user: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrUserNotFound
	}

	return nil
}

// Delete permanently deletes a user.
func (r *PostgresUserRepository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM users WHERE id = $1`

	tag, err := database.GetExecutor(ctx, r.pool).Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrUserNotFound
	}

	return nil
}

// --- V2 repository method implementations ---

// GetByPhone is an alias for FindByPhone.
func (r *PostgresUserRepository) GetByPhone(ctx context.Context, phone string) (*domain.User, error) {
	return r.FindByPhone(ctx, phone)
}

// UpdatePhoneVerified sets the phone_verified flag for a user.
func (r *PostgresUserRepository) UpdatePhoneVerified(ctx context.Context, userID uuid.UUID, verified bool) error {
	tag, err := database.GetExecutor(ctx, r.pool).Exec(ctx,
		`UPDATE users SET phone_verified = $2 WHERE id = $1 AND deleted_at IS NULL`,
		userID, verified,
	)
	if err != nil {
		return fmt.Errorf("update phone verified: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrUserNotFound
	}
	return nil
}

// UpdateProfileComplete sets the profile_complete flag for a user.
func (r *PostgresUserRepository) UpdateProfileComplete(ctx context.Context, userID uuid.UUID, complete bool) error {
	tag, err := database.GetExecutor(ctx, r.pool).Exec(ctx,
		`UPDATE users SET profile_complete = $2 WHERE id = $1 AND deleted_at IS NULL`,
		userID, complete,
	)
	if err != nil {
		return fmt.Errorf("update profile complete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrUserNotFound
	}
	return nil
}

// SetupProfile updates first_name, last_name, role, username, email, and marks profile_complete=true.
func (r *PostgresUserRepository) SetupProfile(ctx context.Context, userID uuid.UUID, firstName, lastName, role, username, email string) error {
	tag, err := database.GetExecutor(ctx, r.pool).Exec(ctx, `
		UPDATE users
		SET first_name = $2, 
		    last_name = $3, 
		    role = $4::user_role, 
		    profile_complete = true, 
		    username = NULLIF($5, ''), 
		    email = NULLIF($6, '')
		WHERE id = $1 AND deleted_at IS NULL`,
		userID, firstName, lastName, role, username, email,
	)
	if err != nil {
		if isDuplicateKeyError(err) && isUsernameConflict(err) {
			return domain.ErrUsernameTaken
		}
		return fmt.Errorf("setup profile: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrUserNotFound
	}
	return nil
}

// EnsurePanditProfile creates an empty pandit_profile for a user if it doesn't exist.
func (r *PostgresUserRepository) EnsurePanditProfile(ctx context.Context, userID uuid.UUID) error {
	_, err := database.GetExecutor(ctx, r.pool).Exec(ctx, `
		INSERT INTO pandit_profiles (id, user_id)
		VALUES ($1, $2)
		ON CONFLICT (user_id) DO NOTHING`,
		uuid.New(), userID,
	)
	if err != nil {
		return fmt.Errorf("ensure pandit profile: %w", err)
	}
	return nil
}

// UpdateLocation stores the user's geographic coordinates using a targeted UPDATE.
func (r *PostgresUserRepository) UpdateLocation(ctx context.Context, userID uuid.UUID, latitude, longitude float64, city, state, pincode string) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE users
		SET location = ST_SetSRID(ST_MakePoint($2, $3), 4326)::geography,
		    city = NULLIF($4, ''),
		    state = NULLIF($5, ''),
		    pincode = NULLIF($6, '')
		WHERE id = $1 AND deleted_at IS NULL`,
		userID, longitude, latitude, city, state, pincode, // PostGIS: ST_MakePoint(lng, lat)
	)
	if err != nil {
		return fmt.Errorf("update location: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrUserNotFound
	}
	return nil
}

// scanUser executes a query and scans a single User row.
// The SELECT must return columns in this exact order:
// id, auth_provider, provider_uid, role, first_name, last_name, username,
// email, profile_photo_url, latitude, longitude,
// created_at, updated_at, deleted_at, phone, phone_verified,
// google_id, apple_id, profile_complete, city, state, pincode,
// is_premium, premium_until, premium_badge, premium_features
func (r *PostgresUserRepository) scanUser(ctx context.Context, query string, args ...any) (*domain.User, error) {
	row := database.GetExecutor(ctx, r.pool).QueryRow(ctx, query, args...)

	var (
		user            domain.User
		role            *string
		username        *string
		email           *string
		photoURL        *string
		latitude        *float64
		longitude       *float64
		deletedAt       *int64
		phone           *string
		phoneVerified   bool
		googleID        string
		appleID         string
		profileComplete bool
		city            string
		state           string
		pincode         string
		isPremium       bool
		premiumUntil    *int64
		premiumBadge    string
		premiumFeatures []byte
	)

	err := row.Scan(
		&user.ID,
		&user.AuthProvider,
		&user.ProviderUID,
		&role,
		&user.FirstName,
		&user.LastName,
		&username,
		&email,
		&photoURL,
		&latitude,
		&longitude,
		&user.CreatedAt,
		&user.UpdatedAt,
		&deletedAt,
		&phone,
		&phoneVerified,
		&googleID,
		&appleID,
		&profileComplete,
		&city,
		&state,
		&pincode,
		&isPremium,
		&premiumUntil,
		&premiumBadge,
		&premiumFeatures,
		&user.CanAuthenticate,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrUserNotFound
		}
		return nil, fmt.Errorf("scan user: %w", err)
	}

	if role != nil {
		user.Role = authdomain.UserRole(*role)
	}
	user.DeletedAt = deletedAt

	if username != nil {
		user.Username = *username
	}
	if email != nil {
		user.Email = *email
	}
	if photoURL != nil {
		user.ProfilePhotoURL = *photoURL
	}
	if phone != nil {
		user.Phone = *phone
	}
	user.PhoneVerified = phoneVerified
	user.GoogleID = googleID
	user.AppleID = appleID
	user.ProfileComplete = profileComplete
	user.City = city
	user.State = state
	user.Pincode = pincode
	user.IsPremium = isPremium
	user.PremiumUntil = premiumUntil
	user.PremiumBadge = premiumBadge
	user.PremiumFeatures = premiumFeatures
	if latitude != nil && longitude != nil {
		user.Location = &authdomain.GeoPoint{
			Latitude:  *latitude,
			Longitude: *longitude,
		}
	}

	return &user, nil
}

// ListAll returns all non-deleted users ordered by role ASC, first_name ASC.
// Intended for dev/testing use only (e.g. dev-users endpoint).
func (r *PostgresUserRepository) ListAll(ctx context.Context) ([]*domain.User, error) {
	query := `
		SELECT id, auth_provider, provider_uid, role, first_name, last_name, username,
		       email, profile_photo_url,
		       ST_Y(location::geometry) AS latitude, ST_X(location::geometry) AS longitude,
		       COALESCE(created_at, 0), COALESCE(updated_at, 0), deleted_at, phone, COALESCE(phone_verified, false),
		       COALESCE(google_id, ''), COALESCE(apple_id, ''), COALESCE(profile_complete, false),
		       COALESCE(city, ''), COALESCE(state, ''), COALESCE(pincode, ''),
		       is_premium, premium_until, COALESCE(premium_badge, ''), COALESCE(premium_features, '{}'::jsonb),
		       can_authenticate
		FROM users
		WHERE deleted_at IS NULL AND phone IS NOT NULL
		ORDER BY COALESCE(role::text, '') ASC, COALESCE(first_name, '') ASC
	`

	rows, err := database.GetExecutor(ctx, r.pool).Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list all users: %w", err)
	}
	defer rows.Close()

	var users []*domain.User
	for rows.Next() {
		var (
			user            domain.User
			role            *string
			username        *string
			email           *string
			photoURL        *string
			latitude        *float64
			longitude       *float64
			deletedAt       *int64
			phone           *string
			phoneVerified   bool
			googleID        string
			appleID         string
			profileComplete bool
			city            string
			state           string
			pincode         string
			isPremium       bool
			premiumUntil    *int64
			premiumBadge    string
			premiumFeatures []byte
		)

		err := rows.Scan(
			&user.ID,
			&user.AuthProvider,
			&user.ProviderUID,
			&role,
			&user.FirstName,
			&user.LastName,
			&username,
			&email,
			&photoURL,
			&latitude,
			&longitude,
			&user.CreatedAt,
			&user.UpdatedAt,
			&deletedAt,
			&phone,
			&phoneVerified,
			&googleID,
			&appleID,
			&profileComplete,
			&city,
			&state,
			&pincode,
			&isPremium,
			&premiumUntil,
			&premiumBadge,
			&premiumFeatures,
			&user.CanAuthenticate,
		)
		if err != nil {
			return nil, fmt.Errorf("list all users: scan row: %w", err)
		}

		if role != nil {
			user.Role = authdomain.UserRole(*role)
		}
		user.DeletedAt = deletedAt
		if username != nil {
			user.Username = *username
		}
		if email != nil {
			user.Email = *email
		}
		if photoURL != nil {
			user.ProfilePhotoURL = *photoURL
		}
		if phone != nil {
			user.Phone = *phone
		}
		user.PhoneVerified = phoneVerified
		user.GoogleID = googleID
		user.AppleID = appleID
		user.ProfileComplete = profileComplete
		user.City = city
		user.State = state
		user.Pincode = pincode
		user.IsPremium = isPremium
		user.PremiumUntil = premiumUntil
		user.PremiumBadge = premiumBadge
		user.PremiumFeatures = premiumFeatures
		if latitude != nil && longitude != nil {
			user.Location = &authdomain.GeoPoint{
				Latitude:  *latitude,
				Longitude: *longitude,
			}
		}

		users = append(users, &user)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list all users: rows error: %w", err)
	}

	return users, nil
}

// nullableString returns nil for empty strings, pointer otherwise.
func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// CheckUsernameExists natively queries the database to see if a username is taken.
func (r *PostgresUserRepository) CheckUsernameExists(ctx context.Context, username string) (bool, error) {
	var exists bool
	err := database.GetExecutor(ctx, r.pool).QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM users WHERE username = $1 AND deleted_at IS NULL)", username).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check username exists: %w", err)
	}
	return exists, nil
}

// isDuplicateKeyError checks if the error is a PostgreSQL unique violation (23505).
func isDuplicateKeyError(err error) bool {
	return err != nil && containsSQLState(err, "23505")
}

// isUsernameConflict checks if the duplicate key error is for the username column.
func isUsernameConflict(err error) bool {
	return err != nil && strings.Contains(err.Error(), "username")
}

func containsSQLState(err error, code string) bool {
	// pgx wraps errors with SQLState method
	type sqlStater interface {
		SQLState() string
	}
	var se sqlStater
	if errors.As(err, &se) {
		return se.SQLState() == code
	}
	return false
}

// CanViewContactInfo checks if a viewer is authorized to view a pandit's contact number.
func (r *PostgresUserRepository) CanViewContactInfo(ctx context.Context, viewerID, panditID uuid.UUID) (bool, error) {
	if viewerID == panditID {
		return true, nil
	}

	query := `
		SELECT EXISTS (
			SELECT 1 FROM matches
			WHERE ((yajman_id = $1 AND pandit_id = $2) OR (yajman_id = $2 AND pandit_id = $1))
			  AND status IN ('matched', 'active', 'completed')
		)
	`
	var allowed bool
	err := database.GetExecutor(ctx, r.pool).QueryRow(ctx, query, viewerID, panditID).Scan(&allowed)
	if err != nil {
		return false, fmt.Errorf("can view contact info check: %w", err)
	}
	return allowed, nil
}

// UpdatePremium updates the premium status, expiry, and badge for a user.
func (r *PostgresUserRepository) UpdatePremium(ctx context.Context, userID uuid.UUID, isPremium bool, premiumUntil *int64, premiumBadge string) error {
	tag, err := database.GetExecutor(ctx, r.pool).Exec(ctx,
		`UPDATE users SET is_premium = $2, premium_until = $3, premium_badge = $4 WHERE id = $1 AND deleted_at IS NULL`,
		userID, isPremium, premiumUntil, nullableString(premiumBadge),
	)
	if err != nil {
		return fmt.Errorf("update premium: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrUserNotFound
	}
	return nil
}

// GetByUsername returns a non-deleted user by username (case-insensitive).
func (r *PostgresUserRepository) GetByUsername(ctx context.Context, username string) (*domain.User, error) {
	query := `
		SELECT id, COALESCE(auth_provider, ''), COALESCE(provider_uid, ''), role, first_name, last_name, username,
		       email, profile_photo_url,
		       ST_Y(location::geometry) AS latitude, ST_X(location::geometry) AS longitude,
		       COALESCE(created_at, 0), COALESCE(updated_at, 0), deleted_at, phone, COALESCE(phone_verified, false),
		       COALESCE(google_id, ''), COALESCE(apple_id, ''), COALESCE(profile_complete, false),
		       COALESCE(city, ''), COALESCE(state, ''), COALESCE(pincode, ''),
		       is_premium, premium_until, COALESCE(premium_badge, ''), COALESCE(premium_features, '{}'::jsonb),
		       can_authenticate
		FROM users
		WHERE LOWER(username) = LOWER($1) AND deleted_at IS NULL
	`
	user, err := r.scanUser(ctx, query, username)
	if err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			var (
				pubID          uuid.UUID
				pubDisplayName string
				pubUsername    string
				pubAvatarURL   string
			)
			pubQuery := `
				SELECT id, display_name, username, COALESCE(avatar_url, '')
				FROM system_publishers
				WHERE LOWER(username) = LOWER($1) AND is_active = TRUE
			`
			pubErr := database.GetExecutor(ctx, r.pool).QueryRow(ctx, pubQuery, username).Scan(
				&pubID, &pubDisplayName, &pubUsername, &pubAvatarURL,
			)
			if pubErr == nil {
				return &domain.User{
					ID:              pubID,
					AuthProvider:    "system",
					ProviderUID:     pubUsername,
					Role:            authdomain.RoleSystem,
					FirstName:       pubDisplayName,
					LastName:        "",
					Username:        pubUsername,
					ProfilePhotoURL: pubAvatarURL,
					ProfileComplete: true,
					CanAuthenticate: false,
				}, nil
			}
		}
		return nil, err
	}
	return user, nil
}

// GetBadgeTier returns a pandit's current badge tier from pandit_badges table, default 'pandit_ji'.
func (r *PostgresUserRepository) GetBadgeTier(ctx context.Context, userID uuid.UUID) (string, error) {
	var tier string
	query := `SELECT badge_tier::text FROM pandit_badges WHERE pandit_id = $1`
	err := database.GetExecutor(ctx, r.pool).QueryRow(ctx, query, userID).Scan(&tier)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "pandit_ji", nil
		}
		return "", fmt.Errorf("get badge tier: %w", err)
	}
	return tier, nil
}
