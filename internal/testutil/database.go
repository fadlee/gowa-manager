package testutil

import (
	"context"
	"testing"

	"github.com/fadlee/gowa-manager/internal/database"
)

func TempDB(t *testing.T) (*database.DB, string) {
	t.Helper()
	dataDir := t.TempDir()
	db, err := database.Open(context.Background(), dataDir)
	if err != nil {
		t.Fatalf("open temp database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, dataDir
}
