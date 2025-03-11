package database

import (
	"testing"
	"time"

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

func TestWriteAheadLogs(t *testing.T) {
	ctx := t.Context()
	q := setupDB(t)

	// Verify migrations by checking if the l0_batches table exists and has the expected structure
	writeAheadLogs, err := q.GetWriteAheadLogs(ctx)
	require.NoError(t, err)
	require.Len(t, writeAheadLogs, 0)

	// 1. Add some test logs
	createdAt := timestamppb.New(time.Now())
	err = q.AddWriteAheadLog(ctx, v1.WriteAheadLog_builder{
		CreatedAt: createdAt,
	}.Build())
	require.NoError(t, err)

	createdAt2 := timestamppb.New(createdAt.AsTime().Add(time.Second))
	err = q.AddWriteAheadLog(ctx, v1.WriteAheadLog_builder{
		CreatedAt: createdAt2,
	}.Build())
	require.NoError(t, err)

	// 2. Verify the logs are added
	logs, err := q.GetWriteAheadLogs(ctx)
	require.NoError(t, err)
	require.Len(t, logs, 2)

	// 3. Updating the status with missing logs should fail
	updated, err := q.MarkWriteAheadLogsAsCompacting(ctx, []int64{1, 2, 3})
	require.NoError(t, err)
	assert.False(t, updated)

	// 4. Update the status using MarkWriteAheadLogsAsCompacting
	updated, err = q.MarkWriteAheadLogsAsCompacting(ctx, []int64{1})
	require.NoError(t, err)
	assert.True(t, updated)

	// 5. Trying to delete a compacting batch should fail
	updated, err = q.DeleteWriteAheadLogs(ctx, []int64{1, 2})
	require.NoError(t, err)
	assert.False(t, updated)

	// 6. Trying to delete only compacting logs should succeed
	updated, err = q.DeleteWriteAheadLogs(ctx, []int64{1})
	require.NoError(t, err)
	assert.True(t, updated)
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
