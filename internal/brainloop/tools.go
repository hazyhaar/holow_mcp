// Package brainloop fournit des outils d'analyse intelligente pour holow-mcp
// Inspiré du worker brainloop de HOROS
package brainloop

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// ToolsManager gère les outils brainloop
type ToolsManager struct {
	mu      sync.Mutex
	toolsDB *sql.DB // Base lifecycle-tools pour actions système
	execDB  *sql.DB // Base lifecycle-execution pour statistiques
}

// NewToolsManager crée un nouveau gestionnaire
func NewToolsManager() *ToolsManager {
	return &ToolsManager{}
}

// SetToolsDB configure la base de données des tools
func (m *ToolsManager) SetToolsDB(db *sql.DB) {
	m.toolsDB = db
}

// SetExecDB configure la base de données d'exécution pour les statistiques
func (m *ToolsManager) SetExecDB(db *sql.DB) {
	m.execDB = db
}

// ToolDefinitions retourne la définition du tool maître brainloop
// Pattern Progressive Disclosure : 1 tool au lieu de 11 = 83% économie tokens contexte
func (m *ToolsManager) ToolDefinitions() []map[string]interface{} {
	return []map[string]interface{}{
		{
			"name":        "brainloop",
			"description": "Smart analysis, generation, and system tool. Actions: create_tool, list_tools, get_tool, audit_system, get_metrics (system); generate_file, generate_sql, explore, loop (generation); read_sqlite, read_code, read_markdown, read_config (reading); list_actions, get_schema, get_stats (discovery)",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"description": "Action to perform",
						"enum": []string{
							// Système
							"create_tool",
							"list_tools",
							"get_tool",
							"audit_system",
							"get_metrics",
							// Génération
							"generate_file",
							"generate_sql",
							"explore",
							"loop",
							// Lecture
							"read_sqlite",
							"read_code",
							"read_markdown",
							"read_config",
							"list_files",
							"search_code",
							// Discovery
							"list_actions",
							"get_schema",
							"get_stats",
						},
					},
					"path": map[string]interface{}{
						"type":        "string",
						"description": "File or directory path",
					},
					"pattern": map[string]interface{}{
						"type":        "string",
						"description": "Search/glob pattern",
					},
					"max_rows": map[string]interface{}{
						"type":        "integer",
						"default":     3,
						"description": "Max sample rows (for read_sqlite)",
					},
					"action_name": map[string]interface{}{
						"type":        "string",
						"description": "Action name (for get_schema)",
					},
					// Paramètres génération
					"prompt": map[string]interface{}{
						"type":        "string",
						"description": "Generation prompt (for generate_* actions)",
					},
					"sql": map[string]interface{}{
						"type":        "string",
						"description": "SQL to execute (for generate_sql)",
					},
					"context": map[string]interface{}{
						"type":        "object",
						"description": "Additional context for generation",
					},
					// Paramètres système
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Tool name (for create_tool, get_tool)",
					},
					"tool_description": map[string]interface{}{
						"type":        "string",
						"description": "Tool description (for create_tool)",
					},
					"parameters": map[string]interface{}{
						"type":        "object",
						"description": "Tool input schema (for create_tool)",
					},
					"category": map[string]interface{}{
						"type":        "string",
						"description": "Tool category (for create_tool, list_tools)",
					},
				},
				"required": []string{"action"},
			},
		},
	}
}

