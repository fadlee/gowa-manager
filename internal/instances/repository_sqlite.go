package instances

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

type SQLiteRepository struct {
	db *sql.DB
}

func NewSQLiteRepository(db *sql.DB) *SQLiteRepository {
	return &SQLiteRepository{db: db}
}

func (r *SQLiteRepository) List(ctx context.Context) ([]Instance, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id, key, name, port, status, config, gowa_version, error_message, created_at, updated_at
FROM instances
ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	instances := []Instance{}
	for rows.Next() {
		instance, err := scanInstance(rows)
		if err != nil {
			return nil, err
		}
		instances = append(instances, instance)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return instances, nil
}

func (r *SQLiteRepository) FindByID(ctx context.Context, id int64) (Instance, error) {
	return scanOneInstance(r.db.QueryRowContext(ctx, `
SELECT id, key, name, port, status, config, gowa_version, error_message, created_at, updated_at
FROM instances
WHERE id = ?`, id))
}

func (r *SQLiteRepository) FindByKey(ctx context.Context, key string) (Instance, error) {
	return scanOneInstance(r.db.QueryRowContext(ctx, `
SELECT id, key, name, port, status, config, gowa_version, error_message, created_at, updated_at
FROM instances
WHERE key = ?`, key))
}

func (r *SQLiteRepository) Create(ctx context.Context, input CreateInput) (Instance, error) {
	instance, err := scanOneInstance(r.db.QueryRowContext(ctx, `
INSERT INTO instances (key, name, port, config, gowa_version)
VALUES (?, ?, ?, ?, ?)
RETURNING id, key, name, port, status, config, gowa_version, error_message, created_at, updated_at`, input.Key, input.Name, input.Port, input.Config, input.GOWAVersion))
	if err != nil {
		return Instance{}, mapSQLiteError(err)
	}
	return instance, nil
}

func (r *SQLiteRepository) Update(ctx context.Context, input UpdateInput) (Instance, error) {
	instance, err := scanOneInstance(r.db.QueryRowContext(ctx, `
UPDATE instances
SET name = ?, config = ?, gowa_version = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?
RETURNING id, key, name, port, status, config, gowa_version, error_message, created_at, updated_at`, input.Name, input.Config, input.GOWAVersion, input.ID))
	if err != nil {
		return Instance{}, mapSQLiteError(err)
	}
	return instance, nil
}

func (r *SQLiteRepository) UpdateStatus(ctx context.Context, id int64, status string, errorMessage *string) (Instance, error) {
	return scanOneInstance(r.db.QueryRowContext(ctx, `
UPDATE instances
SET status = ?, error_message = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?
RETURNING id, key, name, port, status, config, gowa_version, error_message, created_at, updated_at`, status, errorMessage, id))
}

func (r *SQLiteRepository) ClearError(ctx context.Context, id int64) (Instance, error) {
	return scanOneInstance(r.db.QueryRowContext(ctx, `
UPDATE instances
SET error_message = NULL, updated_at = CURRENT_TIMESTAMP
WHERE id = ?
RETURNING id, key, name, port, status, config, gowa_version, error_message, created_at, updated_at`, id))
}

func (r *SQLiteRepository) UpdatePort(ctx context.Context, id int64, port *int) error {
	result, err := r.db.ExecContext(ctx, `UPDATE instances SET port = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, port, id)
	if err != nil {
		return mapSQLiteError(err)
	}
	return requireAffected(result)
}

func (r *SQLiteRepository) Delete(ctx context.Context, id int64) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM instances WHERE id = ?`, id)
	if err != nil {
		return err
	}
	return requireAffected(result)
}

type instanceScanner interface {
	Scan(dest ...any) error
}

func scanOneInstance(scanner instanceScanner) (Instance, error) {
	instance, err := scanInstance(scanner)
	if err != nil {
		return Instance{}, mapSQLiteError(err)
	}
	return instance, nil
}

func scanInstance(scanner instanceScanner) (Instance, error) {
	var instance Instance
	var port sql.NullInt64
	var errorMessage sql.NullString
	if err := scanner.Scan(
		&instance.ID,
		&instance.Key,
		&instance.Name,
		&port,
		&instance.Status,
		&instance.Config,
		&instance.GOWAVersion,
		&errorMessage,
		&instance.CreatedAt,
		&instance.UpdatedAt,
	); err != nil {
		return Instance{}, err
	}
	if port.Valid {
		value := int(port.Int64)
		instance.Port = &value
	}
	if errorMessage.Valid {
		instance.ErrorMessage = &errorMessage.String
	}
	return instance, nil
}

func requireAffected(result sql.Result) error {
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func mapSQLiteError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if isUniqueConstraintError(err) {
		return ErrConflict
	}
	return err
}

func isUniqueConstraintError(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "constraint") && strings.Contains(message, "unique")
}
