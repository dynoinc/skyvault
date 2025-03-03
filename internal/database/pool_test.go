package database

import (
	"testing"

	dto "github.com/dynoinc/skyvault/internal/database/dto"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestPool(t *testing.T) {
	ctx := t.Context()

	// Start PostgreSQL container with version 16
	postgresContainer, err := postgres.Run(ctx, "postgres:16", postgres.BasicWaitStrategies())
	require.NoError(t, err)
	t.Cleanup(func() { _ = postgresContainer.Terminate(ctx) })

	// Get the connection string
	pgURL, err := postgresContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	// Test the Pool function
	pool, err := Pool(ctx, pgURL)
	require.NoError(t, err)
	defer pool.Close()

	// Verify that the connection works
	var one int
	err = pool.QueryRow(ctx, "SELECT 1").Scan(&one)
	require.NoError(t, err)
	assert.Equal(t, 1, one)

	// Create a Queries instance
	q := New(pool)

	// Verify migrations by checking if the l0_batches table exists and has the expected structure
	var count int
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM l0_batches").Scan(&count)
	require.NoError(t, err, "l0_batches table should exist")
	require.Equal(t, 0, count, "l0_batches table should be empty")

	// Test UpdateL0BatchesStatus
	// 1. Add some test batches
	err = q.AddL0Batch(ctx, dto.L0BatchAttrs{
		Path:      "/test/path1",
		SizeBytes: 100,
		MinKey:    "key1",
		MaxKey:    "key100",
	})
	require.NoError(t, err)

	err = q.AddL0Batch(ctx, dto.L0BatchAttrs{
		Path:      "/test/path2",
		SizeBytes: 200,
		MinKey:    "key101",
		MaxKey:    "key200",
	})
	require.NoError(t, err)

	// 2. Verify the batches are added with ACTIVE status by default
	batches, err := q.GetL0BatchesBySeqNo(ctx, []int64{1, 2})
	require.NoError(t, err)
	require.Len(t, batches, 2)

	// 3. Update the status using UpdateL0Batch
	updated, err := q.UpdateL0Batch(ctx, UpdateL0BatchParams{
		SeqNo:   1,
		Version: 1,
		Attrs:   dto.L0BatchAttrs{State: dto.StateMerging},
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), updated.SeqNo)
	assert.Equal(t, int32(2), updated.Version)
	assert.Equal(t, dto.StateMerging, updated.Attrs.State)

	// 4. Test that UpdateL0BatchesStatus only updates rows with the matching version
	err = q.AddL0Batch(ctx, dto.L0BatchAttrs{
		Path:      "/test/path3",
		SizeBytes: 300,
		MinKey:    "key201",
		MaxKey:    "key300",
	})
	require.NoError(t, err)

	// Try to update all batch with version 5
	updated, err = q.UpdateL0Batch(ctx, UpdateL0BatchParams{
		SeqNo:   1,
		Version: 5,
		Attrs:   dto.L0BatchAttrs{State: dto.StateMerging},
	})
	require.Error(t, err)
	require.Equal(t, pgx.ErrNoRows, err)

	// Try to delete a batch with version 5
	_, err = q.DeleteL0Batch(ctx, DeleteL0BatchParams{
		SeqNo:   1,
		Version: 5,
	})
	require.Error(t, err)
	require.Equal(t, pgx.ErrNoRows, err)
}
