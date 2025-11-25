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
	browser *Browser
	mu      sync.RWMutex
	db      *sql.DB
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
func (m *CDPManager) EnsureConnected() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Vérifier si déjà connecté
	if m.browser != nil {
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

	// Si aucune ligne n'existe ou session non connectée, créer nouvelle connexion
	if err == sql.ErrNoRows || (err == nil && (!connected.Valid || connected.Int64 == 0)) {
		port := int(9222) // Port par défaut
		if debugPort.Valid && debugPort.Int64 > 0 {
			port = int(debugPort.Int64)
		}

		browser, err := Connect(port)
		if err != nil {
			return fmt.Errorf("failed to connect to browser on port %d: %w", port, err)
		}

		m.browser = browser

		// Créer ou mettre à jour l'état de session
		_, err = m.db.Exec(`
			INSERT OR REPLACE INTO cdp_session_state (id, ws_url, connected, debug_port, updated_at)
			VALUES (1, ?, 1, ?, strftime('%s', 'now'))
		`, browser.wsURL, port)

		if err != nil {
			return fmt.Errorf("failed to save session state: %w", err)
		}

		return nil
	}

	// Erreur réelle
	if err != nil {
		return fmt.Errorf("failed to get session state: %w", err)
	}

	// Session existe et connectée, se reconnecter
	// Note: Pour l'instant, on se reconnecte au port, pas via wsURL directement
	port := int(9222)
	if debugPort.Valid && debugPort.Int64 > 0 {
		port = int(debugPort.Int64)
	}

	browser, err := Connect(port)
	if err != nil {
		// Marquer comme déconnecté
		m.db.Exec(`UPDATE cdp_session_state SET connected = 0 WHERE id = 1`)
		return fmt.Errorf("failed to reconnect to browser: %w", err)
	}

	m.browser = browser
	return nil
}

// Call exécute une commande CDP et retourne le résultat JSON
func (m *CDPManager) Call(method string, params map[string]interface{}) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.browser == nil {
		return "", fmt.Errorf("browser not connected - call EnsureConnected first")
	}

	result, err := m.browser.Call(method, params)
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

	// Mettre à jour l'état
	m.db.Exec(`UPDATE cdp_session_state SET connected = 0 WHERE id = 1`)

	return err
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
