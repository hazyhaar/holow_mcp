// Package tools gère le chargement et l'exécution des tools MCP
package tools

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"
)

// Tool représente un tool MCP chargé
type Tool struct {
	Name          string          `json:"name"`
	Description   string          `json:"description"`
	InputSchema   json.RawMessage `json:"inputSchema"`
	Category      string          `json:"category"`
	Version       int             `json:"version"`
	Enabled       bool            `json:"enabled"`
	TimeoutSecs   int             `json:"timeout_seconds"`
	RetryPolicy   string          `json:"retry_policy"`
	MaxRetries    int             `json:"max_retries"`
	Steps         []ToolStep      `json:"-"`
}

// ToolStep représente une étape d'exécution d'un tool
type ToolStep struct {
	Order        int
	Name         string
	StepType     string
	SQLTemplate  string
	ErrorHandler string
	Condition    string
}

// Manager gère le hot reload des tools
type Manager struct {
	db          *sql.DB
	tools       map[string]*Tool
	mu          sync.RWMutex
	stopChan    chan struct{}
	reloadChan  chan struct{}
}

// NewManager crée un nouveau gestionnaire de tools
func NewManager(db *sql.DB) *Manager {
	return &Manager{
		db:         db,
		tools:      make(map[string]*Tool),
		stopChan:   make(chan struct{}),
		reloadChan: make(chan struct{}, 1),
	}
}

// Start démarre le hot reload des tools
func (m *Manager) Start(pollInterval time.Duration) error {
	// Chargement initial
	if err := m.reload(); err != nil {
		return err
	}

	// Goroutine de polling trigger-based
	go m.pollLoop(pollInterval)

	return nil
}

// pollLoop vérifie le flag hot_reload_flag toutes les N secondes
func (m *Manager) pollLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopChan:
			return
		case <-ticker.C:
			// Vérifier si tools ont changé (trigger-based)
			var dirty int
			err := m.db.QueryRow(`SELECT tools_dirty FROM hot_reload_flag WHERE id = 1`).Scan(&dirty)
			if err != nil {
				continue
			}

			if dirty == 1 {
				if err := m.reload(); err != nil {
					// Log error mais continuer
					continue
				}
				// Reset flag
				m.db.Exec(`UPDATE hot_reload_flag SET tools_dirty = 0, last_reload_at = strftime('%s', 'now') WHERE id = 1`)
			}
		case <-m.reloadChan:
			m.reload()
		}
	}
}

// reload charge tous les tools depuis la base
func (m *Manager) reload() error {
	rows, err := m.db.Query(`
		SELECT name, description, input_schema, category, version,
		       enabled, timeout_seconds, retry_policy, max_retries
		FROM tool_definitions
		WHERE enabled = 1`)
	if err != nil {
		return err
	}
	defer rows.Close()

	newTools := make(map[string]*Tool)

	for rows.Next() {
		var t Tool
		var enabled int
		var inputSchemaStr string
		err := rows.Scan(
			&t.Name, &t.Description, &inputSchemaStr, &t.Category,
			&t.Version, &enabled, &t.TimeoutSecs, &t.RetryPolicy, &t.MaxRetries)
		if err != nil {
			return err
		}
		t.InputSchema = json.RawMessage(inputSchemaStr)
		t.Enabled = enabled == 1

		// Charger les steps
		steps, err := m.loadSteps(t.Name)
		if err != nil {
			return err
		}
		t.Steps = steps

		newTools[t.Name] = &t
	}

	// Swap atomique
	m.mu.Lock()
	m.tools = newTools
	m.mu.Unlock()

	return nil
}