// Execute exécute le tool maître brainloop avec dispatch sur action
func (m *ToolsManager) Execute(toolName string, args map[string]interface{}) (interface{}, error) {
	// Le tool maître s'appelle "brainloop"
	if toolName != "brainloop" {
		return nil, fmt.Errorf("unknown tool: %s (expected 'brainloop')", toolName)
	}

	action, ok := args["action"].(string)
	if !ok {
		return nil, fmt.Errorf("action parameter is required")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	switch action {
	// Système
	case "create_tool":
		return m.createTool(args)
	case "list_tools":
		return m.listTools(args)
	case "get_tool":
		return m.getTool(args)
	case "audit_system":
		return m.auditSystem()
	case "get_metrics":
		return m.getMetrics()
	// Génération
	case "generate_file":
		return m.generateFile(args)
	case "generate_sql":
		return m.generateSQL(args)
	case "explore":
		return m.explore(args)
	case "loop":
		return m.loop(args)
	// Lecture
	case "read_sqlite":
		return m.readSQLite(args)
	case "read_code":
		return m.readCode(args)
	case "read_markdown":
		return m.readMarkdown(args)
	case "read_config":
		return m.readConfig(args)
	case "list_files":
		return m.listFiles(args)
	case "search_code":
		return m.searchCode(args)
	// Discovery
	case "list_actions":
		return m.listActions()
	case "get_schema":
		return m.getSchema(args)
	case "get_stats":
		return m.getStats()
	default:
		return nil, fmt.Errorf("unknown action: %s", action)
	}
}

// listActions retourne la liste des actions disponibles
func (m *ToolsManager) listActions() (interface{}, error) {
	return map[string]interface{}{
		"actions": []map[string]interface{}{
			// Système (5)
			{"name": "create_tool", "description": "Create a new MCP tool", "requires": []string{"name", "tool_description", "sql"}, "category": "system"},
			{"name": "list_tools", "description": "List available tools", "requires": []string{}, "category": "system"},
			{"name": "get_tool", "description": "Get tool details", "requires": []string{"name"}, "category": "system"},
			{"name": "audit_system", "description": "Audit system status", "requires": []string{}, "category": "system"},
			{"name": "get_metrics", "description": "Get system metrics", "requires": []string{}, "category": "system"},
			// Génération (4)
			{"name": "generate_file", "description": "Generate file from prompt with pattern extraction", "requires": []string{"prompt", "path"}, "category": "generation"},
			{"name": "generate_sql", "description": "Generate and execute SQL from prompt", "requires": []string{"prompt"}, "category": "generation"},
			{"name": "explore", "description": "Creative exploration of codebase", "requires": []string{"prompt"}, "category": "generation"},
			{"name": "loop", "description": "Iterative workflow: propose/audit/refine/commit", "requires": []string{"prompt"}, "category": "generation"},
			// Lecture (4)
			{"name": "read_sqlite", "description": "Analyze SQLite database structure", "requires": []string{"path"}, "category": "reading"},
			{"name": "read_code", "description": "Analyze code file with pattern detection", "requires": []string{"path"}, "category": "reading"},
			{"name": "read_markdown", "description": "Analyze markdown document structure", "requires": []string{"path"}, "category": "reading"},
			{"name": "read_config", "description": "Analyze config file (JSON/YAML/TOML)", "requires": []string{"path"}, "category": "reading"},
			// Utilitaires
			{"name": "list_files", "description": "List files matching glob pattern", "requires": []string{"pattern"}, "category": "utility"},
			{"name": "search_code", "description": "Search pattern in code files", "requires": []string{"pattern"}, "category": "utility"},
			// Discovery (3)
			{"name": "list_actions", "description": "List all available actions", "requires": []string{}, "category": "discovery"},
			{"name": "get_schema", "description": "Get detailed schema for an action", "requires": []string{"action_name"}, "category": "discovery"},
			{"name": "get_stats", "description": "Get usage statistics", "requires": []string{}, "category": "discovery"},
		},
		"total": 18,
	}, nil
}

// generateFile génère un fichier à partir d'un prompt
func (m *ToolsManager) generateFile(args map[string]interface{}) (interface{}, error) {
	prompt, ok := args["prompt"].(string)
	if !ok {
		return nil, fmt.Errorf("prompt is required for generate_file")
	}

	path, ok := args["path"].(string)
	if !ok {
		return nil, fmt.Errorf("path is required for generate_file")
	}

	// TODO: Intégrer avec LLM (Cerebras) pour génération
	// Pour l'instant, retourner un placeholder
	return map[string]interface{}{
		"success": false,
		"action":  "generate_file",
		"prompt":  prompt,
		"path":    path,
		"message": "Generation requires LLM integration (Cerebras). Use MCP to generate content and write to path.",
		"hint":    "Extract patterns from codebase first with read_code, then generate conformant code",
	}, nil
}

// generateSQL génère et exécute du SQL
func (m *ToolsManager) generateSQL(args map[string]interface{}) (interface{}, error) {
	prompt, ok := args["prompt"].(string)
	if !ok {
		return nil, fmt.Errorf("prompt is required for generate_sql")
	}

	// Si SQL fourni directement, l'exécuter
	if sqlQuery, ok := args["sql"].(string); ok && sqlQuery != "" {
		dbPath, _ := args["path"].(string)
		if dbPath == "" {
			return nil, fmt.Errorf("path to database is required when sql is provided")
		}

		db, err := sql.Open("sqlite", dbPath)
		if err != nil {
			return nil, fmt.Errorf("failed to open database: %w", err)
		}
		defer db.Close()

		result, err := db.Exec(sqlQuery)
		if err != nil {
			return nil, fmt.Errorf("SQL execution failed: %w", err)
		}

	
rowsAffected, _ := result.RowsAffected()
		lastID, _ := result.LastInsertId()

		return map[string]interface{}{
			"success":       true,
			"action":        "generate_sql",
			"sql":           sqlQuery,
			"rows_affected": rowsAffected,
			"last_insert_id": lastID,
		}, nil
	}

	// TODO: Intégrer avec LLM pour génération SQL
	return map[string]interface{}{
		"success": false,
		"action":  "generate_sql",
		"prompt":  prompt,
		"message": "SQL generation requires LLM integration. Provide 'sql' parameter to execute directly.",
	}, nil
}

// explore fait une exploration créative du codebase
func (m *ToolsManager) explore(args map[string]interface{}) (interface{}, error) {
	prompt, ok := args["prompt"].(string)
	if !ok {
		return nil, fmt.Errorf("prompt is required for explore")
	}

	basePath := "."
	if p, ok := args["path"].(string); ok {
		basePath = p
	}

	// Collecter des informations sur le codebase
	var stats struct {
		goFiles    int
		sqlFiles   int
		mdFiles    int
		totalFiles int
		totalSize  int64
	}

	filepath.Walk(basePath, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		stats.totalFiles++
		stats.totalSize += info.Size()
		switch filepath.Ext(path) {
		case ".go":
			stats.goFiles++
		case ".sql":
			stats.sqlFiles++
		case ".md":
			stats.mdFiles++
		}
		return nil
	})

	return map[string]interface{}{
		"success": true,
		"action":  "explore",
		"prompt":  prompt,
		"path":    basePath,
		"codebase_stats": map[string]interface{}{
			"total_files": stats.totalFiles,
			"total_size":  stats.totalSize,
			"go_files":    stats.goFiles,
			"sql_files":   stats.sqlFiles,
			"md_files":    stats.mdFiles,
		},
		"message": "Use this context with LLM to explore creatively based on prompt",
	}, nil
}

