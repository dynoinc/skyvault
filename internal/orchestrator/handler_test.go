package orchestrator

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dynoinc/skyvault/internal/database"
	"github.com/dynoinc/skyvault/internal/database/dto"
)

func TestMergeableBatches(t *testing.T) {
	tests := []struct {
		name                 string
		l0Batches            []database.L0Batch
		minL0Batches         int
		maxL0Batches         int
		maxL0MergedBatchSize int
		wantBatchIDs         []database.L0Batch
		wantTotalSize        int64
	}{
		{
			name: "fewer batches than min",
			l0Batches: []database.L0Batch{
				{SeqNo: 1, Attrs: dto.L0BatchAttrs{State: dto.StateCommitted, Path: "batch1", SizeBytes: 1000}},
				{SeqNo: 2, Attrs: dto.L0BatchAttrs{State: dto.StateCommitted, Path: "batch2", SizeBytes: 2000}},
			},
			minL0Batches:         3,
			maxL0Batches:         5,
			maxL0MergedBatchSize: 10000,
			wantBatchIDs:         nil,
			wantTotalSize:        0,
		},
		{
			name: "all batches locked",
			l0Batches: []database.L0Batch{
				{SeqNo: 1, Attrs: dto.L0BatchAttrs{State: dto.StateMerging, Path: "batch1", SizeBytes: 1000}},
				{SeqNo: 2, Attrs: dto.L0BatchAttrs{State: dto.StateMerging, Path: "batch2", SizeBytes: 2000}},
				{SeqNo: 3, Attrs: dto.L0BatchAttrs{State: dto.StateMerging, Path: "batch3", SizeBytes: 3000}},
			},
			minL0Batches:         1,
			maxL0Batches:         3,
			maxL0MergedBatchSize: 10000,
			wantBatchIDs:         nil,
			wantTotalSize:        0,
		},
		{
			name: "some batches locked at beginning",
			l0Batches: []database.L0Batch{
				{SeqNo: 1, Attrs: dto.L0BatchAttrs{State: dto.StateMerging, Path: "batch1", SizeBytes: 1000}},
				{SeqNo: 2, Attrs: dto.L0BatchAttrs{State: dto.StateMerging, Path: "batch2", SizeBytes: 2000}},
				{SeqNo: 3, Attrs: dto.L0BatchAttrs{State: dto.StateCommitted, Path: "batch3", SizeBytes: 3000}},
				{SeqNo: 4, Attrs: dto.L0BatchAttrs{State: dto.StateCommitted, Path: "batch4", SizeBytes: 4000}},
				{SeqNo: 5, Attrs: dto.L0BatchAttrs{State: dto.StateCommitted, Path: "batch5", SizeBytes: 5000}},
			},
			minL0Batches:         2,
			maxL0Batches:         2,
			maxL0MergedBatchSize: 20000,
			wantBatchIDs: []database.L0Batch{
				{SeqNo: 3, Attrs: dto.L0BatchAttrs{State: dto.StateCommitted, Path: "batch3", SizeBytes: 3000}},
				{SeqNo: 4, Attrs: dto.L0BatchAttrs{State: dto.StateCommitted, Path: "batch4", SizeBytes: 4000}},
				{SeqNo: 5, Attrs: dto.L0BatchAttrs{State: dto.StateCommitted, Path: "batch5", SizeBytes: 5000}},
			},
			wantTotalSize: 12000,
		},
		{
			name: "batches need to be sorted",
			l0Batches: []database.L0Batch{
				{SeqNo: 5, Attrs: dto.L0BatchAttrs{State: dto.StateCommitted, Path: "batch5", SizeBytes: 5000}},
				{SeqNo: 3, Attrs: dto.L0BatchAttrs{State: dto.StateCommitted, Path: "batch3", SizeBytes: 3000}},
				{SeqNo: 1, Attrs: dto.L0BatchAttrs{State: dto.StateCommitted, Path: "batch1", SizeBytes: 1000}},
				{SeqNo: 4, Attrs: dto.L0BatchAttrs{State: dto.StateCommitted, Path: "batch4", SizeBytes: 4000}},
				{SeqNo: 2, Attrs: dto.L0BatchAttrs{State: dto.StateCommitted, Path: "batch2", SizeBytes: 2000}},
			},
			minL0Batches:         2,
			maxL0Batches:         2,
			maxL0MergedBatchSize: 20000,
			wantBatchIDs: []database.L0Batch{
				{SeqNo: 1, Attrs: dto.L0BatchAttrs{State: dto.StateCommitted, Path: "batch1", SizeBytes: 1000}},
				{SeqNo: 2, Attrs: dto.L0BatchAttrs{State: dto.StateCommitted, Path: "batch2", SizeBytes: 2000}},
				{SeqNo: 3, Attrs: dto.L0BatchAttrs{State: dto.StateCommitted, Path: "batch3", SizeBytes: 3000}},
				{SeqNo: 4, Attrs: dto.L0BatchAttrs{State: dto.StateCommitted, Path: "batch4", SizeBytes: 4000}},
				{SeqNo: 5, Attrs: dto.L0BatchAttrs{State: dto.StateCommitted, Path: "batch5", SizeBytes: 5000}},
			},
			wantTotalSize: 15000,
		},
		{
			name: "exceed max batches but under size limit",
			l0Batches: []database.L0Batch{
				{SeqNo: 1, Attrs: dto.L0BatchAttrs{State: dto.StateCommitted, Path: "batch1", SizeBytes: 1000}},
				{SeqNo: 2, Attrs: dto.L0BatchAttrs{State: dto.StateCommitted, Path: "batch2", SizeBytes: 1000}},
				{SeqNo: 3, Attrs: dto.L0BatchAttrs{State: dto.StateCommitted, Path: "batch3", SizeBytes: 1000}},
				{SeqNo: 4, Attrs: dto.L0BatchAttrs{State: dto.StateCommitted, Path: "batch4", SizeBytes: 1000}},
			},
			minL0Batches:         2,
			maxL0Batches:         2,
			maxL0MergedBatchSize: 10000,
			wantBatchIDs: []database.L0Batch{
				{SeqNo: 1, Attrs: dto.L0BatchAttrs{State: dto.StateCommitted, Path: "batch1", SizeBytes: 1000}},
				{SeqNo: 2, Attrs: dto.L0BatchAttrs{State: dto.StateCommitted, Path: "batch2", SizeBytes: 1000}},
				{SeqNo: 3, Attrs: dto.L0BatchAttrs{State: dto.StateCommitted, Path: "batch3", SizeBytes: 1000}},
				{SeqNo: 4, Attrs: dto.L0BatchAttrs{State: dto.StateCommitted, Path: "batch4", SizeBytes: 1000}},
			},
			wantTotalSize: 4000,
		},
		{
			name: "exceed size limit but under max batches",
			l0Batches: []database.L0Batch{
				{SeqNo: 1, Attrs: dto.L0BatchAttrs{State: dto.StateCommitted, Path: "batch1", SizeBytes: 4000}},
				{SeqNo: 2, Attrs: dto.L0BatchAttrs{State: dto.StateCommitted, Path: "batch2", SizeBytes: 4000}},
				{SeqNo: 3, Attrs: dto.L0BatchAttrs{State: dto.StateCommitted, Path: "batch3", SizeBytes: 4000}},
			},
			minL0Batches:         2,
			maxL0Batches:         4,
			maxL0MergedBatchSize: 10000,
			wantBatchIDs: []database.L0Batch{
				{SeqNo: 1, Attrs: dto.L0BatchAttrs{State: dto.StateCommitted, Path: "batch1", SizeBytes: 4000}},
				{SeqNo: 2, Attrs: dto.L0BatchAttrs{State: dto.StateCommitted, Path: "batch2", SizeBytes: 4000}},
				{SeqNo: 3, Attrs: dto.L0BatchAttrs{State: dto.StateCommitted, Path: "batch3", SizeBytes: 4000}},
			},
			wantTotalSize: 12000,
		},
		{
			name: "exactly_at_max_batch_size",
			l0Batches: []database.L0Batch{
				{SeqNo: 1, Attrs: dto.L0BatchAttrs{State: dto.StateCommitted, Path: "batch1", SizeBytes: 5000}},
				{SeqNo: 2, Attrs: dto.L0BatchAttrs{State: dto.StateCommitted, Path: "batch2", SizeBytes: 5000}},
				{SeqNo: 3, Attrs: dto.L0BatchAttrs{State: dto.StateCommitted, Path: "batch3", SizeBytes: 5000}},
			},
			minL0Batches:         2,
			maxL0Batches:         4,
			maxL0MergedBatchSize: 15000,
			wantBatchIDs:         nil,
			wantTotalSize:        0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotBatchIDs, gotTotalSize := mergeableBatches(tt.l0Batches, tt.minL0Batches, tt.maxL0Batches, tt.maxL0MergedBatchSize)
			assert.Equal(t, tt.wantBatchIDs, gotBatchIDs)
			assert.Equal(t, tt.wantTotalSize, gotTotalSize)
		})
	}
}
