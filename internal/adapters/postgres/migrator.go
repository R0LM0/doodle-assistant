package postgres

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sort"

	"github.com/jackc/pgx/v5/pgxpool"
)

// migrationLockKey is an arbitrary constant: any process migrating this database
// takes the same advisory lock, so two deploys cannot interleave DDL.
const migrationLockKey int64 = 72_34_56_78

// Migrator applies embedded *.sql files in filename order.
type Migrator struct {
	pool *pgxpool.Pool
	fsys fs.FS
}

func NewMigrator(pool *pgxpool.Pool, fsys fs.FS) (*Migrator, error) {
	if pool == nil {
		return nil, errors.New("postgres migrator: pool is required")
	}
	if fsys == nil {
		return nil, errors.New("postgres migrator: migrations filesystem is required")
	}
	return &Migrator{pool: pool, fsys: fsys}, nil
}

// Apply runs every migration that is not recorded in schema_migrations yet and
// returns their filenames. Each file is one transaction: a syntax error midway
// leaves no partial schema behind.
func (m *Migrator) Apply(ctx context.Context) ([]string, error) {
	conn, err := m.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire migration connection: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, migrationLockKey); err != nil {
		return nil, fmt.Errorf("acquire migration lock: %w", err)
	}
	defer func() {
		// Best effort: the lock is released with the session anyway.
		_, _ = conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, migrationLockKey)
	}()

	const createTable = `
		CREATE TABLE IF NOT EXISTS schema_migrations (
		    version    text        PRIMARY KEY,
		    applied_at timestamptz NOT NULL DEFAULT now()
		)`
	if _, err := conn.Exec(ctx, createTable); err != nil {
		return nil, fmt.Errorf("create schema_migrations: %w", err)
	}

	files, err := fs.Glob(m.fsys, "*.sql")
	if err != nil {
		return nil, fmt.Errorf("list migrations: %w", err)
	}
	sort.Strings(files)

	var applied []string
	for _, name := range files {
		var exists bool
		if err := conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, name).Scan(&exists); err != nil {
			return applied, fmt.Errorf("check migration %s: %w", name, err)
		}
		if exists {
			continue
		}

		statement, err := fs.ReadFile(m.fsys, name)
		if err != nil {
			return applied, fmt.Errorf("read migration %s: %w", name, err)
		}

		tx, err := conn.Begin(ctx)
		if err != nil {
			return applied, fmt.Errorf("begin migration %s: %w", name, err)
		}

		if _, err := tx.Exec(ctx, string(statement)); err != nil {
			_ = tx.Rollback(ctx)
			return applied, fmt.Errorf("apply migration %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, name); err != nil {
			_ = tx.Rollback(ctx)
			return applied, fmt.Errorf("record migration %s: %w", name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return applied, fmt.Errorf("commit migration %s: %w", name, err)
		}

		applied = append(applied, name)
	}

	return applied, nil
}
