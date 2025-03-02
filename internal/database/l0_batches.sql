-- name: AddL0Batch :one
INSERT INTO l0_batches (path, size_bytes, min_key, max_key) VALUES (@path, @size_bytes, @min_key, @max_key) RETURNING id;

-- name: GetL0Batches :many
SELECT * FROM l0_batches;

-- name: GetL0BatchesByID :many
SELECT * FROM l0_batches
WHERE id = ANY(@batch_ids::bigint[]);

-- name: UpdateL0BatchesStatus :one
UPDATE l0_batches
SET status = @new_status::text
WHERE id = ANY(@batch_ids::bigint[])
  AND status = @current_status::text
RETURNING count(*);

-- name: DeleteL0Batches :exec
DELETE FROM l0_batches
WHERE id = ANY(@batch_ids::bigint[])
  AND status = @current_status::text;
