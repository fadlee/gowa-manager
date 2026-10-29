package users_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fadlee/gowa-manager/internal/testutil"
	"github.com/fadlee/gowa-manager/internal/users"
)

func newService(t *testing.T, opts users.Options) *users.Service {
	t.Helper()
	db, _ := testutil.TempDB(t)
	svc, err := users.NewService(context.Background(), db.SQL, opts)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return svc
}

func TestSetupFlow(t *testing.T) {
	ctx := context.Background()
	svc := newService(t, users.Options{})
	token := svc.SetupToken()
	if token == "" {
		t.Fatal("expected setup token on empty database")
	}
	if needs, _ := svc.NeedsSetup(ctx); !needs {
		t.Fatal("NeedsSetup = false, want true")
	}
	if _, err := svc.Setup(ctx, "root", "password123", "wrong"); !errors.Is(err, users.ErrInvalidSetupToken) {
		t.Fatalf("wrong token err = %v", err)
	}
	user, err := svc.Setup(ctx, "root", "password123", token)
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if user.Role != users.RoleSuperadmin || !user.IsActive {
		t.Fatalf("first user = %+v, want active superadmin", user)
	}
	if svc.SetupToken() != "" {
		t.Fatal("setup token should be cleared after setup")
	}
	if _, err := svc.Setup(ctx, "other", "password123", token); !errors.Is(err, users.ErrSetupCompleted) {
		t.Fatalf("second setup err = %v, want ErrSetupCompleted", err)
	}
}

func TestSeedFromEnv(t *testing.T) {
	ctx := context.Background()
	svc := newService(t, users.Options{SeedUsername: "admin", SeedPassword: "short"})
	if svc.SetupToken() != "" {
		t.Fatal("seeded service should not require setup")
	}
	user, err := svc.Authenticate(ctx, "ADMIN", "short")
	if err != nil {
		t.Fatalf("Authenticate seeded user: %v", err)
	}
	if !user.IsSuperadmin() {
		t.Fatalf("seeded role = %q", user.Role)
	}
	if _, err := svc.Authenticate(ctx, "admin", "wrong"); !errors.Is(err, users.ErrInvalidCredentials) {
		t.Fatalf("wrong password err = %v", err)
	}
	if _, err := svc.Authenticate(ctx, "nobody", "short"); !errors.Is(err, users.ErrInvalidCredentials) {
		t.Fatalf("unknown user err = %v", err)
	}
}

func TestLastSuperadminGuard(t *testing.T) {
	ctx := context.Background()
	svc := newService(t, users.Options{SeedUsername: "admin", SeedPassword: "password123"})
	admin, _ := svc.Authenticate(ctx, "admin", "password123")
	role := users.RoleUser
	if _, err := svc.Update(ctx, admin.ID, users.UpdateRequest{Role: &role}); !errors.Is(err, users.ErrLastSuperadmin) {
		t.Fatalf("demote last superadmin err = %v", err)
	}
	inactive := false
	if _, err := svc.Update(ctx, admin.ID, users.UpdateRequest{IsActive: &inactive}); !errors.Is(err, users.ErrLastSuperadmin) {
		t.Fatalf("deactivate last superadmin err = %v", err)
	}
	if err := svc.Delete(ctx, admin.ID); !errors.Is(err, users.ErrLastSuperadmin) {
		t.Fatalf("delete last superadmin err = %v", err)
	}
	second, err := svc.Create(ctx, "second", "password123", users.RoleSuperadmin)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := svc.Create(ctx, "SECOND", "password123", users.RoleUser); !errors.Is(err, users.ErrUsernameTaken) {
		t.Fatalf("duplicate username err = %v", err)
	}
	if err := svc.Delete(ctx, admin.ID); err != nil {
		t.Fatalf("delete with another superadmin: %v", err)
	}
	if err := svc.Delete(ctx, second.ID); !errors.Is(err, users.ErrLastSuperadmin) {
		t.Fatalf("delete remaining superadmin err = %v", err)
	}
}

