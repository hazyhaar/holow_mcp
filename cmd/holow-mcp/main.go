// Package main est le point d'entrée du serveur MCP HOLOW
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/horos/holow-mcp/internal/database"
	"github.com/horos/holow-mcp/internal/server"
)

func main() {
	// Flags
	initDB := flag.Bool("init", false, "Initialize databases with schemas")
	basePath := flag.String("path", "/workspace/projets/holow-mcp", "Base path for databases")
	schemasPath := flag.String("schemas", "", "Path to schema SQL files")
	flag.Parse()

	// Déterminer le chemin des schémas
	if *schemasPath == "" {
		// Par défaut, chercher dans le répertoire schemas relatif au binaire
		execPath, err := os.Executable()
		if err == nil {
			*schemasPath = filepath.Join(filepath.Dir(execPath), "..", "..", "schemas")
		}
		if _, err := os.Stat(*schemasPath); os.IsNotExist(err) {
			// Fallback: chercher relatif au working directory
			*schemasPath = filepath.Join(*basePath, "schemas")
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

	// Mode serveur: créer le serveur (qui créera les bases avec CDP intégré)
	srv, err := server.NewServer(*basePath)
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
