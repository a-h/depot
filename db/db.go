package db

import (
	"database/sql"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
	"github.com/nix-community/go-nix/pkg/sqlite/binary_cache_v6"
	"github.com/nix-community/go-nix/pkg/sqlite/nix_v10"
)

//go:embed nix_v10.sql
var nixSchema string

//go:embed binary_cache_v6.sql
var binaryCacheSchema string

// Init creates and initializes a unified database in the depot store directory
// The database contains both Nix store and binary cache schemas.
func Init(storeDir, cacheURL string) (*sql.DB, *nix_v10.Queries, *binary_cache_v6.Queries, error) {
	if err := os.MkdirAll(storeDir, 0755); err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create store directory %s: %w", storeDir, err)
	}

	dbDir := filepath.Join(storeDir, "var", "nix", "db")
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create db directory %s: %w", dbDir, err)
	}

	dbPath := filepath.Join(dbDir, "db.sqlite")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to open database %s: %w", dbPath, err)
	}

	if err := initNixSchema(db); err != nil {
		db.Close()
		return nil, nil, nil, fmt.Errorf("failed to initialize Nix schema: %w", err)
	}

	if err := initBinaryCacheSchema(db, storeDir, cacheURL); err != nil {
		db.Close()
		return nil, nil, nil, fmt.Errorf("failed to initialize binary cache schema: %w", err)
	}

	// Create query interfaces for both schemas
	nixQueries := nix_v10.New(db)
	cacheQueries := binary_cache_v6.New(db)

	return db, nixQueries, cacheQueries, nil
}

func initNixSchema(db *sql.DB) error {
	_, err := db.Exec(nixSchema)
	if err != nil {
		return fmt.Errorf("failed to create nix schema: %w", err)
	}
	return nil
}

func initBinaryCacheSchema(db *sql.DB, storeDir, cacheURL string) error {
	_, err := db.Exec(binaryCacheSchema)
	if err != nil {
		return fmt.Errorf("failed to create binary cache schema: %w", err)
	}

	_, err = db.Exec(`
		INSERT OR IGNORE INTO BinaryCaches (id, url, timestamp, storeDir, wantMassQuery, priority)
		VALUES (1, ?, ?, ?, 1, 30)
	`, cacheURL, 0, filepath.Join(storeDir, "store"))
	if err != nil {
		return fmt.Errorf("failed to insert default cache: %w", err)
	}

	return nil
}
