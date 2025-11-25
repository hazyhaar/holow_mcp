# HOLOW-MCP

🚀 **Serveur MCP (Model Context Protocol) universel basé sur l'architecture HOROS**

**Master Tool avec 18 actions** - Création dynamique d'outils - Hot reload 2s - Intégration OpenCode 1.0.78+

## Architecture

### ⚡ Pattern 6-BDD Shardé (61 tables)

HOLOW-MCP utilise le pattern HOROS **AVANCÉ** avec sharding de lifecycle.db pour réduire la contention WAL :

```
holow-mcp.input.db              (8 tables)   - Queue requêtes MCP
holow-mcp.lifecycle-tools.db    (8 tables)   - 🧠 Tools dynamiques (création LLM)
holow-mcp.lifecycle-execution.db (10 tables) - ⚡ Idempotence, retry, circuit breaker  
holow-mcp.lifecycle-core.db     (13 tables)  - 🔐 Config, sécurité, whitelist ATTACH
holow-mcp.output.db              (10 tables)  - 📊 Résultats, heartbeat, métriques
holow-mcp.metadata.db            (12 tables)  - 📈 Métriques système, alerting
```

### 🎯 Fonctionnalités UNIQUES

- **🧠 Tools Programmables** : LLM peut créer des tools via INSERT SQL
- **⚡ Hot Reload Trigger-based** : Détection changements par trigger SQLite (2s)  
- **🔧 Master Tool Brainloop** : 18 actions (génération, analyse, audit système)
- **🛡️ Circuit Breaker** : Protection cascading failures avec success_threshold
- **🔐 Idempotence** : SHA256(method+params) dans processed_log
- **🔒 Whitelist ATTACH** : Sécurité ATTACH via table allowed_attach_paths
- **📊 Observabilité Native** : Heartbeat 15s, métriques dans SQLite
- **🛑 Graceful Shutdown** : Timeout 60s, poison pill pattern

### Fonctionnalités

- **Tools Programmables** : LLM peut créer des tools via INSERT SQL
- **Hot Reload Trigger-based** : Détection changements par trigger SQLite (2s)
- **Circuit Breaker** : Protection cascading failures avec success_threshold
- **Idempotence** : SHA256(method+params) dans processed_log
- **Whitelist ATTACH** : Sécurité ATTACH via table allowed_attach_paths
- **Observabilité Native** : Heartbeat 15s, métriques dans SQLite
- **Graceful Shutdown** : Timeout 60s, poison pill pattern

## Installation

### Prérequis

- Go 1.21+
- Accès réseau pour télécharger les dépendances

### Build

```bash
# Télécharger les dépendances
go mod tidy

# Compiler
go build -o bin/holow-mcp ./cmd/holow-mcp

# Ou avec Mage
mage build
```

### Initialisation des bases

```bash
# ✅ CORRECT - Initialiser les 6 bases avec les schémas SPÉCIFIES holow-mcp
./bin/holow-mcp -init -schemas ./schemas

# Ou avec Mage
mage initdb

# ❌ FAUX - Ne PAS utiliser les templates génériques !
# ./bin/holow-mcp -init -schemas /workspace/templates/schemas/  # ERREUR !
```

### 🎯 Intégration OpenCode 1.0.78+

```json
// ~/.config/opencode/opencode.json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "holow-mcp": {
      "type": "local",
      "command": ["/workspace/projets/holow-mcp/bin/holow-mcp"],
      "enabled": true
    }
  }
}
```

**Dans OpenCode** : `use the holow-mcp tool to [action]`

## Utilisation

### Démarrage

```bash
./bin/holow-mcp
```

Le serveur écoute sur stdin/stdout en JSON-RPC 2.0.

### Configuration MCP Client

Ajouter dans la configuration du client MCP :

```json
{
  "mcpServers": {
    "holow": {
      "command": "/path/to/holow-mcp",
      "args": []
    }
  }
}
```

## Commandes Mage

```bash
mage build      # Compiler le binaire
mage test       # Exécuter les tests
mage lint       # Lancer les linters
mage initdb     # Initialiser les bases de données
mage run        # Démarrer le serveur
mage validate   # Vérifier conformité HOROS
mage check      # Full validation (validate + lint + test + build)
mage clean      # Supprimer fichiers générés
mage info       # Afficher informations projet
mage proto      # Générer code protobuf
```

## 🧠 Master Tool BRAINLOOP (18 actions)

### Actions disponibles par catégorie :

#### 🔧 System (5 actions)
- `create_tool` - Créer dynamiquement des outils MCP
- `list_tools` - Lister les outils disponibles  
- `get_tool` - Détails d'un outil spécifique
- `audit_system` - Audit complet du système
- `get_metrics` - Métriques de performance

#### 🎨 Generation (4 actions)  
- `generate_file` - Génération de fichiers avec patterns
- `generate_sql` - Génération SQL depuis prompt
- `explore` - Exploration créative du codebase
- `loop` - Workflow itératif (propose/audit/refine/commit)

#### 📖 Reading (4 actions)
- `read_sqlite` - Analyse structurelle de bases SQLite
- `read_code` - Analyse de code avec détection de patterns 
- `read_markdown` - Analyse de documents markdown
- `read_config` - Analyse de configs (JSON/YAML/TOML)

#### 🔍 Discovery (3 actions)
- `list_actions` - Liste toutes les actions disponibles
- `get_schema` - Schéma détaillé d'une action
- `get_stats` - Statistiques d'utilisation

