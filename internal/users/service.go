package users

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	SessionTTL = 30 * 24 * time.Hour
	// sessionRefreshAfter limits sliding-expiry writes to once per day
	// per session.
	sessionRefreshAfter = 24 * time.Hour
	// apiKeyTouchInterval limits last_used_at writes per API key.
	apiKeyTouchInterval = time.Minute
	APIKeyPrefix        = "gowa_"
	magicSecretKey      = "magic_link_secret"
)

type Options struct {
	// SeedUsername and SeedPassword create the first superadmin when the
	// users table is empty (from ADMIN_USERNAME/ADMIN_PASSWORD). Both must
	// be set; they are ignored once any user exists.
	SeedUsername string
	SeedPassword string
	Logger       *slog.Logger
	Now          func() time.Time
}

type Service struct {
	db  *sql.DB
	now func() time.Time

	mu         sync.Mutex
	setupToken string
}

// NewService seeds the first superadmin from Options when possible and,
// if no users exist afterwards, generates a one-time setup token that
// must be presented to register the first account.
func NewService(ctx context.Context, db *sql.DB, opts Options) (*Service, error) {
	s := &Service{db: db, now: opts.Now}
	if s.now == nil {
		s.now = time.Now
	}
	count, err := s.countUsers(ctx, db)
	if err != nil {
		return nil, err
	}
	if count == 0 && opts.SeedUsername != "" && opts.SeedPassword != "" {
		if err := s.seed(ctx, opts.SeedUsername, opts.SeedPassword); err != nil {
			return nil, err
		}
		if opts.Logger != nil {
			opts.Logger.Info("created superadmin from ADMIN_USERNAME/ADMIN_PASSWORD", "username", opts.SeedUsername)
		}
		return s, nil
	}
	if count == 0 {
		s.setupToken = randomToken(18)
	}
	return s, nil
}

func (s *Service) seed(ctx context.Context, username, password string) error {
	username, err := NormalizeUsername(username)
	if err != nil {
		return err
	}
	// Seeded passwords predate the length policy, so only the bcrypt
	// limit applies here.
	if err := validatePasswordMax(password); err != nil {
		return err
	}
	_, err = s.insertUser(ctx, s.db, username, password, RoleSuperadmin)
	return err
}

// SetupToken returns the pending first-run setup token, or "" when setup
// is not required.
func (s *Service) SetupToken() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.setupToken
}

func (s *Service) NeedsSetup(ctx context.Context) (bool, error) {
	count, err := s.countUsers(ctx, s.db)
	return count == 0, err
}

// Setup registers the first user as superadmin. It fails once any user
// exists or when the setup token does not match.
func (s *Service) Setup(ctx context.Context, username, password, token string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback()
	count, err := s.countUsers(ctx, tx)
	if err != nil {
		return User{}, err
	}
	if count > 0 || s.setupToken == "" {
		return User{}, ErrSetupCompleted
	}
	if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(token)), []byte(s.setupToken)) != 1 {
		return User{}, ErrInvalidSetupToken
	}
	if err := ValidatePassword(password); err != nil {
		return User{}, err
	}
	username, err = NormalizeUsername(username)
	if err != nil {
		return User{}, err
	}
	user, err := s.insertUser(ctx, tx, username, password, RoleSuperadmin)
	if err != nil {
		return User{}, err
	}
	if err := tx.Commit(); err != nil {
		return User{}, err
	}
	s.setupToken = ""
	return user, nil
}

// Authenticate checks a username/password pair against active users.
func (s *Service) Authenticate(ctx context.Context, username, password string) (User, error) {
	var hash string
	user, err := scanUser(s.db.QueryRowContext(ctx, `SELECT `+userColumns+`, password_hash FROM users WHERE username = ?`, strings.TrimSpace(username)), &hash)
	if errors.Is(err, ErrNotFound) {
		// Spend comparable time so unknown usernames are not observable.
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return User{}, ErrInvalidCredentials
	}
	if err != nil {
		return User{}, err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil || !user.IsActive {
		return User{}, ErrInvalidCredentials
	}
	return user, nil
}

var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("gowa-manager-dummy-password"), bcrypt.DefaultCost)

// ---- Users (superadmin management) ----

func (s *Service) List(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+userColumns+` FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []User{}
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, user)
	}
	return list, rows.Err()
}

func (s *Service) Get(ctx context.Context, id int64) (User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id = ?`, id))
}

func (s *Service) Create(ctx context.Context, username, password string, role Role) (User, error) {
	username, err := NormalizeUsername(username)
	if err != nil {
		return User{}, err
	}
	if err := ValidatePassword(password); err != nil {
		return User{}, err
	}
	if !role.Valid() {
		return User{}, &ValidationError{"Role must be superadmin or user"}
	}
	return s.insertUser(ctx, s.db, username, password, role)
}

type UpdateRequest struct {
	Role     *Role
	IsActive *bool
}

