package db

import (
	"context"
	"fmt"
	"strings"

	"github.com/a-h/kv"
	"github.com/nix-community/go-nix/pkg/narinfo"
)

func New(store kv.Store) (db *DB) {
	return &DB{store: store}
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
