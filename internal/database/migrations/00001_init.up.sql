-- Write-Ahead Logs
CREATE TABLE IF NOT EXISTS write_ahead_logs (
    seq_no BIGSERIAL PRIMARY KEY,
    state TEXT NOT NULL DEFAULT 'NEW',
    attrs JSONB DEFAULT '{}' :: JSONB NOT NULL
);

CREATE OR REPLACE FUNCTION notify_new_write_ahead_log()
RETURNS TRIGGER AS $$
BEGIN
    PERFORM pg_notify('new_write_ahead_log', NEW.seq_no::text);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER write_ahead_log_notify
    AFTER INSERT OR UPDATE ON write_ahead_logs
    FOR EACH STATEMENT
    EXECUTE FUNCTION notify_new_write_ahead_log();

-- Shared runs
CREATE TABLE IF NOT EXISTS shared_runs (
    seq_no BIGSERIAL PRIMARY KEY,
    attrs JSONB DEFAULT '{}' :: JSONB NOT NULL
);

CREATE OR REPLACE FUNCTION notify_shared_run_change()
RETURNS TRIGGER AS $$
BEGIN
    PERFORM pg_notify('shared_run_change', NEW.seq_no::text);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER shared_run_notify
    AFTER INSERT OR UPDATE ON shared_runs
    FOR EACH STATEMENT
    EXECUTE FUNCTION notify_shared_run_change();

-- Partitions
CREATE TABLE IF NOT EXISTS partitions (
    inclusive_start_key TEXT PRIMARY KEY,
    attrs JSONB DEFAULT '{}' :: JSONB NOT NULL
);

INSERT INTO partitions (inclusive_start_key, attrs)
SELECT '', '{}';

CREATE OR REPLACE FUNCTION notify_partition_change()
RETURNS TRIGGER AS $$
BEGIN
    PERFORM pg_notify('partition_change', NEW.inclusive_start_key);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER partition_notify
    AFTER INSERT OR UPDATE ON partitions
    FOR EACH STATEMENT
    EXECUTE FUNCTION notify_partition_change();

-- Jobs
CREATE TABLE IF NOT EXISTS jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    state TEXT NOT NULL DEFAULT 'PENDING',
    attrs JSONB DEFAULT '{}' :: JSONB NOT NULL
);

CREATE OR REPLACE FUNCTION notify_job_change()
RETURNS TRIGGER AS $$
BEGIN
    PERFORM pg_notify('job_change', NEW.id::text);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER job_notify
    AFTER INSERT OR UPDATE ON jobs
    FOR EACH STATEMENT
    EXECUTE FUNCTION notify_job_change();
