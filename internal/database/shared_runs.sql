-- name: AddSharedRun :exec
INSERT INTO shared_runs (attrs)
VALUES (@attrs);

-- name: GetSharedRuns :many
SELECT *
FROM shared_runs;

-- name: DeleteSharedRuns :one
WITH validation AS (
  SELECT COUNT(*) = array_length(@seq_nos::bigint[], 1) as all_exist
  FROM shared_runs 
  WHERE seq_no = ANY (@seq_nos::bigint[])
),
delete_result AS (
  DELETE FROM shared_runs
  WHERE seq_no = ANY (@seq_nos::bigint[])
  AND EXISTS (
    SELECT 1 FROM validation WHERE all_exist
  )
  RETURNING seq_no
)
SELECT EXISTS (
  SELECT 1 FROM validation WHERE all_exist
  AND EXISTS (SELECT 1 FROM delete_result)
);