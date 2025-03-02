package database

import (
	"testing"

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
	batch1ID, err := q.AddL0Batch(ctx, AddL0BatchParams{
		Path:      "/test/path1",
		SizeBytes: 100,
		MinKey:    "key1",
		MaxKey:    "key100",
	})
	require.NoError(t, err)

	batch2ID, err := q.AddL0Batch(ctx, AddL0BatchParams{
		Path:      "/test/path2",
		SizeBytes: 200,
		MinKey:    "key101",
		MaxKey:    "key200",
	})
	require.NoError(t, err)

	// 2. Verify the batches are added with ACTIVE status by default
	batches, err := q.GetL0BatchesByID(ctx, []int64{batch1ID, batch2ID})
	require.NoError(t, err)
	require.Len(t, batches, 2)
	assert.Equal(t, "ACTIVE", batches[0].Status)
	assert.Equal(t, "ACTIVE", batches[1].Status)

	// 3. Update the status using UpdateL0BatchesStatus
	updated, err := q.UpdateL0BatchesStatus(ctx, UpdateL0BatchesStatusParams{
		NewStatus:     "COMPACTED",
		BatchIds:      []int64{batch1ID, batch2ID},
		CurrentStatus: "ACTIVE",
	})
	require.NoError(t, err)
	assert.Equal(t, int64(2), updated, "Both batches should be updated")

	// 4. Verify the status was updated
	batches, err = q.GetL0BatchesByID(ctx, []int64{batch1ID, batch2ID})
	require.NoError(t, err)
	require.Len(t, batches, 2)
	assert.Equal(t, "COMPACTED", batches[0].Status)
	assert.Equal(t, "COMPACTED", batches[1].Status)

	// 5. Test that UpdateL0BatchesStatus only updates rows with the matching current status
	_, err = q.AddL0Batch(ctx, AddL0BatchParams{
		Path:      "/test/path3",
		SizeBytes: 300,
		MinKey:    "key201",
		MaxKey:    "key300",
	})
	require.NoError(t, err)

	// Try to update all batches but only those with COMPACTED status should be affected
	updated, err = q.UpdateL0BatchesStatus(ctx, UpdateL0BatchesStatusParams{
		NewStatus:     "DELETED",
		BatchIds:      []int64{batch1ID, batch2ID},
		CurrentStatus: "COMPACTED",
	})
	require.NoError(t, err)
	assert.Equal(t, int64(2), updated, "Only the compacted batches should be updated")
}
