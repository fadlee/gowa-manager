package instances

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
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

func TestServiceResetStoppedStagesUpdatesStatusClearsErrorPurgesAndEnsures(t *testing.T) {
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

	if err := service.ResetData(ctx, created.ID); err != nil {
		t.Fatalf("ResetData() error = %v", err)
	}
	got := repo.items[created.ID]
	if got.Status != "stopped" || got.ErrorMessage != nil {
		t.Fatalf("ResetData() status/error = %#v", got)
	}
	if !reflect.DeepEqual(fs.staged, []int64{created.ID}) || len(fs.purged) != 1 || !reflect.DeepEqual(fs.ensured, []int64{created.ID}) {
		t.Fatalf("ResetData() filesystem staged=%#v purged=%#v ensured=%#v", fs.staged, fs.purged, fs.ensured)
	}
	if err := service.ResetData(ctx, created.ID+1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ResetData() missing error = %v, want ErrNotFound", err)
	}
}

func TestServiceDeleteAndResetStopRunningInstanceBeforeStagingFiles(t *testing.T) {
	ctx := context.Background()
	for _, operation := range []string{"delete", "reset"} {
		t.Run(operation, func(t *testing.T) {
			repo := newFakeRepository()
			fs := &fakeFilesystem{}
			lifecycle := &fakeLifecycle{status: Status{State: "running"}}
			service := NewService(repo, fs, &fakePortAllocator{next: 7300}, lifecycle)
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
		})
	}
}

type fakeRepository struct {
	nextID          int64
	items           map[int64]Instance
	createErr       error
	updateErr       error
	deleteErr       error
	updateStatusErr error
	clearErrorErr   error
	deleted         []int64
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{nextID: 1, items: map[int64]Instance{}}
}

func (r *fakeRepository) List(context.Context) ([]Instance, error) {
	instances := make([]Instance, 0, len(r.items))
	for _, instance := range r.items {
		instances = append(instances, instance)
	}
	return instances, nil
}

func (r *fakeRepository) FindByID(_ context.Context, id int64) (Instance, error) {
	instance, ok := r.items[id]
	if !ok {
		return Instance{}, ErrNotFound
	}
	return instance, nil
}

func (r *fakeRepository) FindByKey(_ context.Context, key string) (Instance, error) {
	for _, instance := range r.items {
		if instance.Key == key {
			return instance, nil
		}
	}
	return Instance{}, ErrNotFound
}

func (r *fakeRepository) Create(_ context.Context, input CreateInput) (Instance, error) {
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
	instance, ok := r.items[id]
	if !ok {
		return ErrNotFound
	}
	instance.Port = port
	r.items[id] = instance
	return nil
}

func (r *fakeRepository) Delete(_ context.Context, id int64) error {
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
	f.events = append(f.events, "ensure")
	f.ensured = append(f.ensured, id)
	if f.ensureErr != nil {
		return "", f.ensureErr
	}
	return "dir", nil
}

func (f *fakeFilesystem) StageDelete(_ context.Context, id int64) (Trash, error) {
	f.events = append(f.events, "stage")
	f.staged = append(f.staged, id)
	if f.stageErr != nil {
		return Trash{}, f.stageErr
	}
	return Trash{InstanceID: id, OriginalPath: "original", TrashPath: "trash"}, nil
}

func (f *fakeFilesystem) Restore(_ context.Context, trash Trash) error {
	f.events = append(f.events, "restore")
	f.restored = append(f.restored, trash)
	return f.restoreErr
}

func (f *fakeFilesystem) Purge(_ context.Context, trash Trash) error {
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
	status  Status
	err     error
	stopErr error
	stopped []int64
}

func (l *fakeLifecycle) Status(context.Context, int64) (Status, error) {
	return l.status, l.err
}

func (l *fakeLifecycle) Stop(_ context.Context, id int64) (Status, error) {
	l.stopped = append(l.stopped, id)
	return Status{State: "stopped"}, l.stopErr
}

func newTestService() *Service {
	return NewService(newFakeRepository(), &fakeFilesystem{}, &fakePortAllocator{next: 6000}, nil)
}

func withStatus(instance Instance, status string, message *string) Instance {
	instance.Status = status
	instance.ErrorMessage = message
	return instance
}

func stringPtr(value string) *string {
	return &value
}
