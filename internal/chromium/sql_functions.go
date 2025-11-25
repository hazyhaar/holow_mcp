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
	// Note: Pas de flag DETERMINISTIC car le résultat dépend de l'état du browser
	err := conn.CreateFunction("cdp_call", 2,
		0, // Non-déterministe: dépend de l'état externe du browser
		CDPCallFunction(manager))
	if err != nil {
		return fmt.Errorf("failed to register cdp_call: %w", err)
	}

	// Enregistrer cdp_connected() -> INTEGER (1 si connecté, 0 sinon)
	err = conn.CreateFunction("cdp_connected", 0,
		0, // Non-déterministe
		func(ctx sqlite3.Context, args ...sqlite3.Value) {
			manager.mu.RLock()
			connected := manager.browser != nil && manager.sessionID != ""
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

	// Enregistrer cdp_session_id() -> TEXT (retourne le sessionId actuel)
	err = conn.CreateFunction("cdp_session_id", 0,
		0, // Non-déterministe
		func(ctx sqlite3.Context, args ...sqlite3.Value) {
			sessionID := manager.GetSessionID()
			if sessionID == "" {
				ctx.ResultNull()
			} else {
				ctx.ResultText(sessionID)
			}
		})
	if err != nil {
		return fmt.Errorf("failed to register cdp_session_id: %w", err)
	}

	// Enregistrer cdp_list_pages() -> TEXT JSON array des pages
	err = conn.CreateFunction("cdp_list_pages", 0,
		0, // Non-déterministe
		func(ctx sqlite3.Context, args ...sqlite3.Value) {
			if err := manager.EnsureConnected(); err != nil {
				ctx.ResultError(fmt.Errorf("not connected: %w", err))
				return
			}

			targets, err := manager.GetTargets()
			if err != nil {
				ctx.ResultError(fmt.Errorf("failed to get targets: %w", err))
				return
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
				ctx.ResultError(fmt.Errorf("failed to marshal pages: %w", err))
				return
			}

			ctx.ResultText(string(jsonData))
		})
	if err != nil {
		return fmt.Errorf("failed to register cdp_list_pages: %w", err)
	}

	// Enregistrer cdp_create_page(url TEXT) -> TEXT (retourne le targetId)
	err = conn.CreateFunction("cdp_create_page", 1,
		0, // Non-déterministe
		func(ctx sqlite3.Context, args ...sqlite3.Value) {
			url := ""
			if len(args) > 0 {
				url = args[0].Text()
			}

			if err := manager.EnsureConnected(); err != nil {
				ctx.ResultError(fmt.Errorf("not connected: %w", err))
				return
			}

			targetID, err := manager.CreatePage(url)
			if err != nil {
				ctx.ResultError(fmt.Errorf("failed to create page: %w", err))
				return
			}

			ctx.ResultText(targetID)
		})
	if err != nil {
		return fmt.Errorf("failed to register cdp_create_page: %w", err)
	}

	// Enregistrer cdp_switch_page(target_id TEXT) -> INTEGER (1 si succès)
	err = conn.CreateFunction("cdp_switch_page", 1,
		0, // Non-déterministe
		func(ctx sqlite3.Context, args ...sqlite3.Value) {
			if len(args) < 1 {
				ctx.ResultError(fmt.Errorf("cdp_switch_page requires target_id argument"))
				return
			}

			targetID := args[0].Text()
			if targetID == "" {
				ctx.ResultError(fmt.Errorf("target_id cannot be empty"))
				return
			}

			if err := manager.EnsureConnected(); err != nil {
				ctx.ResultError(fmt.Errorf("not connected: %w", err))
				return
			}

			if err := manager.SwitchToTarget(targetID); err != nil {
				ctx.ResultError(fmt.Errorf("failed to switch page: %w", err))
				return
			}

			ctx.ResultInt(1)
		})
	if err != nil {
		return fmt.Errorf("failed to register cdp_switch_page: %w", err)
	}

	// Enregistrer cdp_close_page(target_id TEXT) -> INTEGER (1 si succès)
	err = conn.CreateFunction("cdp_close_page", 1,
		0, // Non-déterministe
		func(ctx sqlite3.Context, args ...sqlite3.Value) {
			if len(args) < 1 {
				ctx.ResultError(fmt.Errorf("cdp_close_page requires target_id argument"))
				return
			}

			targetID := args[0].Text()
			if targetID == "" {
				ctx.ResultError(fmt.Errorf("target_id cannot be empty"))
				return
			}

			if err := manager.EnsureConnected(); err != nil {
				ctx.ResultError(fmt.Errorf("not connected: %w", err))
				return
			}

			if err := manager.ClosePage(targetID); err != nil {
				ctx.ResultError(fmt.Errorf("failed to close page: %w", err))
				return
			}

			ctx.ResultInt(1)
		})
	if err != nil {
		return fmt.Errorf("failed to register cdp_close_page: %w", err)
	}

	return nil
}
