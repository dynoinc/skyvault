-- name: AddL0Batch :exec
INSERT INTO l0_batches (attrs) VALUES (@attrs);

-- name: GetL0Batches :many
SELECT * FROM l0_batches;

-- name: GetL0BatchesBySeqNo :many
SELECT * FROM l0_batches
WHERE seq_no = ANY(@batch_seq_nos::bigint[]);

-- name: UpdateL0Batch :one
UPDATE l0_batches
SET attrs = attrs || @attrs
WHERE seq_no = @seq_no AND version = @version
RETURNING *;

-- name: DeleteL0Batch :one
DELETE FROM l0_batches
WHERE seq_no = @seq_no AND version = @version
RETURNING *;