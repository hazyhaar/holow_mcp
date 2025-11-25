// Package database - Validation et santé des bases SQLite
package database

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DBHealth représente l'état de santé d'une base
type DBHealth struct {
	Name          string
	Path          string
	Exists        bool
	IsHolow       bool   // application_id == HolowAppID
	AppID         int    // application_id actuel
	IntegrityOK   bool   // PRAGMA integrity_check
	IntegrityMsg  string // Message d'erreur si !IntegrityOK
	HasWAL        bool   // Fichier .db-wal présent
	HasSHM        bool   // Fichier .db-shm présent
	TableCount    int    // Nombre de tables
	SchemaVersion int    // user_version
}

// ValidationResult résultat de la validation de toutes les bases
type ValidationResult struct {
	BasePath     string
	Databases    []DBHealth
	AllExist     bool
	AllHealthy   bool
	HasOrphanWAL bool
	Issues       []string
}

// ValidateDatabases vérifie l'état de toutes les bases HOLOW
func ValidateDatabases(basePath string) *ValidationResult {
	result := &ValidationResult{
		BasePath: basePath,
		AllExist: true,
		AllHealthy: true,
	}

	dbNames := []string{
		"input",
		"lifecycle-tools",
		"lifecycle-execution",
		"lifecycle-core",
		"output",
		"metadata",
	}

	for _, name := range dbNames {
		health := checkDatabase(basePath, name)
		result.Databases = append(result.Databases, health)

		if !health.Exists {
			result.AllExist = false
		}
		if health.Exists && (!health.IntegrityOK || !health.IsHolow) {
			result.AllHealthy = false
		}
		if health.HasWAL || health.HasSHM {
			result.HasOrphanWAL = true
		}
	}

	// Générer les issues
	for _, db := range result.Databases {
		if !db.Exists {
			result.Issues = append(result.Issues, fmt.Sprintf("%s: manquante", db.Name))
		} else if !db.IntegrityOK {
			result.Issues = append(result.Issues, fmt.Sprintf("%s: corrompue (%s)", db.Name, db.IntegrityMsg))
		} else if !db.IsHolow && db.AppID != 0 {
			result.Issues = append(result.Issues, fmt.Sprintf("%s: application_id invalide (0x%X)", db.Name, db.AppID))
		}
	}

	return result
}

func checkDatabase(basePath, name string) DBHealth {
	health := DBHealth{
		Name: name,
		Path: filepath.Join(basePath, fmt.Sprintf("holow-mcp.%s.db", name)),
	}

	// Vérifier existence
	if _, err := os.Stat(health.Path); os.IsNotExist(err) {
		return health
	}
	health.Exists = true

	// Vérifier WAL/SHM orphelins
	if _, err := os.Stat(health.Path + "-wal"); err == nil {
		health.HasWAL = true
	}
	if _, err := os.Stat(health.Path + "-shm"); err == nil {
		health.HasSHM = true
	}

	// Ouvrir la base avec modernc.org/sqlite
	db, err := sql.Open("sqlite", health.Path)
	if err != nil {
		health.IntegrityMsg = fmt.Sprintf("impossible d'ouvrir: %v", err)
		return health
	}
	defer db.Close()

	// Vérifier application_id
	var appID int
	if err := db.QueryRow("PRAGMA application_id").Scan(&appID); err == nil {
		health.AppID = appID
		health.IsHolow = appID == HolowAppID || appID == 0 // 0 = nouvelle base non marquée
	}

	// Vérifier user_version (schema version)
	db.QueryRow("PRAGMA user_version").Scan(&health.SchemaVersion)

	// Compter les tables
	db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table'").Scan(&health.TableCount)

	// Vérifier intégrité
	var integrityResult string
	if err := db.QueryRow("PRAGMA integrity_check(1)").Scan(&integrityResult); err != nil {
		health.IntegrityMsg = fmt.Sprintf("check failed: %v", err)
	} else if integrityResult == "ok" {
		health.IntegrityOK = true
	} else {
		health.IntegrityMsg = integrityResult
	}

	return health
}

// CleanOrphanWAL supprime les fichiers WAL/SHM orphelins
func CleanOrphanWAL(basePath string) ([]string, error) {
	var cleaned []string

	patterns := []string{"*.db-wal", "*.db-shm"}
	for _, pattern := range patterns {
		files, _ := filepath.Glob(filepath.Join(basePath, pattern))
		for _, f := range files {
			if err := os.Remove(f); err == nil {
				cleaned = append(cleaned, filepath.Base(f))
			}
		}
	}

	return cleaned, nil
}

// SetApplicationID marque une base comme HOLOW
func SetApplicationID(dbPath string) error {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	_, err = db.Exec(fmt.Sprintf("PRAGMA application_id = %d", HolowAppID))
	return err
}

// ResetDatabase supprime une base et ses fichiers associés
func ResetDatabase(dbPath string) error {
	// Supprimer la base et les fichiers WAL/SHM
	files := []string{dbPath, dbPath + "-wal", dbPath + "-shm"}
	for _, f := range files {
		os.Remove(f) // Ignorer les erreurs (fichier peut ne pas exister)
	}
	return nil
}

// ResetAllDatabases supprime toutes les bases HOLOW
func ResetAllDatabases(basePath string) error {
	dbNames := []string{
		"input",
		"lifecycle-tools",
		"lifecycle-execution",
		"lifecycle-core",
		"output",
		"metadata",
	}

	for _, name := range dbNames {
		path := filepath.Join(basePath, fmt.Sprintf("holow-mcp.%s.db", name))
		ResetDatabase(path)
	}

	return nil
}

// PrintValidationReport affiche un rapport de validation
func (r *ValidationResult) PrintReport() {
	fmt.Println("\n--- Validation des bases de données ---")
	fmt.Printf("Chemin: %s\n\n", r.BasePath)

	for _, db := range r.Databases {
		status := "❌"
		details := []string{}

		if !db.Exists {
			details = append(details, "manquante")
		} else {
			if db.IntegrityOK {
				status = "✓"
			} else {
				details = append(details, fmt.Sprintf("corrompue: %s", db.IntegrityMsg))
			}

			if db.IsHolow {
				details = append(details, "HOLOW")
			} else if db.AppID != 0 {
				details = append(details, fmt.Sprintf("app_id=0x%X", db.AppID))
			}

			details = append(details, fmt.Sprintf("%d tables", db.TableCount))

			if db.HasWAL {
				details = append(details, "WAL orphelin")
			}
		}

		fmt.Printf("  %s %s (%s)\n", status, db.Name, strings.Join(details, ", "))
	}

	if len(r.Issues) > 0 {
		fmt.Println("\nProblèmes détectés:")
		for _, issue := range r.Issues {
			fmt.Printf("  ! %s\n", issue)
		}
	}
}
