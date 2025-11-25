-- ============================================================================
-- HOLOW-MCP: lifecycle-core.db Schema (13 tables)
-- Configuration, télémétrie, sécurité, environnement
-- ============================================================================

PRAGMA journal_mode = WAL;
PRAGMA synchronous = NORMAL;
PRAGMA foreign_keys = ON;
PRAGMA busy_timeout = 5000;
PRAGMA cache_size = -64000;
PRAGMA wal_autocheckpoint = 10000;
PRAGMA temp_store = MEMORY;

-- ============================================================================
-- Table 1: config - Configuration runtime modifiable
-- ============================================================================
CREATE TABLE IF NOT EXISTS config (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    value_type TEXT NOT NULL DEFAULT 'string', -- string, number, boolean, json
    description TEXT,
    editable INTEGER NOT NULL DEFAULT 1,
    created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
    updated_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
);

-- Configuration initiale
INSERT OR IGNORE INTO config (key, value, value_type, description) VALUES
    ('server.name', 'holow-mcp', 'string', 'Nom du serveur MCP'),
    ('server.version', '1.0.0', 'string', 'Version du serveur'),
    ('polling.interval_ms', '2000', 'number', 'Intervalle hot reload tools'),
    ('heartbeat.interval_seconds', '15', 'number', 'Intervalle heartbeat'),
    ('shutdown.timeout_seconds', '60', 'number', 'Timeout graceful shutdown'),
    ('cache.default_ttl_seconds', '3600', 'number', 'TTL cache par défaut'),
    ('retry.max_attempts', '3', 'number', 'Nombre max retries'),
    ('circuit_breaker.failure_threshold', '5', 'number', 'Seuil échecs circuit breaker');

-- ============================================================================
-- Table 2: ego_index - 15 dimensions documentées ⭐
-- ============================================================================
CREATE TABLE IF NOT EXISTS ego_index (
    key TEXT PRIMARY KEY,
    description TEXT NOT NULL,
    value TEXT NOT NULL,
    created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
);

-- 15 dimensions HOROS
INSERT OR IGNORE INTO ego_index (key, description, value) VALUES
    ('dim_origines', 'Source/provenance', 'Requêtes MCP via stdio JSON-RPC'),
    ('dim_composition', 'Éléments internes', '6 bases SQLite, tools programmables'),
    ('dim_finalites', 'Objectifs métier', 'Serveur MCP universel avec persistance'),
    ('dim_interactions', 'Interfaces communication', 'stdio JSON-RPC, ATTACH SQLite'),
    ('dim_dependances', 'Dépendances requises', 'modernc.org/sqlite, Go 1.21+'),
    ('dim_temporalite', 'Timing exécution', 'Event-driven polling 2s'),
    ('dim_cardinalite', 'Instances simultanées', '1 instance par terminal'),
    ('dim_observabilite', 'Monitoring métriques', 'Heartbeat 15s, métriques SQLite'),
    ('dim_reversibilite', 'Capacité rollback', 'Idempotence via processed_log'),
    ('dim_congruence', 'Cohérence nom/path', 'holow-mcp/*'),
    ('dim_anticipation', 'Problèmes anticipés', 'Contention WAL, ATTACH security'),
    ('dim_granularite', 'Niveau détail', 'Tool = unité atomique'),
    ('dim_conditionnalite', 'Conditions activation', 'Requête MCP entrante'),
    ('dim_autorite', 'Permissions modification', 'LLM peut créer tools via SQL'),
    ('dim_mutabilite', 'Changements runtime', 'Hot reload tools sans restart');

-- ============================================================================
-- Table 3: dependencies - Dépendances externes
-- ============================================================================
CREATE TABLE IF NOT EXISTS dependencies (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    version TEXT NOT NULL,
    dep_type TEXT NOT NULL,                 -- "go_module", "system", "service"
    required INTEGER NOT NULL DEFAULT 1,
    status TEXT NOT NULL DEFAULT 'unknown', -- unknown, available, missing
    checked_at INTEGER
);

-- ============================================================================
-- Table 4: telemetry_traces - Traces distribuées
-- ============================================================================
CREATE TABLE IF NOT EXISTS telemetry_traces (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    trace_id TEXT NOT NULL,
    span_id TEXT NOT NULL,
    parent_span_id TEXT,
    operation_name TEXT NOT NULL,
    service_name TEXT NOT NULL DEFAULT 'holow-mcp',
    status TEXT NOT NULL,                   -- ok, error
    duration_ms INTEGER NOT NULL,
    tags TEXT,                              -- JSON
    started_at INTEGER NOT NULL,
    ended_at INTEGER NOT NULL
);

