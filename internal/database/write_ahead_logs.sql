-- name: AddWriteAheadLog :exec
INSERT INTO write_ahead_logs (attrs)
VALUES (@attrs);

-- name: GetWriteAheadLogs :many
SELECT *
FROM write_ahead_logs;

-- name: GetWriteAheadLogsToCompact :many
SELECT *
FROM write_ahead_logs
WHERE state = 'NEW';

-- name: MarkWriteAheadLogsAsCompacting :one
WITH validation AS (
  SELECT COUNT(*) = array_length(@seq_nos::bigint[], 1) as all_exist
  FROM write_ahead_logs 
  WHERE seq_no = ANY (@seq_nos::bigint[])
  AND state = 'NEW'
),
update_result AS (
  UPDATE write_ahead_logs
  SET state = 'COMPACTING'
  WHERE seq_no = ANY (@seq_nos::bigint[])
  AND state = 'NEW'
  AND EXISTS (
    SELECT 1 FROM validation WHERE all_exist
  )
  RETURNING seq_no
)
SELECT EXISTS (
  SELECT 1 FROM validation WHERE all_exist
  AND EXISTS (SELECT 1 FROM update_result)
);

-- name: DeleteWriteAheadLogs :one
WITH validation AS (
  SELECT COUNT(*) = array_length(@seq_nos::bigint[], 1) as all_exist_and_compacting
  FROM write_ahead_logs 
  WHERE seq_no = ANY (@seq_nos::bigint[])
  AND state = 'COMPACTING'
),
delete_result AS (
  DELETE FROM write_ahead_logs
  WHERE seq_no = ANY (@seq_nos::bigint[])
  AND state = 'COMPACTING'
  AND EXISTS (
    SELECT 1 FROM validation WHERE all_exist_and_compacting
  )
  RETURNING seq_no
)
SELECT EXISTS (
  SELECT 1 FROM validation WHERE all_exist_and_compacting
  AND EXISTS (SELECT 1 FROM delete_result)
);