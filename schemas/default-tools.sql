-- ============================================================================
-- HOLOW-MCP: Tools par défaut
-- Ces tools sont essentiels au fonctionnement du serveur MCP
-- ============================================================================

-- ============================================================================
-- Tool 1: create_tool - Méta-outil pour créer de nouveaux outils
-- ============================================================================
INSERT OR REPLACE INTO tool_definitions (
    name, description, input_schema, category, version, enabled,
    timeout_seconds, retry_policy, max_retries, created_by, created_at, updated_at
) VALUES (
    'create_tool',
    'Crée un nouveau tool MCP avec sa définition et ses steps d''implémentation. Permet au LLM de créer dynamiquement de nouveaux outils.',
    '{
        "type": "object",
        "properties": {
            "name": {
                "type": "string",
                "description": "Nom unique du tool (snake_case)"
            },
            "description": {
                "type": "string",
                "description": "Description du tool"
            },
            "parameters": {
                "type": "object",
                "description": "JSON Schema des paramètres d''entrée (object avec properties, required)",
                "properties": {
                    "type": {"type": "string"},
                    "properties": {"type": "object"},
                    "required": {"type": "array", "items": {"type": "string"}}
                }
            },
            "category": {
                "type": "string",
                "enum": ["meta", "data", "compute", "io", "debug"],
                "description": "Catégorie du tool"
            },
            "sql_query": {
                "type": "string",
                "description": "Requête SQL à exécuter (avec placeholders {{param}})"
            }
        },
        "required": ["name", "description", "parameters", "sql_query"]
    }',
    'meta',
    1,
    1,
    30,
    'none',
    0,
    'system',
    strftime('%s', 'now'),
    strftime('%s', 'now')
);

INSERT OR REPLACE INTO tool_implementations (tool_name, step_order, step_name, step_type, sql_template)
VALUES ('create_tool', 1, 'insert_definition', 'sql',
'INSERT INTO tool_definitions (name, description, input_schema, category, version, enabled, timeout_seconds, created_by, created_at, updated_at)
VALUES (''{{name}}'', ''{{description}}'', ''{{parameters}}'', COALESCE(''{{category}}'', ''data''), 1, 1, 30, ''llm'', strftime(''%s'', ''now''), strftime(''%s'', ''now''))');

INSERT OR REPLACE INTO tool_implementations (tool_name, step_order, step_name, step_type, sql_template)
VALUES ('create_tool', 2, 'insert_implementation', 'sql',
'INSERT INTO tool_implementations (tool_name, step_order, step_name, step_type, sql_template)
VALUES (''{{name}}'', 1, ''execute'', ''sql'', ''{{sql_query}}'')');

INSERT OR REPLACE INTO tool_implementations (tool_name, step_order, step_name, step_type, sql_template)
VALUES ('create_tool', 3, 'return_result', 'sql',
'SELECT json_object(''success'', 1, ''tool_name'', ''{{name}}'', ''message'', ''Tool créé avec succès'')');

-- ============================================================================
-- Tool 2: list_tools - Liste tous les tools disponibles
-- ============================================================================
INSERT OR REPLACE INTO tool_definitions (
    name, description, input_schema, category, version, enabled,
    timeout_seconds, retry_policy, max_retries, created_by, created_at, updated_at
) VALUES (
    'list_tools',
    'Liste tous les tools MCP disponibles avec leurs métadonnées.',
    '{
        "type": "object",
        "properties": {
            "category": {
                "type": "string",
                "description": "Filtrer par catégorie (optionnel)"
            },
            "enabled_only": {
                "type": "boolean",
                "default": true,
                "description": "Afficher uniquement les tools actifs"
            }
        }
    }',
    'meta',
    1,
    1,
    10,
    'none',
    0,
    'system',
    strftime('%s', 'now'),
    strftime('%s', 'now')
);

INSERT OR REPLACE INTO tool_implementations (tool_name, step_order, step_name, step_type, sql_template)
VALUES ('list_tools', 1, 'list', 'sql',
'SELECT json_group_array(json_object(
    ''name'', name,
    ''description'', description,
    ''category'', category,
    ''version'', version,
    ''enabled'', enabled,
    ''timeout'', timeout_seconds
)) FROM tool_definitions
WHERE (''{{category}}'' = '''' OR category = ''{{category}}'')
AND (''{{enabled_only}}'' = ''false'' OR enabled = 1)');

-- ============================================================================
-- Tool 3: get_tool - Récupère les détails d'un tool
-- ============================================================================
INSERT OR REPLACE INTO tool_definitions (
    name, description, input_schema, category, version, enabled,
    timeout_seconds, retry_policy, max_retries, created_by, created_at, updated_at
) VALUES (
    'get_tool',
    'Récupère les détails complets d''un tool incluant ses steps d''implémentation.',
    '{
        "type": "object",
        "properties": {
            "name": {
                "type": "string",
                "description": "Nom du tool"
            }
        },
        "required": ["name"]
    }',
    'meta',
    1,
    1,
    10,
    'none',
    0,
    'system',
    strftime('%s', 'now'),
    strftime('%s', 'now')
);

INSERT OR REPLACE INTO tool_implementations (tool_name, step_order, step_name, step_type, sql_template)
VALUES ('get_tool', 1, 'get_details', 'sql',
'SELECT json_object(
    ''name'', d.name,
    ''description'', d.description,
    ''category'', d.category,
    ''version'', d.version,
    ''enabled'', d.enabled,
    ''input_schema'', json(d.input_schema),
    ''steps'', (
        SELECT json_group_array(json_object(
            ''order'', step_order,
            ''name'', step_name,
            ''type'', step_type,
            ''sql'', sql_template
        ))
        FROM tool_implementations i
        WHERE i.tool_name = d.name
        ORDER BY step_order
    )
) FROM tool_definitions d WHERE d.name = ''{{name}}''');