// Update changes a user's role and/or active flag. Deactivating a user
// revokes their sessions; the last active superadmin cannot be demoted
// or deactivated.
func (s *Service) Update(ctx context.Context, id int64, req UpdateRequest) (User, error) {
	if req.Role != nil && !req.Role.Valid() {
		return User{}, &ValidationError{"Role must be superadmin or user"}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback()
	user, err := scanUser(tx.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id = ?`, id))
	if err != nil {
		return User{}, err
	}
	role, active := user.Role, user.IsActive
	if req.Role != nil {
		role = *req.Role
	}
	if req.IsActive != nil {
		active = *req.IsActive
	}
	if user.IsSuperadmin() && user.IsActive && (role != RoleSuperadmin || !active) {
		if err := ensureOtherSuperadmin(ctx, tx, id); err != nil {
			return User{}, err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE users SET role = ?, is_active = ?, updated_at = ? WHERE id = ?`, role, active, s.now().UTC(), id); err != nil {
		return User{}, err
	}
	if !active {
		if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, id); err != nil {
			return User{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return User{}, err
	}
	return s.Get(ctx, id)
}

func (s *Service) Delete(ctx context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	user, err := scanUser(tx.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id = ?`, id))
	if err != nil {
		return err
	}
	if user.IsSuperadmin() && user.IsActive {
		if err := ensureOtherSuperadmin(ctx, tx, id); err != nil {
			return err
		}
	}
	for _, stmt := range []string{`DELETE FROM sessions WHERE user_id = ?`, `DELETE FROM api_keys WHERE user_id = ?`, `DELETE FROM users WHERE id = ?`} {
		if _, err := tx.ExecContext(ctx, stmt, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func ensureOtherSuperadmin(ctx context.Context, tx *sql.Tx, excludeID int64) error {
	var others int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE role = ? AND is_active = 1 AND id != ?`, RoleSuperadmin, excludeID).Scan(&others); err != nil {
		return err
	}
	if others == 0 {
		return ErrLastSuperadmin
	}
	return nil
}

// SetPassword replaces a user's password and revokes all their
// sessions except keepSessionToken (pass "" to revoke every session).
func (s *Service) SetPassword(ctx context.Context, id int64, password, keepSessionToken string) error {
	if err := ValidatePassword(password); err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ?`, string(hash), s.now().UTC(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ? AND token_hash != ?`, id, hashToken(keepSessionToken)); err != nil {
		return err
	}
	return tx.Commit()
}

// ChangeOwnPassword verifies the current password before replacing it.
func (s *Service) ChangeOwnPassword(ctx context.Context, id int64, current, next, keepSessionToken string) error {
	var hash string
	if err := s.db.QueryRowContext(ctx, `SELECT password_hash FROM users WHERE id = ?`, id).Scan(&hash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(current)) != nil {
		return &ValidationError{"Current password is incorrect"}
	}
	return s.SetPassword(ctx, id, next, keepSessionToken)
}

// ResetPasswordByUsername is used by the reset-password CLI command.
func (s *Service) ResetPasswordByUsername(ctx context.Context, username, password string) (User, error) {
	user, err := scanUser(s.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE username = ?`, strings.TrimSpace(username)))
	if err != nil {
		return User{}, err
	}
	return user, s.SetPassword(ctx, user.ID, password, "")
}

// ---- Sessions ----

// CreateSession issues a new session token for the user.
func (s *Service) CreateSession(ctx context.Context, userID int64) (token string, expiresAt time.Time, err error) {
	token = randomToken(32)
	expiresAt = s.now().UTC().Add(SessionTTL)
	_, err = s.db.ExecContext(ctx, `INSERT INTO sessions (token_hash, user_id, expires_at) VALUES (?, ?, ?)`, hashToken(token), userID, expiresAt)
	return token, expiresAt, err
}

// ResolveSession returns the active user owning the session. When the
// sliding expiry was extended, refreshedUntil is non-zero so the caller
// can re-issue the cookie.
func (s *Service) ResolveSession(ctx context.Context, token string) (user User, refreshedUntil time.Time, err error) {
	if token == "" {
		return User{}, time.Time{}, ErrNotFound
	}
	now := s.now().UTC()
	var expiresAt time.Time
	user, err = scanUser(s.db.QueryRowContext(ctx, `
SELECT `+prefixedUserColumns+`, s.expires_at FROM sessions s JOIN users u ON u.id = s.user_id
WHERE s.token_hash = ? AND u.is_active = 1`, hashToken(token)), &expiresAt)
	if err != nil {
		return User{}, time.Time{}, err
	}
	if !expiresAt.After(now) {
		_, _ = s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, hashToken(token))
		return User{}, time.Time{}, ErrNotFound
	}
	if expiresAt.Sub(now) < SessionTTL-sessionRefreshAfter {
		refreshedUntil = now.Add(SessionTTL)
		if _, err := s.db.ExecContext(ctx, `UPDATE sessions SET expires_at = ? WHERE token_hash = ?`, refreshedUntil, hashToken(token)); err != nil {
			return User{}, time.Time{}, err
		}
	}
	return user, refreshedUntil, nil
}

func (s *Service) DeleteSession(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, hashToken(token))
	return err
}

// PruneExpiredSessions removes sessions past their expiry.
func (s *Service) PruneExpiredSessions(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, s.now().UTC())
	return err
}

// ---- API keys ----

// CreateAPIKey returns the stored key metadata and the plaintext key,
// which is only available at creation time.
func (s *Service) CreateAPIKey(ctx context.Context, userID int64, name string) (APIKey, string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return APIKey{}, "", &ValidationError{"API key name is required"}
	}
	if len(name) > maxAPIKeyName {
		return APIKey{}, "", &ValidationError{"API key name cannot be longer than 100 characters"}
	}
	plaintext := APIKeyPrefix + randomToken(32)
	prefix := plaintext[:len(APIKeyPrefix)+6]
	var key APIKey
	err := s.db.QueryRowContext(ctx, `
INSERT INTO api_keys (user_id, name, prefix, key_hash, created_at) VALUES (?, ?, ?, ?, ?)
RETURNING id, name, prefix, last_used_at, created_at`, userID, name, prefix, hashToken(plaintext), s.now().UTC()).
		Scan(&key.ID, &key.Name, &key.Prefix, nullTime{&key.LastUsedAt}, &key.CreatedAt)
	if err != nil {
		return APIKey{}, "", err
	}
	return key, plaintext, nil
}

func (s *Service) ListAPIKeys(ctx context.Context, userID int64) ([]APIKey, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, prefix, last_used_at, created_at FROM api_keys WHERE user_id = ? ORDER BY id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	keys := []APIKey{}
	for rows.Next() {
		var key APIKey
		if err := rows.Scan(&key.ID, &key.Name, &key.Prefix, nullTime{&key.LastUsedAt}, &key.CreatedAt); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

func (s *Service) RevokeAPIKey(ctx context.Context, userID, keyID int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM api_keys WHERE id = ? AND user_id = ?`, keyID, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ResolveAPIKey returns the active user owning the key and records its
// last use.
func (s *Service) ResolveAPIKey(ctx context.Context, plaintext string) (User, error) {
	if !strings.HasPrefix(plaintext, APIKeyPrefix) {
		return User{}, ErrNotFound
	}
	var keyID int64
	user, err := scanUser(s.db.QueryRowContext(ctx, `
SELECT `+prefixedUserColumns+`, k.id FROM api_keys k JOIN users u ON u.id = k.user_id
WHERE k.key_hash = ? AND u.is_active = 1`, hashToken(plaintext)), &keyID)
	if err != nil {
		return User{}, err
	}
	now := s.now().UTC()
	_, _ = s.db.ExecContext(ctx, `UPDATE api_keys SET last_used_at = ? WHERE id = ? AND (last_used_at IS NULL OR last_used_at < ?)`, now, keyID, now.Add(-apiKeyTouchInterval))
	return user, nil
}

// ---- Settings ----

// MagicLinkSecret returns the persisted magic admin link secret,
// generating it on first use.
func (s *Service) MagicLinkSecret(ctx context.Context) (string, error) {
	if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO settings (key, value) VALUES (?, ?)`, magicSecretKey, randomToken(32)); err != nil {
		return "", err
	}
	var secret string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, magicSecretKey).Scan(&secret)
	return secret, err
}

// ---- helpers ----

type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func (s *Service) countUsers(ctx context.Context, q queryer) (int, error) {
	var count int
	err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count)
	return count, err
}

func (s *Service) insertUser(ctx context.Context, q queryer, username, password string, role Role) (User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, err
	}
	now := s.now().UTC()
	user, err := scanUser(q.QueryRowContext(ctx, `
INSERT INTO users (username, password_hash, role, is_active, created_at, updated_at) VALUES (?, ?, ?, 1, ?, ?)
RETURNING `+userColumns, username, string(hash), role, now, now))
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "unique") {
		return User{}, ErrUsernameTaken
	}
	return user, err
}

const userColumns = `id, username, role, is_active, created_at, updated_at`
const prefixedUserColumns = `u.id, u.username, u.role, u.is_active, u.created_at, u.updated_at`

type scanner interface{ Scan(dest ...any) error }

func scanUser(row scanner, extra ...any) (User, error) {
	var user User
	dest := append([]any{&user.ID, &user.Username, &user.Role, &user.IsActive, &user.CreatedAt, &user.UpdatedAt}, extra...)
	if err := row.Scan(dest...); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, ErrNotFound
		}
		return User{}, err
	}
	return user, nil
}

type nullTime struct{ dest **time.Time }

func (n nullTime) Scan(value any) error {
	var t sql.NullTime
	if err := t.Scan(value); err != nil {
		return err
	}
	if t.Valid {
		*n.dest = &t.Time
	} else {
		*n.dest = nil
	}
	return nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func randomToken(bytes int) string {
	b := make([]byte, bytes)
	if _, err := rand.Read(b); err != nil {
		panic("users: crypto/rand failed: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// RandomPassword returns a random password that satisfies the policy.
func RandomPassword() string { return randomToken(15) }
