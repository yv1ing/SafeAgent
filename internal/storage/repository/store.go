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

// Open connects to one database. instanceKey is the operator-supplied 32-byte instance key; it is
// kept in memory only and never written to the database.
func Open(dsn string, instanceKey []byte) *Store {
	if instanceKey != nil && len(instanceKey) != 32 {
		panic("repository: the instance key must be 32 bytes")
	}
	connection := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn)))
	connection.SetMaxOpenConns(32)
	connection.SetMaxIdleConns(8)
	connection.SetConnMaxLifetime(30 * time.Minute)
	store := &Store{DB: bun.NewDB(connection, pgdialect.New()), Secret: instanceKey}
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
