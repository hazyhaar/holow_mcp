-- ============================================================================
-- HOLOW-MCP: metadata.db Schema (12 tables)
-- Métriques système, alerting, shutdown, performance
-- ============================================================================

PRAGMA journal_mode = WAL;
PRAGMA synchronous = NORMAL;
PRAGMA foreign_keys = ON;
PRAGMA busy_timeout = 5000;
PRAGMA cache_size = -64000;
PRAGMA wal_autocheckpoint = 10000;
PRAGMA temp_store = MEMORY;

-- ============================================================================
-- Table 1: system_metrics - Métriques runtime Go ⭐
-- ============================================================================
CREATE TABLE IF NOT EXISTS system_metrics (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    cpu_percent REAL,
    memory_used_mb REAL,
    memory_total_mb REAL,
    heap_alloc_mb REAL,
    heap_sys_mb REAL,
    goroutines INTEGER,
    gc_pause_ms REAL,
    open_files INTEGER,
    disk_used_percent REAL,
    network_rx_bytes INTEGER,
    network_tx_bytes INTEGER,
    p50_latency_ms REAL,
    p95_latency_ms REAL,
    p99_latency_ms REAL,
    created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
);

CREATE INDEX idx_system_metrics_time ON system_metrics(created_at DESC);

-- ============================================================================
-- Table 2: build_metrics - Métriques build/déploiement
-- ============================================================================
CREATE TABLE IF NOT EXISTS build_metrics (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    build_version TEXT NOT NULL,
    git_commit TEXT,
    git_branch TEXT,
    build_time INTEGER,
    go_version TEXT,
    goos TEXT,
    goarch TEXT,
    compiler TEXT,
    binary_size_mb REAL,
    created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
);

-- ============================================================================
-- Table 3: poisonpill - Déclenchement shutdown gracieux ⭐
-- ============================================================================
CREATE TABLE IF NOT EXISTS poisonpill (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    triggered INTEGER NOT NULL DEFAULT 0,
    reason TEXT,
    triggered_by TEXT,                      -- "signal", "api", "watchdog"
    triggered_at INTEGER,
    shutdown_timeout_seconds INTEGER NOT NULL DEFAULT 60
);

INSERT OR IGNORE INTO poisonpill (id, triggered) VALUES (1, 0);

-- ============================================================================
-- Table 4: secrets_audit_log - Audit accès secrets
-- ============================================================================
CREATE TABLE IF NOT EXISTS secrets_audit_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    secret_name TEXT NOT NULL,
    action TEXT NOT NULL,                   -- "read", "write", "rotate", "delete"
    actor TEXT NOT NULL,
    success INTEGER NOT NULL,
    error_message TEXT,
    created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
);

CREATE INDEX idx_secrets_audit_log_secret ON secrets_audit_log(secret_name, created_at DESC);

-- ============================================================================
-- Table 5: import_stats - Statistiques imports données
-- ============================================================================
CREATE TABLE IF NOT EXISTS import_stats (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    source_name TEXT NOT NULL,
    import_type TEXT NOT NULL,
    rows_imported INTEGER NOT NULL,
    rows_skipped INTEGER NOT NULL DEFAULT 0,
    rows_failed INTEGER NOT NULL DEFAULT 0,
    duration_ms INTEGER NOT NULL,
    started_at INTEGER NOT NULL,
    completed_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
);

CREATE INDEX idx_import_stats_source ON import_stats(source_name, completed_at DESC);

-- ============================================================================
-- Table 6: performance_baseline - Percentiles historiques
-- ============================================================================
CREATE TABLE IF NOT EXISTS performance_baseline (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    metric_name TEXT NOT NULL,
    baseline_type TEXT NOT NULL,            -- "hourly", "daily", "weekly"
    p50 REAL NOT NULL,
    p75 REAL NOT NULL,
    p90 REAL NOT NULL,
    p95 REAL NOT NULL,
    p99 REAL NOT NULL,
    sample_count INTEGER NOT NULL,
    period_start INTEGER NOT NULL,
    period_end INTEGER NOT NULL,
    created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
    UNIQUE(metric_name, baseline_type, period_start)
);

CREATE INDEX idx_performance_baseline_metric ON performance_baseline(metric_name, baseline_type, period_start DESC);

