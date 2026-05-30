package migrate

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

type Result struct {
	Name    string
	Applied bool
}

func Run(ctx context.Context, databaseURL string) ([]Result, error) {
	if strings.TrimSpace(databaseURL) == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	defer pool.Close()
	return RunWithPool(ctx, pool)
}

func RunWithPool(ctx context.Context, pool *pgxpool.Pool) ([]Result, error) {
	if _, err := pool.Exec(ctx, `
		create table if not exists schema_migrations (
			name text primary key,
			applied_at timestamptz not null default now()
		)`,
	); err != nil {
		return nil, err
	}

	names, err := Names()
	if err != nil {
		return nil, err
	}
	results := make([]Result, 0, len(names))
	for _, name := range names {
		applied, err := alreadyApplied(ctx, pool, name)
		if err != nil {
			return results, err
		}
		if applied {
			results = append(results, Result{Name: name, Applied: false})
			continue
		}
		sql, err := migrationFiles.ReadFile("migrations/" + name)
		if err != nil {
			return results, err
		}
		if err := apply(ctx, pool, name, string(sql)); err != nil {
			return results, fmt.Errorf("%s: %w", name, err)
		}
		results = append(results, Result{Name: name, Applied: true})
	}
	return results, nil
}

func Names() ([]string, error) {
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names, nil
}

func alreadyApplied(ctx context.Context, pool *pgxpool.Pool, name string) (bool, error) {
	var exists bool
	err := pool.QueryRow(ctx, `select exists(select 1 from schema_migrations where name = $1)`, name).Scan(&exists)
	return exists, err
}

func apply(ctx context.Context, pool *pgxpool.Pool, name, sql string) error {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, sql); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `insert into schema_migrations (name) values ($1)`, name); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