#### ⚙️ Utility (2 actions)
- `list_files` - Listing de fichiers avec glob patterns
- `search_code` - Recherche de patterns dans le code

### 💡 Création de Tools par LLM

```sql
-- Définir le tool dynamiquement
INSERT INTO tool_definitions
(name, description, input_schema, category, created_by)
VALUES (
    'horoscope-generator',
    'Génère un horoscope personnalisé',
    '{"type": "object", "properties": {"sign": {"type": "string"}}, "required": ["sign"]}',
    'meta',
    'llm'
);

-- Ajouter l'implémentation
INSERT INTO tool_implementations
(tool_name, step_order, step_name, step_type, sql_template)
VALUES
    ('horoscope-generator', 1, 'execute', 'sql', 
     'SELECT ''Horoscope pour '' || {{sign}} || '' : Aujourd''hui sera magnifique!'' AS prediction');
```

**🚀 Hot reload automatique** : Le tool apparaît dans OpenCode en 2 secondes !

## Debugging SQL

### Vérifier l'état du serveur

```sql
-- Heartbeat
SELECT * FROM output.heartbeat;

-- Métriques système
SELECT * FROM metadata.system_metrics ORDER BY created_at DESC LIMIT 10;

-- Circuit breakers
SELECT name, state, failure_count, success_count
FROM lifecycle_execution.circuit_breakers;
```

### Vérifier les tools chargés

```sql
SELECT name, description, enabled, version
FROM lifecycle_tools.tool_definitions
WHERE enabled = 1;
```

### Vérifier l'idempotence

```sql
SELECT hash, tool_name, status, created_at
FROM lifecycle_execution.processed_log
ORDER BY created_at DESC
LIMIT 20;
```

### Dead letter queue

```sql
SELECT tool_name, error_message, attempts, last_attempt_at
FROM output.dead_letter_queue
WHERE resolved = 0;
```

## Sécurité ATTACH

Pour permettre ATTACH à un worker HOROS externe :

```sql
INSERT INTO lifecycle_core.allowed_attach_paths
(worker_name, db_path, db_type, description)
VALUES (
    'token-chunker',
    '/workspace/horos-rag/workers/token-chunker/output.db',
    'output',
    'Worker RAG token chunker'
);
```

## Structure du Code

```
holow-mcp/
├── cmd/holow-mcp/main.go      # Point d'entrée
├── internal/
│   ├── database/db.go         # Gestionnaire 6 bases
│   ├── server/server.go       # Serveur MCP JSON-RPC
│   ├── tools/tools.go         # Hot reload tools
│   ├── circuit/breaker.go     # Circuit breaker
│   ├── observability/metrics.go # Métriques et logs
│   └── config/config.go       # Configuration
├── schemas/                   # Schémas SQL des 6 bases
├── Magefile.go               # Build automation
├── go.mod                    # Dépendances Go
└── .golangci.yml             # Configuration linters
```

## 🎯 Quick Start (3 minutes)

```bash
# 1. Build
cd /workspace/projets/holow-mcp
mage build

# 2. Initialiser les bases (IMPORTANT : utiliser les schémas holow-mcp)
mage initdb

# 3. Configurer OpenCode (ajouter dans ~/.config/opencode/opencode.json)
{
  "mcp": {
    "holow-mcp": {
      "type": "local", 
      "command": ["/workspace/projets/holow-mcp/bin/holow-mcp"],
      "enabled": true
    }
  }
}

# 4. Redémarrer OpenCode et utiliser :
# "use the holow-mcp tool to analyze this codebase"
```

## 🧪 Test Rapide

```bash
# Test MCP protocol
echo '{"jsonrpc": "2.0", "id": 1, "method": "tools/list"}' | \
./bin/holow-mcp | jq '.result.tools | length'
# → 2 (browser + brainloop)

# Test brainloop actions
echo '{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": {"name": "brainloop", "arguments": {"action": "list_actions"}}}' | \
./bin/holow-mcp | jq '.result.content[0].text' | jq '.actions | length'
# → 18 actions disponibles !
```

## ⚠️ Erreurs Communes

- **❌ Utiliser `/workspace/templates/schemas/`** → Templates workers génériques
- **✅ Utiliser `./schemas`** → Schémas holow-mcp spécifiques (6-BDD)
- **❌ Oublier `mage initdb`** → Bases non initialisées
- **✅ Configurer OpenCode** → Sinon tools non disponibles

## 🏗️ Conformité HOROS

HOLOW-MCP respecte tous les invariants HOROS :

- ✅ modernc.org/sqlite (pas mattn/go-sqlite3)
- ✅ Idempotence via processed_log
- ✅ Hash SHA256 comme identité
- ✅ ATTACH temporaire uniquement
- ✅ Heartbeat 15s
- ✅ Graceful shutdown 60s
- ✅ Pragmas SQLite (WAL, NORMAL, etc.)
- ✅ 15 dimensions documentées dans ego_index

## 📚 Documentation Complète

- **Guide d'utilisation** : `/workspace/HOLOW_MCP_CORRECT_USAGE.md`
- **Patterns architecturaux** : `/workspace/docs/architecture/`
- **Intégration OpenCode** : https://opencode.ai/docs/mcp-servers

---

🎉 **HOLOW-MCP = Le serveur MCP le plus avancé de l'écosystème HOROS !**

## Licence

Propriétaire HOROS
