// Package database - Ouverture unifiée des bases SQLite avec callbacks optionnels
package database

import (
	"database/sql"
	"fmt"

	"github.com/ncruces/go-sqlite3"
	"github.com/ncruces/go-sqlite3/driver"
)

// ConnCallback est un callback appelé lors de l'ouverture d'une connexion
type ConnCallback func(conn *sqlite3.Conn) error

// horosPragmas contient les pragmas optimisés pour HOROS
var horosPragmas = []string{
	"PRAGMA journal_mode = WAL",
	"PRAGMA synchronous = NORMAL",
	"PRAGMA foreign_keys = ON",
	"PRAGMA busy_timeout = 5000",
	"PRAGMA cache_size = -64000",
	"PRAGMA wal_autocheckpoint = 10000",
}

// applyPragmas applique les pragmas HOROS à une connexion
func applyPragmas(conn *sqlite3.Conn) error {
	for _, pragma := range horosPragmas {
		if err := conn.Exec(pragma); err != nil {
			return fmt.Errorf("failed to set pragma: %w", err)
		}
	}
	return nil
}

// openDBWithConnector ouvre une base SQLite avec un callback optionnel
// C'est la méthode unifiée pour TOUTES les bases holow-mcp
func openDBWithConnector(path string, callback ConnCallback) (*sql.DB, error) {
	// Créer le callback qui applique les pragmas + callback custom
	initCallback := func(conn *sqlite3.Conn) error {
		// Appliquer les pragmas HOROS
		if err := applyPragmas(conn); err != nil {
			return err
		}

		// Appeler le callback custom si fourni
		if callback != nil {
			if err := callback(conn); err != nil {
				return fmt.Errorf("custom callback failed: %w", err)
			}
		}

		return nil
	}

	// Ouvrir la base avec le callback via driver.Open()
	db, err := driver.Open(path, initCallback)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Tester la connexion
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return db, nil
}