-- ============================================================================
-- Tool 4: audit_system - Audit complet du système
-- ============================================================================
INSERT OR REPLACE INTO tool_definitions (
    name, description, input_schema, category, version, enabled,
    timeout_seconds, retry_policy, max_retries, created_by, created_at, updated_at
) VALUES (
    'audit_system',
    'Audit complet du système HOLOW-MCP. Retourne l''état des tools disponibles et les statistiques.',
    '{
        "type": "object",
        "properties": {}
    }',
    'debug',
    1,
    1,
    30,
    'none',
    0,
    'system',
    strftime('%s', 'now'),
    strftime('%s', 'now')
);

INSERT OR REPLACE INTO tool_implementations (tool_name, step_order, step_name, step_type, sql_template)
VALUES ('audit_system', 1, 'audit', 'sql',
'SELECT json_object(
    ''timestamp'', strftime(''%Y-%m-%dT%H:%M:%SZ'', ''now''),
    ''tools'', json_object(
        ''total'', (SELECT COUNT(*) FROM tool_definitions),
        ''enabled'', (SELECT COUNT(*) FROM tool_definitions WHERE enabled = 1),
        ''by_category'', (
            SELECT json_group_object(category, cnt)
            FROM (SELECT category, COUNT(*) as cnt FROM tool_definitions GROUP BY category)
        )
    ),
    ''patterns'', json_object(
        ''total'', (SELECT COUNT(*) FROM action_patterns),
        ''high_confidence'', (SELECT COUNT(*) FROM action_patterns WHERE confidence_score > 0.7)
    ),
    ''hot_reload'', (SELECT json_object(''dirty'', tools_dirty) FROM hot_reload_flag WHERE id = 1)
)');

-- ============================================================================
-- Tool 5: get_metrics - Métriques temps réel
-- ============================================================================
INSERT OR REPLACE INTO tool_definitions (
    name, description, input_schema, category, version, enabled,
    timeout_seconds, retry_policy, max_retries, created_by, created_at, updated_at
) VALUES (
    'get_metrics',
    'Récupère les métriques temps réel du serveur HOLOW-MCP.',
    '{
        "type": "object",
        "properties": {}
    }',
    'debug',
    1,
    1,
    10,
    'none',
    0,
    'system',
    strftime('%s', 'now'),
    strftime('%s', 'now')
);

INSERT OR REPLACE INTO tool_implementations (tool_name, step_order, step_name, step_type, sql_template)
VALUES ('get_metrics', 1, 'metrics', 'sql',
'SELECT json_object(
    ''timestamp'', strftime(''%Y-%m-%dT%H:%M:%SZ'', ''now''),
    ''tools_loaded'', (SELECT COUNT(*) FROM tool_definitions WHERE enabled = 1),
    ''patterns_detected'', (SELECT COUNT(*) FROM action_patterns)
)');
