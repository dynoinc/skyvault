package orchestrator

import (
	"testing"

	"github.com/stretchr/testify/assert"

	commonv1 "github.com/dynoinc/skyvault/gen/proto/common/v1"
	"github.com/dynoinc/skyvault/internal/database"
)

func TestMergeableBatches(t *testing.T) {
	type l0Batch struct {
		State commonv1.L0Batch_State
		Size  int64
	}

	tests := []struct {
		name                 string
		l0Batches            []l0Batch
		minL0Batches         int
		minL0MergedBatchSize int
		wantBatchIDs         []int64
		wantTotalSize        int64
	}{
		{
			name: "all batches locked",
			l0Batches: []l0Batch{
				{State: commonv1.L0Batch_MERGING, Size: 1000},
				{State: commonv1.L0Batch_MERGING, Size: 2000},
				{State: commonv1.L0Batch_MERGING, Size: 3000},
			},
			minL0Batches:         1,
			minL0MergedBatchSize: 10000,
			wantBatchIDs:         nil,
			wantTotalSize:        0,
		},
		{
			name: "fewer batches than min",
			l0Batches: []l0Batch{
				{State: commonv1.L0Batch_NEW, Size: 1000},
				{State: commonv1.L0Batch_NEW, Size: 2000},
			},
			minL0Batches:         3,
			minL0MergedBatchSize: 10000,
			wantBatchIDs:         nil,
			wantTotalSize:        0,
		},
		{
			name: "exceeds minL0Batches",
			l0Batches: []l0Batch{
				{State: commonv1.L0Batch_MERGING, Size: 1000},
				{State: commonv1.L0Batch_MERGING, Size: 2000},
				{State: commonv1.L0Batch_NEW, Size: 3000},
				{State: commonv1.L0Batch_NEW, Size: 4000},
				{State: commonv1.L0Batch_NEW, Size: 5000},
			},
			minL0Batches:         2,
			minL0MergedBatchSize: 20000,
			wantBatchIDs:         []int64{3, 4, 5},
			wantTotalSize:        12000,
		},
		{
			name: "exceeds minL0MergedBatchSize",
			l0Batches: []l0Batch{
				{State: commonv1.L0Batch_NEW, Size: 1000},
				{State: commonv1.L0Batch_NEW, Size: 2000},
			},
			minL0Batches:         5,
			minL0MergedBatchSize: 2000,
			wantBatchIDs:         []int64{1, 2},
			wantTotalSize:        3000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l0Batches := make([]database.L0Batch, len(tt.l0Batches))
			for i, batch := range tt.l0Batches {
				l0Batches[i] = database.L0Batch{
					SeqNo: int64(i + 1),
					Attrs: commonv1.L0Batch_builder{
						State:     batch.State,
						SizeBytes: batch.Size,
					}.Build(),
				}
			}

			gotBatches, gotTotalSize := mergeableBatches(l0Batches, tt.minL0Batches, tt.minL0MergedBatchSize)
			gotBatchIDs := make([]int64, len(gotBatches))
			for i, batch := range gotBatches {
				gotBatchIDs[i] = batch.SeqNo
			}

			assert.ElementsMatch(t, tt.wantBatchIDs, gotBatchIDs)
			assert.Equal(t, tt.wantTotalSize, gotTotalSize)
		})
	}
}
