package background

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"maps"
	"path"
	"slices"
	"sort"

	"github.com/dynoinc/skyvault/internal/database"
	"github.com/dynoinc/skyvault/internal/recordio"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lithammer/shortuuid/v4"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
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
	l0Batches, err := database.New(w.db).GetL0BatchesByID(ctx, job.Args.BatchIDs)
	if err != nil {
		return fmt.Errorf("getting L0 batches: %w", err)
	}

	if len(l0Batches) != len(job.Args.BatchIDs) {
		return fmt.Errorf("some L0 batches were not found: %d", len(job.Args.BatchIDs)-len(l0Batches))
	}

	// sort the L0 batches by ID (descending)
	sort.Slice(l0Batches, func(i, j int) bool {
		return l0Batches[i].ID > l0Batches[j].ID
	})

	// read all the objects from the L0 batches
	objs := make([][]byte, len(l0Batches))
	for i, l0Batch := range l0Batches {
		obj, err := w.objstore.Get(ctx, l0Batch.Path)
		if err != nil {
			return fmt.Errorf("getting object: %w", err)
		}
		defer obj.Close()

		buf := bytes.NewBuffer(nil)
		if _, err := io.Copy(buf, obj); err != nil {
			return fmt.Errorf("copying object: %w", err)
		}

		objs[i] = buf.Bytes()
	}

	merged := map[string]recordio.Record{}
	for _, obj := range objs {
		for record := range recordio.Records(obj) {
			if _, ok := merged[record.Key]; ok {
				continue
			}

			merged[record.Key] = record
		}
	}

	records := slices.Collect(maps.Values(merged))
	sort.Slice(records, func(i, j int) bool {
		return records[i].Key < records[j].Key
	})

	// Calculate size bytes using ComputeSize
	sizeBytes := int64(recordio.ComputeSize(slices.Values(records)))

	minKey := records[0].Key
	maxKey := records[len(records)-1].Key

	// Write records to buffer
	buf := recordio.WriteRecords(slices.Values(records))

	// Generate a short UUID for the batch
	id := shortuuid.New()

	// Write batch to object store under l0_batches directory
	objPath := path.Join("l0_batches", id)
	if err := w.objstore.Upload(ctx, objPath, bytes.NewReader(buf)); err != nil {
		return fmt.Errorf("writing batch to storage: %w", err)
	}

	tx, err := w.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	qtx := database.New(tx)
	if _, err := qtx.AddL0Batch(ctx, database.AddL0BatchParams{
		Path:      objPath,
		SizeBytes: sizeBytes,
		MinKey:    minKey,
		MaxKey:    maxKey,
	}); err != nil {
		return fmt.Errorf("adding batch record: %w", err)
	}

	if err := qtx.DeleteL0Batches(ctx, database.DeleteL0BatchesParams{
		BatchIds:      job.Args.BatchIDs,
		CurrentStatus: "LOCKED",
	}); err != nil {
		return fmt.Errorf("deleting L0 batches: %w", err)
	}

	if _, err = river.JobCompleteTx[*riverpgxv5.Driver](ctx, tx, job); err != nil {
		return fmt.Errorf("completing job: %w", err)
	}

	return tx.Commit(ctx)
}
