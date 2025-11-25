// Package chromium - Fonction SQL cdp_call() pour appeler CDP depuis SQL
package chromium

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/ncruces/go-sqlite3"
	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
)

// CDPManager gère la connexion CDP persistante et expose cdp_call() à SQLite
type CDPManager struct {
	browser   *Browser
	sessionID string // Session CDP active pour la page courante
	mu        sync.RWMutex
	db        *sql.DB
}

// NewCDPManager crée un gestionnaire CDP avec connexion persistante
func NewCDPManager(db *sql.DB) *CDPManager {
	return &CDPManager{
		db: db,
	}
}

// SetDB configure la base de données (utilisé pour initialisation en 2 étapes)
func (m *CDPManager) SetDB(db *sql.DB) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.db = db
}

// EnsureConnected vérifie et établit la connexion au browser si nécessaire
// Établit également une session vers une page (target) pour les commandes CDP
func (m *CDPManager) EnsureConnected() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Vérifier si déjà connecté avec une session active
	if m.browser != nil && m.sessionID != "" {
		return nil
	}

	// Récupérer l'état de session depuis la base (utiliser sql.Null* pour gérer NULL)
	var wsURL sql.NullString
	var connected sql.NullInt64
	var debugPort sql.NullInt64
	err := m.db.QueryRow(`
		SELECT ws_url, connected, debug_port
		FROM cdp_session_state WHERE id = 1
	`).Scan(&wsURL, &connected, &debugPort)

	port := 9222 // Port par défaut
	if debugPort.Valid && debugPort.Int64 > 0 {
		port = int(debugPort.Int64)
	}

	// Connecter au browser si nécessaire
	if m.browser == nil {
		browser, connErr := Connect(port)
		if connErr != nil {
			return fmt.Errorf("failed to connect to browser on port %d: %w", port, connErr)
		}
		m.browser = browser
	}

	// Établir une session vers une page (target)
	sessionID, err := m.browser.EnsurePageSession()
	if err != nil {
		return fmt.Errorf("failed to establish page session: %w", err)
	}
	m.sessionID = sessionID

	// Mettre à jour l'état de session en base
	_, err = m.db.Exec(`
		INSERT OR REPLACE INTO cdp_session_state (id, ws_url, connected, debug_port, session_id, target_id, updated_at)
		VALUES (1, ?, 1, ?, ?, ?, strftime('%s', 'now'))
	`, m.browser.wsURL, port, m.sessionID, m.browser.GetCurrentTargetID())

	if err != nil {
		// Log mais ne pas échouer - la session est établie
		fmt.Printf("warning: failed to save session state: %v\n", err)
	}

	return nil
}

// isBrowserLevelMethod vérifie si une méthode CDP est de niveau browser (pas besoin de session)
func isBrowserLevelMethod(method string) bool {
	browserMethods := []string{
		"Target.", "Browser.", "SystemInfo.", "Tracing.",
	}
	for _, prefix := range browserMethods {
		if len(method) >= len(prefix) && method[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}

// Call exécute une commande CDP et retourne le résultat JSON
// Utilise automatiquement la session pour les commandes de page (Page, DOM, Runtime, etc.)
func (m *CDPManager) Call(method string, params map[string]interface{}) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.browser == nil {
		return "", fmt.Errorf("browser not connected - call EnsureConnected first")
	}

	var result json.RawMessage
	var err error

	// Les commandes browser-level n'ont pas besoin de session
	if isBrowserLevelMethod(method) {
		result, err = m.browser.Call(method, params)
	} else {
		// Les commandes page-level utilisent la session
		if m.sessionID == "" {
			return "", fmt.Errorf("no page session - call EnsureConnected first")
		}
		result, err = m.browser.CallWithSession(m.sessionID, method, params)
	}

	if err != nil {
		return "", err
	}

	return string(result), nil
}

// Disconnect ferme la connexion browser
func (m *CDPManager) Disconnect() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.browser == nil {
		return nil
	}

	err := m.browser.Close()
	m.browser = nil
	m.sessionID = "" // Réinitialiser la session

	// Mettre à jour l'état
	m.db.Exec(`UPDATE cdp_session_state SET connected = 0, session_id = NULL, target_id = NULL WHERE id = 1`)

	return err
}

// GetSessionID retourne l'ID de session actuel
func (m *CDPManager) GetSessionID() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessionID
}

// GetTargets retourne la liste des targets disponibles
func (m *CDPManager) GetTargets() ([]TargetInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.browser == nil {
		return nil, fmt.Errorf("browser not connected")
	}

	return m.browser.GetTargets()
}

// CreatePage crée une nouvelle page et s'y attache
func (m *CDPManager) CreatePage(url string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.browser == nil {
		return "", fmt.Errorf("browser not connected")
	}

	// Créer le target
	targetID, err := m.browser.CreateTarget(url)
	if err != nil {
		return "", err
	}

	// S'attacher au nouveau target
	sessionID, err := m.browser.AttachToTarget(targetID)
	if err != nil {
		return "", err
	}

	m.sessionID = sessionID

	// Mettre à jour l'état en base
	m.db.Exec(`UPDATE cdp_session_state SET session_id = ?, target_id = ? WHERE id = 1`,
		sessionID, targetID)

	return targetID, nil
}

