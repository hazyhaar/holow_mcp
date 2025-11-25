// Package database - Ouverture unifiée des bases SQLite avec modernc.org/sqlite
package database

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// HolowAppID est l'identifiant d'application SQLite pour HOLOW-MCP
// Permet de détecter si une base a été créée par holow-mcp
// Valeur: 0x484F4C57 = "HOLW" en ASCII
const HolowAppID = 0x484F4C57

// horosPragmas contient les pragmas optimisés pour HOROS
var horosPragmas = []string{
	"PRAGMA journal_mode = WAL",
	"PRAGMA synchronous = NORMAL",
	"PRAGMA foreign_keys = ON",
	"PRAGMA busy_timeout = 5000",
	"PRAGMA cache_size = -64000",
	"PRAGMA wal_autocheckpoint = 10000",
}

// ConnCallback est un callback appelé après l'ouverture d'une connexion
// Note: avec modernc, les custom functions sont enregistrées globalement
type ConnCallback func(db *sql.DB) error

// applyPragmas applique les pragmas HOROS à une base
func applyPragmas(db *sql.DB) error {
	for _, pragma := range horosPragmas {
		if _, err := db.Exec(pragma); err != nil {
			return fmt.Errorf("failed to set pragma %s: %w", pragma, err)
		}
	}
	return nil
}

// openDBWithConnector ouvre une base SQLite avec un callback optionnel
// C'est la méthode unifiée pour TOUTES les bases holow-mcp
func openDBWithConnector(path string, callback ConnCallback) (*sql.DB, error) {
	// Ouvrir la base avec modernc.org/sqlite
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Appliquer les pragmas HOROS
	if err := applyPragmas(db); err != nil {
		db.Close()
		return nil, err
	}

	// Appeler le callback custom si fourni
	if callback != nil {
		if err := callback(db); err != nil {
			db.Close()
			return nil, fmt.Errorf("custom callback failed: %w", err)
		}
	}

	// Tester la connexion
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return db, nil
}
