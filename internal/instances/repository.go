package instances

import "context"

type Repository interface {
	List(ctx context.Context) ([]Instance, error)
	FindByID(ctx context.Context, id int64) (Instance, error)
	FindByKey(ctx context.Context, key string) (Instance, error)
	Create(ctx context.Context, input CreateInput) (Instance, error)
	Update(ctx context.Context, input UpdateInput) (Instance, error)
	UpdateStatus(ctx context.Context, id int64, status string, errorMessage *string) (Instance, error)
	ClearError(ctx context.Context, id int64) (Instance, error)
	UpdatePort(ctx context.Context, id int64, port *int) error
	Delete(ctx context.Context, id int64) error
}
