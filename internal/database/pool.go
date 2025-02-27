package database

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"log/slog"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

func Pool(ctx context.Context, dbURL string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		return nil, fmt.Errorf("parsing database config: %w", err)
	}

	tempPool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("connecting to database: %w", err)
	}
	defer tempPool.Close()

	// River migrations
	rm, err := rivermigrate.New(riverpgxv5.New(tempPool), nil)
	if err != nil {
		return nil, fmt.Errorf("creating river migrate: %w", err)
	}

	if _, err = rm.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		return nil, fmt.Errorf("applying river migrations: %w", err)
	}

	// Ratchet migrations
	d, err := iofs.New(migrationFiles, "migrations")
	if err != nil {
		return nil, fmt.Errorf("loading migrations: %w", err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", d, dbURL)
	if err != nil {
		return nil, fmt.Errorf("creating driver: %w", err)
	}

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return nil, fmt.Errorf("applying migrations: %w", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("connecting to database: %w", err)
	}

	slog.Info("Migrations applied successfully!")
	return pool, nil
}
