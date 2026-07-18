package instances

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fadlee/gowa-manager/internal/database"
)

func TestSQLiteRepositoryListOrdersByCreatedAtDescending(t *testing.T) {
	ctx := context.Background()
	repo, closeDB := newSQLiteRepository(t, ctx)
	defer closeDB()

	first := mustCreateInstance(t, ctx, repo, CreateInput{Key: "FIRST", Name: "first", Port: intPtr(5001), Config: `{"n":1}`, GOWAVersion: "v1"})
	time.Sleep(1100 * time.Millisecond)
	second := mustCreateInstance(t, ctx, repo, CreateInput{Key: "SECOND", Name: "second", Port: intPtr(5002), Config: `{"n":2}`, GOWAVersion: "v2"})

	instances, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(instances) != 2 {
		t.Fatalf("List() returned %d instances, want 2: %#v", len(instances), instances)
	}
	if instances[0].ID != second.ID || instances[1].ID != first.ID {
		t.Fatalf("List() order IDs = [%d, %d], want [%d, %d]", instances[0].ID, instances[1].ID, second.ID, first.ID)
	}
}

func TestSQLiteRepositoryFindByIDAndKey(t *testing.T) {
	ctx := context.Background()
	repo, closeDB := newSQLiteRepository(t, ctx)
	defer closeDB()

	created := mustCreateInstance(t, ctx, repo, CreateInput{Key: "LOOKUP", Name: "lookup", Port: intPtr(5010), Config: `{"lookup":true}`, GOWAVersion: "latest"})

	byID, err := repo.FindByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("FindByID() error = %v", err)
	}
	assertSameInstance(t, byID, created)

	byKey, err := repo.FindByKey(ctx, created.Key)
	if err != nil {
		t.Fatalf("FindByKey() error = %v", err)
	}
	assertSameInstance(t, byKey, created)

	if _, err := repo.FindByID(ctx, created.ID+1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("FindByID() missing error = %v, want ErrNotFound", err)
	}
	if _, err := repo.FindByKey(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("FindByKey() missing error = %v, want ErrNotFound", err)
	}
}

