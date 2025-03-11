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

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lithammer/shortuuid/v4"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/thanos-io/objstore"

	commonv1 "github.com/dynoinc/skyvault/gen/proto/common/v1"
	"github.com/dynoinc/skyvault/internal/database"
	"github.com/dynoinc/skyvault/internal/sstable"
)

type CompactWALsArgs struct {
	WALs []database.WriteAheadLog
}

func (m CompactWALsArgs) Kind() string {
	return "CompactWALs"
}

type CompactWALsWorker struct {
	river.WorkerDefaults[CompactWALsArgs]

	db       *pgxpool.Pool
	objstore objstore.Bucket
}

func NewCompactWALsWorker(db *pgxpool.Pool, objstore objstore.Bucket) *CompactWALsWorker {
	return &CompactWALsWorker{
		db:       db,
		objstore: objstore,
	}
}

func (w *CompactWALsWorker) Work(ctx context.Context, job *river.Job[CompactWALsArgs]) error {
	// sort WALs by SeqNo (descending), because newer values take precedence
	sort.Slice(job.Args.WALs, func(i, j int) bool {
		return job.Args.WALs[i].SeqNo > job.Args.WALs[j].SeqNo
	})

	// read all the objects from the WALs
	objs := make([][]byte, len(job.Args.WALs))
	for i, wal := range job.Args.WALs {
		obj, err := w.objstore.Get(ctx, wal.Attrs.GetPath())
		if err != nil {
			return fmt.Errorf("getting object: %w", err)
		}
		defer obj.Close()

		buf := bytes.NewBuffer(nil)
		buf.Grow(int(wal.Attrs.GetSizeBytes()))
		if _, err := io.Copy(buf, obj); err != nil {
			return fmt.Errorf("copying object: %w", err)
		}

		objs[i] = buf.Bytes()
	}

	merged := map[string]sstable.Record{}
	for _, obj := range objs {
		for record := range sstable.Records(obj) {
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

	// Write records to buffer
	buf := sstable.WriteRecords(slices.Values(records))

	// Generate a short UUID for the batch
	id := shortuuid.New()

	// Write batch to object store under l0_batches directory
	objPath := path.Join("runs", id+".sstable")
	if err := w.objstore.Upload(ctx, objPath, bytes.NewReader(buf)); err != nil {
		return fmt.Errorf("writing batch to storage: %w", err)
	}

	tx, err := w.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	qtx := database.New(tx)

	// Insert a shared run
	if err := qtx.AddSharedRun(ctx, commonv1.Run_builder{
		Path: objPath,
	}.Build()); err != nil {
		return fmt.Errorf("adding shared run: %w", err)
	}

	seqNos := make([]int64, len(job.Args.WALs))
	for i, wal := range job.Args.WALs {
		seqNos[i] = wal.SeqNo
	}

	if _, err := qtx.DeleteWriteAheadLogs(ctx, seqNos); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			slog.WarnContext(ctx, "concurrent update, no-oping merge", "seqNo", seqNos)
			return nil
		}

		return fmt.Errorf("deleting batch record: %w", err)
	}

	if _, err := river.JobCompleteTx[*riverpgxv5.Driver](ctx, tx, job); err != nil {
		return fmt.Errorf("completing job: %w", err)
	}

	return tx.Commit(ctx)
}
