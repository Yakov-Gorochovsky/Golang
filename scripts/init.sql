CREATE TABLE IF NOT EXISTS telemetry_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id VARCHAR(255) NOT NULL,
    event_type VARCHAR(100) NOT NULL,
    payload JSONB NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Index the JSONB payload so we can efficiently query by arbitrary keys later
CREATE INDEX idx_telemetry_payload ON telemetry_logs USING GIN (payload);
CREATE INDEX idx_telemetry_device ON telemetry_logs (device_id);