func TestSessionsSlideAndRevoke(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	svc := newService(t, users.Options{SeedUsername: "admin", SeedPassword: "password123", Now: func() time.Time { return now }})
	admin, _ := svc.Authenticate(ctx, "admin", "password123")

	token, _, err := svc.CreateSession(ctx, admin.ID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, refreshed, err := svc.ResolveSession(ctx, token); err != nil || !refreshed.IsZero() {
		t.Fatalf("fresh session: refreshed=%v err=%v", refreshed, err)
	}
	now = now.Add(2 * 24 * time.Hour)
	if _, refreshed, err := svc.ResolveSession(ctx, token); err != nil || !refreshed.Equal(now.Add(users.SessionTTL)) {
		t.Fatalf("aged session: refreshed=%v err=%v", refreshed, err)
	}
	now = now.Add(users.SessionTTL + time.Second)
	if _, _, err := svc.ResolveSession(ctx, token); !errors.Is(err, users.ErrNotFound) {
		t.Fatalf("expired session err = %v", err)
	}

	keep, _, _ := svc.CreateSession(ctx, admin.ID)
	other, _, _ := svc.CreateSession(ctx, admin.ID)
	if err := svc.ChangeOwnPassword(ctx, admin.ID, "wrong-pass", "newpassword1", keep); err == nil {
		t.Fatal("expected wrong current password to fail")
	}
	if err := svc.ChangeOwnPassword(ctx, admin.ID, "password123", "newpassword1", keep); err != nil {
		t.Fatalf("ChangeOwnPassword: %v", err)
	}
	if _, _, err := svc.ResolveSession(ctx, keep); err != nil {
		t.Fatalf("kept session revoked: %v", err)
	}
	if _, _, err := svc.ResolveSession(ctx, other); !errors.Is(err, users.ErrNotFound) {
		t.Fatalf("other session err = %v, want revoked", err)
	}
}

func TestAPIKeys(t *testing.T) {
	ctx := context.Background()
	svc := newService(t, users.Options{SeedUsername: "admin", SeedPassword: "password123"})
	admin, _ := svc.Authenticate(ctx, "admin", "password123")
	member, _ := svc.Create(ctx, "member", "password123", users.RoleUser)

	key, plaintext, err := svc.CreateAPIKey(ctx, member.ID, "ci")
	if err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}
	if len(plaintext) < 40 || plaintext[:len(key.Prefix)] != key.Prefix {
		t.Fatalf("plaintext %q does not start with prefix %q", plaintext, key.Prefix)
	}
	got, err := svc.ResolveAPIKey(ctx, plaintext)
	if err != nil || got.ID != member.ID {
		t.Fatalf("ResolveAPIKey = %+v, %v", got, err)
	}
	keys, _ := svc.ListAPIKeys(ctx, member.ID)
	if len(keys) != 1 || keys[0].LastUsedAt == nil {
		t.Fatalf("keys = %+v, want one key with lastUsedAt", keys)
	}
	if err := svc.RevokeAPIKey(ctx, admin.ID, key.ID); !errors.Is(err, users.ErrNotFound) {
		t.Fatalf("revoking another user's key err = %v", err)
	}

	inactive := false
	if _, err := svc.Update(ctx, member.ID, users.UpdateRequest{IsActive: &inactive}); err != nil {
		t.Fatalf("deactivate: %v", err)
	}
	if _, err := svc.ResolveAPIKey(ctx, plaintext); !errors.Is(err, users.ErrNotFound) {
		t.Fatalf("inactive user's key err = %v", err)
	}
	if err := svc.Delete(ctx, member.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if keys, _ := svc.ListAPIKeys(ctx, member.ID); len(keys) != 0 {
		t.Fatalf("keys after delete = %+v", keys)
	}
}

func TestMagicLinkSecretPersists(t *testing.T) {
	ctx := context.Background()
	svc := newService(t, users.Options{})
	first, err := svc.MagicLinkSecret(ctx)
	if err != nil || first == "" {
		t.Fatalf("MagicLinkSecret = %q, %v", first, err)
	}
	if second, _ := svc.MagicLinkSecret(ctx); second != first {
		t.Fatalf("secret changed: %q != %q", second, first)
	}
}

func TestLoginLimiter(t *testing.T) {
	l := users.NewLoginLimiter(2, time.Minute)
	l.Fail("k")
	if !l.Allowed("k") {
		t.Fatal("one failure should still be allowed")
	}
	l.Fail("k")
	if l.Allowed("k") {
		t.Fatal("two failures should block")
	}
	l.Reset("k")
	if !l.Allowed("k") {
		t.Fatal("reset should unblock")
	}
}
