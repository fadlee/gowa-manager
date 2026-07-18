package database

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

type DB struct {
	SQL *sql.DB
}

func Open(ctx context.Context, dataDir string) (*DB, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data directory %q: %w", dataDir, err)
	}
	dbPath := filepath.Join(dataDir, "gowa.db")
	sqlDB, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("open sqlite %q: %w", dbPath, err)
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	if err := sqlDB.PingContext(ctx); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("ping sqlite %q: %w", dbPath, err)
	}
	if err := migrate(ctx, sqlDB); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("migrate sqlite %q: %w", dbPath, err)
	}
	return &DB{SQL: sqlDB}, nil
}

func (d *DB) IntegrityCheck(ctx context.Context) error {
	var result string
	if err := d.SQL.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&result); err != nil {
		return fmt.Errorf("run sqlite integrity_check: %w", err)
	}
	if result != "ok" {
		return fmt.Errorf("sqlite integrity_check failed: %s", result)
	}
	return nil
}

func (d *DB) Close() error {
	if d == nil || d.SQL == nil {
		return nil
	}
	return d.SQL.Close()
}

func migrate(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, createInstancesTableSQL); err != nil {
		return err
	}
	columns, err := instanceColumns(ctx, tx)
	if err != nil {
		return err
	}
	for _, column := range additiveInstanceColumns {
		if columns[column.name] {
			continue
		}
		if _, err := tx.ExecContext(ctx, column.sql); err != nil {
			if !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
				return err
			}
		}
	}
	return tx.Commit()
}

func instanceColumns(ctx context.Context, tx *sql.Tx) (map[string]bool, error) {
	rows, err := tx.QueryContext(ctx, `PRAGMA table_info(instances)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return nil, err
		}
		columns[name] = true
	}
	return columns, rows.Err()
}
