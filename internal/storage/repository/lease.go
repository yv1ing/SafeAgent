package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"time"
)

const instanceLockID int64 = 5999146967533040974

type Lease struct {
	connection *sql.Conn
}

func (store *Store) AcquireInstance(ctx context.Context) (*Lease, error) {
	connection, err := store.DB.DB.Conn(ctx)
	if err != nil {
		return nil, err
	}
	var acquired bool
	if err := connection.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", instanceLockID).Scan(&acquired); err != nil || !acquired {
		_ = connection.Close()
		if err != nil {
			return nil, err
		}
		return nil, errors.New("another SafeAgent instance owns this database")
	}
	return &Lease{connection: connection}, nil
}

func (lease *Lease) Check(ctx context.Context) error {
	return lease.connection.PingContext(ctx)
}

func (lease *Lease) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := lease.connection.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", instanceLockID)
	if err != nil {
		_ = lease.connection.Raw(func(any) error { return driver.ErrBadConn })
	}
	return errors.Join(err, lease.connection.Close())
}
