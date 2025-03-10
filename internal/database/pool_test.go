package database

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tc "github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1 "github.com/dynoinc/skyvault/gen/proto/common/v1"
)

func setupDB(t *testing.T) *Queries {
	ctx := t.Context()

	// https://github.com/testcontainers/testcontainers-go/issues/2264
	t.Setenv("TESTCONTAINERS_RYUK_DISABLED", "true")

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
	t.Cleanup(func() { pool.Close() })

	// Verify that the connection works
	var one int
	err = pool.QueryRow(ctx, "SELECT 1").Scan(&one)
	require.NoError(t, err)
	assert.Equal(t, 1, one)

	return New(pool)
}

func TestL0Batches(t *testing.T) {
	ctx := t.Context()
	q := setupDB(t)

	// Verify migrations by checking if the l0_batches table exists and has the expected structure
	l0Batches, err := q.GetL0Batches(ctx)
	require.NoError(t, err)
	require.Len(t, l0Batches, 0)

	// Test UpdateL0Batch
	// 1. Add some test batches
	createdAt := timestamppb.New(time.Now())
	err = q.AddL0Batch(ctx, v1.L0Batch_builder{
		CreatedAt: createdAt,
		State:     v1.L0Batch_NEW,
	}.Build())
	require.NoError(t, err)

	createdAt2 := timestamppb.New(createdAt.AsTime().Add(time.Second))
	err = q.AddL0Batch(ctx, v1.L0Batch_builder{
		CreatedAt: createdAt2,
		State:     v1.L0Batch_NEW,
	}.Build())
	require.NoError(t, err)

	// 2. Verify the batches are added
	batches, err := q.GetL0BatchesBySeqNo(ctx, []int64{1, 2})
	require.NoError(t, err)
	require.Len(t, batches, 2)

	// 3. Update the status using UpdateL0Batch
	updated, err := q.UpdateL0Batch(ctx, UpdateL0BatchParams{
		SeqNo:   1,
		Version: 1,
		Attrs:   v1.L0Batch_builder{State: v1.L0Batch_MERGING}.Build(),
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), updated.SeqNo)
	assert.Equal(t, int32(2), updated.Version)
	assert.Equal(t, v1.L0Batch_MERGING, updated.Attrs.GetState())
	assert.Equal(t, createdAt, updated.Attrs.GetCreatedAt())

	// 4. Test that UpdateL0Batch only updates rows with the matching version
	updated, err = q.UpdateL0Batch(ctx, UpdateL0BatchParams{
		SeqNo:   1,
		Version: 5,
		Attrs:   v1.L0Batch_builder{State: v1.L0Batch_MERGED}.Build(),
	})
	require.Error(t, err)
	require.Equal(t, pgx.ErrNoRows, err)

	// 5. Try to delete a batch with version 5
	_, err = q.DeleteL0Batch(ctx, DeleteL0BatchParams{
		SeqNo:   1,
		Version: 5,
	})
	require.Error(t, err)
	require.Equal(t, pgx.ErrNoRows, err)
}

func TestPartitions(t *testing.T) {
	ctx := t.Context()
	q := setupDB(t)

	// Get partitions
	partitions, err := q.GetPartitions(ctx)
	require.NoError(t, err)
	require.Len(t, partitions, 1)
	assert.Equal(t, "", partitions[0].InclusiveStartKey)
	assert.True(t, proto.Equal(v1.Partition_builder{}.Build(), partitions[0].Attrs))
}
