// Package database gère les connexions aux 6 bases SQLite HOLOW-MCP
package database

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Manager gère les connexions aux 6 bases de données
type Manager struct {
	basePath string

	Input             *sql.DB
	LifecycleTools    *sql.DB
	LifecycleExec     *sql.DB
	LifecycleCore     *sql.DB
	Output            *sql.DB
	Metadata          *sql.DB

	mu sync.RWMutex
}

// DBNames contient les noms des fichiers de base de données
var DBNames = struct {
	Input          string
	LifecycleTools string
	LifecycleExec  string
	LifecycleCore  string
	Output         string
	Metadata       string
}{
	Input:          "holow-mcp.input.db",
	LifecycleTools: "holow-mcp.lifecycle-tools.db",
	LifecycleExec:  "holow-mcp.lifecycle-execution.db",
	LifecycleCore:  "holow-mcp.lifecycle-core.db",
	Output:         "holow-mcp.output.db",
	Metadata:       "holow-mcp.metadata.db",
}

// NewManager crée un nouveau gestionnaire de bases de données
// cdpCallback est un callback optionnel pour LifecycleTools (fonctions SQL CDP)
func NewManager(basePath string, cdpCallback ConnCallback) (*Manager, error) {
	m := &Manager{basePath: basePath}

	var err error

	// Ouvrir toutes les bases avec la méthode unifiée
	// Input, Exec, Core, Output, Metadata : pas de callback (nil)
	m.Input, err = openDBWithConnector(filepath.Join(basePath, DBNames.Input), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to open input.db: %w", err)
	}

	// LifecycleTools : avec callback CDP si fourni
	m.LifecycleTools, err = openDBWithConnector(filepath.Join(basePath, DBNames.LifecycleTools), cdpCallback)
	if err != nil {
		return nil, fmt.Errorf("failed to open lifecycle-tools.db: %w", err)
	}

	m.LifecycleExec, err = openDBWithConnector(filepath.Join(basePath, DBNames.LifecycleExec), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to open lifecycle-execution.db: %w", err)
	}

	m.LifecycleCore, err = openDBWithConnector(filepath.Join(basePath, DBNames.LifecycleCore), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to open lifecycle-core.db: %w", err)
	}

	m.Output, err = openDBWithConnector(filepath.Join(basePath, DBNames.Output), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to open output.db: %w", err)
	}

	m.Metadata, err = openDBWithConnector(filepath.Join(basePath, DBNames.Metadata), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to open metadata.db: %w", err)
	}

	return m, nil
}


// InitSchemas initialise les schémas depuis les fichiers SQL
func (m *Manager) InitSchemas(schemasPath string) error {
	// Schémas de base (1 par DB)
	schemas := map[string]*sql.DB{
		"input.sql":               m.Input,
		"lifecycle-tools.sql":     m.LifecycleTools,
		"lifecycle-execution.sql": m.LifecycleExec,
		"lifecycle-core.sql":      m.LifecycleCore,
		"output.sql":              m.Output,
		"metadata.sql":            m.Metadata,
	}

	for schemaFile, db := range schemas {
		schemaPath := filepath.Join(schemasPath, schemaFile)
		content, err := os.ReadFile(schemaPath)
		if err != nil {
			return fmt.Errorf("failed to read schema %s: %w", schemaFile, err)
		}

		if _, err := db.Exec(string(content)); err != nil {
			return fmt.Errorf("failed to execute schema %s: %w", schemaFile, err)
		}
	}

	// Schémas additionnels (tools, etc.) - tous dans LifecycleTools
	additionalSchemas := []string{
		"cdp-cache.sql",
		"browser-tools.sql",
		"default-tools.sql",
	}

	for _, schemaFile := range additionalSchemas {
		schemaPath := filepath.Join(schemasPath, schemaFile)
		content, err := os.ReadFile(schemaPath)
		if err != nil {
			// Fichier optionnel - ne pas bloquer
			continue
		}

		if _, err := m.LifecycleTools.Exec(string(content)); err != nil {
			return fmt.Errorf("failed to execute schema %s: %w", schemaFile, err)
		}
	}

	return nil
}

// ValidateAttachPath vérifie si un chemin ATTACH est autorisé
func (m *Manager) ValidateAttachPath(path string) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var allowed int
	err := m.LifecycleCore.QueryRow(`
		SELECT allowed FROM allowed_attach_paths
		WHERE db_path = ?`, path).Scan(&allowed)

	if err == sql.ErrNoRows {
		return fmt.Errorf("ATTACH forbidden: path not in whitelist: %s", path)
	}
	if err != nil {
		return fmt.Errorf("failed to check attach path: %w", err)
	}
	if allowed != 1 {
		return fmt.Errorf("ATTACH forbidden: path disabled: %s", path)
	}

	return nil
}

// AddAllowedAttachPath ajoute un chemin à la whitelist
func (m *Manager) AddAllowedAttachPath(workerName, dbPath, dbType, description string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, err := m.LifecycleCore.Exec(`
		INSERT OR REPLACE INTO allowed_attach_paths
		(worker_name, db_path, db_type, allowed, description, added_at)
		VALUES (?, ?, ?, 1, ?, strftime('%s', 'now'))`,
		workerName, dbPath, dbType, description)

	return err
}

// CheckProcessed vérifie si une requête a déjà été traitée (idempotence)
func (m *Manager) CheckProcessed(hash string) (bool, error) {
	var exists int
	err := m.LifecycleExec.QueryRow(`
		SELECT 1 FROM processed_log WHERE hash = ?`, hash).Scan(&exists)

	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// MarkProcessed marque une requête comme traitée
func (m *Manager) MarkProcessed(hash, requestID, toolName, status, resultHash string, processingTimeMs int64) error {
	_, err := m.LifecycleExec.Exec(`
		INSERT INTO processed_log (hash, request_id, tool_name, status, result_hash, processing_time_ms)
		VALUES (?, ?, ?, ?, ?, ?)`,
		hash, requestID, toolName, status, resultHash, processingTimeMs)
	return err
}

// Close ferme toutes les connexions
func (m *Manager) Close() error {
	var errs []error

	if err := m.Input.Close(); err != nil {
		errs = append(errs, err)
	}
	if err := m.LifecycleTools.Close(); err != nil {
		errs = append(errs, err)
	}
	if err := m.LifecycleExec.Close(); err != nil {
		errs = append(errs, err)
	}
	if err := m.LifecycleCore.Close(); err != nil {
		errs = append(errs, err)
	}
	if err := m.Output.Close(); err != nil {
		errs = append(errs, err)
	}
	if err := m.Metadata.Close(); err != nil {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors closing databases: %v", errs)
	}
	return nil
}

// Checkpoint force le checkpoint WAL sur toutes les bases
func (m *Manager) Checkpoint() error {
	dbs := []*sql.DB{
		m.Input, m.LifecycleTools, m.LifecycleExec,
		m.LifecycleCore, m.Output, m.Metadata,
	}

	for _, db := range dbs {
		if _, err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
			return err
		}
	}
	return nil
}
