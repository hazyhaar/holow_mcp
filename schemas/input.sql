-- ============================================================================
-- HOLOW-MCP: input.db Schema (8 tables)
-- Queue des requêtes MCP entrantes et sources externes
-- ============================================================================

PRAGMA journal_mode = WAL;
PRAGMA synchronous = NORMAL;
PRAGMA foreign_keys = ON;
PRAGMA busy_timeout = 5000;
PRAGMA cache_size = -64000;
PRAGMA wal_autocheckpoint = 10000;
PRAGMA temp_store = MEMORY;

-- ============================================================================
-- Table 1: mcp_requests - Queue principale requêtes MCP
-- ============================================================================
CREATE TABLE IF NOT EXISTS mcp_requests (
    id TEXT PRIMARY KEY,                    -- UUID requête
    method TEXT NOT NULL,                   -- Méthode MCP (tools/call, etc.)
    params_hash TEXT NOT NULL,              -- SHA256(params) pour idempotence
    status TEXT NOT NULL DEFAULT 'pending', -- pending, processing, completed, failed
    priority INTEGER NOT NULL DEFAULT 5,    -- 1=urgent, 10=low
    correlation_id TEXT,                    -- Tracking session multi-requêtes
    session_id TEXT,                        -- Session utilisateur
    created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
    started_at INTEGER,
    completed_at INTEGER,
    error_message TEXT,
    retry_count INTEGER DEFAULT 0
);

CREATE INDEX idx_mcp_requests_status_priority
ON mcp_requests(status, priority DESC, created_at);

CREATE INDEX idx_mcp_requests_correlation
ON mcp_requests(correlation_id);

CREATE INDEX idx_mcp_requests_session
ON mcp_requests(session_id, created_at);

-- ============================================================================
-- Table 2: request_params - Paramètres désérialisés (1-N avec mcp_requests)
-- ============================================================================
CREATE TABLE IF NOT EXISTS request_params (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    request_id TEXT NOT NULL REFERENCES mcp_requests(id) ON DELETE CASCADE,
    key TEXT NOT NULL,
    value TEXT NOT NULL,                    -- JSON value
    value_type TEXT NOT NULL DEFAULT 'string', -- string, number, boolean, object, array
    created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
);

CREATE INDEX idx_request_params_request ON request_params(request_id);

-- ============================================================================
-- Table 3: request_priority - Gestion priorités dynamiques
-- ============================================================================
CREATE TABLE IF NOT EXISTS request_priority (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    request_id TEXT NOT NULL REFERENCES mcp_requests(id) ON DELETE CASCADE,
    original_priority INTEGER NOT NULL,
    current_priority INTEGER NOT NULL,
    boost_reason TEXT,                      -- "aging", "vip_user", "deadline"
    boosted_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
);

CREATE INDEX idx_request_priority_request ON request_priority(request_id);

-- ============================================================================
-- Table 4: input_sources - Sources externes de données
-- ============================================================================
CREATE TABLE IF NOT EXISTS input_sources (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    source_type TEXT NOT NULL,              -- "horos_worker", "external_api", "file"
    connection_string TEXT,                 -- Path ou URL
    polling_interval_seconds INTEGER DEFAULT 5,
    last_polled_at INTEGER,
    status TEXT NOT NULL DEFAULT 'active',  -- active, paused, error
    error_message TEXT,
    created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
);

-- ============================================================================
-- Table 5: input_contracts - Contrats attendus par tool
-- ============================================================================
CREATE TABLE IF NOT EXISTS input_contracts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    tool_name TEXT NOT NULL,
    param_name TEXT NOT NULL,
    param_type TEXT NOT NULL,               -- string, number, boolean, object, array
    required INTEGER NOT NULL DEFAULT 1,
    default_value TEXT,
    validation_regex TEXT,
    description TEXT,
    created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
    UNIQUE(tool_name, param_name)
);

CREATE INDEX idx_input_contracts_tool ON input_contracts(tool_name);

-- ============================================================================
-- Table 6: input_schemas - Schémas validation JSON
-- ============================================================================
CREATE TABLE IF NOT EXISTS input_schemas (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    tool_name TEXT NOT NULL UNIQUE,
    json_schema TEXT NOT NULL,              -- JSON Schema complet
    version INTEGER NOT NULL DEFAULT 1,
    created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
    updated_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
);

-- ============================================================================
-- Table 7: input_health - Health check sources
-- ============================================================================
CREATE TABLE IF NOT EXISTS input_health (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    source_id INTEGER NOT NULL REFERENCES input_sources(id) ON DELETE CASCADE,
    check_type TEXT NOT NULL,               -- "connectivity", "latency", "availability"
    status TEXT NOT NULL,                   -- "healthy", "degraded", "unhealthy"
    latency_ms INTEGER,
    details TEXT,                           -- JSON details
    checked_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
);

CREATE INDEX idx_input_health_source ON input_health(source_id, checked_at DESC);

-- ============================================================================
-- Table 8: request_correlation - Tracking sessions multi-requêtes
-- ============================================================================
CREATE TABLE IF NOT EXISTS request_correlation (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    correlation_id TEXT NOT NULL,
    parent_request_id TEXT REFERENCES mcp_requests(id),
    child_request_id TEXT NOT NULL REFERENCES mcp_requests(id),
    relationship TEXT NOT NULL DEFAULT 'child', -- child, retry, continuation
    created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
);

CREATE INDEX idx_request_correlation_parent ON request_correlation(correlation_id);
CREATE INDEX idx_request_correlation_child ON request_correlation(child_request_id);