// SwitchToTarget change de target (page) actif
func (m *CDPManager) SwitchToTarget(targetID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.browser == nil {
		return fmt.Errorf("browser not connected")
	}

	// S'attacher au target
	sessionID, err := m.browser.AttachToTarget(targetID)
	if err != nil {
		return err
	}

	m.sessionID = sessionID

	// Mettre à jour l'état en base
	m.db.Exec(`UPDATE cdp_session_state SET session_id = ?, target_id = ? WHERE id = 1`,
		sessionID, targetID)

	return nil
}

// ClosePage ferme une page par son targetId
func (m *CDPManager) ClosePage(targetID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.browser == nil {
		return fmt.Errorf("browser not connected")
	}

	// Si c'est la page active, réinitialiser la session
	if m.browser.GetCurrentTargetID() == targetID {
		m.sessionID = ""
	}

	return m.browser.CloseTarget(targetID)
}

// RegisterSQLFunctions est obsolète - utiliser sql_functions.RegisterCDPFunctions à la place
// Cette méthode est conservée pour compatibilité mais ne fait plus rien
func (m *CDPManager) RegisterSQLFunctions() error {
	return nil
}

// ProcessPendingCommands traite les commandes CDP en attente (à appeler en boucle)
func (m *CDPManager) ProcessPendingCommands() error {
	rows, err := m.db.Query(`
		SELECT id, method, params
		FROM cdp_commands
		WHERE status = 'pending'
		ORDER BY id ASC
		LIMIT 10
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var id int
		var method string
		var paramsJSON string

		if err := rows.Scan(&id, &method, &paramsJSON); err != nil {
			continue
		}

		// Parser les paramètres
		var params map[string]interface{}
		if paramsJSON != "" && paramsJSON != "{}" {
			if err := json.Unmarshal([]byte(paramsJSON), &params); err != nil {
				// Marquer comme erreur
				m.db.Exec(`
					UPDATE cdp_commands
					SET status = 'error',
						error = ?,
						processed_at = strftime('%s', 'now')
					WHERE id = ?
				`, fmt.Sprintf("invalid params JSON: %v", err), id)
				continue
			}
		}

		// S'assurer que le browser est connecté
		if err := m.EnsureConnected(); err != nil {
			m.db.Exec(`
				UPDATE cdp_commands
				SET status = 'error',
					error = ?,
					processed_at = strftime('%s', 'now')
				WHERE id = ?
			`, fmt.Sprintf("connection failed: %v", err), id)
			continue
		}

		// Exécuter la commande CDP
		result, err := m.Call(method, params)
		if err != nil {
			m.db.Exec(`
				UPDATE cdp_commands
				SET status = 'error',
					error = ?,
					processed_at = strftime('%s', 'now')
				WHERE id = ?
			`, err.Error(), id)
			continue
		}

		// Stocker le résultat
		m.db.Exec(`
			UPDATE cdp_commands
			SET status = 'success',
				result = ?,
				processed_at = strftime('%s', 'now')
			WHERE id = ?
		`, result, id)

		// Si c'est un événement console/network, l'extraire et le stocker
		m.handleCDPEvent(method, result)
	}

	return nil
}

// handleCDPEvent extrait les événements console/network et les stocke
func (m *CDPManager) handleCDPEvent(method string, resultJSON string) {
	// Pour console.log : Runtime.consoleAPICalled
	if method == "Runtime.enable" {
		// On ne fait rien ici, les events viendront automatiquement
		return
	}

	// Pour network : Network.requestWillBeSent, Network.responseReceived
	if method == "Network.enable" {
		return
	}

	// TODO: Implémenter l'écoute des événements CDP en temps réel
	// Pour l'instant, on lit via Runtime.evaluate
}

// CreateCDPCallFunction crée une fonction helper SQL qui insère dans cdp_commands
func (m *CDPManager) CreateCDPCallFunction() error {
	// Créer une vue SQL qui simule cdp_call()
	_, err := m.db.Exec(`
		-- Vue helper pour insérer des commandes CDP
		-- Usage: INSERT INTO cdp_call_helper (method, params) VALUES ('Page.navigate', '{"url":"..."}')
		CREATE VIEW IF NOT EXISTS cdp_call_helper AS
		SELECT NULL AS should_not_select_from_this_view;
	`)
	if err != nil {
		return err
	}

	// En SQL, on utilisera :
	// INSERT INTO cdp_commands (method, params) VALUES ('Page.enable', '{}');
	// SELECT result FROM cdp_commands WHERE id = last_insert_rowid() AND status = 'success';

	// Ou plus simple, créer une fonction SQL qui attend la réponse:
	// SELECT cdp_sync_call('Page.navigate', '{"url":"https://..."}')

	return nil
}

// SyncCall exécute une commande CDP et attend le résultat (bloquant)
func (m *CDPManager) SyncCall(method string, paramsJSON string) (string, error) {
	// S'assurer de la connexion
	if err := m.EnsureConnected(); err != nil {
		return "", err
	}

	// Parser les params
	var params map[string]interface{}
	if paramsJSON != "" && paramsJSON != "{}" {
		if err := json.Unmarshal([]byte(paramsJSON), &params); err != nil {
			return "", fmt.Errorf("invalid params JSON: %w", err)
		}
	}

	// Appeler directement
	return m.Call(method, params)
}

// RegisterCDPFunctions implémente l'interface pour database.OpenWithCDPFunctions
func (m *CDPManager) RegisterCDPFunctions(conn *sqlite3.Conn) error {
	return RegisterCDPFunctions(conn, m)
}
