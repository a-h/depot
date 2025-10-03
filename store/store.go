package store

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	rqlitehttp "github.com/rqlite/rqlite-go-http"

	"github.com/a-h/kv"
	"github.com/a-h/kv/postgreskv"
	"github.com/a-h/kv/rqlitekv"
	"github.com/a-h/kv/sqlitekv"
	"github.com/jackc/pgx/v5/pgxpool"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

func New(ctx context.Context, dbType, url string) (store kv.Store, closer func() error, err error) {
	switch dbType {
	case "sqlite":
		store, closer, err = newSqliteStore(url)
	case "rqlite":
		store, closer, err = newRqliteStore(url)
	case "postgres":
		store, closer, err = newPostgresStore(url)
	default:
		return nil, nil, fmt.Errorf("unsupported database type: %s", dbType)
	}
	if err != nil {
		return nil, nil, err
	}
	if err = store.Init(ctx); err != nil {
		_ = closer()
		return nil, nil, err
	}
	return store, closer, nil
}

func newSqliteStore(dsn string) (store kv.Store, closer func() error, err error) {
	dsnURI, err := url.Parse(dsn)
	if err != nil {
		return nil, nil, err
	}
	opts := sqlitex.PoolOptions{
		Flags: sqlite.OpenReadWrite | sqlite.OpenCreate | sqlite.OpenURI,
	}
	// Enable WAL mode if specified in the DSN.
	// WAL doesn't work well with container volumes.
	journalMode := dsnURI.Query().Get("_journal_mode")
	if strings.EqualFold(journalMode, "wal") {
		opts.Flags |= sqlite.OpenWAL
	}
	pool, err := sqlitex.NewPool(dsn, opts)
	if err != nil {
		return nil, nil, err
	}
	store = sqlitekv.NewStore(pool)
	return store, pool.Close, nil
}

func newRqliteStore(dsn string) (store kv.Store, closer func() error, err error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return nil, nil, err
	}
	client := rqlitehttp.NewClient(dsn, nil)
	if u.User != nil {
		pwd, _ := u.User.Password()
		client.SetBasicAuth(u.User.Username(), pwd)
	}
	store = rqlitekv.NewStore(client)
	return store, func() error { return nil }, nil
}

func newPostgresStore(dsn string) (store kv.Store, closer func() error, err error) {
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		return nil, nil, err
	}
	store = postgreskv.NewStore(pool)
	closer = func() error {
		pool.Close()
		return nil
	}
	return store, closer, nil
}
