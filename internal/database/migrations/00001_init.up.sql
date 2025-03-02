CREATE TABLE IF NOT EXISTS l0_batches (
    id BIGSERIAL PRIMARY KEY,
    path TEXT NOT NULL,
    size_bytes BIGINT NOT NULL,
    min_key TEXT NOT NULL,
    max_key TEXT NOT NULL,
    status TEXT DEFAULT 'ACTIVE',
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE OR REPLACE FUNCTION notify_new_l0_batch()
RETURNS TRIGGER AS $$
BEGIN
    PERFORM pg_notify('new_l0_batch', NEW.id::text);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER l0_batch_notify
    AFTER INSERT ON l0_batches
    FOR EACH ROW
    EXECUTE FUNCTION notify_new_l0_batch();
