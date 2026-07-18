package instances

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestServiceCreateGeneratesNameKeyPortConfigVersionAndDirectory(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	fs := &fakeFilesystem{}
	ports := &fakePortAllocator{next: 6123}
	service := NewService(repo, fs, ports, nil)
	service.generateKey = func() (string, error) { return "KEY12345", nil }
	service.generateName = func() string { return "generated-name" }

	created, err := service.Create(ctx, CreateRequest{Config: stringPtr(`{"flags":{"os":"Custom","basePath":"/ignored"},"env":{"A":"B"}}`)})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.Key != "KEY12345" || created.Name != "generated-name" {
		t.Fatalf("Create() key/name = %q/%q", created.Key, created.Name)
	}
	if created.Port == nil || *created.Port != 6123 {
		t.Fatalf("Create() port = %#v, want 6123", created.Port)
	}
	if created.GOWAVersion != "latest" {
		t.Fatalf("Create() GOWAVersion = %q, want latest", created.GOWAVersion)
	}
	config := ParseConfig(created.Config)
	if config.Flags.BasePath != "/app/KEY12345" || config.Flags.OS != "Custom" || config.Env == nil {
		t.Fatalf("Create() config = %s", created.Config)
	}
	if !reflect.DeepEqual(fs.ensured, []int64{created.ID}) {
		t.Fatalf("Ensure calls = %#v, want [%d]", fs.ensured, created.ID)
	}
	if ports.calls != 1 {
		t.Fatalf("Next() calls = %d, want 1", ports.calls)
	}
}

