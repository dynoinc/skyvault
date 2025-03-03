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
		maxL0Batches         int
		minL0MergedBatchSize int
		maxL0MergedBatchSize int
		wantBatchIDs         []database.L0Batch
		wantTotalSize        int64
	}{
		{
			name: "fewer batches than max",
			l0Batches: []database.L0Batch{
				{Attrs: dto.L0BatchAttrs{Path: "batch1", SizeBytes: 1000}},
				{Attrs: dto.L0BatchAttrs{Path: "batch2", SizeBytes: 2000}},
			},
			maxL0Batches:         3,
			minL0MergedBatchSize: 10000,
			maxL0MergedBatchSize: 10000,
			wantBatchIDs:         nil,
			wantTotalSize:        0,
		},
		{
			name: "all batches locked",
			l0Batches: []database.L0Batch{
				{Attrs: dto.L0BatchAttrs{Path: "batch1", SizeBytes: 1000}},
				{Attrs: dto.L0BatchAttrs{Path: "batch2", SizeBytes: 2000}},
				{Attrs: dto.L0BatchAttrs{Path: "batch3", SizeBytes: 3000}},
				{Attrs: dto.L0BatchAttrs{Path: "batch4", SizeBytes: 4000}},
				{Attrs: dto.L0BatchAttrs{Path: "batch5", SizeBytes: 5000}},
			},
			maxL0Batches:         3,
			minL0MergedBatchSize: 10000,
			maxL0MergedBatchSize: 10000,
			wantBatchIDs:         nil,
			wantTotalSize:        0,
		},
		{
			name: "some batches locked at beginning",
			l0Batches: []database.L0Batch{
				{Attrs: dto.L0BatchAttrs{Path: "batch1", SizeBytes: 1000}},
				{Attrs: dto.L0BatchAttrs{Path: "batch2", SizeBytes: 2000}},
				{Attrs: dto.L0BatchAttrs{Path: "batch3", SizeBytes: 3000}},
				{Attrs: dto.L0BatchAttrs{Path: "batch4", SizeBytes: 4000}},
				{Attrs: dto.L0BatchAttrs{Path: "batch5", SizeBytes: 5000}},
			},
			maxL0Batches:         2,
			minL0MergedBatchSize: 10000,
			maxL0MergedBatchSize: 20000,
			wantBatchIDs: []database.L0Batch{
				{Attrs: dto.L0BatchAttrs{Path: "batch3", SizeBytes: 3000}},
				{Attrs: dto.L0BatchAttrs{Path: "batch4", SizeBytes: 4000}},
				{Attrs: dto.L0BatchAttrs{Path: "batch5", SizeBytes: 5000}},
			},
			wantTotalSize: 12000,
		},
		{
			name: "batches need to be sorted",
			l0Batches: []database.L0Batch{
				{Attrs: dto.L0BatchAttrs{Path: "batch5", SizeBytes: 5000}},
				{Attrs: dto.L0BatchAttrs{Path: "batch3", SizeBytes: 3000}},
				{Attrs: dto.L0BatchAttrs{Path: "batch1", SizeBytes: 1000}},
				{Attrs: dto.L0BatchAttrs{Path: "batch4", SizeBytes: 4000}},
				{Attrs: dto.L0BatchAttrs{Path: "batch2", SizeBytes: 2000}},
			},
			maxL0Batches:         3,
			minL0MergedBatchSize: 10000,
			maxL0MergedBatchSize: 20000,
			wantBatchIDs: []database.L0Batch{
				{Attrs: dto.L0BatchAttrs{Path: "batch1", SizeBytes: 1000}},
				{Attrs: dto.L0BatchAttrs{Path: "batch2", SizeBytes: 2000}},
				{Attrs: dto.L0BatchAttrs{Path: "batch3", SizeBytes: 3000}},
				{Attrs: dto.L0BatchAttrs{Path: "batch4", SizeBytes: 4000}},
				{Attrs: dto.L0BatchAttrs{Path: "batch5", SizeBytes: 5000}},
			},
			wantTotalSize: 15000,
		},
		{
			name: "batches limited by size",
			l0Batches: []database.L0Batch{
				{Attrs: dto.L0BatchAttrs{Path: "batch1", SizeBytes: 3000}},
				{Attrs: dto.L0BatchAttrs{Path: "batch2", SizeBytes: 4000}},
				{Attrs: dto.L0BatchAttrs{Path: "batch3", SizeBytes: 5000}},
				{Attrs: dto.L0BatchAttrs{Path: "batch4", SizeBytes: 6000}},
				{Attrs: dto.L0BatchAttrs{Path: "batch5", SizeBytes: 7000}},
			},
			maxL0Batches:         3,
			minL0MergedBatchSize: 10000,
			maxL0MergedBatchSize: 10000,
			wantBatchIDs: []database.L0Batch{
				{Attrs: dto.L0BatchAttrs{Path: "batch1", SizeBytes: 3000}},
				{Attrs: dto.L0BatchAttrs{Path: "batch2", SizeBytes: 4000}},
				{Attrs: dto.L0BatchAttrs{Path: "batch3", SizeBytes: 5000}},
			},
			wantTotalSize: 12000,
		},
		{
			name: "only one batch to merge",
			l0Batches: []database.L0Batch{
				{Attrs: dto.L0BatchAttrs{Path: "batch1", SizeBytes: 3000}},
				{Attrs: dto.L0BatchAttrs{Path: "batch2", SizeBytes: 4000}},
				{Attrs: dto.L0BatchAttrs{Path: "batch3", SizeBytes: 5000}},
				{Attrs: dto.L0BatchAttrs{Path: "batch4", SizeBytes: 6000}},
			},
			maxL0Batches:         3,
			minL0MergedBatchSize: 10000,
			maxL0MergedBatchSize: 20000,
			wantBatchIDs:         nil,
			wantTotalSize:        0,
		},
		{
			name: "exactly at max batch size",
			l0Batches: []database.L0Batch{
				{Attrs: dto.L0BatchAttrs{Path: "batch1", SizeBytes: 5000}},
				{Attrs: dto.L0BatchAttrs{Path: "batch2", SizeBytes: 5000}},
				{Attrs: dto.L0BatchAttrs{Path: "batch3", SizeBytes: 5000}},
				{Attrs: dto.L0BatchAttrs{Path: "batch4", SizeBytes: 5000}},
				{Attrs: dto.L0BatchAttrs{Path: "batch5", SizeBytes: 5000}},
			},
			maxL0Batches:         3,
			minL0MergedBatchSize: 10000,
			maxL0MergedBatchSize: 10000,
			wantBatchIDs: []database.L0Batch{
				{Attrs: dto.L0BatchAttrs{Path: "batch1", SizeBytes: 5000}},
				{Attrs: dto.L0BatchAttrs{Path: "batch2", SizeBytes: 5000}},
			},
			wantTotalSize: 10000,
		},
		{
			name: "below min batch size",
			l0Batches: []database.L0Batch{
				{Attrs: dto.L0BatchAttrs{Path: "batch1", SizeBytes: 3000}},
				{Attrs: dto.L0BatchAttrs{Path: "batch2", SizeBytes: 4000}},
				{Attrs: dto.L0BatchAttrs{Path: "batch3", SizeBytes: 5000}},
				{Attrs: dto.L0BatchAttrs{Path: "batch4", SizeBytes: 6000}},
				{Attrs: dto.L0BatchAttrs{Path: "batch5", SizeBytes: 7000}},
			},
			maxL0Batches:         3,
			minL0MergedBatchSize: 30000, // Set higher than total size
			maxL0MergedBatchSize: 40000,
			wantBatchIDs:         nil,
			wantTotalSize:        0,
		},
		{
			name: "exactly at min batch size",
			l0Batches: []database.L0Batch{
				{Attrs: dto.L0BatchAttrs{Path: "batch1", SizeBytes: 5000}},
				{Attrs: dto.L0BatchAttrs{Path: "batch2", SizeBytes: 5000}},
				{Attrs: dto.L0BatchAttrs{Path: "batch3", SizeBytes: 5000}},
				{Attrs: dto.L0BatchAttrs{Path: "batch4", SizeBytes: 5000}},
				{Attrs: dto.L0BatchAttrs{Path: "batch5", SizeBytes: 5000}},
			},
			maxL0Batches:         3,
			minL0MergedBatchSize: 25000, // Exactly the total size of all batches
			maxL0MergedBatchSize: 30000,
			wantBatchIDs: []database.L0Batch{
				{Attrs: dto.L0BatchAttrs{Path: "batch1", SizeBytes: 5000}},
				{Attrs: dto.L0BatchAttrs{Path: "batch2", SizeBytes: 5000}},
				{Attrs: dto.L0BatchAttrs{Path: "batch3", SizeBytes: 5000}},
				{Attrs: dto.L0BatchAttrs{Path: "batch4", SizeBytes: 5000}},
				{Attrs: dto.L0BatchAttrs{Path: "batch5", SizeBytes: 5000}},
			},
			wantTotalSize: 25000,
		},
		{
			name: "multiple batches but not enough size",
			l0Batches: []database.L0Batch{
				{Attrs: dto.L0BatchAttrs{Path: "batch1", SizeBytes: 1000}},
				{Attrs: dto.L0BatchAttrs{Path: "batch2", SizeBytes: 1000}},
				{Attrs: dto.L0BatchAttrs{Path: "batch3", SizeBytes: 1000}},
				{Attrs: dto.L0BatchAttrs{Path: "batch4", SizeBytes: 1000}},
				{Attrs: dto.L0BatchAttrs{Path: "batch5", SizeBytes: 1000}},
			},
			maxL0Batches:         2,
			minL0MergedBatchSize: 10000,
			maxL0MergedBatchSize: 20000,
			wantBatchIDs:         nil,
			wantTotalSize:        0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotBatchIDs, gotTotalSize := mergeableBatches(tt.l0Batches, tt.maxL0Batches, tt.minL0MergedBatchSize, tt.maxL0MergedBatchSize)
			assert.Equal(t, tt.wantBatchIDs, gotBatchIDs)
			assert.Equal(t, tt.wantTotalSize, gotTotalSize)
		})
	}
}
