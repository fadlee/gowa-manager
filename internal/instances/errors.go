package instances

import "errors"

var (
	ErrNotFound = errors.New("instance not found")
	ErrConflict = errors.New("instance conflict")
)