func TestServiceCreateFallsBackToDefaultConfigWhenUserConfigIsInvalid(t *testing.T) {
	ctx := context.Background()
	service := newTestService()
	service.generateKey = func() (string, error) { return "BADCFG01", nil }

	created, err := service.Create(ctx, CreateRequest{Name: "bad-config", Config: stringPtr(`{bad json`), GOWAVersion: "v1.0.0"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	config := ParseConfig(created.Config)
	if config.Flags.AccountValidation == nil || !*config.Flags.AccountValidation || config.Flags.OS != "GowaManager" || config.Flags.BasePath != "/app/BADCFG01" {
		t.Fatalf("Create() default config = %s", created.Config)
	}
	if created.GOWAVersion != "v1.0.0" {
		t.Fatalf("Create() GOWAVersion = %q", created.GOWAVersion)
	}
}

func TestServiceCreateMapsConflictAndCompensatesFilesystem(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	service := NewService(repo, &fakeFilesystem{}, &fakePortAllocator{next: 7000}, nil)
	service.generateKey = func() (string, error) { return "DUPKEY01", nil }
	if _, err := service.Create(ctx, CreateRequest{Name: "same"}); err != nil {
		t.Fatalf("Create() seed error = %v", err)
	}
	service.generateKey = func() (string, error) { return "DUPKEY02", nil }
	if _, err := service.Create(ctx, CreateRequest{Name: "same"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("Create() duplicate error = %v, want ErrConflict", err)
	}

	repo = newFakeRepository()
	fs := &fakeFilesystem{ensureErr: errors.New("disk full")}
	service = NewService(repo, fs, &fakePortAllocator{next: 7001}, nil)
	service.generateKey = func() (string, error) { return "FSFAIL01", nil }
	if _, err := service.Create(ctx, CreateRequest{Name: "fs-fail"}); !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("Create() filesystem error = %v", err)
	}
	if len(repo.items) != 0 || !reflect.DeepEqual(repo.deleted, []int64{1}) {
		t.Fatalf("Create() did not compensate DB row: items=%#v deleted=%#v", repo.items, repo.deleted)
	}

	repo = newFakeRepository()
	repo.createErr = errors.New("db down")
	fs = &fakeFilesystem{}
	service = NewService(repo, fs, &fakePortAllocator{next: 7002}, nil)
	service.generateKey = func() (string, error) { return "DBFAIL01", nil }
	if _, err := service.Create(ctx, CreateRequest{Name: "db-fail"}); !strings.Contains(err.Error(), "db down") {
		t.Fatalf("Create() db error = %v", err)
	}
	if len(fs.ensured) != 0 || len(fs.purged) != 0 {
		t.Fatalf("Create() touched filesystem after DB failure: ensured=%#v purged=%#v", fs.ensured, fs.purged)
	}
}

func TestServiceCreateReturnsPortAllocatorFailureBeforeDBOrFilesystem(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	fs := &fakeFilesystem{}
	ports := &fakePortAllocator{err: errors.New("no ports")}
	service := NewService(repo, fs, ports, nil)
	service.generateKey = func() (string, error) { return "NOPORT01", nil }

	if _, err := service.Create(ctx, CreateRequest{Name: "no-port"}); !strings.Contains(err.Error(), "no ports") {
		t.Fatalf("Create() port error = %v", err)
	}
	if len(repo.items) != 0 || len(fs.ensured) != 0 || ports.calls != 1 {
		t.Fatalf("Create() side effects after port failure: items=%#v ensured=%#v calls=%d", repo.items, fs.ensured, ports.calls)
	}
}

func TestServiceUpdatePreservesKeyPortAndDefaults(t *testing.T) {
	ctx := context.Background()
	service := newTestService()
	service.generateKey = func() (string, error) { return "UPDKEY01", nil }
	created, err := service.Create(ctx, CreateRequest{Name: "before", Config: stringPtr(`{"flags":{"basePath":"/wrong","debug":true}}`)})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	nextConfig := `{"flags":{"basePath":"/still-wrong","os":"Updated"}}`
	updated, err := service.Update(ctx, created.ID, UpdateRequest{Config: &nextConfig})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.Key != created.Key || updated.Port == nil || *updated.Port != *created.Port {
		t.Fatalf("Update() changed key/port: created=%#v updated=%#v", created, updated)
	}
	if updated.Name != created.Name || updated.GOWAVersion != created.GOWAVersion {
		t.Fatalf("Update() defaults = name %q version %q", updated.Name, updated.GOWAVersion)
	}
	if ParseConfig(updated.Config).Flags.BasePath != "/app/UPDKEY01" {
		t.Fatalf("Update() config = %s", updated.Config)
	}
}

func TestServiceDeleteStoppedStagesDeletesPurgesAndRestoresOnRepoFailure(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	fs := &fakeFilesystem{}
	service := NewService(repo, fs, &fakePortAllocator{next: 7100}, nil)
	service.generateKey = func() (string, error) { return "DELETE01", nil }
	created, err := service.Create(ctx, CreateRequest{Name: "delete"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := service.Delete(ctx, created.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if !reflect.DeepEqual(fs.staged, []int64{created.ID}) || len(fs.purged) != 1 || len(repo.items) != 0 {
		t.Fatalf("Delete() calls staged=%#v purged=%#v items=%#v", fs.staged, fs.purged, repo.items)
	}
	if err := service.Delete(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Delete() missing error = %v, want ErrNotFound", err)
	}

	repo = newFakeRepository()
	fs = &fakeFilesystem{}
	service = NewService(repo, fs, &fakePortAllocator{next: 7101}, nil)
	service.generateKey = func() (string, error) { return "DELFAIL1", nil }
	created, _ = service.Create(ctx, CreateRequest{Name: "delete-fail"})
	repo.deleteErr = errors.New("delete failed")
	if err := service.Delete(ctx, created.ID); !strings.Contains(err.Error(), "delete failed") {
		t.Fatalf("Delete() repo error = %v", err)
	}
	if len(fs.restored) != 1 || len(fs.purged) != 0 {
		t.Fatalf("Delete() compensation restored=%#v purged=%#v", fs.restored, fs.purged)
	}
}

func TestServiceDeleteReturnsStageAndPurgeFailures(t *testing.T) {
	ctx := context.Background()

	repo := newFakeRepository()
	fs := &fakeFilesystem{stageErr: errors.New("stage failed")}
	service := NewService(repo, fs, &fakePortAllocator{next: 7110}, nil)
	service.generateKey = func() (string, error) { return "DELSTG01", nil }
	created, err := service.Create(ctx, CreateRequest{Name: "delete-stage-fail"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := service.Delete(ctx, created.ID); !strings.Contains(err.Error(), "stage failed") {
		t.Fatalf("Delete() stage error = %v", err)
	}
	if len(repo.deleted) != 0 || len(fs.purged) != 0 || len(fs.restored) != 0 {
		t.Fatalf("Delete() side effects after stage failure deleted=%#v purged=%#v restored=%#v", repo.deleted, fs.purged, fs.restored)
	}

	repo = newFakeRepository()
	fs = &fakeFilesystem{purgeErr: errors.New("purge failed")}
	service = NewService(repo, fs, &fakePortAllocator{next: 7111}, nil)
	service.generateKey = func() (string, error) { return "DELPRG01", nil }
	created, err = service.Create(ctx, CreateRequest{Name: "delete-purge-fail"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := service.Delete(ctx, created.ID); !strings.Contains(err.Error(), "purge failed") {
		t.Fatalf("Delete() purge error = %v", err)
	}
	if len(repo.items) != 0 || len(fs.purged) != 1 || len(fs.restored) != 0 {
		t.Fatalf("Delete() purge failure commit state items=%#v purged=%#v restored=%#v", repo.items, fs.purged, fs.restored)
	}
}

func TestServiceDeleteReturnsRepositoryAndRestoreFailures(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	fs := &fakeFilesystem{restoreErr: errors.New("restore failed")}
	service := NewService(repo, fs, &fakePortAllocator{next: 7112}, nil)
	service.generateKey = func() (string, error) { return "DELRST01", nil }
	created, err := service.Create(ctx, CreateRequest{Name: "delete-restore-fail"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	repo.deleteErr = errors.New("delete failed")

	err = service.Delete(ctx, created.ID)
	if err == nil || !strings.Contains(err.Error(), "delete failed") || !strings.Contains(err.Error(), "restore failed") {
		t.Fatalf("Delete() joined error = %v", err)
	}
	if len(fs.restored) != 1 || len(fs.purged) != 0 {
		t.Fatalf("Delete() restore failure restored=%#v purged=%#v", fs.restored, fs.purged)
	}
}

func TestServiceDeleteTreatsMissingDirectoryAsAbsent(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	fs := &fakeFilesystem{stageErr: ErrNotFound}
	service := NewService(repo, fs, &fakePortAllocator{next: 7113}, nil)
	created := mustFakeCreate(t, ctx, repo, CreateInput{Key: "DELMISS1", Name: "delete-missing", Config: `{}`, GOWAVersion: "latest"})

	if err := service.Delete(ctx, created.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, ok := repo.items[created.ID]; ok || !reflect.DeepEqual(repo.deleted, []int64{created.ID}) {
		t.Fatalf("Delete() missing dir repo state items=%#v deleted=%#v", repo.items, repo.deleted)
	}
	if len(fs.purged) != 0 || len(fs.restored) != 0 {
		t.Fatalf("Delete() missing dir filesystem purged=%#v restored=%#v", fs.purged, fs.restored)
	}
}

func TestServiceDeleteRunningTreatsMissingDirectoryAsAbsent(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	fs := &fakeFilesystem{stageErr: ErrNotFound}
	lifecycle := &fakeLifecycle{status: Status{State: "running"}}
	service := NewService(repo, fs, &fakePortAllocator{next: 7114}, lifecycle)
	created := mustFakeCreate(t, ctx, repo, CreateInput{Key: "DELRUNM1", Name: "delete-running-missing", Config: `{}`, GOWAVersion: "latest"})
	repo.items[created.ID] = withStatus(created, "running", nil)

	if err := service.Delete(ctx, created.ID); err != nil {
		t.Fatalf("Delete() running missing dir error = %v", err)
	}
	if !reflect.DeepEqual(lifecycle.stopped, []int64{created.ID}) {
		t.Fatalf("Stop calls = %#v, want [%d]", lifecycle.stopped, created.ID)
	}
	if _, ok := repo.items[created.ID]; ok || !reflect.DeepEqual(repo.deleted, []int64{created.ID}) {
		t.Fatalf("Delete() running missing dir repo state items=%#v deleted=%#v", repo.items, repo.deleted)
	}
	if !reflect.DeepEqual(fs.staged, []int64{created.ID}) || len(fs.purged) != 0 || len(fs.restored) != 0 {
		t.Fatalf("Delete() running missing dir filesystem staged=%#v purged=%#v restored=%#v", fs.staged, fs.purged, fs.restored)
	}
}

func TestServiceResetStoppedStagesUpdatesStatusEnsuresAndPurges(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	fs := &fakeFilesystem{}
	service := NewService(repo, fs, &fakePortAllocator{next: 7200}, nil)
	service.generateKey = func() (string, error) { return "RESET001", nil }
	created, err := service.Create(ctx, CreateRequest{Name: "reset"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	repo.items[created.ID] = withStatus(repo.items[created.ID], "error", stringPtr("boom"))
	fs.ensured = nil
	fs.events = nil

	if err := service.ResetData(ctx, created.ID); err != nil {
		t.Fatalf("ResetData() error = %v", err)
	}
	got := repo.items[created.ID]
	if got.Status != "stopped" || got.ErrorMessage != nil {
		t.Fatalf("ResetData() status/error = %#v", got)
	}
	if repo.clearErrorCalls != 0 {
		t.Fatalf("ResetData() ClearError calls = %d, want 0", repo.clearErrorCalls)
	}
	if !reflect.DeepEqual(fs.staged, []int64{created.ID}) || len(fs.purged) != 1 || !reflect.DeepEqual(fs.ensured, []int64{created.ID}) {
		t.Fatalf("ResetData() filesystem staged=%#v purged=%#v ensured=%#v", fs.staged, fs.purged, fs.ensured)
	}
	wantEvents := []string{"stage", "ensure", "purge"}
	if !reflect.DeepEqual(fs.events, wantEvents) {
		t.Fatalf("ResetData() filesystem events = %#v, want %#v", fs.events, wantEvents)
	}
	if err := service.ResetData(ctx, created.ID+1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ResetData() missing error = %v, want ErrNotFound", err)
	}
}

func TestServiceResetTreatsMissingDirectoryAsAbsent(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	fs := &fakeFilesystem{stageErr: ErrNotFound}
	service := NewService(repo, fs, &fakePortAllocator{next: 7201}, nil)
	created := mustFakeCreate(t, ctx, repo, CreateInput{Key: "RSTMISS1", Name: "reset-missing", Config: `{}`, GOWAVersion: "latest"})
	repo.items[created.ID] = withStatus(created, "error", stringPtr("boom"))

	if err := service.ResetData(ctx, created.ID); err != nil {
		t.Fatalf("ResetData() error = %v", err)
	}
	got := repo.items[created.ID]
	if got.Status != "stopped" || got.ErrorMessage != nil {
		t.Fatalf("ResetData() missing dir status/error = %#v", got)
	}
	if !reflect.DeepEqual(fs.ensured, []int64{created.ID}) || len(fs.purged) != 0 || len(fs.restored) != 0 {
		t.Fatalf("ResetData() missing dir filesystem ensured=%#v purged=%#v restored=%#v", fs.ensured, fs.purged, fs.restored)
	}
}

func TestServiceResetRunningTreatsMissingDirectoryAsAbsent(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	fs := &fakeFilesystem{stageErr: ErrNotFound}
	lifecycle := &fakeLifecycle{status: Status{State: "running"}}
	service := NewService(repo, fs, &fakePortAllocator{next: 7202}, lifecycle)
	created := mustFakeCreate(t, ctx, repo, CreateInput{Key: "RSTRUNM1", Name: "reset-running-missing", Config: `{}`, GOWAVersion: "latest"})
	repo.items[created.ID] = withStatus(created, "running", stringPtr("boom"))

	if err := service.ResetData(ctx, created.ID); err != nil {
		t.Fatalf("ResetData() running missing dir error = %v", err)
	}
	if !reflect.DeepEqual(lifecycle.stopped, []int64{created.ID}) {
		t.Fatalf("Stop calls = %#v, want [%d]", lifecycle.stopped, created.ID)
	}
	got := repo.items[created.ID]
	if got.Status != "stopped" || got.ErrorMessage != nil {
		t.Fatalf("ResetData() running missing dir status/error = %#v", got)
	}
	if !reflect.DeepEqual(fs.staged, []int64{created.ID}) || !reflect.DeepEqual(fs.ensured, []int64{created.ID}) || len(fs.purged) != 0 || len(fs.restored) != 0 {
		t.Fatalf("ResetData() running missing dir filesystem staged=%#v ensured=%#v purged=%#v restored=%#v", fs.staged, fs.ensured, fs.purged, fs.restored)
	}
}

func TestServiceResetCompensatesFilesystemWhenUpdateStatusFails(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	fs := &fakeFilesystem{}
	service := NewService(repo, fs, &fakePortAllocator{next: 7210}, nil)
	service.generateKey = func() (string, error) { return "RSTDB001", nil }
	created, err := service.Create(ctx, CreateRequest{Name: "reset-db-fail"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	before := withStatus(repo.items[created.ID], "error", stringPtr("boom"))
	repo.items[created.ID] = before
	fs.ensured = nil
	fs.events = nil
	repo.updateStatusErr = errors.New("status failed")

	if err := service.ResetData(ctx, created.ID); !strings.Contains(err.Error(), "status failed") {
		t.Fatalf("ResetData() update status error = %v", err)
	}
	if got := repo.items[created.ID]; got != before {
		t.Fatalf("ResetData() changed DB on update failure: got=%#v want=%#v", got, before)
	}
	if len(fs.restored) != 1 || len(fs.ensured) != 1 || len(fs.staged) != 2 || len(fs.purged) != 1 {
		t.Fatalf("ResetData() compensation restored=%#v ensured=%#v purged=%#v", fs.restored, fs.ensured, fs.purged)
	}
	if !reflect.DeepEqual(fs.events, []string{"stage", "ensure", "stage", "purge", "restore"}) {
		t.Fatalf("ResetData() events = %#v", fs.events)
	}
}

func TestServiceResetRestoresTrashWhenEnsureFails(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	fs := &fakeFilesystem{ensureErr: errors.New("ensure failed")}
	service := NewService(repo, fs, &fakePortAllocator{next: 7211}, nil)
	service.generateKey = func() (string, error) { return "RSTENS01", nil }
	created, err := service.Create(ctx, CreateRequest{Name: "seed"})
	if err == nil {
		t.Fatalf("Create() with failing ensure error = nil")
	}
	fs.ensureErr = nil
	created, err = service.Create(ctx, CreateRequest{Name: "reset-ensure-fail"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	before := withStatus(repo.items[created.ID], "error", stringPtr("boom"))
	repo.items[created.ID] = before
	fs.ensureErr = errors.New("ensure failed")
	fs.ensured = nil
	fs.events = nil

	if err := service.ResetData(ctx, created.ID); !strings.Contains(err.Error(), "ensure failed") {
		t.Fatalf("ResetData() ensure error = %v", err)
	}
	if got := repo.items[created.ID]; got != before {
		t.Fatalf("ResetData() DB after ensure failure = %#v, want status/error from %#v", got, before)
	}
	if len(fs.restored) != 1 || len(fs.purged) != 0 {
		t.Fatalf("ResetData() ensure compensation restored=%#v purged=%#v", fs.restored, fs.purged)
	}
	if !reflect.DeepEqual(fs.events, []string{"stage", "ensure", "restore"}) {
		t.Fatalf("ResetData() events = %#v", fs.events)
	}
}

func TestServiceResetReturnsPurgeFailureAfterCommitWithoutRestore(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	fs := &fakeFilesystem{}
	service := NewService(repo, fs, &fakePortAllocator{next: 7212}, nil)
	service.generateKey = func() (string, error) { return "RSTPRG01", nil }
	created, err := service.Create(ctx, CreateRequest{Name: "reset-purge-fail"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	repo.items[created.ID] = withStatus(repo.items[created.ID], "error", stringPtr("boom"))
	fs.purgeErr = errors.New("purge failed")
	fs.ensured = nil
	fs.events = nil

	if err := service.ResetData(ctx, created.ID); !strings.Contains(err.Error(), "purge failed") {
		t.Fatalf("ResetData() purge error = %v", err)
	}
	got := repo.items[created.ID]
	if got.Status != "stopped" || got.ErrorMessage != nil {
		t.Fatalf("ResetData() DB was not committed before purge failure: %#v", got)
	}
	if len(fs.ensured) != 1 || len(fs.restored) != 0 || len(fs.purged) != 1 {
		t.Fatalf("ResetData() purge failure filesystem ensured=%#v restored=%#v purged=%#v", fs.ensured, fs.restored, fs.purged)
	}
}

func TestServiceResetStageFailureDoesNotTouchDBOrCommitFilesystem(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	fs := &fakeFilesystem{}
	service := NewService(repo, fs, &fakePortAllocator{next: 7213}, nil)
	service.generateKey = func() (string, error) { return "RSTSTG01", nil }
	created, err := service.Create(ctx, CreateRequest{Name: "reset-stage-fail"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	before := withStatus(repo.items[created.ID], "error", stringPtr("boom"))
	repo.items[created.ID] = before
	fs.stageErr = errors.New("stage failed")
	fs.ensured = nil

	if err := service.ResetData(ctx, created.ID); !strings.Contains(err.Error(), "stage failed") {
		t.Fatalf("ResetData() stage error = %v", err)
	}
	if got := repo.items[created.ID]; got != before {
		t.Fatalf("ResetData() changed DB on stage failure: got=%#v want=%#v", got, before)
	}
	if len(fs.ensured) != 0 || len(fs.purged) != 0 || len(fs.restored) != 0 {
		t.Fatalf("ResetData() filesystem after stage failure ensured=%#v purged=%#v restored=%#v", fs.ensured, fs.purged, fs.restored)
	}
}

func TestServiceDeleteAndResetStopRunningInstanceBeforeStagingFiles(t *testing.T) {
	ctx := context.Background()
	for _, operation := range []string{"delete", "reset"} {
		t.Run(operation, func(t *testing.T) {
			repo := newFakeRepository()
			fs := &fakeFilesystem{}
			lifecycle := &fakeLifecycle{status: Status{State: "running"}}
			cache := &fakeDeviceCacheCleaner{}
			service := NewService(repo, fs, &fakePortAllocator{next: 7300}, lifecycle, WithDeviceCacheCleaner(cache))
			service.generateKey = func() (string, error) { return "RUNNING1", nil }
			created, err := service.Create(ctx, CreateRequest{Name: operation})
			if err != nil {
				t.Fatalf("Create() error = %v", err)
			}
			repo.items[created.ID] = withStatus(repo.items[created.ID], "running", nil)
			fs.events = nil
			if operation == "delete" {
				err = service.Delete(ctx, created.ID)
			} else {
				err = service.ResetData(ctx, created.ID)
			}
			if err != nil {
				t.Fatalf("%s error = %v", operation, err)
			}
			if !reflect.DeepEqual(lifecycle.stopped, []int64{created.ID}) {
				t.Fatalf("Stop calls = %#v", lifecycle.stopped)
			}
			if len(fs.events) == 0 || fs.events[0] != "stage" {
				t.Fatalf("filesystem events = %#v, want staging after stop", fs.events)
			}
			if !reflect.DeepEqual(cache.cleared, []int64{created.ID, created.ID}) {
				t.Fatalf("ClearCache calls = %#v, want [%d %d]", cache.cleared, created.ID, created.ID)
			}
		})
	}
}

func TestServiceClearsDeviceCacheAfterStoppedDeleteAndReset(t *testing.T) {
	ctx := context.Background()
	for _, operation := range []string{"delete", "reset"} {
		t.Run(operation, func(t *testing.T) {
			repo := newFakeRepository()
			fs := &fakeFilesystem{}
			cache := &fakeDeviceCacheCleaner{}
			service := NewService(repo, fs, &fakePortAllocator{next: 7302}, nil, WithDeviceCacheCleaner(cache))
			service.generateKey = func() (string, error) { return "CACHE001", nil }
			created, err := service.Create(ctx, CreateRequest{Name: operation})
			if err != nil {
				t.Fatalf("Create() error = %v", err)
			}

			if operation == "delete" {
				err = service.Delete(ctx, created.ID)
			} else {
				err = service.ResetData(ctx, created.ID)
			}
			if err != nil {
				t.Fatalf("%s error = %v", operation, err)
			}
			if !reflect.DeepEqual(cache.cleared, []int64{created.ID}) {
				t.Fatalf("ClearCache calls = %#v, want [%d]", cache.cleared, created.ID)
			}
		})
	}
}

func TestServiceSerializesDeleteAndResetForSameInstance(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	fs := newBlockingFilesystem()
	lifecycle := &fakeLifecycle{}
	service := NewService(repo, fs, &fakePortAllocator{next: 7301}, lifecycle)
	created := mustFakeCreate(t, ctx, repo, CreateInput{Key: "SERIAL01", Name: "serial", Config: `{}`, GOWAVersion: "latest"})
	repo.items[created.ID] = withStatus(created, "running", nil)

	deleteDone := make(chan error, 1)
	go func() { deleteDone <- service.Delete(ctx, created.ID) }()
	fs.waitForStage(t)

	resetDone := make(chan error, 1)
	go func() { resetDone <- service.ResetData(ctx, created.ID) }()
	fs.assertNoSecondStage(t)
	fs.releaseStage()

	if err := <-deleteDone; err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if err := <-resetDone; !errors.Is(err, ErrNotFound) {
		t.Fatalf("ResetData() after serialized delete error = %v, want ErrNotFound", err)
	}
	if fs.maxActive != 1 {
		t.Fatalf("StageDelete max active = %d, want 1", fs.maxActive)
	}
}

func TestServiceWithRealFilesystemHandlesMissingDirectoryDeleteAndReset(t *testing.T) {
	ctx := context.Background()
	fs, err := NewFilesystem(t.TempDir())
	if err != nil {
		t.Fatalf("NewFilesystem() error = %v", err)
	}

	repo := newFakeRepository()
	deleteService := NewService(repo, fs, &fakePortAllocator{next: 7400}, nil)
	toDelete := mustFakeCreate(t, ctx, repo, CreateInput{Key: "REALDEL1", Name: "real-delete", Config: `{}`, GOWAVersion: "latest"})
	if err := deleteService.Delete(ctx, toDelete.ID); err != nil {
		t.Fatalf("Delete() missing real directory error = %v", err)
	}
	if _, ok := repo.items[toDelete.ID]; ok {
		t.Fatalf("Delete() left repo row for missing real directory: %#v", repo.items[toDelete.ID])
	}

	resetService := NewService(repo, fs, &fakePortAllocator{next: 7401}, nil)
	toReset := mustFakeCreate(t, ctx, repo, CreateInput{Key: "REALRST1", Name: "real-reset", Config: `{}`, GOWAVersion: "latest"})
	repo.items[toReset.ID] = withStatus(toReset, "error", stringPtr("boom"))
	if err := resetService.ResetData(ctx, toReset.ID); err != nil {
		t.Fatalf("ResetData() missing real directory error = %v", err)
	}
	got := repo.items[toReset.ID]
	if got.Status != "stopped" || got.ErrorMessage != nil {
		t.Fatalf("ResetData() missing real directory status/error = %#v", got)
	}
	dir, err := fs.InstanceDir(toReset.ID)
	if err != nil {
		t.Fatalf("InstanceDir() error = %v", err)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("ResetData() directory stat = (%#v, %v), want existing directory", info, err)
	}
}

type fakeRepository struct {
	mu              sync.Mutex
	nextID          int64
	items           map[int64]Instance
	createErr       error
	updateErr       error
	deleteErr       error
	updateStatusErr error
	clearErrorErr   error
	clearErrorCalls int
	deleted         []int64
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{nextID: 1, items: map[int64]Instance{}}
}

func (r *fakeRepository) List(context.Context) ([]Instance, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	instances := make([]Instance, 0, len(r.items))
	for _, instance := range r.items {
		instances = append(instances, instance)
	}
	return instances, nil
}

func (r *fakeRepository) FindByID(_ context.Context, id int64) (Instance, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	instance, ok := r.items[id]
	if !ok {
		return Instance{}, ErrNotFound
	}
	return instance, nil
}

func (r *fakeRepository) FindByKey(_ context.Context, key string) (Instance, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, instance := range r.items {
		if instance.Key == key {
			return instance, nil
		}
	}
	return Instance{}, ErrNotFound
}

func (r *fakeRepository) Create(_ context.Context, input CreateInput) (Instance, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.createErr != nil {
		return Instance{}, r.createErr
	}
	for _, instance := range r.items {
		if instance.Key == input.Key || instance.Name == input.Name {
			return Instance{}, ErrConflict
		}
	}
	instance := Instance{ID: r.nextID, Key: input.Key, Name: input.Name, Port: input.Port, Status: "stopped", Config: input.Config, GOWAVersion: input.GOWAVersion}
	r.items[instance.ID] = instance
	r.nextID++
	return instance, nil
}

func (r *fakeRepository) Update(_ context.Context, input UpdateInput) (Instance, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.updateErr != nil {
		return Instance{}, r.updateErr
	}
	instance, ok := r.items[input.ID]
	if !ok {
		return Instance{}, ErrNotFound
	}
	for _, existing := range r.items {
		if existing.ID != input.ID && existing.Name == input.Name {
			return Instance{}, ErrConflict
		}
	}
	instance.Name = input.Name
	instance.Config = input.Config
	instance.GOWAVersion = input.GOWAVersion
	r.items[input.ID] = instance
	return instance, nil
}

func (r *fakeRepository) UpdateStatus(_ context.Context, id int64, status string, errorMessage *string) (Instance, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.updateStatusErr != nil {
		return Instance{}, r.updateStatusErr
	}
	instance, ok := r.items[id]
	if !ok {
		return Instance{}, ErrNotFound
	}
	instance.Status = status
	instance.ErrorMessage = errorMessage
	r.items[id] = instance
	return instance, nil
}

func (r *fakeRepository) ClearError(_ context.Context, id int64) (Instance, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clearErrorCalls++
	if r.clearErrorErr != nil {
		return Instance{}, r.clearErrorErr
	}
	instance, ok := r.items[id]
	if !ok {
		return Instance{}, ErrNotFound
	}
	instance.ErrorMessage = nil
	r.items[id] = instance
	return instance, nil
}

func (r *fakeRepository) UpdatePort(_ context.Context, id int64, port *int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	instance, ok := r.items[id]
	if !ok {
		return ErrNotFound
	}
	instance.Port = port
	r.items[id] = instance
	return nil
}

func (r *fakeRepository) Delete(_ context.Context, id int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deleted = append(r.deleted, id)
	if r.deleteErr != nil {
		return r.deleteErr
	}
	if _, ok := r.items[id]; !ok {
		return ErrNotFound
	}
	delete(r.items, id)
	return nil
}

type fakeFilesystem struct {
	mu         sync.Mutex
	ensureErr  error
	stageErr   error
	restoreErr error
	purgeErr   error
	ensured    []int64
	staged     []int64
	restored   []Trash
	purged     []Trash
	events     []string
}

func (f *fakeFilesystem) Ensure(_ context.Context, id int64) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, "ensure")
	f.ensured = append(f.ensured, id)
	if f.ensureErr != nil {
		return "", f.ensureErr
	}
	return "dir", nil
}

func (f *fakeFilesystem) StageDelete(_ context.Context, id int64) (Trash, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, "stage")
	f.staged = append(f.staged, id)
	if f.stageErr != nil {
		return Trash{}, f.stageErr
	}
	return Trash{InstanceID: id, OriginalPath: "original", TrashPath: "trash"}, nil
}

func (f *fakeFilesystem) Restore(_ context.Context, trash Trash) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, "restore")
	f.restored = append(f.restored, trash)
	return f.restoreErr
}

func (f *fakeFilesystem) Purge(_ context.Context, trash Trash) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, "purge")
	f.purged = append(f.purged, trash)
	return f.purgeErr
}

func (f *fakeFilesystem) Reset(context.Context, int64) error {
	panic("service should coordinate reset staging")
}

type fakePortAllocator struct {
	next  int
	err   error
	calls int
}

func (p *fakePortAllocator) Next(context.Context) (int, error) {
	p.calls++
	return p.next, p.err
}

type fakeLifecycle struct {
	mu      sync.Mutex
	status  Status
	err     error
	stopErr error
	stopped []int64
}

func (l *fakeLifecycle) Status(context.Context, int64) (Status, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.status, l.err
}

func (l *fakeLifecycle) Stop(_ context.Context, id int64) (Status, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.stopped = append(l.stopped, id)
	return Status{State: "stopped"}, l.stopErr
}

type fakeDeviceCacheCleaner struct {
	cleared []int64
}

func (c *fakeDeviceCacheCleaner) ClearCache(id int64) {
	c.cleared = append(c.cleared, id)
}

type blockingFilesystem struct {
	stageEntered chan struct{}
	release      chan struct{}

	mu        sync.Mutex
	active    int
	maxActive int
}

func newBlockingFilesystem() *blockingFilesystem {
	return &blockingFilesystem{stageEntered: make(chan struct{}, 2), release: make(chan struct{})}
}

func (f *blockingFilesystem) Ensure(context.Context, int64) (string, error) {
	return "dir", nil
}

func (f *blockingFilesystem) StageDelete(context.Context, int64) (Trash, error) {
	f.mu.Lock()
	f.active++
	if f.active > f.maxActive {
		f.maxActive = f.active
	}
	f.mu.Unlock()

	f.stageEntered <- struct{}{}
	<-f.release

	f.mu.Lock()
	f.active--
	f.mu.Unlock()
	return Trash{InstanceID: 1, OriginalPath: "original", TrashPath: "trash"}, nil
}

func (f *blockingFilesystem) Restore(context.Context, Trash) error {
	return nil
}

func (f *blockingFilesystem) Purge(context.Context, Trash) error {
	return nil
}

func (f *blockingFilesystem) waitForStage(t *testing.T) {
	t.Helper()
	select {
	case <-f.stageEntered:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for StageDelete")
	}
}

func (f *blockingFilesystem) assertNoSecondStage(t *testing.T) {
	t.Helper()
	select {
	case <-f.stageEntered:
		t.Fatal("second StageDelete started before first mutation completed")
	case <-time.After(50 * time.Millisecond):
	}
}

func (f *blockingFilesystem) releaseStage() {
	f.release <- struct{}{}
}

func newTestService() *Service {
	return NewService(newFakeRepository(), &fakeFilesystem{}, &fakePortAllocator{next: 6000}, nil)
}

func mustFakeCreate(t *testing.T, ctx context.Context, repo *fakeRepository, input CreateInput) Instance {
	t.Helper()
	instance, err := repo.Create(ctx, input)
	if err != nil {
		t.Fatalf("Create(%#v) error = %v", input, err)
	}
	return instance
}

func withStatus(instance Instance, status string, message *string) Instance {
	instance.Status = status
	instance.ErrorMessage = message
	return instance
}

func stringPtr(value string) *string {
	return &value
}