// loop exécute un workflow itératif propose/audit/refine/commit
func (m *ToolsManager) loop(args map[string]interface{}) (interface{}, error) {
	prompt, ok := args["prompt"].(string)
	if !ok {
		return nil, fmt.Errorf("prompt is required for loop")
	}

	// TODO: Implémenter le workflow itératif complet
	return map[string]interface{}{
		"success": false,
		"action":  "loop",
		"prompt":  prompt,
		"workflow": []string{
			"1. propose - Generate initial proposal",
			"2. audit - Analyze proposal against patterns",
			"3. refine - Improve based on audit",
			"4. commit - Finalize and commit",
		},
		"message": "Loop workflow requires LLM integration for iterative refinement",
	}, nil
}

// getSchema retourne le schéma détaillé d'une action
func (m *ToolsManager) getSchema(args map[string]interface{}) (interface{}, error) {
	actionName, ok := args["action_name"].(string)
	if !ok {
		return nil, fmt.Errorf("action_name is required")
	}

	schemas := map[string]interface{}{
		// Génération
		"generate_file": map[string]interface{}{
			"action":   "generate_file",
			"required": []string{"prompt", "path"},
			"optional": map[string]interface{}{
				"context": "object - Additional context for generation",
			},
			"example": map[string]interface{}{
				"action": "generate_file",
				"prompt": "Create a Go worker that polls input.db every 5s",
				"path":   "/workspace/projets/my-worker/main.go",
			},
		},
		"generate_sql": map[string]interface{}{
			"action":   "generate_sql",
			"required": []string{"prompt"},
			"optional": map[string]interface{}{
				"sql":  "string - SQL to execute directly (bypasses generation)",
				"path": "string - Database path (required if sql provided)",
			},
			"example": map[string]interface{}{
				"action": "generate_sql",
				"prompt": "Create table for tracking user sessions",
				"path":   "/workspace/projets/my-worker/lifecycle.db",
			},
		},
		"explore": map[string]interface{}{
			"action":   "explore",
			"required": []string{"prompt"},
			"optional": map[string]interface{}{
				"path": "string - Base directory to explore",
			},
			"example": map[string]interface{}{
				"action": "explore",
				"prompt": "Find all error handling patterns",
				"path":   "/workspace",
			},
		},
		"loop": map[string]interface{}{
			"action":   "loop",
			"required": []string{"prompt"},
			"workflow": []string{"propose", "audit", "refine", "commit"},
			"example": map[string]interface{}{
				"action": "loop",
				"prompt": "Refactor authentication module to use JWT",
			},
		},
		// Lecture
		"read_sqlite": map[string]interface{}{
			"action":   "read_sqlite",
			"required": []string{"path"},
			"optional": map[string]interface{}{
				"max_rows": "integer (default: 3) - Maximum sample rows per table",
			},
			"example": map[string]interface{}{
				"action":   "read_sqlite",
				"path":     "/path/to/database.db",
				"max_rows": 5,
			},
		},
		"read_code": map[string]interface{}{
			"action":   "read_code",
			"required": []string{"path"},
			"example": map[string]interface{}{
				"action": "read_code",
				"path":   "/path/to/file.go",
			},
		},
		"read_markdown": map[string]interface{}{
			"action":   "read_markdown",
			"required": []string{"path"},
			"example": map[string]interface{}{
				"action": "read_markdown",
				"path":   "/path/to/README.md",
			},
		},
		"read_config": map[string]interface{}{
			"action":   "read_config",
			"required": []string{"path"},
			"example": map[string]interface{}{
				"action": "read_config",
				"path":   "/path/to/config.json",
			},
		},
		"list_files": map[string]interface{}{
			"action":   "list_files",
			"required": []string{"pattern"},
			"optional": map[string]interface{}{
				"path": "string - Base directory (default: current)",
			},
			"example": map[string]interface{}{
				"action":  "list_files",
				"pattern": "**/*.go",
				"path":    "/workspace",
			},
		},
		"search_code": map[string]interface{}{
			"action":   "search_code",
			"required": []string{"pattern"},
			"optional": map[string]interface{}{
				"path": "string - Base directory",
			},
			"example": map[string]interface{}{
				"action":  "search_code",
				"pattern": "func.*Error",
				"path":    "/workspace",
			},
		},
		// Discovery
		"get_stats": map[string]interface{}{
			"action":   "get_stats",
			"required": []string{},
			"returns": map[string]interface{}{
				"total_calls":    "int - Total action invocations",
				"cache_hit_rate": "float - Cache efficiency (0.0-1.0)",
				"by_action":      "map - Calls per action",
			},
			"example": map[string]interface{}{
				"action": "get_stats",
			},
		},
	}

	schema, ok := schemas[actionName]
	if !ok {
		return nil, fmt.Errorf("unknown action: %s", actionName)
	}

	return schema, nil
}

