// Package database - Résilience KISS: checkpoint WAL + migrations au boot
package database

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SchemaVersion actuelle (incrémenter à chaque migration)
const SchemaVersion = 1

// RecoverAndMigrate exécute la récupération et migrations au démarrage
// Appelé une seule fois au boot, pas de goroutine
func (m *Manager) RecoverAndMigrate(migrationsPath string) error {
	dbs := map[string]*sql.DB{
		"input":               m.Input,
		"lifecycle-tools":     m.LifecycleTools,
		"lifecycle-execution": m.LifecycleExec,
		"lifecycle-core":      m.LifecycleCore,
		"output":              m.Output,
		"metadata":            m.Metadata,
	}

	for name, db := range dbs {
		if err := recoverDB(name, db, migrationsPath); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}

	return nil
}

func recoverDB(name string, db *sql.DB, migrationsPath string) error {
	// 1. Checkpoint WAL (évite corruption après crash)
	if _, err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		// Non fatal, on continue
		fmt.Fprintf(os.Stderr, "[warn] %s: checkpoint failed: %v\n", name, err)
	}

	// 2. Marquer comme HOLOW si pas déjà fait
	var appID int
	db.QueryRow("PRAGMA application_id").Scan(&appID)
	if appID == 0 {
		db.Exec(fmt.Sprintf("PRAGMA application_id = %d", HolowAppID))
	}

	// 3. Vérifier version du schéma
	var version int
	db.QueryRow("PRAGMA user_version").Scan(&version)

	// 4. Appliquer migrations si nécessaire
	if version < SchemaVersion {
		if err := applyMigrations(name, db, migrationsPath, version); err != nil {
			return fmt.Errorf("migration failed: %w", err)
		}
	}

	return nil
}

func applyMigrations(dbName string, db *sql.DB, migrationsPath string, currentVersion int) error {
	// Chercher les migrations pour cette base
	// Format: migrations/{dbname}/001_description.sql
	dbMigrationsPath := filepath.Join(migrationsPath, "migrations", dbName)

	if _, err := os.Stat(dbMigrationsPath); os.IsNotExist(err) {
		// Pas de migrations pour cette base, juste mettre à jour la version
		_, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d", SchemaVersion))
		return err
	}

	// Lister les fichiers de migration
	files, err := os.ReadDir(dbMigrationsPath)
	if err != nil {
		return err
	}

	// Trier par nom (001_, 002_, etc.)
	var migrations []string
	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".sql") {
			migrations = append(migrations, f.Name())
		}
	}
	sort.Strings(migrations)

	// Appliquer les migrations manquantes
	for _, mig := range migrations {
		// Extraire le numéro de version (001_xxx.sql -> 1)
		var migVersion int
		fmt.Sscanf(mig, "%d_", &migVersion)

		if migVersion > currentVersion {
			migPath := filepath.Join(dbMigrationsPath, mig)
			content, err := os.ReadFile(migPath)
			if err != nil {
				return fmt.Errorf("read %s: %w", mig, err)
			}

			fmt.Fprintf(os.Stderr, "[migrate] %s: applying %s\n", dbName, mig)

			if _, err := db.Exec(string(content)); err != nil {
				return fmt.Errorf("exec %s: %w", mig, err)
			}
		}
	}

	// Mettre à jour la version
	_, err = db.Exec(fmt.Sprintf("PRAGMA user_version = %d", SchemaVersion))
	return err
}

// QuickHealthCheck vérifie rapidement la santé des bases (sans réparer)
func (m *Manager) QuickHealthCheck() (healthy bool, issues []string) {
	healthy = true
	dbs := map[string]*sql.DB{
		"input":               m.Input,
		"lifecycle-tools":     m.LifecycleTools,
		"lifecycle-execution": m.LifecycleExec,
		"lifecycle-core":      m.LifecycleCore,
		"output":              m.Output,
		"metadata":            m.Metadata,
	}

	for name, db := range dbs {
		// Ping
		if err := db.Ping(); err != nil {
			healthy = false
			issues = append(issues, fmt.Sprintf("%s: ping failed", name))
			continue
		}

		// Quick integrity check (1 page seulement)
		var result string
		if err := db.QueryRow("PRAGMA quick_check(1)").Scan(&result); err != nil || result != "ok" {
			healthy = false
			issues = append(issues, fmt.Sprintf("%s: integrity issue", name))
		}
	}

	return healthy, issues
}
