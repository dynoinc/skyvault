package background

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"path"
	"slices"
	"sort"
	"time"

	"github.com/dynoinc/skyvault/internal/database"
	"github.com/dynoinc/skyvault/internal/recordio"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lithammer/shortuuid/v4"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/thanos-io/objstore"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	commonv1 "github.com/dynoinc/skyvault/gen/proto/common/v1"
)

type MergeL0BatchesArgs struct {
	Batches []database.L0Batch
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
	// sort the L0 batches by SeqNo (descending), because newer values take precedence
	sort.Slice(job.Args.Batches, func(i, j int) bool {
		return job.Args.Batches[i].SeqNo > job.Args.Batches[j].SeqNo
	})

	// read all the objects from the L0 batches
	objs := make([][]byte, len(job.Args.Batches))
	for i, batch := range job.Args.Batches {
		obj, err := w.objstore.Get(ctx, batch.Attrs.GetPath())
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

	// update the smallest seqno batch with the updated info
	batchToUpdate := job.Args.Batches[len(job.Args.Batches)-1]
	if _, err := qtx.UpdateL0Batch(ctx, database.UpdateL0BatchParams{
		SeqNo:   batchToUpdate.SeqNo,
		Version: batchToUpdate.Version,
		Attrs: commonv1.L0Batch_builder{
			State:     commonv1.L0Batch_MERGED.Enum(),
			Path:      proto.String(objPath),
			CreatedAt: timestamppb.New(time.Now()),
			SizeBytes: proto.Int64(sizeBytes),
			MinKey:    proto.String(minKey),
			MaxKey:    proto.String(maxKey),
		}.Build(),
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			slog.WarnContext(ctx, "concurrent update, no-oping merge", "seqNo", batchToUpdate.SeqNo, "version", batchToUpdate.Version)
			return nil
		}

		return fmt.Errorf("adding batch record: %w", err)
	}

	for _, batch := range job.Args.Batches[:len(job.Args.Batches)-1] {
		if _, err := qtx.DeleteL0Batch(ctx, database.DeleteL0BatchParams{
			SeqNo:   batch.SeqNo,
			Version: batch.Version,
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				slog.WarnContext(ctx, "concurrent update, no-oping merge", "seqNo", batch.SeqNo, "version", batch.Version)
				return nil
			}

			return fmt.Errorf("deleting batch record: %w", err)
		}
	}

	if _, err = river.JobCompleteTx[*riverpgxv5.Driver](ctx, tx, job); err != nil {
		return fmt.Errorf("completing job: %w", err)
	}

	return tx.Commit(ctx)
}
