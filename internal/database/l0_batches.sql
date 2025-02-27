-- name: AddL0Batch :one
INSERT INTO l0_batches (path) VALUES ($1) RETURNING id;

-- name: GetAllL0Batches :many
SELECT * FROM l0_batches;