-- ============================================================================
-- Table 7: alert_rules - Définitions règles alerting ⭐
-- ============================================================================
CREATE TABLE IF NOT EXISTS alert_rules (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    metric_name TEXT NOT NULL,
    condition TEXT NOT NULL,                -- "gt", "lt", "eq", "ne"
    threshold REAL NOT NULL,
    severity TEXT NOT NULL DEFAULT 'warning', -- info, warning, critical
    duration_seconds INTEGER NOT NULL DEFAULT 0, -- Durée avant déclenchement
    enabled INTEGER NOT NULL DEFAULT 1,
    notification_channels TEXT,             -- JSON array channels
    cooldown_seconds INTEGER NOT NULL DEFAULT 300, -- Délai entre alertes
    last_triggered_at INTEGER,
    created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
);

CREATE INDEX idx_alert_rules_enabled ON alert_rules(enabled, metric_name);

-- ============================================================================
-- Table 8: dependency_health - Santé dépendances externes
-- ============================================================================
CREATE TABLE IF NOT EXISTS dependency_health (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    dependency_name TEXT NOT NULL,
    dependency_type TEXT NOT NULL,          -- "horos_worker", "database", "service"
    status TEXT NOT NULL,                   -- "healthy", "degraded", "unhealthy", "unknown"
    last_check_at INTEGER NOT NULL,
    last_success_at INTEGER,
    consecutive_failures INTEGER NOT NULL DEFAULT 0,
    latency_ms INTEGER,
    error_message TEXT,
    metadata TEXT,                          -- JSON
    UNIQUE(dependency_name)
);

CREATE INDEX idx_dependency_health_status ON dependency_health(status);

-- ============================================================================
-- Table 9: resource_usage - Usage ressources par tool
-- ============================================================================
CREATE TABLE IF NOT EXISTS resource_usage (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    tool_name TEXT NOT NULL,
    execution_count INTEGER NOT NULL DEFAULT 0,
    total_cpu_ms INTEGER NOT NULL DEFAULT 0,
    total_memory_mb REAL NOT NULL DEFAULT 0,
    total_io_bytes INTEGER NOT NULL DEFAULT 0,
    period_start INTEGER NOT NULL,
    period_end INTEGER NOT NULL,
    created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
);

CREATE INDEX idx_resource_usage_tool ON resource_usage(tool_name, period_start DESC);

-- ============================================================================
-- Table 10: sla_tracking - Suivi SLA
-- ============================================================================
CREATE TABLE IF NOT EXISTS sla_tracking (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    sla_name TEXT NOT NULL,
    target_type TEXT NOT NULL,              -- "latency_p99", "availability", "error_rate"
    target_value REAL NOT NULL,
    actual_value REAL NOT NULL,
    met INTEGER NOT NULL,                   -- 1 si SLA respecté
    period_type TEXT NOT NULL,              -- "hour", "day", "week", "month"
    period_start INTEGER NOT NULL,
    period_end INTEGER NOT NULL,
    created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
);

CREATE INDEX idx_sla_tracking_name ON sla_tracking(sla_name, period_start DESC);

-- ============================================================================
-- Table 11: capacity_planning - Données planification capacité
-- ============================================================================
CREATE TABLE IF NOT EXISTS capacity_planning (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    resource_type TEXT NOT NULL,            -- "cpu", "memory", "disk", "connections"
    current_usage REAL NOT NULL,
    max_capacity REAL NOT NULL,
    utilization_percent REAL NOT NULL,
    growth_rate_daily REAL,                 -- % croissance/jour
    projected_exhaustion_days INTEGER,      -- Jours avant saturation
    recommendation TEXT,
    created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
);

CREATE INDEX idx_capacity_planning_resource ON capacity_planning(resource_type, created_at DESC);

-- ============================================================================
-- Table 12: incident_log - Journal incidents
-- ============================================================================
CREATE TABLE IF NOT EXISTS incident_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    incident_id TEXT NOT NULL UNIQUE,
    severity TEXT NOT NULL,                 -- "minor", "major", "critical"
    title TEXT NOT NULL,
    description TEXT NOT NULL,
    affected_tools TEXT,                    -- JSON array
    started_at INTEGER NOT NULL,
    detected_at INTEGER NOT NULL,
    resolved_at INTEGER,
    resolution_note TEXT,
    root_cause TEXT,
    prevention_measures TEXT,
    created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
);

CREATE INDEX idx_incident_log_severity ON incident_log(severity, started_at DESC);
CREATE INDEX idx_incident_log_unresolved ON incident_log(resolved_at) WHERE resolved_at IS NULL;
