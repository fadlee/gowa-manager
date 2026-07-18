package instances

type Instance struct {
	ID           int64
	Key          string
	Name         string
	Port         *int
	Status       string
	Config       string
	GOWAVersion  string
	ErrorMessage *string
	CreatedAt    string
	UpdatedAt    string
}

type CreateInput struct {
	Key         string
	Name        string
	Port        *int
	Config      string
	GOWAVersion string
}

type UpdateInput struct {
	ID          int64
	Name        string
	Config      string
	GOWAVersion string
}
