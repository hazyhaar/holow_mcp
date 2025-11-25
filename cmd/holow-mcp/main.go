// Package main est le point d'entrée du serveur MCP HOLOW
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/horos/holow-mcp/internal/database"
	"github.com/horos/holow-mcp/internal/initcli"
	"github.com/horos/holow-mcp/internal/server"
	"github.com/horos/holow-mcp/internal/sqlshell"
)

func main() {
	// Flags
	initDB := flag.Bool("init", false, "Initialize databases with schemas")
	initInteractive := flag.Bool("setup", false, "Run interactive setup wizard")
	basePath := flag.String("path", "", "Base path for databases")
	testMode := flag.Bool("test", false, "Use isolated test environment (creates temp path)")
	schemasPath := flag.String("schemas", "", "Path to schema SQL files")
	showConfig := flag.Bool("config", false, "Show current configuration")
	listCreds := flag.Bool("list-creds", false, "List configured credentials")
	mcpStatus := flag.Bool("mcp-status", false, "Show MCP configuration status for AI clients")
	sqlQuery := flag.String("sql", "", "Execute SQL query or start interactive shell (use -sql \"query\" or -sql alone)")
	sqlDB := flag.String("db", "lifecycle-tools", "Database to query with -sql")
	flag.Parse()

	// Mode test: environnement isolé
	if *testMode {
		testPath := filepath.Join(os.TempDir(), fmt.Sprintf("holow-test-%d", os.Getpid()))
		fmt.Fprintf(os.Stderr, "[TEST MODE] Using isolated path: %s\n", testPath)
		*basePath = testPath
	}

	// Déterminer le chemin de base
	if *basePath == "" {
		// Essayer de charger depuis config existante
		home, _ := os.UserHomeDir()
		defaultPath := filepath.Join(home, ".holow-mcp")

		if initcli.ConfigExists(defaultPath) {
			cfg, _ := initcli.LoadAppConfig(defaultPath)
			if cfg != nil {
				*basePath = cfg.BasePath
			}
		}

		// Fallback
		if *basePath == "" {
			*basePath = defaultPath
		}
	}

	// Mode setup interactif
	if *initInteractive {
		cfg, err := initcli.Run()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Erreur setup: %v\n", err)
			os.Exit(1)
		}

		// Sauvegarder la config
		appCfg := &initcli.AppConfig{
			BasePath:       cfg.BasePath,
			CredentialsDB:  cfg.CredentialsDB,
			BackupEnabled:  true,
			BackupMaxCount: 5,
			DebugPort:      9222,
		}
		if err := initcli.SaveAppConfig(appCfg); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: impossible de sauvegarder config.json: %v\n", err)
		}

		// Mettre à jour basePath pour l'init des schémas
		*basePath = cfg.BasePath
		*initDB = true // Continuer vers l'init des schémas
	}

	// Mode affichage config
	if *showConfig {
		cfg, err := initcli.LoadAppConfig(*basePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Erreur chargement config: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Configuration HOLOW-MCP:\n")
		fmt.Printf("  Chemin: %s\n", cfg.BasePath)
		fmt.Printf("  Base credentials: %s\n", cfg.CredentialsDB)
		fmt.Printf("  Backup activé: %v\n", cfg.BackupEnabled)
		fmt.Printf("  Backups max: %d\n", cfg.BackupMaxCount)
		fmt.Printf("  Port CDP: %d\n", cfg.DebugPort)

		if cfg.CredentialsAvailable() {
			fmt.Printf("  Fingerprint clé: %s\n", initcli.KeyFingerprint(cfg.BasePath, cfg.CredentialsDB))
		}
		return
	}

	// Mode liste credentials
	if *listCreds {
		cfg, err := initcli.LoadAppConfig(*basePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Erreur chargement config: %v\n", err)
			os.Exit(1)
		}

		providers, err := cfg.GetProviders()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Erreur lecture credentials: %v\n", err)
			os.Exit(1)
		}

		fmt.Println("Credentials configurés:")
		for _, p := range providers {
			hint := initcli.CredentialHint(cfg.BasePath, cfg.CredentialsDB, p)
			fmt.Printf("  - %s (%s)\n", p, hint)
		}
		return
	}

	// Mode statut MCP
	if *mcpStatus {
		initcli.PrintMCPConfigStatus()
		return
	}

	// Mode SQL shell
	if *sqlQuery != "" || isFlagPassed("sql") {
		shell := sqlshell.New(*basePath)
		if *sqlQuery != "" {
			// Exécuter une requête unique
			if err := shell.Run(*sqlDB, *sqlQuery); err != nil {
				fmt.Fprintf(os.Stderr, "SQL Error: %v\n", err)
				os.Exit(1)
			}
		} else {
			// Mode interactif
			if err := shell.Interactive(); err != nil {
				fmt.Fprintf(os.Stderr, "Shell Error: %v\n", err)
				os.Exit(1)
			}
		}
		return
	}

	// Déterminer le chemin des schémas
	if *schemasPath == "" {
		execPath, err := os.Executable()
		if err == nil {
			*schemasPath = filepath.Join(filepath.Dir(execPath), "..", "..", "schemas")
		}
		if _, err := os.Stat(*schemasPath); os.IsNotExist(err) {
			*schemasPath = filepath.Join(*basePath, "schemas")
		}
		// Fallback: chercher dans le répertoire courant
		if _, err := os.Stat(*schemasPath); os.IsNotExist(err) {
			cwd, _ := os.Getwd()
			*schemasPath = filepath.Join(cwd, "schemas")
		}
	}

	// Mode init: créer les bases et initialiser les schémas
	if *initDB {
		dbManager, err := database.NewManager(*basePath, nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error opening databases: %v\n", err)
			os.Exit(1)
		}

		fmt.Fprintf(os.Stderr, "Initializing databases from %s...\n", *schemasPath)
		if err := dbManager.InitSchemas(*schemasPath); err != nil {
			fmt.Fprintf(os.Stderr, "Error initializing schemas: %v\n", err)
			dbManager.Close()
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "Databases initialized successfully")
		dbManager.Close()
		return
	}

	// Vérifier si l'installation existe
	if !initcli.ConfigExists(*basePath) {
		fmt.Fprintln(os.Stderr, "HOLOW-MCP n'est pas initialisé.")
		fmt.Fprintln(os.Stderr, "Lancez d'abord: holow-mcp -setup")
		os.Exit(1)
	}

	// Charger la configuration
	appCfg, err := initcli.LoadAppConfig(*basePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Erreur chargement config: %v\n", err)
		os.Exit(1)
	}

	// Mode serveur: créer le serveur (qui créera les bases avec CDP intégré)
	srv, err := server.NewServerWithConfig(*basePath, appCfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating server: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintln(os.Stderr, "HOLOW-MCP server starting...")

	ctx := context.Background()
	if err := srv.Start(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintln(os.Stderr, "HOLOW-MCP server stopped")
}

// isFlagPassed vérifie si un flag a été passé (même sans valeur)
func isFlagPassed(name string) bool {
	found := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}
