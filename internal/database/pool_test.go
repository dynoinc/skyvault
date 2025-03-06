package database

import (
	"testing"
	"time"

	v1 "github.com/dynoinc/skyvault/gen/proto/common/v1"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tc "github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestPool(t *testing.T) {
	ctx := t.Context()

	// Start PostgreSQL container with version 16
	postgresContainer, err := postgres.Run(ctx, "postgres:16", tc.CustomizeRequestOption(func(req *tc.GenericContainerRequest) error {
		req.ProviderType = tc.ProviderPodman
		return nil
	}), postgres.BasicWaitStrategies())

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
	id := "1"
	createdAt := timestamppb.New(time.Now())
	err = q.AddL0Batch(ctx, v1.L0Batch_builder{
		Id:        &id,
		CreatedAt: createdAt,
		State:     v1.L0Batch_NEW.Enum(),
	}.Build())
	require.NoError(t, err)

	id2 := "2"
	createdAt2 := timestamppb.New(createdAt.AsTime().Add(time.Second))
	err = q.AddL0Batch(ctx, v1.L0Batch_builder{
		Id:        &id2,
		CreatedAt: createdAt2,
		State:     v1.L0Batch_MERGING.Enum(),
	}.Build())
	require.NoError(t, err)

	// 2. Verify the batches are added with ACTIVE status by default
	batches, err := q.GetL0BatchesBySeqNo(ctx, []int64{1, 2})
	require.NoError(t, err)
	require.Len(t, batches, 2)

	// 3. Update the status using UpdateL0Batch
	updated, err := q.UpdateL0Batch(ctx, UpdateL0BatchParams{
		SeqNo:   1,
		Version: 1,
		Attrs:   v1.L0Batch_builder{State: v1.L0Batch_MERGING.Enum()}.Build(),
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), updated.SeqNo)
	assert.Equal(t, int32(2), updated.Version)
	assert.Equal(t, v1.L0Batch_MERGING, updated.Attrs.GetState())

	// 4. Test that UpdateL0BatchesStatus only updates rows with the matching version
	id3 := "3"
	createdAt3 := timestamppb.New(createdAt.AsTime().Add(time.Second * 2))
	err = q.AddL0Batch(ctx, v1.L0Batch_builder{
		Id:        &id3,
		CreatedAt: createdAt3,
		State:     v1.L0Batch_MERGING.Enum(),
	}.Build())
	require.NoError(t, err)

	// Try to update all batch with version 5
	updated, err = q.UpdateL0Batch(ctx, UpdateL0BatchParams{
		SeqNo:   1,
		Version: 5,
		Attrs:   v1.L0Batch_builder{State: v1.L0Batch_MERGED.Enum()}.Build(),
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
