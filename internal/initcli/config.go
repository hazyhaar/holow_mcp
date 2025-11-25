// Package initcli - Configuration persistante pour HOLOW-MCP
package initcli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// AppConfig configuration globale de l'application (fichier config.json)
type AppConfig struct {
	BasePath       string `json:"base_path"`
	CredentialsDB  string `json:"credentials_db"`  // Nom de la base credentials (sans extension)
	BackupEnabled  bool   `json:"backup_enabled"`
	BackupMaxCount int    `json:"backup_max_count"`
	DebugPort      int    `json:"debug_port"`      // Port CDP par défaut
}

const configFileName = "config.json"

// DefaultAppConfig retourne la configuration par défaut
func DefaultAppConfig(basePath string) *AppConfig {
	return &AppConfig{
		BasePath:       basePath,
		CredentialsDB:  "credentials",
		BackupEnabled:  true,
		BackupMaxCount: 5,
		DebugPort:      9222,
	}
}

// LoadAppConfig charge la configuration depuis le fichier config.json
func LoadAppConfig(basePath string) (*AppConfig, error) {
	configPath := filepath.Join(basePath, configFileName)

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Fichier n'existe pas, retourner la config par défaut
			return DefaultAppConfig(basePath), nil
		}
		return nil, fmt.Errorf("erreur lecture config: %w", err)
	}

	var config AppConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("erreur parsing config: %w", err)
	}

	// S'assurer que BasePath est cohérent
	config.BasePath = basePath

	return &config, nil
}

// SaveAppConfig sauvegarde la configuration dans config.json
func SaveAppConfig(config *AppConfig) error {
	configPath := filepath.Join(config.BasePath, configFileName)

	// Créer le dossier si nécessaire
	if err := os.MkdirAll(config.BasePath, 0700); err != nil {
		return fmt.Errorf("impossible de créer le dossier: %w", err)
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("erreur sérialisation config: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0600); err != nil {
		return fmt.Errorf("erreur écriture config: %w", err)
	}

	return nil
}

// ConfigExists vérifie si un fichier config.json existe
func ConfigExists(basePath string) bool {
	configPath := filepath.Join(basePath, configFileName)
	_, err := os.Stat(configPath)
	return err == nil
}

// CredentialsDBPath retourne le chemin complet de la base credentials
func (c *AppConfig) CredentialsDBPath() string {
	return filepath.Join(c.BasePath, fmt.Sprintf("holow-mcp.%s.db", c.CredentialsDB))
}

// CredentialsAvailable vérifie si la base credentials existe
func (c *AppConfig) CredentialsAvailable() bool {
	_, err := os.Stat(c.CredentialsDBPath())
	return err == nil
}

// GetCredential récupère une clé API depuis la config
func (c *AppConfig) GetCredential(provider string) (string, error) {
	return GetCredential(c.BasePath, c.CredentialsDB, provider)
}

// GetProviders liste les providers configurés
func (c *AppConfig) GetProviders() ([]string, error) {
	return ListProviders(c.BasePath, c.CredentialsDB)
}

// CreateBackupNow crée un backup immédiat
func (c *AppConfig) CreateBackupNow() (string, error) {
	if !c.BackupEnabled {
		return "", fmt.Errorf("backup désactivé")
	}

	return CreateBackup(&BackupConfig{
		BasePath:   c.BasePath,
		MaxBackups: c.BackupMaxCount,
	})
}
