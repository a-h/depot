package db

import (
	"context"
	_ "embed"
	"fmt"
	"net/url"
	"strings"

	"github.com/a-h/kv"
	"github.com/a-h/kv/postgreskv"
	"github.com/a-h/kv/rqlitekv"
	"github.com/a-h/kv/sqlitekv"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nix-community/go-nix/pkg/narinfo"
	rqlitehttp "github.com/rqlite/rqlite-go-http"
	"zombiezen.com/go/sqlite/sqlitex"
)

func New(ctx context.Context, dbType, dsn string) (db *DB, closer func() error, err error) {
	store, closer, err := createStore(dbType, dsn)
	if err != nil {
		return nil, nil, err
	}
	if err = store.Init(ctx); err != nil {
		_ = closer()
		return nil, nil, err
	}
	db = &DB{store: store}
	return db, closer, nil
}

func createStore(dbType, url string) (store kv.Store, closer func() error, err error) {
	switch dbType {
	case "sqlite":
		return newSqliteStore(url)
	case "rqlite":
		return newRqliteStore(url)
	case "postgres":
		return newPostgresStore(url)
	default:
		err = fmt.Errorf("unsupported database type: %s", dbType)
	}
	return
}

func newSqliteStore(dsn string) (store kv.Store, closer func() error, err error) {
	pool, err := sqlitex.NewPool(dsn, sqlitex.PoolOptions{})
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

type DB struct {
	store kv.Store
}

type narInfoRecord struct {
	NarInfo string `kv:"ni"`
}

// GetNarInfo retrieves a narinfo from the database. The narinfoPath is the URL path, e.g. /cache-name/16hvpw4b3r05girazh4rnwbw0jgjkb4l.narinfo
func (db *DB) GetNarInfo(ctx context.Context, narinfoPath string) (ni *narinfo.NarInfo, ok bool, err error) {
	var nir narInfoRecord
	_, ok, err = db.store.Get(ctx, narinfoPath, &nir)
	if err != nil {
		return nil, false, err
	}
	if !ok {
		return nil, false, nil
	}
	ni, err = narinfo.Parse(strings.NewReader(nir.NarInfo))
	if err != nil {
		return nil, false, err
	}
	return ni, true, nil
}

// PutNarInfo stores a narinfo in the database. The narinfoPath is the URL path, e.g. /cache-name/16hvpw4b3r05girazh4rnwbw0jgjkb4l.narinfo
func (db *DB) PutNarInfo(ctx context.Context, narinfoPath string, ni *narinfo.NarInfo) (err error) {
	if ni == nil {
		return fmt.Errorf("cannot store nil narinfo")
	}
	nir := narInfoRecord{
		NarInfo: ni.String(),
	}
	return db.store.Put(ctx, narinfoPath, -1, nir)
}
