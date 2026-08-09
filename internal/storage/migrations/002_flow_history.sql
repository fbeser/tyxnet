CREATE TABLE IF NOT EXISTS flow_history(
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    recorded_at TEXT NOT NULL,
    source TEXT NOT NULL,
    destination TEXT NOT NULL,
    protocol TEXT NOT NULL,
    protocol_number INTEGER NOT NULL,
    source_port INTEGER,
    destination_port INTEGER,
    icmp_type INTEGER,
    icmp_code INTEGER,
    bytes INTEGER NOT NULL,
    packets INTEGER NOT NULL,
    storage_bytes INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_flow_history_recorded_at ON flow_history(recorded_at, id);
CREATE INDEX IF NOT EXISTS idx_flow_history_protocol_recorded_at ON flow_history(protocol, recorded_at);
