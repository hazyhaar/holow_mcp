-- ============================================================================
-- Tables de cache pour événements CDP (Chrome DevTools Protocol)
-- Stockage temporaire des logs console et requêtes réseau
-- ============================================================================

-- Table: cdp_console_logs
-- Stocke les messages de la console Chrome (console.log, error, warn, info)
CREATE TABLE IF NOT EXISTS cdp_console_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
    level TEXT NOT NULL CHECK(level IN ('log', 'error', 'warn', 'info', 'debug')),
    message TEXT NOT NULL,
    source TEXT,                    -- URL du fichier source
    line_number INTEGER,            -- Numéro de ligne
    stack_trace TEXT,               -- Stack trace pour les erreurs
    created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_cdp_console_timestamp ON cdp_console_logs(timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_cdp_console_level ON cdp_console_logs(level);

-- Table: cdp_network_requests
-- Stocke les requêtes HTTP interceptées par le Network panel
CREATE TABLE IF NOT EXISTS cdp_network_requests (
    request_id TEXT PRIMARY KEY,
    timestamp INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
    url TEXT NOT NULL,
    method TEXT NOT NULL,           -- GET, POST, PUT, DELETE, etc.
    status INTEGER,                 -- Code HTTP (200, 404, 500, etc.)
    status_text TEXT,               -- "OK", "Not Found", etc.
    request_headers TEXT,           -- JSON des headers de requête
    response_headers TEXT,          -- JSON des headers de réponse
    request_body TEXT,              -- Corps de la requête (si POST/PUT)
    response_body TEXT,             -- Corps de la réponse
    mime_type TEXT,                 -- Type MIME de la réponse
    resource_type TEXT,             -- "Document", "Script", "XHR", "Image", etc.
    timing_duration_ms INTEGER,     -- Durée totale en millisecondes
    from_cache INTEGER DEFAULT 0,   -- 1 si servi depuis le cache
    created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_cdp_network_timestamp ON cdp_network_requests(timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_cdp_network_url ON cdp_network_requests(url);
CREATE INDEX IF NOT EXISTS idx_cdp_network_status ON cdp_network_requests(status);
CREATE INDEX IF NOT EXISTS idx_cdp_network_resource_type ON cdp_network_requests(resource_type);

-- Table: cdp_session_state
-- État de la session browser (connexion WebSocket, page actuelle, etc.)
CREATE TABLE IF NOT EXISTS cdp_session_state (
    id INTEGER PRIMARY KEY CHECK(id = 1),  -- Une seule ligne
    ws_url TEXT,                            -- URL WebSocket CDP
    connected INTEGER DEFAULT 0,            -- 1 si connecté
    current_url TEXT,                       -- URL de la page actuelle
    current_title TEXT,                     -- Titre de la page actuelle
    chromium_pid INTEGER,                   -- PID du processus Chromium
    debug_port INTEGER DEFAULT 9222,        -- Port de debug
    session_id TEXT,                        -- ID de session CDP (pour Target.attachToTarget)
    target_id TEXT,                         -- ID du target (page) actif
    last_activity_at INTEGER,               -- Timestamp dernière activité
    created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
    updated_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
);

-- Initialiser avec une ligne par défaut
INSERT OR IGNORE INTO cdp_session_state (id, connected) VALUES (1, 0);

-- Table: cdp_commands
-- Queue de commandes CDP (pour fonctions SQL)
CREATE TABLE IF NOT EXISTS cdp_commands (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    method TEXT NOT NULL,
    params TEXT DEFAULT '{}',
    status TEXT DEFAULT 'pending' CHECK(status IN ('pending', 'success', 'error')),
    result TEXT,
    error TEXT,
    created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
    processed_at INTEGER
);

CREATE INDEX IF NOT EXISTS idx_cdp_commands_status ON cdp_commands(status);
CREATE INDEX IF NOT EXISTS idx_cdp_commands_created ON cdp_commands(created_at DESC);

-- Trigger pour nettoyer les vieux logs (garder seulement les 1000 derniers)
CREATE TRIGGER IF NOT EXISTS cleanup_old_console_logs
AFTER INSERT ON cdp_console_logs
WHEN (SELECT COUNT(*) FROM cdp_console_logs) > 1000
BEGIN
    DELETE FROM cdp_console_logs
    WHERE id IN (
        SELECT id FROM cdp_console_logs
        ORDER BY timestamp ASC
        LIMIT (SELECT COUNT(*) - 1000 FROM cdp_console_logs)
    );
END;

-- Trigger pour nettoyer les vieilles requêtes (garder seulement les 500 dernières)
CREATE TRIGGER IF NOT EXISTS cleanup_old_network_requests
AFTER INSERT ON cdp_network_requests
WHEN (SELECT COUNT(*) FROM cdp_network_requests) > 500
BEGIN
    DELETE FROM cdp_network_requests
    WHERE request_id IN (
        SELECT request_id FROM cdp_network_requests
        ORDER BY timestamp ASC
        LIMIT (SELECT COUNT(*) - 500 FROM cdp_network_requests)
    );
END;