func TestSQLiteRepositoryCreateReturnsRowAndUniqueConflicts(t *testing.T) {
	ctx := context.Background()
	repo, closeDB := newSQLiteRepository(t, ctx)
	defer closeDB()

	created := mustCreateInstance(t, ctx, repo, CreateInput{Key: "CREATE", Name: "create", Port: intPtr(5020), Config: `{"created":true}`, GOWAVersion: "v-create"})
	if created.ID == 0 || created.CreatedAt == "" || created.UpdatedAt == "" {
		t.Fatalf("Create() did not return generated fields: %#v", created)
	}
	if created.Key != "CREATE" || created.Name != "create" || created.Port == nil || *created.Port != 5020 || created.Status != "stopped" || created.Config != `{"created":true}` || created.GOWAVersion != "v-create" || created.ErrorMessage != nil {
		t.Fatalf("Create() returned unexpected instance: %#v", created)
	}

	if _, err := repo.Create(ctx, CreateInput{Key: "CREATE", Name: "other", Config: `{}`, GOWAVersion: "latest"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("Create() duplicate key error = %v, want ErrConflict", err)
	}
	if _, err := repo.Create(ctx, CreateInput{Key: "OTHER", Name: "create", Config: `{}`, GOWAVersion: "latest"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("Create() duplicate name error = %v, want ErrConflict", err)
	}
}

func TestSQLiteRepositoryUpdatePreservesKeyAndPort(t *testing.T) {
	ctx := context.Background()
	repo, closeDB := newSQLiteRepository(t, ctx)
	defer closeDB()

	created := mustCreateInstance(t, ctx, repo, CreateInput{Key: "UPDATE", Name: "before", Port: intPtr(5030), Config: `{"before":true}`, GOWAVersion: "old"})
	updated, err := repo.Update(ctx, UpdateInput{ID: created.ID, Name: "after", Config: `{"after":true}`, GOWAVersion: "new"})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.ID != created.ID || updated.Key != created.Key || updated.Port == nil || *updated.Port != 5030 {
		t.Fatalf("Update() changed immutable fields: created=%#v updated=%#v", created, updated)
	}
	if updated.Name != "after" || updated.Config != `{"after":true}` || updated.GOWAVersion != "new" {
		t.Fatalf("Update() returned unexpected mutable fields: %#v", updated)
	}
	if _, err := repo.Update(ctx, UpdateInput{ID: created.ID + 1, Name: "missing", Config: `{}`, GOWAVersion: "latest"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Update() missing error = %v, want ErrNotFound", err)
	}
}

func TestSQLiteRepositoryStatusErrorPortAndDelete(t *testing.T) {
	ctx := context.Background()
	repo, closeDB := newSQLiteRepository(t, ctx)
	defer closeDB()

	created := mustCreateInstance(t, ctx, repo, CreateInput{Key: "MUTATE", Name: "mutate", Config: `{}`, GOWAVersion: "latest"})
	message := "failed to start"
	withError, err := repo.UpdateStatus(ctx, created.ID, "error", &message)
	if err != nil {
		t.Fatalf("UpdateStatus() error = %v", err)
	}
	if withError.Status != "error" || withError.ErrorMessage == nil || *withError.ErrorMessage != message {
		t.Fatalf("UpdateStatus() returned unexpected instance: %#v", withError)
	}

	cleared, err := repo.ClearError(ctx, created.ID)
	if err != nil {
		t.Fatalf("ClearError() error = %v", err)
	}
	if cleared.ErrorMessage != nil {
		t.Fatalf("ClearError() error message = %q, want nil", *cleared.ErrorMessage)
	}

	if err := repo.UpdatePort(ctx, created.ID, intPtr(5040)); err != nil {
		t.Fatalf("UpdatePort() set error = %v", err)
	}
	withPort, err := repo.FindByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("FindByID() after UpdatePort error = %v", err)
	}
	if withPort.Port == nil || *withPort.Port != 5040 {
		t.Fatalf("UpdatePort() port = %#v, want 5040", withPort.Port)
	}
	if err := repo.UpdatePort(ctx, created.ID, nil); err != nil {
		t.Fatalf("UpdatePort() clear error = %v", err)
	}
	withoutPort, err := repo.FindByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("FindByID() after clear port error = %v", err)
	}
	if withoutPort.Port != nil {
		t.Fatalf("UpdatePort() cleared port = %#v, want nil", *withoutPort.Port)
	}

	if err := repo.Delete(ctx, created.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := repo.FindByID(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("FindByID() after Delete error = %v, want ErrNotFound", err)
	}
	if err := repo.UpdatePort(ctx, created.ID, intPtr(5041)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdatePort() missing error = %v, want ErrNotFound", err)
	}
	if err := repo.Delete(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Delete() missing error = %v, want ErrNotFound", err)
	}
}

func newSQLiteRepository(t *testing.T, ctx context.Context) (Repository, func()) {
	t.Helper()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("database.Open() error = %v", err)
	}
	return NewSQLiteRepository(db.SQL), func() {
		if err := db.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}
}

func mustCreateInstance(t *testing.T, ctx context.Context, repo Repository, input CreateInput) Instance {
	t.Helper()
	instance, err := repo.Create(ctx, input)
	if err != nil {
		t.Fatalf("Create(%#v) error = %v", input, err)
	}
	return instance
}

func assertSameInstance(t *testing.T, got, want Instance) {
	t.Helper()
	if got.ID != want.ID || got.Key != want.Key || got.Name != want.Name || got.Status != want.Status || got.Config != want.Config || got.GOWAVersion != want.GOWAVersion || got.CreatedAt != want.CreatedAt || got.UpdatedAt != want.UpdatedAt {
		t.Fatalf("instance mismatch got=%#v want=%#v", got, want)
	}
	if (got.Port == nil) != (want.Port == nil) || (got.Port != nil && *got.Port != *want.Port) {
		t.Fatalf("port mismatch got=%#v want=%#v", got.Port, want.Port)
	}
	if (got.ErrorMessage == nil) != (want.ErrorMessage == nil) || (got.ErrorMessage != nil && *got.ErrorMessage != *want.ErrorMessage) {
		t.Fatalf("error message mismatch got=%#v want=%#v", got.ErrorMessage, want.ErrorMessage)
	}
}

func intPtr(value int) *int {
	return &value
}