// readSQLite analyse une base SQLite
func (m *ToolsManager) readSQLite(args map[string]interface{}) (interface{}, error) {
	dbPath, ok := args["path"].(string)
	if !ok {
		return nil, fmt.Errorf("path is required for read_sqlite")
	}

	maxRows := 3
	if mr, ok := args["max_rows"].(float64); ok {
		maxRows = int(mr)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	// Get tables

rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []map[string]interface{}
	var tableNames []string

	for rows.Next() {
		var name string
		rows.Scan(&name)
		tableNames = append(tableNames, name)
	}

	for _, tableName := range tableNames {
		tableInfo := map[string]interface{}{
			"name": tableName,
		}

		// Get columns
		colRows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", tableName))
		if err != nil {
			continue
		}

		var columns []map[string]interface{}
		for colRows.Next() {
			var cid int
			var name, colType string
			var notnull, pk int
			var dfltValue interface{}
			colRows.Scan(&cid, &name, &colType, &notnull, &dfltValue, &pk)

			columns = append(columns, map[string]interface{}{
				"name":     name,
				"type":     colType,
				"notnull":  notnull == 1,
				"pk":       pk == 1,
			})
		}
		colRows.Close()
		tableInfo["columns"] = columns

		// Get row count
		var count int
		db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", tableName)).Scan(&count)
		tableInfo["row_count"] = count

		// Get sample rows
		if maxRows > 0 && count > 0 {
			sampleRows, _ := db.Query(fmt.Sprintf("SELECT * FROM %s LIMIT %d", tableName, maxRows))
			if sampleRows != nil {
				cols, _ := sampleRows.Columns()
				var samples []map[string]interface{}

				for sampleRows.Next() {
					values := make([]interface{}, len(cols))
					valuePtrs := make([]interface{}, len(cols))
					for i := range values {
						valuePtrs[i] = &values[i]
					}
				sampleRows.Scan(valuePtrs...)

					row := make(map[string]interface{})
					for i, col := range cols {
						val := values[i]
						if b, ok := val.([]byte); ok {
							row[col] = string(b)
						} else {
							row[col] = val
						}
					}
					samples = append(samples, row)
				}
				sampleRows.Close()
				tableInfo["samples"] = samples
			}
		}

		// Get indexes
		idxRows, _ := db.Query(fmt.Sprintf("PRAGMA index_list(%s)", tableName))
		if idxRows != nil {
			var indexes []string
			for idxRows.Next() {
				var seq int
				var name, unique, origin, partial string
				idxRows.Scan(&seq, &name, &unique, &origin, &partial)
				indexes = append(indexes, name)
			}
			idxRows.Close()
			if len(indexes) > 0 {
				tableInfo["indexes"] = indexes
			}
		}

		tables = append(tables, tableInfo)
	}

	return map[string]interface{}{
		"success":     true,
		"db_path":     dbPath,
		"table_count": len(tables),
		"tables":      tables,
	}, nil
}

// readCode analyse un fichier de code
func (m *ToolsManager) readCode(args map[string]interface{}) (interface{}, error) {
	filePath, ok := args["path"].(string)
	if !ok {
		return nil, fmt.Errorf("path is required for read_code")
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	code := string(content)
	lines := strings.Split(code, "\n")
	ext := filepath.Ext(filePath)

	// Detect language
language := detectLanguage(ext)

	result := map[string]interface{}{
		"success":    true,
		"file_path":  filePath,
		"language":   language,
		"line_count": len(lines),
		"byte_size":  len(content),
	}

	// Extract patterns based on language
	switch language {
	case "go":
		result["imports"] = extractGoImports(code)
		result["functions"] = extractGoFunctions(code)
		result["types"] = extractGoTypes(code)
		result["patterns"] = detectGoPatterns(code)
	case "python":
		result["imports"] = extractPythonImports(code)
		result["functions"] = extractPythonFunctions(code)
		result["classes"] = extractPythonClasses(code)
	case "sql":
		result["tables"] = extractSQLTables(code)
		result["indexes"] = extractSQLIndexes(code)
	default:
		result["functions"] = extractGenericFunctions(code)
	}

	return result, nil
}

// readMarkdown analyse un fichier markdown
func (m *ToolsManager) readMarkdown(args map[string]interface{}) (interface{}, error) {
	filePath, ok := args["path"].(string)
	if !ok {
		return nil, fmt.Errorf("path is required for read_markdown")
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	md := string(content)
	lines := strings.Split(md, "\n")

	// Extract headers
	var headers []map[string]interface{}
	headerRegex := regexp.MustCompile(`^(#{1,6})\s+(.+)$`)
	for i, line := range lines {
		if matches := headerRegex.FindStringSubmatch(line); matches != nil {
			headers = append(headers, map[string]interface{}{
				"level": len(matches[1]),
				"text":  matches[2],
				"line":  i + 1,
			})
		}
	}

	// Extract code blocks
	var codeBlocks []map[string]interface{}
	codeBlockRegex := regexp.MustCompile("```(\\w*)")
	inBlock := false
	var currentLang string
	var blockStart int

	for i, line := range lines {
		if matches := codeBlockRegex.FindStringSubmatch(line); matches != nil {
			if !inBlock {
				inBlock = true
				currentLang = matches[1]
				blockStart = i + 1
			} else {
				codeBlocks = append(codeBlocks, map[string]interface{}{
					"language":   currentLang,
					"start_line": blockStart,
					"end_line":   i + 1,
				})
				inBlock = false
			}
		}
	}

	// Extract links
	var links []string
	linkRegex := regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`) // Escaped parentheses for regex
	for _, match := range linkRegex.FindAllStringSubmatch(md, -1) {
		links = append(links, match[2])
	}

	return map[string]interface{}{
		"success":      true,
		"file_path":    filePath,
		"line_count":   len(lines),
		"header_count": len(headers),
		"headers":      headers,
		"code_blocks":  codeBlocks,
		"link_count":   len(links),
		"links":        links,
	}, nil
}

// readConfig analyse un fichier de configuration
func (m *ToolsManager) readConfig(args map[string]interface{}) (interface{}, error) {
	filePath, ok := args["path"].(string)
	if !ok {
		return nil, fmt.Errorf("path is required for read_config")
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	ext := filepath.Ext(filePath)
	result := map[string]interface{}{
		"success":   true,
		"file_path": filePath,
		"format":    strings.TrimPrefix(ext, "."),
	}

	// Parse JSON
	if ext == ".json" {
		var data interface{}
		if err := json.Unmarshal(content, &data); err != nil {
			result["parse_error"] = err.Error()
		} else {
			result["keys"] = extractKeys(data, "")
			result["parsed"] = true
		}
	}

	// Detect environment variables
	envVarRegex := regexp.MustCompile(`\$\{?([A-Z_][A-Z0-9_]*)\}?`)
	var envVars []string
	for _, match := range envVarRegex.FindAllStringSubmatch(string(content), -1) {
		envVars = append(envVars, match[1])
	}
	if len(envVars) > 0 {
		result["env_vars"] = unique(envVars)
	}

	// Detect potential secrets
	secretPatterns := []string{
		"password", "secret", "key", "token", "api_key", "apikey",
		"auth", "credential", "private",
	}
	var potentialSecrets []string
	lowerContent := strings.ToLower(string(content))
	for _, pattern := range secretPatterns {
		if strings.Contains(lowerContent, pattern) {
			potentialSecrets = append(potentialSecrets, pattern)
		}
	}
	if len(potentialSecrets) > 0 {
		result["potential_secrets"] = potentialSecrets
		result["warning"] = "File may contain sensitive data"
	}

	return result, nil
}

// listFiles liste les fichiers correspondant à un pattern
func (m *ToolsManager) listFiles(args map[string]interface{}) (interface{}, error) {
	pattern, ok := args["pattern"].(string)
	if !ok {
		return nil, fmt.Errorf("pattern is required for list_files")
	}

	// Extraire basePath du pattern si absolu
	basePath := "."
	if bp, ok := args["path"].(string); ok {
		basePath = bp
	} else if strings.HasPrefix(pattern, "/") {
		// Pattern absolu: extraire le basePath avant le premier wildcard
		parts := strings.Split(pattern, "/")
		var baseparts []string
		for _, p := range parts {
			if strings.ContainsAny(p, "*?[") {
				break
			}
			baseparts = append(baseparts, p)
		}
		if len(baseparts) > 0 {
			basePath = strings.Join(baseparts, "/")
			if basePath == "" {
				basePath = "/"
			}
		}
	}

	// Extraire le pattern de fichier (après **)
	filePattern := "*"
	if idx := strings.LastIndex(pattern, "/"); idx != -1 {
		filePattern = pattern[idx+1:]
	}

	var files []map[string]interface{}

	err := filepath.Walk(basePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			// Skip hidden and common non-code dirs
			base := filepath.Base(path)
			if strings.HasPrefix(base, ".") || base == "node_modules" || base == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}

		// Match le pattern de fichier
		matched, _ := filepath.Match(filePattern, filepath.Base(path))
		if matched {
			files = append(files, map[string]interface{}{
				"path":     path,
				"size":     info.Size(),
				"modified": info.ModTime().Unix(),
			})
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"success":    true,
		"pattern":    pattern,
		"base_path":  basePath,
		"file_count": len(files),
		"files":      files,
	}, nil
}

// searchCode recherche un pattern dans les fichiers de code
func (m *ToolsManager) searchCode(args map[string]interface{}) (interface{}, error) {
	pattern, ok := args["pattern"].(string)
	if !ok {
		return nil, fmt.Errorf("pattern is required for search_code")
	}

	regex, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex pattern: %w", err)
	}

	filePattern := "*"
	if fp, ok := args["file_pattern"].(string); ok {
		filePattern = fp
	}

	basePath := "."
	if bp, ok := args["path"].(string); ok {
		basePath = bp
	}

	var matches []map[string]interface{}

	// Dossiers à exclure
	excludeDirs := map[string]bool{
		"bin": true, ".git": true, "node_modules": true, "vendor": true,
		"dist": true, "build": true, "__pycache__": true,
	}

	filepath.Walk(basePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		// Skip excluded directories
		if info.IsDir() {
			if excludeDirs[info.Name()] || strings.HasPrefix(info.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip large files (>1MB)
		if info.Size() > 1024*1024 {
			return nil
		}

		matched, _ := filepath.Match(filePattern, filepath.Base(path))
		if !matched {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		// Skip binary files (check for null bytes in first 512 bytes)
		checkLen := len(content)
		if checkLen > 512 {
			checkLen = 512
		}
		for i := 0; i < checkLen; i++ {
			if content[i] == 0 {
				return nil // Binary file
			}
		}

		lines := strings.Split(string(content), "\n")
		for i, line := range lines {
			if regex.MatchString(line) {
				matches = append(matches, map[string]interface{}{
					"file": path,
					"line": i + 1,
					"text": strings.TrimSpace(line),
				})
			}
		}
		return nil
	})

	return map[string]interface{}{
		"success":     true,
		"pattern":     pattern,
		"match_count": len(matches),
		"matches":     matches,
	}, nil
}

// IsBrainloopTool vérifie si c'est le tool maître brainloop

// createTool crée un nouveau tool MCP
func (m *ToolsManager) createTool(args map[string]interface{}) (interface{}, error) {
	if m.toolsDB == nil {
		return nil, fmt.Errorf("tools database not configured")
	}

	name, _ := args["name"].(string)
	desc, _ := args["tool_description"].(string)
	category, _ := args["category"].(string)
	sqlQuery, _ := args["sql"].(string)

	if name == "" || desc == "" || sqlQuery == "" {
		return nil, fmt.Errorf("name, tool_description, and sql are required for create_tool")
	}

	if category == "" {
		category = "custom"
	}

	// Sérialiser parameters
	paramsJSON := "{}"
	if params, ok := args["parameters"]; ok {
		jsonBytes, _ := json.Marshal(params)
		paramsJSON = string(jsonBytes)
	}

	// Insérer le tool
	_, err := m.toolsDB.Exec(`
		INSERT INTO tool_definitions (name, description, input_schema, category, version, enabled, timeout_seconds, created_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, 1, 1, 30, 'brainloop', strftime('%s', 'now'), strftime('%s', 'now'))
	`, name, desc, paramsJSON, category)
	if err != nil {
		return nil, fmt.Errorf("failed to create tool: %w", err)
	}

	// Insérer l'implémentation
	_, err = m.toolsDB.Exec(`
		INSERT INTO tool_implementations (tool_name, step_order, step_name, step_type, sql_template)
		VALUES (?, 1, 'execute', 'sql', ?)
	`, name, sqlQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to create tool implementation: %w", err)
	}

	return map[string]interface{}{
		"success": true,
		"action":  "create_tool",
		"name":    name,
		"message": fmt.Sprintf("Tool '%s' created successfully", name),
	}, nil
}

// listTools liste tous les tools disponibles
func (m *ToolsManager) listTools(args map[string]interface{}) (interface{}, error) {
	if m.toolsDB == nil {
		return nil, fmt.Errorf("tools database not configured")
	}

	query := `SELECT name, description, category, enabled FROM tool_definitions WHERE enabled = 1`
	filterCategory, hasCategory := args["category"].(string)
	if hasCategory && filterCategory != "" {
		query += fmt.Sprintf(" AND category = '%s'", filterCategory)
	}
	query += " ORDER BY name"


rows, err := m.toolsDB.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to list tools: %w", err)
	}
	defer rows.Close()

	var tools []map[string]interface{}
	for rows.Next() {
		var name, desc, category string
		var enabled int
		rows.Scan(&name, &desc, &category, &enabled)
		tools = append(tools, map[string]interface{}{
			"name":        name,
			"description": desc,
			"category":    category,
		})
	}

	return map[string]interface{}{
		"success": true,
		"action":  "list_tools",
		"tools":   tools,
		"count":   len(tools),
	}, nil
}

// getTool retourne les détails d'un tool
func (m *ToolsManager) getTool(args map[string]interface{}) (interface{}, error) {
	if m.toolsDB == nil {
		return nil, fmt.Errorf("tools database not configured")
	}

	name, ok := args["name"].(string)
	if !ok || name == "" {
		return nil, fmt.Errorf("name is required for get_tool")
	}

	var desc, inputSchema, category string
	var version, enabled, timeout int
	err := m.toolsDB.QueryRow(`
		SELECT description, input_schema, category, version, enabled, timeout_seconds
		FROM tool_definitions WHERE name = ?
	`, name).Scan(&desc, &inputSchema, &category, &version, &enabled, &timeout)
	if err != nil {
		return nil, fmt.Errorf("tool not found: %s", name)
	}

	// Get implementations

rows, _ := m.toolsDB.Query(`
		SELECT step_order, step_name, step_type, sql_template
		FROM tool_implementations WHERE tool_name = ? ORDER BY step_order
	`, name)
	defer rows.Close()

	var steps []map[string]interface{}
	for rows.Next() {
		var order int
		var stepName, stepType, sqlTemplate string
		rows.Scan(&order, &stepName, &stepType, &sqlTemplate)
		steps = append(steps, map[string]interface{}{
			"order":    order,
			"name":     stepName,
			"type":     stepType,
			"template": sqlTemplate,
		})
	}

	return map[string]interface{}{
		"success":     true,
		"action":      "get_tool",
		"name":        name,
		"description": desc,
		"schema":      inputSchema,
		"category":    category,
		"version":     version,
		"enabled":     enabled == 1,
		"timeout":     timeout,
		"steps":       steps,
	}, nil
}

// auditSystem retourne un audit du système
func (m *ToolsManager) auditSystem() (interface{}, error) {
	if m.toolsDB == nil {
		return nil, fmt.Errorf("tools database not configured")
	}

	var toolCount, enabledCount int
	m.toolsDB.QueryRow("SELECT COUNT(*) FROM tool_definitions").Scan(&toolCount)
	m.toolsDB.QueryRow("SELECT COUNT(*) FROM tool_definitions WHERE enabled = 1").Scan(&enabledCount)

	// Count by category

rows, _ := m.toolsDB.Query("SELECT category, COUNT(*) FROM tool_definitions GROUP BY category")
	defer rows.Close()

	categories := make(map[string]int)
	for rows.Next() {
		var cat string
		var count int
		rows.Scan(&cat, &count)
		categories[cat] = count
	}

	return map[string]interface{}{
		"success":      true,
		"action":       "audit_system",
		"total_tools":  toolCount,
		"enabled":      enabledCount,
		"disabled":     toolCount - enabledCount,
		"by_category":  categories,
	}, nil
}

// getMetrics retourne les métriques du système
func (m *ToolsManager) getMetrics() (interface{}, error) {
	if m.toolsDB == nil {
		return nil, fmt.Errorf("tools database not configured")
	}

	var toolCount int
	m.toolsDB.QueryRow("SELECT COUNT(*) FROM tool_definitions WHERE enabled = 1").Scan(&toolCount)

	return map[string]interface{}{
		"success":       true,
		"action":        "get_metrics",
		"active_tools":  toolCount,
		"message":       "Full metrics available in output.db",
	}, nil
}

// getStats retourne les statistiques d'usage depuis processed_log
func (m *ToolsManager) getStats() (interface{}, error) {
	if m.execDB == nil {
		return map[string]interface{}{
			"success": false,
			"action":  "get_stats",
			"error":   "execution database not configured",
		},
		nil
	}

	// Total des appels
	var totalCalls int
	m.execDB.QueryRow("SELECT COUNT(*) FROM processed_log").Scan(&totalCalls)

	// Appels réussis/échoués
	var successCount, failedCount int
	m.execDB.QueryRow("SELECT COUNT(*) FROM processed_log WHERE status = 'success'").Scan(&successCount)
	m.execDB.QueryRow("SELECT COUNT(*) FROM processed_log WHERE status = 'failed'").Scan(&failedCount)

	// Statistiques par méthode
	byMethod := make(map[string]int)

rows, err := m.execDB.Query(`
		SELECT method, COUNT(*) as count
		FROM processed_log
		GROUP BY method
		ORDER BY count DESC
	`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var method string
			var count int
			if rows.Scan(&method, &count) == nil {
				byMethod[method] = count
			}
		}
	}

	// Latence moyenne
	var avgLatency float64
	m.execDB.QueryRow("SELECT COALESCE(AVG(latency_ms), 0) FROM processed_log").Scan(&avgLatency)

	// Latence par méthode
	latencyByMethod := make(map[string]float64)
	latRows, err := m.execDB.Query(`
		SELECT method, AVG(latency_ms) as avg_latency
		FROM processed_log
		GROUP BY method
	`)
	if err == nil {
		defer latRows.Close()
		for latRows.Next() {
			var method string
			var lat float64
			if latRows.Scan(&method, &lat) == nil {
				latencyByMethod[method] = lat
			}
		}
	}

	// Dernière heure
	var lastHourCalls int
	m.execDB.QueryRow(`
		SELECT COUNT(*) FROM processed_log
		WHERE created_at >= strftime('%s', 'now') - 3600
	`).Scan(&lastHourCalls)

	// Taux de succès
	successRate := 0.0
	if totalCalls > 0 {
		successRate = float64(successCount) / float64(totalCalls) * 100
	}

	return map[string]interface{}{
		"success": true,
		"action":  "get_stats",
		"stats": map[string]interface{}{
			"total_calls":       totalCalls,
			"success_count":     successCount,
			"failed_count":      failedCount,
			"success_rate":      fmt.Sprintf("%.1f%%", successRate),
			"avg_latency_ms":    fmt.Sprintf("%.2f", avgLatency),
			"by_method":         byMethod,
			"latency_by_method": latencyByMethod,
			"last_hour_calls":   lastHourCalls,
		},
	}, nil
}
func IsBrainloopTool(name string) bool {
	return name == "brainloop"
}

// Helper functions

func detectLanguage(ext string) string {
	switch ext {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".js", ".jsx":
		return "javascript"
	case ".ts", ".tsx":
		return "typescript"
	case ".sql":
		return "sql"
	case ".rs":
		return "rust"
	case ".java":
		return "java"
	case ".c", ".h":
		return "c"
	case ".cpp", ".hpp":
		return "cpp"
	case ".rb":
		return "ruby"
	case ".php":
		return "php"
	case ".sh":
		return "bash"
	default:
		return "unknown"
	}
}

func extractGoImports(code string) []string {
	var imports []string
	importRegex := regexp.MustCompile(`import\s+(?:\(([^)]+)\)|"([^"]+)")`)

	if matches := importRegex.FindAllStringSubmatch(code, -1); matches != nil {
		for _, match := range matches {
			if match[1] != "" {
				// Multi-line import
				lines := strings.Split(match[1], "\n")
				for _, line := range lines {
					line = strings.TrimSpace(line)
					if line != "" && !strings.HasPrefix(line, "//") {
						// Extract package name from quoted string
						if idx := strings.Index(line, `"`); idx >= 0 {
								end := strings.LastIndex(line, `"`)
								if end > idx {
									imports = append(imports, line[idx+1:end])
								}
						}
					}
				}
			} else if match[2] != "" {
				imports = append(imports, match[2])
			}
		}
	}
	return imports
}

func extractGoFunctions(code string) []string {
	var functions []string
	funcRegex := regexp.MustCompile(`func\s+(?:\([^)]+\)\s+)?(\w+)\s*\(`)

	for _, match := range funcRegex.FindAllStringSubmatch(code, -1) {
		functions = append(functions, match[1])
	}
	return functions
}

func extractGoTypes(code string) []string {
	var types []string
	typeRegex := regexp.MustCompile(`type\s+(\w+)\s+(?:struct|interface)`)

	for _, match := range typeRegex.FindAllStringSubmatch(code, -1) {
		types = append(types, match[1])
	}
	return types
}

func detectGoPatterns(code string) []string {
	var patterns []string

	if strings.Contains(code, "sync.Mutex") || strings.Contains(code, "sync.RWMutex") {
		patterns = append(patterns, "mutex_locking")
	}
	if strings.Contains(code, "context.Context") {
		patterns = append(patterns, "context_usage")
	}
	if strings.Contains(code, "defer ") {
		patterns = append(patterns, "defer_cleanup")
	}
	if strings.Contains(code, "go func") || strings.Contains(code, "go ") {
		patterns = append(patterns, "goroutines")
	}
	if strings.Contains(code, "chan ") || strings.Contains(code, "<-") {
		patterns = append(patterns, "channels")
	}
	if strings.Contains(code, "database/sql") || strings.Contains(code, "github.com/ncruces/go-sqlite3") {
		patterns = append(patterns, "database_access")
	}

	return patterns
}

func extractPythonImports(code string) []string {
	var imports []string
	importRegex := regexp.MustCompile(`(?:from\s+(\S+)\s+import|import\s+(\S+))`)

	for _, match := range importRegex.FindAllStringSubmatch(code, -1) {
		if match[1] != "" {
			imports = append(imports, match[1])
		} else if match[2] != "" {
			imports = append(imports, match[2])
		}
	}
	return imports
}

func extractPythonFunctions(code string) []string {
	var functions []string
	funcRegex := regexp.MustCompile(`def\s+(\w+)\s*\(`)

	for _, match := range funcRegex.FindAllStringSubmatch(code, -1) {
		functions = append(functions, match[1])
	}
	return functions
}

func extractPythonClasses(code string) []string {
	var classes []string
	classRegex := regexp.MustCompile(`class\s+(\w+)`)

	for _, match := range classRegex.FindAllStringSubmatch(code, -1) {
		classes = append(classes, match[1])
	}
	return classes
}

func extractSQLTables(code string) []string {
	var tables []string
	tableRegex := regexp.MustCompile(`(?i)CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?(\w+)`)

	for _, match := range tableRegex.FindAllStringSubmatch(code, -1) {
		tables = append(tables, match[1])
	}
	return tables
}

func extractSQLIndexes(code string) []string {
	var indexes []string
	indexRegex := regexp.MustCompile(`(?i)CREATE\s+(?:UNIQUE\s+)?INDEX\s+(?:IF\s+NOT\s+EXISTS\s+)?(\w+)`)

	for _, match := range indexRegex.FindAllStringSubmatch(code, -1) {
		indexes = append(indexes, match[1])
	}
	return indexes
}

func extractGenericFunctions(code string) []string {
	var functions []string
	// Generic function detection for various languages
	funcRegex := regexp.MustCompile(`(?:function|def|func|fn)\s+(\w+)`)

	for _, match := range funcRegex.FindAllStringSubmatch(code, -1) {
		functions = append(functions, match[1])
	}
	return functions
}

func extractKeys(data interface{}, prefix string) []string {
	var keys []string

	switch v := data.(type) {
	case map[string]interface{}:
		for key, value := range v {
			fullKey := key
			if prefix != "" {
				fullKey = prefix + "." + key
			}
			keys = append(keys, fullKey)
			keys = append(keys, extractKeys(value, fullKey)...)
		}
	case []interface{}:
		for i, item := range v {
			arrayKey := fmt.Sprintf("%s[%d]", prefix, i)
			keys = append(keys, extractKeys(item, arrayKey)...)
		}
	}

	return keys
}

func unique(slice []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, s := range slice {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}

func hashContent(content string) string {
	hash := sha256.Sum256([]byte(content))
	return hex.EncodeToString(hash[:])
}