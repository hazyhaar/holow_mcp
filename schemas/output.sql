-- ============================================================================
-- HOLOW-MCP: output.db Schema (10 tables)
-- Résultats, heartbeat, métriques, audit
-- ============================================================================

PRAGMA journal_mode = WAL;
PRAGMA synchronous = NORMAL;
PRAGMA foreign_keys = ON;
PRAGMA busy_timeout = 5000;
PRAGMA cache_size = -64000;
PRAGMA wal_autocheckpoint = 10000;
PRAGMA temp_store = MEMORY;

-- ============================================================================
-- Table 1: tool_results - Résultats exécution tools ⭐
-- ============================================================================
CREATE TABLE IF NOT EXISTS tool_results (
    hash TEXT PRIMARY KEY,                  -- SHA256(result)
    request_id TEXT NOT NULL,
    tool_name TEXT NOT NULL,
    result_json TEXT NOT NULL,
    result_type TEXT NOT NULL DEFAULT 'success', -- success, error, partial
    correlation_id TEXT,
    session_id TEXT,
    consumed INTEGER NOT NULL DEFAULT 0,    -- Flag pour polling non destructif
    processing_time_ms INTEGER,
    created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
);

CREATE INDEX idx_tool_results_unconsumed
ON tool_results(consumed, created_at)
WHERE consumed = 0;

CREATE INDEX idx_tool_results_correlation
ON tool_results(correlation_id, created_at);

CREATE INDEX idx_tool_results_session
ON tool_results(session_id, created_at);

CREATE INDEX idx_tool_results_tool
ON tool_results(tool_name, created_at DESC);

-- ============================================================================
-- Table 2: heartbeat - État serveur toutes 15s ⭐
-- ============================================================================
CREATE TABLE IF NOT EXISTS heartbeat (
    id INTEGER PRIMARY KEY CHECK (id = 1),  -- Single row
    status TEXT NOT NULL DEFAULT 'running', -- starting, running, shutting_down, stopped
    pid INTEGER NOT NULL,
    started_at INTEGER NOT NULL,
    last_heartbeat_at INTEGER NOT NULL,
    requests_processed INTEGER NOT NULL DEFAULT 0,
    requests_failed INTEGER NOT NULL DEFAULT 0,
    tools_loaded INTEGER NOT NULL DEFAULT 0,
    memory_mb INTEGER,
    goroutines INTEGER,
    version TEXT NOT NULL DEFAULT '1.0.0'
);

-- ============================================================================
-- Table 3: metrics_realtime - Métriques temps réel
-- ============================================================================
CREATE TABLE IF NOT EXISTS metrics_realtime (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    metric_name TEXT NOT NULL,
    metric_type TEXT NOT NULL,              -- counter, gauge, histogram
    value REAL NOT NULL,
    labels TEXT,                            -- JSON labels
    created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
);

CREATE INDEX idx_metrics_realtime_name ON metrics_realtime(metric_name, created_at DESC);

-- ============================================================================
-- Table 4: metrics_aggregated - Métriques agrégées périodiquement
-- ============================================================================
CREATE TABLE IF NOT EXISTS metrics_aggregated (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    metric_name TEXT NOT NULL,
    period_type TEXT NOT NULL,              -- minute, hour, day
    period_start INTEGER NOT NULL,
    count INTEGER NOT NULL,
    sum REAL NOT NULL,
    min REAL NOT NULL,
    max REAL NOT NULL,
    avg REAL NOT NULL,
    p50 REAL,
    p95 REAL,
    p99 REAL,
    created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
    UNIQUE(metric_name, period_type, period_start)
);

CREATE INDEX idx_metrics_aggregated_period ON metrics_aggregated(metric_name, period_type, period_start DESC);

-- ============================================================================
-- Table 5: dead_letter_queue - Échecs après tous retries
-- ============================================================================
CREATE TABLE IF NOT EXISTS dead_letter_queue (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    request_id TEXT NOT NULL,
    tool_name TEXT NOT NULL,
    params_json TEXT NOT NULL,
    error_message TEXT NOT NULL,
    attempts INTEGER NOT NULL,
    first_attempt_at INTEGER NOT NULL,
    last_attempt_at INTEGER NOT NULL,
    resolved INTEGER NOT NULL DEFAULT 0,
    resolved_at INTEGER,
    resolution_note TEXT
);

