package db

import (
	"github.com/a-h/kv"
)

func New(store kv.Store) (db *DB) {
	return &DB{store: store}
}

type DB struct {
	store kv.Store
}

//TODO: Add any NPM specific database methods here.
