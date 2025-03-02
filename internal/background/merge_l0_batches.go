package background

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/thanos-io/objstore"
)

type MergeL0BatchesArgs struct {
	BatchIDs []int64
}

func (m MergeL0BatchesArgs) Kind() string {
	return "MergeL0Batches"
}

type MergeL0BatchesWorker struct {
	river.WorkerDefaults[MergeL0BatchesArgs]

	db       *pgxpool.Pool
	objstore objstore.Bucket
}

func NewMergeL0BatchesWorker(db *pgxpool.Pool, objstore objstore.Bucket) *MergeL0BatchesWorker {
	return &MergeL0BatchesWorker{
		db:       db,
		objstore: objstore,
	}
}

func (w *MergeL0BatchesWorker) Work(ctx context.Context, job *river.Job[MergeL0BatchesArgs]) error {
	return nil
}
