// Package users owns manager accounts: users and roles, browser
// sessions, API keys, the first-run setup flow, and persisted settings
// such as the magic admin link secret.
package users

import (
	"errors"
	"strings"
	"time"
	"unicode"
)

type Role string

const (
	RoleSuperadmin Role = "superadmin"
	RoleUser       Role = "user"
)

func (r Role) Valid() bool { return r == RoleSuperadmin || r == RoleUser }

type User struct {
	ID        int64     `json:"id"`
	Username  string    `json:"username"`
	Role      Role      `json:"role"`
	IsActive  bool      `json:"isActive"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (u User) IsSuperadmin() bool { return u.Role == RoleSuperadmin }

type APIKey struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	LastUsedAt *time.Time `json:"lastUsedAt"`
	CreatedAt  time.Time  `json:"createdAt"`
}

// ValidationError is returned for invalid client input. Its message is
// safe to show to the caller.
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

var (
	ErrNotFound           = errors.New("not found")
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrUsernameTaken      = errors.New("username already exists")
	ErrSetupCompleted     = errors.New("setup has already been completed")
	ErrInvalidSetupToken  = errors.New("invalid setup token")
	ErrLastSuperadmin     = errors.New("at least one active superadmin must remain")
)

const (
	minPasswordLength = 8
	// bcrypt ignores input beyond 72 bytes; reject it instead of
	// silently truncating.
	maxPasswordBytes  = 72
	maxUsernameLength = 50
	maxAPIKeyName     = 100
)

// NormalizeUsername trims the username and validates its characters.
func NormalizeUsername(username string) (string, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return "", &ValidationError{"Username is required"}
	}
	if len(username) > maxUsernameLength {
		return "", &ValidationError{"Username cannot be longer than 50 characters"}
	}
	for _, r := range username {
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("._-@", r)) {
			return "", &ValidationError{"Username may only contain letters, digits, and . _ - @"}
		}
	}
	return username, nil
}

// ValidatePassword enforces the password policy for passwords chosen
// through the UI, API, or CLI.
func ValidatePassword(password string) error {
	if len(password) < minPasswordLength {
		return &ValidationError{"Password must be at least 8 characters"}
	}
	return validatePasswordMax(password)
}

func validatePasswordMax(password string) error {
	if len(password) > maxPasswordBytes {
		return &ValidationError{"Password cannot be longer than 72 bytes"}
	}
	return nil
}
