-- name: InitPartitions :exec
INSERT INTO partitions (inclusive_start_key, attrs)
SELECT '', @attrs
WHERE NOT EXISTS (SELECT 1 FROM partitions);

-- name: GetPartitions :many
SELECT * FROM partitions;
