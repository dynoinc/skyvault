CREATE TABLE IF NOT EXISTS l0_batches (
    seq_no BIGSERIAL PRIMARY KEY,
    version integer NOT NULL DEFAULT 1,
    attrs JSONB DEFAULT '{}' :: JSONB
);

CREATE OR REPLACE FUNCTION notify_new_l0_batch()
RETURNS TRIGGER AS $$
BEGIN
    PERFORM pg_notify('new_l0_batch', NEW.seq_no::text);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION update_version()
RETURNS TRIGGER AS $$
BEGIN
    NEW.version := OLD.version + 1;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER l0_batch_notify
    AFTER INSERT OR UPDATE ON l0_batches
    FOR EACH STATEMENT
    EXECUTE FUNCTION notify_new_l0_batch();

CREATE TRIGGER trigger_update_version
    BEFORE UPDATE ON l0_batches
    FOR EACH ROW
    EXECUTE FUNCTION update_version();