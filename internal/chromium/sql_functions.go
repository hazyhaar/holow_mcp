// Package chromium - Enregistrement de la fonction SQL cdp_call()
package chromium

import (
	"encoding/json"
	"fmt"

	"github.com/ncruces/go-sqlite3"
)

// CDPCallFunction est la fonction SQL cdp_call(method, params_json) -> result_json
// Elle communique avec le CDPManager global pour exécuter des commandes CDP
func CDPCallFunction(manager *CDPManager) func(ctx sqlite3.Context, args ...sqlite3.Value) {
	return func(ctx sqlite3.Context, args ...sqlite3.Value) {
		// Valider les arguments
		if len(args) != 2 {
			ctx.ResultError(fmt.Errorf("cdp_call() requires 2 arguments: method and params_json"))
			return
		}

		method := args[0].Text()
		paramsJSON := args[1].Text()

		// Parser les paramètres JSON
		var params map[string]interface{}
		if paramsJSON != "" && paramsJSON != "{}" && paramsJSON != "null" {
			if err := json.Unmarshal([]byte(paramsJSON), &params); err != nil {
				ctx.ResultError(fmt.Errorf("invalid JSON params: %w", err))
				return
			}
		}

		// S'assurer que le browser est connecté
		if err := manager.EnsureConnected(); err != nil {
			ctx.ResultError(fmt.Errorf("browser not connected: %w", err))
			return
		}

		// Exécuter la commande CDP
		result, err := manager.Call(method, params)
		if err != nil {
			ctx.ResultError(fmt.Errorf("CDP call failed: %w", err))
			return
		}

		// Retourner le résultat JSON
		ctx.ResultText(result)
	}
}

// RegisterCDPFunctions enregistre toutes les fonctions SQL CDP sur une connexion
func RegisterCDPFunctions(conn *sqlite3.Conn, manager *CDPManager) error {
	// Enregistrer cdp_call(method TEXT, params TEXT) -> TEXT
	err := conn.CreateFunction("cdp_call", 2,
		sqlite3.DETERMINISTIC, // Non-déterministe car dépend de l'état du browser
		CDPCallFunction(manager))
	if err != nil {
		return fmt.Errorf("failed to register cdp_call: %w", err)
	}

	// Enregistrer cdp_connected() -> INTEGER (1 si connecté, 0 sinon)
	err = conn.CreateFunction("cdp_connected", 0,
		sqlite3.DETERMINISTIC,
		func(ctx sqlite3.Context, args ...sqlite3.Value) {
			manager.mu.RLock()
			connected := manager.browser != nil
			manager.mu.RUnlock()

			if connected {
				ctx.ResultInt(1)
			} else {
				ctx.ResultInt(0)
			}
		})
	if err != nil {
		return fmt.Errorf("failed to register cdp_connected: %w", err)
	}

	return nil
}
