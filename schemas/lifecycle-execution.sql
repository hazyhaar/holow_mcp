-- ============================================================================
-- HOLOW-MCP: lifecycle-execution.db Schema (10 tables)
-- Exécution: idempotence, retry, circuit breaker, cache
-- ============================================================================

PRAGMA journal_mode = WAL;
PRAGMA synchronous = NORMAL;
PRAGMA foreign_keys = ON;
PRAGMA busy_timeout = 5000;
PRAGMA cache_size = -64000;
PRAGMA wal_autocheckpoint = 10000;
PRAGMA temp_store = MEMORY;

-- ============================================================================
-- Table 1: processed_log - Idempotence via SHA256 hash ⭐ CRITIQUE
-- ============================================================================
CREATE TABLE IF NOT EXISTS processed_log (
    hash TEXT PRIMARY KEY,                  -- SHA256(method + params)
    request_id TEXT NOT NULL,
    tool_name TEXT NOT NULL,
    status TEXT NOT NULL,                   -- success, failed
    result_hash TEXT,                       -- Hash résultat dans output.db
    processing_time_ms INTEGER,
    created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
);

CREATE INDEX idx_processed_log_tool ON processed_log(tool_name, created_at DESC);
CREATE INDEX idx_processed_log_request ON processed_log(request_id);

-- ============================================================================
-- Table 2: retry_queue - Queue retry avec backoff exponentiel
-- ============================================================================
CREATE TABLE IF NOT EXISTS retry_queue (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    request_id TEXT NOT NULL,
    tool_name TEXT NOT NULL,
    params_json TEXT NOT NULL,
    attempt_number INTEGER NOT NULL DEFAULT 1,
    max_attempts INTEGER NOT NULL DEFAULT 3,
    next_retry_at INTEGER NOT NULL,
    backoff_seconds INTEGER NOT NULL DEFAULT 2, -- Exponential: 2, 4, 8, 16...
    status TEXT NOT NULL DEFAULT 'pending',     -- pending, processing, exhausted
    last_error TEXT,
    created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
);

CREATE INDEX idx_retry_queue_next
ON retry_queue(next_retry_at)
WHERE status = 'pending';

CREATE INDEX idx_retry_queue_request ON retry_queue(request_id);

-- ============================================================================
-- Table 3: circuit_breakers - Protection cascading failures ⭐
-- ============================================================================
CREATE TABLE IF NOT EXISTS circuit_breakers (
    name TEXT PRIMARY KEY,                  -- tool_name ou service_name
    state TEXT NOT NULL DEFAULT 'closed',   -- closed, open, half_open
    failure_count INTEGER NOT NULL DEFAULT 0,
    success_count INTEGER NOT NULL DEFAULT 0, -- Pour transition half_open → closed
    failure_threshold INTEGER NOT NULL DEFAULT 5,
    success_threshold INTEGER NOT NULL DEFAULT 3, -- Succès requis pour fermer
    timeout_seconds INTEGER NOT NULL DEFAULT 60,  -- Durée état open
    last_failure_at INTEGER,
    last_success_at INTEGER,
    last_state_change_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
    half_open_max_calls INTEGER NOT NULL DEFAULT 3
);

CREATE INDEX idx_circuit_breakers_state ON circuit_breakers(state, last_state_change_at);

-- ============================================================================
-- Table 4: cache - Cache résultats tools
-- ============================================================================
CREATE TABLE IF NOT EXISTS cache (
    key TEXT PRIMARY KEY,                   -- SHA256(tool_name + params)
    value TEXT NOT NULL,                    -- Résultat JSON
    tool_name TEXT NOT NULL,
    ttl_seconds INTEGER NOT NULL DEFAULT 3600,
    created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
    expires_at INTEGER NOT NULL,
    hit_count INTEGER NOT NULL DEFAULT 0,
    last_hit_at INTEGER
);

CREATE INDEX idx_cache_expires ON cache(expires_at);
CREATE INDEX idx_cache_tool ON cache(tool_name);

-- ============================================================================
-- Table 5: rate_limiters - Limitation débit par tool/user
-- ============================================================================
CREATE TABLE IF NOT EXISTS rate_limiters (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    limiter_key TEXT NOT NULL,              -- tool_name ou user_id
    limiter_type TEXT NOT NULL,             -- "tool", "user", "global"
    max_requests INTEGER NOT NULL,
    window_seconds INTEGER NOT NULL,
    current_count INTEGER NOT NULL DEFAULT 0,
    window_start_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
    UNIQUE(limiter_key, limiter_type)
);

CREATE INDEX idx_rate_limiters_key ON rate_limiters(limiter_key, limiter_type);

-- ============================================================================
-- Table 6: concurrency_control - Limites exécution parallèle
-- ============================================================================
CREATE TABLE IF NOT EXISTS concurrency_control (
    resource_name TEXT PRIMARY KEY,         -- tool_name ou "global"
    max_concurrent INTEGER NOT NULL DEFAULT 10,
    current_concurrent INTEGER NOT NULL DEFAULT 0,
    queue_size INTEGER NOT NULL DEFAULT 0,
    updated_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
);

-- ============================================================================
-- Table 7: resource_locks - Verrous ressources partagées
-- ============================================================================
CREATE TABLE IF NOT EXISTS resource_locks (
    resource_id TEXT PRIMARY KEY,
    lock_type TEXT NOT NULL DEFAULT 'exclusive', -- exclusive, shared
    holder_id TEXT NOT NULL,                -- request_id qui détient le lock
    acquired_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
    expires_at INTEGER NOT NULL,
    metadata TEXT                           -- JSON infos supplémentaires
);

CREATE INDEX idx_resource_locks_expires ON resource_locks(expires_at);
CREATE INDEX idx_resource_locks_holder ON resource_locks(holder_id);

-- ============================================================================
-- Table 8: job_queue - Queue jobs asynchrones
-- ============================================================================
CREATE TABLE IF NOT EXISTS job_queue (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    job_type TEXT NOT NULL,
    payload TEXT NOT NULL,                  -- JSON
    priority INTEGER NOT NULL DEFAULT 5,
    status TEXT NOT NULL DEFAULT 'pending', -- pending, processing, completed, failed
    worker_id TEXT,
    scheduled_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
    started_at INTEGER,
    completed_at INTEGER,
    result TEXT,
    error_message TEXT
);

CREATE INDEX idx_job_queue_status ON job_queue(status, priority DESC, scheduled_at);

-- ============================================================================
-- Table 9: job_history - Historique jobs terminés
-- ============================================================================
CREATE TABLE IF NOT EXISTS job_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    original_job_id INTEGER NOT NULL,
    job_type TEXT NOT NULL,
    payload TEXT NOT NULL,
    status TEXT NOT NULL,
    worker_id TEXT,
    duration_ms INTEGER,
    result TEXT,
    error_message TEXT,
    completed_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
);

CREATE INDEX idx_job_history_type ON job_history(job_type, completed_at DESC);

-- ============================================================================
-- Table 10: last_check_timestamps - Timestamps dernières vérifications
-- ============================================================================
CREATE TABLE IF NOT EXISTS last_check_timestamps (
    check_name TEXT PRIMARY KEY,
    last_checked_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
    next_check_at INTEGER,
    check_interval_seconds INTEGER NOT NULL DEFAULT 60
);
