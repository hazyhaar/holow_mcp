// Package chromium - Enregistrement des fonctions SQL CDP
// Note: Avec modernc.org/sqlite, les fonctions custom sont gérées différemment
// On utilise un pattern de CDPManager global accessible par les fonctions Go
package chromium

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
)

// CDPFunctionRegistry gère l'accès au CDPManager pour les fonctions SQL
type CDPFunctionRegistry struct {
	manager *CDPManager
	mu      sync.RWMutex
}

// globalRegistry est le registre global pour les fonctions CDP
var globalRegistry = &CDPFunctionRegistry{}

// SetCDPManager définit le CDPManager global pour les fonctions SQL
func SetCDPManager(manager *CDPManager) {
	globalRegistry.mu.Lock()
	defer globalRegistry.mu.Unlock()
	globalRegistry.manager = manager
}

// GetCDPManager retourne le CDPManager global
func GetCDPManager() *CDPManager {
	globalRegistry.mu.RLock()
	defer globalRegistry.mu.RUnlock()
	return globalRegistry.manager
}

// RegisterCDPFunctions est maintenant un no-op car les fonctions sont appelées via Go
// Gardé pour compatibilité API
func RegisterCDPFunctions(db *sql.DB, manager *CDPManager) error {
	SetCDPManager(manager)
	return nil
}

// ExecuteCDPCall exécute un appel CDP depuis Go (remplace la fonction SQL cdp_call)
func ExecuteCDPCall(method string, paramsJSON string) (string, error) {
	manager := GetCDPManager()
	if manager == nil {
		return "", fmt.Errorf("CDP manager not initialized")
	}

	// Parser les paramètres JSON
	var params map[string]interface{}
	if paramsJSON != "" && paramsJSON != "{}" && paramsJSON != "null" {
		if err := json.Unmarshal([]byte(paramsJSON), &params); err != nil {
			return "", fmt.Errorf("invalid JSON params: %w", err)
		}
	}

	// S'assurer que le browser est connecté
	if err := manager.EnsureConnected(); err != nil {
		return "", fmt.Errorf("browser not connected: %w", err)
	}

	// Exécuter la commande CDP
	result, err := manager.Call(method, params)
	if err != nil {
		return "", fmt.Errorf("CDP call failed: %w", err)
	}

	return result, nil
}

// CDPConnected vérifie si le browser est connecté
func CDPConnected() bool {
	manager := GetCDPManager()
	if manager == nil {
		return false
	}
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	return manager.browser != nil && manager.sessionID != ""
}

// CDPSessionID retourne l'ID de session actuel
func CDPSessionID() string {
	manager := GetCDPManager()
	if manager == nil {
		return ""
	}
	return manager.GetSessionID()
}

// CDPListPages retourne la liste des pages en JSON
func CDPListPages() (string, error) {
	manager := GetCDPManager()
	if manager == nil {
		return "", fmt.Errorf("CDP manager not initialized")
	}

	if err := manager.EnsureConnected(); err != nil {
		return "", fmt.Errorf("not connected: %w", err)
	}

	targets, err := manager.GetTargets()
	if err != nil {
		return "", fmt.Errorf("failed to get targets: %w", err)
	}

	// Filtrer uniquement les pages
	var pages []TargetInfo
	for _, t := range targets {
		if t.Type == "page" {
			pages = append(pages, t)
		}
	}

	jsonData, err := json.Marshal(pages)
	if err != nil {
		return "", fmt.Errorf("failed to marshal pages: %w", err)
	}

	return string(jsonData), nil
}

// CDPCreatePage crée une nouvelle page et retourne son targetId
func CDPCreatePage(url string) (string, error) {
	manager := GetCDPManager()
	if manager == nil {
		return "", fmt.Errorf("CDP manager not initialized")
	}

	if err := manager.EnsureConnected(); err != nil {
		return "", fmt.Errorf("not connected: %w", err)
	}

	targetID, err := manager.CreatePage(url)
	if err != nil {
		return "", fmt.Errorf("failed to create page: %w", err)
	}

	return targetID, nil
}

// CDPSwitchPage change de page active
func CDPSwitchPage(targetID string) error {
	manager := GetCDPManager()
	if manager == nil {
		return fmt.Errorf("CDP manager not initialized")
	}

	if err := manager.EnsureConnected(); err != nil {
		return fmt.Errorf("not connected: %w", err)
	}

	return manager.SwitchToTarget(targetID)
}

// CDPClosePage ferme une page
func CDPClosePage(targetID string) error {
	manager := GetCDPManager()
	if manager == nil {
		return fmt.Errorf("CDP manager not initialized")
	}

	if err := manager.EnsureConnected(); err != nil {
		return fmt.Errorf("not connected: %w", err)
	}

	return manager.ClosePage(targetID)
}
