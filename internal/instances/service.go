package instances

import (
	"context"
	"errors"
)

const latestVersion = "latest"

var ErrRuntimeNotReady = errors.New("instance runtime not ready")

type PortAllocator interface {
	Next(context.Context) (int, error)
}

type Lifecycle interface {
	Stop(context.Context, int64) (Status, error)
	Status(context.Context, int64) (Status, error)
}

type InstanceFilesystem interface {
	Ensure(context.Context, int64) (string, error)
	StageDelete(context.Context, int64) (Trash, error)
	Restore(context.Context, Trash) error
	Purge(context.Context, Trash) error
}

type Status struct {
	State string
}

type Service struct {
	repo         Repository
	fs           InstanceFilesystem
	ports        PortAllocator
	lifecycle    Lifecycle
	generateKey  func() (string, error)
	generateName func() string
}

type CreateRequest struct {
	Name        string
	Config      *string
	GOWAVersion string
}

type UpdateRequest struct {
	Name        *string
	Config      *string
	GOWAVersion *string
}

func NewService(repo Repository, fs InstanceFilesystem, ports PortAllocator, lifecycle Lifecycle) *Service {
	return &Service{
		repo:         repo,
		fs:           fs,
		ports:        ports,
		lifecycle:    lifecycle,
		generateKey:  GenerateKey,
		generateName: func() string { return RandomName(nil) },
	}
}

func (s *Service) List(ctx context.Context) ([]Instance, error) {
	return s.repo.List(ctx)
}

func (s *Service) Get(ctx context.Context, id int64) (Instance, error) {
	return s.repo.FindByID(ctx, id)
}

func (s *Service) Create(ctx context.Context, request CreateRequest) (Instance, error) {
	key, err := s.generateKey()
	if err != nil {
		return Instance{}, err
	}
	name := request.Name
	if name == "" {
		name = s.generateName()
	}
	port, err := s.ports.Next(ctx)
	if err != nil {
		return Instance{}, err
	}
	version := request.GOWAVersion
	if version == "" {
		version = latestVersion
	}
	instance, err := s.repo.Create(ctx, CreateInput{
		Key:         key,
		Name:        name,
		Port:        &port,
		Config:      BuildCreateConfig(request.Config, key),
		GOWAVersion: version,
	})
	if err != nil {
		return Instance{}, err
	}
	if _, err := s.fs.Ensure(ctx, instance.ID); err != nil {
		if deleteErr := s.repo.Delete(context.Background(), instance.ID); deleteErr != nil {
			return Instance{}, errors.Join(err, deleteErr)
		}
		return Instance{}, err
	}
	return instance, nil
}

func (s *Service) Update(ctx context.Context, id int64, request UpdateRequest) (Instance, error) {
	existing, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return Instance{}, err
	}
	name := existing.Name
	if request.Name != nil && *request.Name != "" {
		name = *request.Name
	}
	version := existing.GOWAVersion
	if version == "" {
		version = latestVersion
	}
	if request.GOWAVersion != nil && *request.GOWAVersion != "" {
		version = *request.GOWAVersion
	}
	return s.repo.Update(ctx, UpdateInput{
		ID:          id,
		Name:        name,
		Config:      NormalizeUpdateConfig(existing.Config, request.Config, existing.Key),
		GOWAVersion: version,
	})
}

func (s *Service) Delete(ctx context.Context, id int64) error {
	instance, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if err := s.stopIfRunning(ctx, instance); err != nil {
		return err
	}
	trash, err := s.fs.StageDelete(ctx, id)
	if err != nil {
		return err
	}
	if err := s.repo.Delete(ctx, id); err != nil {
		return errors.Join(err, s.fs.Restore(context.Background(), trash))
	}
	return s.fs.Purge(ctx, trash)
}

func (s *Service) ResetData(ctx context.Context, id int64) error {
	instance, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if err := s.stopIfRunning(ctx, instance); err != nil {
		return err
	}
	trash, err := s.fs.StageDelete(ctx, id)
	if err != nil {
		return err
	}
	if _, err := s.repo.UpdateStatus(ctx, id, "stopped", nil); err != nil {
		return errors.Join(err, s.fs.Restore(context.Background(), trash))
	}
	if _, err := s.repo.ClearError(ctx, id); err != nil {
		return errors.Join(err, s.fs.Restore(context.Background(), trash))
	}
	if err := s.fs.Purge(ctx, trash); err != nil {
		return err
	}
	_, err = s.fs.Ensure(ctx, id)
	return err
}

func (s *Service) stopIfRunning(ctx context.Context, instance Instance) error {
	if instance.Status != "running" {
		return nil
	}
	if s.lifecycle == nil {
		return ErrRuntimeNotReady
	}
	_, err := s.lifecycle.Stop(ctx, instance.ID)
	return err
}
