-- ============================================================================
-- HOLOW-MCP: lifecycle-tools.db Schema (8 tables)
-- Définitions outils, patterns, workflows créés par LLM
-- ============================================================================

PRAGMA journal_mode = WAL;
PRAGMA synchronous = NORMAL;
PRAGMA foreign_keys = ON;
PRAGMA busy_timeout = 5000;
PRAGMA cache_size = -64000;
PRAGMA wal_autocheckpoint = 10000;
PRAGMA temp_store = MEMORY;

-- ============================================================================
-- Table 1: tool_definitions - Bibliothèque tools créés par LLM
-- ============================================================================
CREATE TABLE IF NOT EXISTS tool_definitions (
    name TEXT PRIMARY KEY,
    description TEXT NOT NULL,
    input_schema TEXT NOT NULL,             -- JSON Schema paramètres
    category TEXT,                          -- "data", "compute", "io", "meta"
    version INTEGER NOT NULL DEFAULT 1,
    enabled INTEGER NOT NULL DEFAULT 1,
    timeout_seconds INTEGER NOT NULL DEFAULT 30,
    retry_policy TEXT DEFAULT 'exponential', -- none, fixed, exponential
    max_retries INTEGER DEFAULT 3,
    created_by TEXT,                        -- "system", "llm", "user"
    created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
    updated_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
);

CREATE INDEX idx_tool_definitions_enabled ON tool_definitions(enabled, category);

-- ============================================================================
-- Table 2: tool_implementations - Workflows SQL pour chaque tool
-- ============================================================================
CREATE TABLE IF NOT EXISTS tool_implementations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    tool_name TEXT NOT NULL REFERENCES tool_definitions(name) ON DELETE CASCADE,
    step_order INTEGER NOT NULL,
    step_name TEXT NOT NULL,
    step_type TEXT NOT NULL,                -- "sql", "attach", "validate", "transform"
    sql_template TEXT NOT NULL,             -- SQL avec placeholders {{param}}
    error_handler TEXT,                     -- SQL si erreur
    condition TEXT,                         -- Condition exécution (SQL expression)
    created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
    UNIQUE(tool_name, step_order)
);

CREATE INDEX idx_tool_implementations_tool ON tool_implementations(tool_name, step_order);

-- ============================================================================
-- Table 3: action_patterns - Patterns détectés automatiquement
-- ============================================================================
CREATE TABLE IF NOT EXISTS action_patterns (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    pattern_name TEXT NOT NULL UNIQUE,
    pattern_type TEXT NOT NULL,             -- "sequence", "frequency", "correlation"
    detection_query TEXT NOT NULL,          -- SQL query qui détecte le pattern
    tool_sequence TEXT NOT NULL,            -- JSON array des tools impliqués
    occurrence_count INTEGER NOT NULL DEFAULT 0,
    confidence_score REAL NOT NULL DEFAULT 0.0, -- 0.0 à 1.0
    suggested_tool_name TEXT,               -- Nom suggéré pour nouveau tool
    auto_create INTEGER NOT NULL DEFAULT 0, -- Créer tool automatiquement si confidence > 0.8
    last_detected_at INTEGER,
    created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
);

CREATE INDEX idx_action_patterns_confidence ON action_patterns(confidence_score DESC);

-- ============================================================================
-- Table 4: tool_parameters - Paramètres par défaut et contraintes
-- ============================================================================
CREATE TABLE IF NOT EXISTS tool_parameters (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    tool_name TEXT NOT NULL REFERENCES tool_definitions(name) ON DELETE CASCADE,
    param_name TEXT NOT NULL,
    param_type TEXT NOT NULL,               -- string, number, boolean, object, array
    default_value TEXT,
    min_value REAL,
    max_value REAL,
    enum_values TEXT,                       -- JSON array valeurs possibles
    description TEXT,
    UNIQUE(tool_name, param_name)
);

CREATE INDEX idx_tool_parameters_tool ON tool_parameters(tool_name);

-- ============================================================================
-- Table 5: tool_dependencies - Dépendances entre tools
-- ============================================================================
CREATE TABLE IF NOT EXISTS tool_dependencies (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    tool_name TEXT NOT NULL REFERENCES tool_definitions(name) ON DELETE CASCADE,
    depends_on TEXT NOT NULL,               -- Nom tool requis
    dependency_type TEXT NOT NULL DEFAULT 'required', -- required, optional, conditional
    condition TEXT,                         -- Condition si conditional
    created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
    UNIQUE(tool_name, depends_on)
);

CREATE INDEX idx_tool_dependencies_tool ON tool_dependencies(tool_name);

-- ============================================================================
-- Table 6: tool_versioning - Historique versions tools
-- ============================================================================
CREATE TABLE IF NOT EXISTS tool_versioning (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    tool_name TEXT NOT NULL,
    version INTEGER NOT NULL,
    definition_snapshot TEXT NOT NULL,      -- JSON complet définition
    implementation_snapshot TEXT NOT NULL,  -- JSON array des steps
    change_reason TEXT,
    changed_by TEXT,
    created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
    UNIQUE(tool_name, version)
);

CREATE INDEX idx_tool_versioning_tool ON tool_versioning(tool_name, version DESC);

-- ============================================================================
-- Table 7: workflow_state - État variables workflows en cours
-- ============================================================================
CREATE TABLE IF NOT EXISTS workflow_state (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    request_id TEXT NOT NULL,
    tool_name TEXT NOT NULL,
    current_step INTEGER NOT NULL DEFAULT 0,
    state_data TEXT NOT NULL DEFAULT '{}',  -- JSON variables workflow
    status TEXT NOT NULL DEFAULT 'running', -- running, paused, completed, failed
    started_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
    updated_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
    UNIQUE(request_id, tool_name)
);

CREATE INDEX idx_workflow_state_status ON workflow_state(status, updated_at);

-- ============================================================================
-- Table 8: workflow_variables - Variables persistantes workflows
-- ============================================================================
CREATE TABLE IF NOT EXISTS workflow_variables (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    workflow_state_id INTEGER NOT NULL REFERENCES workflow_state(id) ON DELETE CASCADE,
    var_name TEXT NOT NULL,
    var_value TEXT NOT NULL,
    var_type TEXT NOT NULL DEFAULT 'string',
    created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
    UNIQUE(workflow_state_id, var_name)
);

CREATE INDEX idx_workflow_variables_state ON workflow_variables(workflow_state_id);

-- ============================================================================
-- Hot Reload Support - Flag pour optimisation polling
-- ============================================================================
CREATE TABLE IF NOT EXISTS hot_reload_flag (
    id INTEGER PRIMARY KEY CHECK (id = 1),  -- Single row
    tools_dirty INTEGER NOT NULL DEFAULT 0,
    last_reload_at INTEGER
);

INSERT OR IGNORE INTO hot_reload_flag (id, tools_dirty) VALUES (1, 0);

-- Trigger: marquer dirty après INSERT tool
CREATE TRIGGER IF NOT EXISTS tool_inserted AFTER INSERT ON tool_definitions
BEGIN
    UPDATE hot_reload_flag SET tools_dirty = 1;
END;

-- Trigger: marquer dirty après UPDATE tool
CREATE TRIGGER IF NOT EXISTS tool_updated AFTER UPDATE ON tool_definitions
BEGIN
    UPDATE hot_reload_flag SET tools_dirty = 1;
END;

-- Trigger: marquer dirty après DELETE tool
CREATE TRIGGER IF NOT EXISTS tool_deleted AFTER DELETE ON tool_definitions
BEGIN
    UPDATE hot_reload_flag SET tools_dirty = 1;
END;
