-- name: AddL0Batch :one
INSERT INTO l0_batches (path, size_bytes, min_key, max_key) VALUES (@path, @size_bytes, @min_key, @max_key) RETURNING id;

-- name: GetAllL0Batches :many
SELECT * FROM l0_batches;

-- name: LockL0Batches :one
UPDATE l0_batches
SET status = 'LOCKED'
WHERE id = ANY(@batch_ids::bigint[])
  AND status = 'ACTIVE'
RETURNING count(*);