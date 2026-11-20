// Package repository is the PostgreSQL persistence layer. It owns the schema baseline, typed
// queries, transactions and the background job and rollup operations.
package repository

import (
	"context"
	"database/sql"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
	"time"
)

type Store struct {
	DB     *bun.DB
	Secret []byte
	events *EventWriter
}

func Open(dsn string) *Store {
	connection := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn)))
	connection.SetMaxOpenConns(32)
	connection.SetMaxIdleConns(8)
	connection.SetConnMaxLifetime(30 * time.Minute)
	store := &Store{DB: bun.NewDB(connection, pgdialect.New())}
	store.events = newEventWriter(store)
	return store
}

func (store *Store) Close() error {
	if store.events != nil {
		store.events.Close()
	}
	return store.DB.Close()
}

func (store *Store) Ping(ctx context.Context) error { return store.DB.PingContext(ctx) }

// FlushEvents writes every queued audit event; it is called once the server has drained.
func (store *Store) FlushEvents() { store.events.Flush() }