CREATE INDEX idx_dead_letter_queue_unresolved
ON dead_letter_queue(resolved, last_attempt_at DESC)
WHERE resolved = 0;

CREATE INDEX idx_dead_letter_queue_tool ON dead_letter_queue(tool_name);

-- ============================================================================
-- Table 6: audit_trail - Trace complète pour compliance
-- ============================================================================
CREATE TABLE IF NOT EXISTS audit_trail (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    event_type TEXT NOT NULL,               -- tool_executed, config_changed, error_occurred
    actor TEXT NOT NULL,                    -- system, llm, user
    action TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    old_value TEXT,
    new_value TEXT,
    metadata TEXT,                          -- JSON
    created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
);

CREATE INDEX idx_audit_trail_event ON audit_trail(event_type, created_at DESC);
CREATE INDEX idx_audit_trail_resource ON audit_trail(resource_type, resource_id);
CREATE INDEX idx_audit_trail_actor ON audit_trail(actor, created_at DESC);

-- ============================================================================
-- Table 7: alert_events - Alertes déclenchées
-- ============================================================================
CREATE TABLE IF NOT EXISTS alert_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    alert_rule_id INTEGER,
    severity TEXT NOT NULL,                 -- info, warning, critical
    title TEXT NOT NULL,
    message TEXT NOT NULL,
    metric_name TEXT,
    metric_value REAL,
    threshold_value REAL,
    acknowledged INTEGER NOT NULL DEFAULT 0,
    acknowledged_by TEXT,
    acknowledged_at INTEGER,
    created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
);

CREATE INDEX idx_alert_events_unacked
ON alert_events(acknowledged, severity, created_at DESC)
WHERE acknowledged = 0;

-- ============================================================================
-- Table 8: notification_queue - Queue notifications sortantes
-- ============================================================================
CREATE TABLE IF NOT EXISTS notification_queue (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    channel TEXT NOT NULL,                  -- "webhook", "email", "slack"
    recipient TEXT NOT NULL,
    subject TEXT NOT NULL,
    body TEXT NOT NULL,
    priority INTEGER NOT NULL DEFAULT 5,
    status TEXT NOT NULL DEFAULT 'pending', -- pending, sent, failed
    attempts INTEGER NOT NULL DEFAULT 0,
    last_attempt_at INTEGER,
    sent_at INTEGER,
    error_message TEXT,
    created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
);

CREATE INDEX idx_notification_queue_pending
ON notification_queue(status, priority DESC, created_at)
WHERE status = 'pending';

-- ============================================================================
-- Table 9: health_checks - Résultats health checks
-- ============================================================================
CREATE TABLE IF NOT EXISTS health_checks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    check_name TEXT NOT NULL,
    check_type TEXT NOT NULL,               -- "database", "memory", "disk", "dependency"
    status TEXT NOT NULL,                   -- healthy, degraded, unhealthy
    message TEXT,
    latency_ms INTEGER,
    details TEXT,                           -- JSON
    checked_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
);

CREATE INDEX idx_health_checks_name ON health_checks(check_name, checked_at DESC);

-- ============================================================================
-- Table 10: export_queue - Queue exports données
-- ============================================================================
CREATE TABLE IF NOT EXISTS export_queue (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    export_type TEXT NOT NULL,              -- "metrics", "logs", "audit"
    format TEXT NOT NULL,                   -- "json", "csv", "parquet"
    destination TEXT NOT NULL,              -- Path ou URL
    filters TEXT,                           -- JSON filtres
    status TEXT NOT NULL DEFAULT 'pending',
    started_at INTEGER,
    completed_at INTEGER,
    rows_exported INTEGER,
    file_path TEXT,
    error_message TEXT,
    created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
);

CREATE INDEX idx_export_queue_status ON export_queue(status, created_at);