// loadSteps charge les étapes d'exécution d'un tool
func (m *Manager) loadSteps(toolName string) ([]ToolStep, error) {
	rows, err := m.db.Query(`
		SELECT step_order, step_name, step_type, sql_template,
		       COALESCE(error_handler, ''), COALESCE(condition, '')
		FROM tool_implementations
		WHERE tool_name = ?
		ORDER BY step_order`, toolName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var steps []ToolStep
	for rows.Next() {
		var s ToolStep
		if err := rows.Scan(&s.Order, &s.Name, &s.StepType, &s.SQLTemplate, &s.ErrorHandler, &s.Condition); err != nil {
			return nil, err
		}
		steps = append(steps, s)
	}

	return steps, nil
}

// Get retourne un tool par son nom
func (m *Manager) Get(name string) (*Tool, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	t, ok := m.tools[name]
	return t, ok
}

// List retourne la liste de tous les tools
func (m *Manager) List() []*Tool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	list := make([]*Tool, 0, len(m.tools))
	for _, t := range m.tools {
		list = append(list, t)
	}
	return list
}

// Count retourne le nombre de tools chargés
func (m *Manager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.tools)
}

// GetAllToolDefinitions retourne tous les tools comme liste (alias de List)
func (m *Manager) GetAllToolDefinitions() []*Tool {
	return m.List()
}

// ToMCPSchema convertit un Tool en schéma MCP compatible
func (t *Tool) ToMCPSchema() map[string]interface{} {
	return map[string]interface{}{
		"name":        t.Name,
		"description": t.Description,
		"inputSchema": json.RawMessage(t.InputSchema),
	}
}

// ForceReload force un rechargement immédiat
func (m *Manager) ForceReload() {
	select {
	case m.reloadChan <- struct{}{}:
	default:
		// Canal déjà plein, reload en cours
	}
}

// Stop arrête le hot reload
func (m *Manager) Stop() {
	close(m.stopChan)
}

// HashParams calcule le hash SHA256 des paramètres pour idempotence
func HashParams(toolName string, params map[string]interface{}) string {
	data := map[string]interface{}{
		"tool":   toolName,
		"params": params,
	}
	jsonData, _ := json.Marshal(data)
	hash := sha256.Sum256(jsonData)
	return hex.EncodeToString(hash[:])
}

// CreateTool crée un nouveau tool dans la base (pour LLM)
func (m *Manager) CreateTool(name, description string, inputSchema json.RawMessage, category string) error {
	_, err := m.db.Exec(`
		INSERT INTO tool_definitions
		(name, description, input_schema, category, created_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'llm', strftime('%s', 'now'), strftime('%s', 'now'))`,
		name, description, string(inputSchema), category)
	return err
}

// AddToolStep ajoute une étape à un tool
func (m *Manager) AddToolStep(toolName string, stepOrder int, stepName, stepType, sqlTemplate string) error {
	_, err := m.db.Exec(`
		INSERT INTO tool_implementations
		(tool_name, step_order, step_name, step_type, sql_template)
		VALUES (?, ?, ?, ?, ?)`,
		toolName, stepOrder, stepName, stepType, sqlTemplate)
	return err
}

// DetectPatterns détecte les patterns d'action répétitifs
func (m *Manager) DetectPatterns(db *sql.DB) error {
	// Query de détection avec window function
	_, err := db.Exec(`
		INSERT OR REPLACE INTO action_patterns
		(pattern_name, pattern_type, detection_query, tool_sequence,
		 occurrence_count, confidence_score, last_detected_at)
		SELECT
			'auto_' || group_concat(tool_name, '_') as pattern_name,
			'sequence' as pattern_type,
			'' as detection_query,
			json_group_array(tool_name) as tool_sequence,
			COUNT(*) as occurrence_count,
			CASE WHEN COUNT(*) >= 10 THEN 0.9
			     WHEN COUNT(*) >= 5 THEN 0.7
			     ELSE 0.5 END as confidence_score,
			strftime('%s', 'now') as last_detected_at
		FROM (
			SELECT
				tool_name,
				session_id,
				ROW_NUMBER() OVER (PARTITION BY session_id ORDER BY created_at) as seq
			FROM tool_results
			WHERE created_at > strftime('%s', 'now', '-24 hours')
		)
		GROUP BY session_id
		HAVING COUNT(*) >= 3`)

	return err
}
