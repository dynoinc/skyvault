// Code generated by sqlc. DO NOT EDIT.
// versions:
//   sqlc v1.28.0
// source: partitions.sql

package database

import (
	"context"
)

const getPartitions = `-- name: GetPartitions :many
SELECT inclusive_start_key, attrs FROM partitions
`

func (q *Queries) GetPartitions(ctx context.Context) ([]Partition, error) {
	rows, err := q.db.Query(ctx, getPartitions)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []Partition
	for rows.Next() {
		var i Partition
		if err := rows.Scan(&i.InclusiveStartKey, &i.Attrs); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}