CREATE INDEX idx_telemetry_traces_trace ON telemetry_traces(trace_id);
CREATE INDEX idx_telemetry_traces_time ON telemetry_traces(started_at DESC);

-- ============================================================================
-- Table 5: telemetry_logs - Logs structurés
-- ============================================================================
CREATE TABLE IF NOT EXISTS telemetry_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    level TEXT NOT NULL,                    -- debug, info, warn, error
    message TEXT NOT NULL,
    logger TEXT NOT NULL DEFAULT 'main',
    trace_id TEXT,
    fields TEXT,                            -- JSON champs additionnels
    created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
);

CREATE INDEX idx_telemetry_logs_level ON telemetry_logs(level, created_at DESC);
CREATE INDEX idx_telemetry_logs_trace ON telemetry_logs(trace_id);

-- ============================================================================
-- Table 6: telemetry_llm_metrics - Métriques spécifiques LLM
-- ============================================================================
CREATE TABLE IF NOT EXISTS telemetry_llm_metrics (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    request_id TEXT NOT NULL,
    model TEXT,
    prompt_tokens INTEGER,
    completion_tokens INTEGER,
    total_tokens INTEGER,
    latency_ms INTEGER,
    cost_usd REAL,
    created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
);

CREATE INDEX idx_telemetry_llm_metrics_time ON telemetry_llm_metrics(created_at DESC);

-- ============================================================================
-- Table 7: telemetry_security_events - Événements sécurité
-- ============================================================================
CREATE TABLE IF NOT EXISTS telemetry_security_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    event_type TEXT NOT NULL,               -- "auth_failure", "rate_limit", "forbidden_attach"
    severity TEXT NOT NULL,                 -- info, warning, critical
    source_ip TEXT,
    user_id TEXT,
    details TEXT NOT NULL,                  -- JSON
    created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
);

CREATE INDEX idx_telemetry_security_events_type ON telemetry_security_events(event_type, created_at DESC);
CREATE INDEX idx_telemetry_security_events_severity ON telemetry_security_events(severity, created_at DESC);

-- ============================================================================
-- Table 8: secrets_registry - Registre secrets (références, pas valeurs)
-- ============================================================================
CREATE TABLE IF NOT EXISTS secrets_registry (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    secret_type TEXT NOT NULL,              -- "api_key", "password", "token"
    storage_location TEXT NOT NULL,         -- "env", "file", "vault"
    env_var_name TEXT,
    file_path TEXT,
    description TEXT,
    last_rotated_at INTEGER,
    created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
);

-- ============================================================================
-- Table 9: environment_config - Configuration environnement
-- ============================================================================
CREATE TABLE IF NOT EXISTS environment_config (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    env_type TEXT NOT NULL DEFAULT 'development', -- development, staging, production
    encrypted INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
);

-- ============================================================================
-- Table 10: network_config - Configuration réseau
-- ============================================================================
CREATE TABLE IF NOT EXISTS network_config (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    config_type TEXT NOT NULL,              -- "proxy", "dns", "firewall"
    name TEXT NOT NULL,
    value TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1,
    created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
    UNIQUE(config_type, name)
);

-- ============================================================================
-- Table 11: allowed_attach_paths - Whitelist ATTACH sécurisé ⭐ SÉCURITÉ
-- ============================================================================
CREATE TABLE IF NOT EXISTS allowed_attach_paths (
    worker_name TEXT PRIMARY KEY,
    db_path TEXT NOT NULL,
    db_type TEXT NOT NULL DEFAULT 'output', -- input, output, lifecycle, metadata
    allowed INTEGER NOT NULL DEFAULT 1,
    description TEXT,
    added_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
);

-- ============================================================================
-- Table 12: schema_metadata - Version schéma
-- ============================================================================
CREATE TABLE IF NOT EXISTS schema_metadata (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    version INTEGER NOT NULL DEFAULT 1,
    created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
    updated_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
);

INSERT OR IGNORE INTO schema_metadata (id, version) VALUES (1, 1);

-- ============================================================================
-- Table 13: schema_versions - Historique migrations
-- ============================================================================
CREATE TABLE IF NOT EXISTS schema_versions (
    version INTEGER PRIMARY KEY,
    applied_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
    migration_sql TEXT NOT NULL,
    rollback_sql TEXT,
    description TEXT
);

INSERT OR IGNORE INTO schema_versions (version, migration_sql, description) VALUES
    (1, 'Initial schema creation', 'Schema initial holow-mcp v1.0.0');
