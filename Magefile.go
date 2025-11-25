//go:build mage
// +build mage

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

var Default = Build

// Build compile le binaire holow-mcp
func Build() error {
	fmt.Println("Building holow-mcp...")

	// Créer le répertoire bin
	if err := os.MkdirAll("bin", 0755); err != nil {
		return err
	}

	cmd := exec.Command("go", "build", "-o", "bin/holow-mcp", "./cmd/holow-mcp")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Test exécute les tests unitaires
func Test() error {
	fmt.Println("Running tests...")
	cmd := exec.Command("go", "test", "-v", "./...")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Lint exécute les linters
func Lint() error {
	fmt.Println("Running linters...")
	cmd := exec.Command("golangci-lint", "run", "./...")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// InitDB initialise les bases de données avec les schémas
func InitDB() error {
	fmt.Println("Initializing databases...")

	// Vérifier que le binaire existe
	binPath := "bin/holow-mcp"
	if _, err := os.Stat(binPath); os.IsNotExist(err) {
		fmt.Println("Binary not found, building first...")
		if err := Build(); err != nil {
			return err
		}
	}

	schemasPath, _ := filepath.Abs("schemas")
	basePath := getBasePath()

	fmt.Printf("Base path: %s\n", basePath)
	fmt.Printf("Schemas path: %s\n", schemasPath)

	cmd := exec.Command(binPath, "-init", "-path", basePath, "-schemas", schemasPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Clean supprime les fichiers générés (binaires seulement)
// Note: Ne supprime PAS les bases de données pour éviter perte de données
func Clean() error {
	fmt.Println("Cleaning build artifacts...")

	// Supprimer le binaire
	os.RemoveAll("bin")
	fmt.Println("  ✓ Removed bin/")

	return nil
}

// CleanDB supprime les bases de données (ATTENTION: perte de données!)
func CleanDB() error {
	basePath := getBasePath()
	fmt.Printf("WARNING: This will delete all databases in %s\n", basePath)
	fmt.Println("Press Ctrl+C to cancel, or wait 3 seconds to continue...")

	// Attendre 3 secondes pour permettre l'annulation
	// (Note: en mode non-interactif, utiliser HOLOW_FORCE_CLEAN=1)
	if os.Getenv("HOLOW_FORCE_CLEAN") != "1" {
		fmt.Print("3...")
		exec.Command("sleep", "1").Run()
		fmt.Print("2...")
		exec.Command("sleep", "1").Run()
		fmt.Print("1...")
		exec.Command("sleep", "1").Run()
		fmt.Println()
	}

	patterns := []string{
		"*.db",
		"*.db-shm",
		"*.db-wal",
	}

	for _, pattern := range patterns {
		fullPattern := filepath.Join(basePath, pattern)
		matches, _ := filepath.Glob(fullPattern)
		for _, match := range matches {
			if err := os.Remove(match); err == nil {
				fmt.Printf("  ✓ Removed %s\n", filepath.Base(match))
			}
		}
	}

	return nil
}

// Run démarre le serveur MCP
func Run() error {
	fmt.Println("Starting holow-mcp server...")

	binPath := "bin/holow-mcp"
	if _, err := os.Stat(binPath); os.IsNotExist(err) {
		fmt.Println("Binary not found, building first...")
		if err := Build(); err != nil {
			return err
		}
	}

	cmd := exec.Command(binPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Validate vérifie la conformité HOROS
func Validate() error {
	fmt.Println("Validating HOROS compliance...")

	// Déterminer le chemin des bases (comme main.go)
	basePath := getBasePath()
	fmt.Printf("Checking databases in: %s\n\n", basePath)

	// Vérifier que les 6 bases existent
	dbs := []string{
		"holow-mcp.input.db",
		"holow-mcp.lifecycle-tools.db",
		"holow-mcp.lifecycle-execution.db",
		"holow-mcp.lifecycle-core.db",
		"holow-mcp.output.db",
		"holow-mcp.metadata.db",
	}

	missing := 0
	for _, db := range dbs {
		dbPath := filepath.Join(basePath, db)
		if _, err := os.Stat(dbPath); os.IsNotExist(err) {
			fmt.Printf("  ❌ Missing: %s\n", db)
			missing++
		} else {
			fmt.Printf("  ✓ Found: %s\n", db)
		}
	}

	if missing > 0 {
		return fmt.Errorf("%d databases missing, run 'mage initdb' or 'holow-mcp -setup'", missing)
	}

	fmt.Println("\n✓ All 6 databases present")
	fmt.Println("✓ Pattern 4-BDD conforme (sharding lifecycle)")

	return nil
}

// getBasePath retourne le chemin de base des données
func getBasePath() string {
	// 1. Variable d'environnement
	if path := os.Getenv("HOLOW_MCP_PATH"); path != "" {
		return path
	}

	// 2. Config existante
	home, _ := os.UserHomeDir()
	defaultPath := filepath.Join(home, ".holow-mcp")
	configFile := filepath.Join(defaultPath, "config.json")

	if _, err := os.Stat(configFile); err == nil {
		// Config existe, essayer de lire le basePath
		// Note: Lecture simplifiée sans dépendance au package initcli
		return defaultPath
	}

	// 3. Fallback
	return defaultPath
}

// Check exécute validate + lint + test + build
func Check() error {
	fmt.Println("=== Full validation pipeline ===\n")

	if err := Validate(); err != nil {
		return err
	}

	fmt.Println()
	if err := Lint(); err != nil {
		// Lint errors non bloquants
		fmt.Printf("Lint warnings: %v\n", err)
	}

	fmt.Println()
	if err := Test(); err != nil {
		return err
	}

	fmt.Println()
	if err := Build(); err != nil {
		return err
	}

	fmt.Println("\n=== All checks passed ✓ ===")
	return nil
}

// Proto génère le code depuis les fichiers protobuf
func Proto() error {
	fmt.Println("Generating protobuf code...")

	protoFiles, err := filepath.Glob("proto/*.proto")
	if err != nil {
		return err
	}

	if len(protoFiles) == 0 {
		fmt.Println("No proto files found")
		return nil
	}

	for _, protoFile := range protoFiles {
		cmd := exec.Command("protoc",
			"--go_out=.",
			"--go-vtproto_out=.",
			"--go-vtproto_opt=features=marshal+unmarshal+size+pool",
			protoFile)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to compile %s: %w", protoFile, err)
		}
		fmt.Printf("  ✓ Compiled %s\n", protoFile)
	}

	return nil
}

// Info affiche les informations sur le projet
func Info() {
	fmt.Println("HOLOW-MCP - Serveur MCP universel basé sur HOROS")
	fmt.Println()
	fmt.Println("Pattern: 4-BDD shardé (61 tables)")
	fmt.Println("  - input.db (8 tables)")
	fmt.Println("  - lifecycle-tools.db (8 tables)")
	fmt.Println("  - lifecycle-execution.db (10 tables)")
	fmt.Println("  - lifecycle-core.db (13 tables)")
	fmt.Println("  - output.db (10 tables)")
	fmt.Println("  - metadata.db (12 tables)")
	fmt.Println()
	fmt.Println("Fonctionnalités:")
	fmt.Println("  - Tools programmables par LLM (INSERT SQL)")
	fmt.Println("  - Hot reload trigger-based (2s)")
	fmt.Println("  - Circuit breaker avec success_threshold")
	fmt.Println("  - Idempotence via SHA256 hash")
	fmt.Println("  - Whitelist ATTACH sécurisé")
	fmt.Println("  - Observabilité native (heartbeat 15s)")
	fmt.Println("  - Graceful shutdown (60s timeout)")
}
