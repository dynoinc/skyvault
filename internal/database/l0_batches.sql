-- name: AddL0Batch :one
INSERT INTO l0_batches (path, size_bytes, min_key, max_key) VALUES (@path, @size_bytes, @min_key, @max_key) RETURNING id;

-- name: GetAllL0Batches :many
SELECT * FROM l0_batches;