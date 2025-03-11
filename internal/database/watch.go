package database

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

func Watch(ctx context.Context, db *pgxpool.Pool, notif string, callback func() error) error {
	if err := callback(); err != nil {
		return fmt.Errorf("failed to process initial state: %w", err)
	}

	conn, err := db.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("failed to acquire db conn: %w", err)
	}

	_, err = conn.Exec(ctx, fmt.Sprintf("LISTEN %s", notif))
	if err != nil {
		return fmt.Errorf("failed to listen for notifications: %w", err)
	}

	go func() {
		defer conn.Release()

		for {
			notification, err := conn.Conn().WaitForNotification(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}

				slog.ErrorContext(ctx, "error waiting for notification", "error", err)
				continue
			}

			slog.InfoContext(ctx, "postgres notification", "name", notif, "payload", notification.Payload)
			if err := callback(); err != nil {
				slog.ErrorContext(ctx, "error processing notification", "error", err)
			}
		}
	}()

	return nil
}
