// Package initcli - Gestion des configurations MCP pour différents providers
package initcli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// MCPProvider représente un provider AI supporté
type MCPProvider string

const (
	ProviderClaudeCode MCPProvider = "claude-code"
	ProviderGeminiCLI  MCPProvider = "gemini-cli"
	ProviderOpenCode   MCPProvider = "opencode"
)

// MCPServerConfig configuration d'un serveur MCP
type MCPServerConfig struct {
	Type    string            `json:"type,omitempty"`    // "stdio" ou "http"
	Command string            `json:"command,omitempty"` // Pour stdio
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	URL     string            `json:"url,omitempty"`     // Pour http
	Headers map[string]string `json:"headers,omitempty"` // Pour http
	Enabled *bool             `json:"enabled,omitempty"` // Pour OpenCode
}

// MCPConfigFile représente un fichier de configuration MCP
type MCPConfigFile struct {
	MCPServers map[string]MCPServerConfig `json:"mcpServers,omitempty"` // Claude/Gemini
	MCP        map[string]MCPServerConfig `json:"mcp,omitempty"`        // OpenCode
}

// ProviderConfigInfo informations sur la config d'un provider
type ProviderConfigInfo struct {
	Provider     MCPProvider
	ConfigPath   string
	Exists       bool
	IsConformant bool
	HasHolow     bool
	Issues       []string
	Config       *MCPConfigFile
}

// GetProviderConfigPaths retourne les chemins de config pour chaque provider
func GetProviderConfigPaths() map[MCPProvider][]string {
	home, _ := os.UserHomeDir()

	return map[MCPProvider][]string{
		ProviderClaudeCode: {
			filepath.Join(home, ".claude", "settings.local.json"),
			filepath.Join(home, ".mcp.json"),
			".mcp.json", // Projet local
		},
		ProviderGeminiCLI: {
			filepath.Join(home, ".gemini", "settings.json"),
			".gemini/settings.json", // Projet local
		},
		ProviderOpenCode: {
			filepath.Join(home, ".config", "opencode", "opencode.json"),
			"opencode.json", // Projet local
		},
	}
}

// DetectProviderConfig détecte la configuration existante pour un provider
func DetectProviderConfig(provider MCPProvider) *ProviderConfigInfo {
	paths := GetProviderConfigPaths()[provider]

	info := &ProviderConfigInfo{
		Provider: provider,
		Issues:   []string{},
	}

	for _, path := range paths {
		// Résoudre le chemin
		fullPath := path
		if !filepath.IsAbs(path) {
			cwd, _ := os.Getwd()
			fullPath = filepath.Join(cwd, path)
		}

		// Vérifier si le fichier existe
		if _, err := os.Stat(fullPath); err == nil {
			info.ConfigPath = fullPath
			info.Exists = true

			// Lire et parser la config
			data, err := os.ReadFile(fullPath)
			if err != nil {
				info.Issues = append(info.Issues, fmt.Sprintf("Erreur lecture: %v", err))
				continue
			}

			config := &MCPConfigFile{}
			if err := json.Unmarshal(data, config); err != nil {
				info.Issues = append(info.Issues, fmt.Sprintf("JSON invalide: %v", err))
				continue
			}

			info.Config = config
			info.IsConformant = validateConfig(provider, config, &info.Issues)
			info.HasHolow = hasHolowServer(provider, config)
			break
		}
	}

	return info
}

// validateConfig vérifie si la config est conforme
func validateConfig(provider MCPProvider, config *MCPConfigFile, issues *[]string) bool {
	conformant := true

	switch provider {
	case ProviderClaudeCode, ProviderGeminiCLI:
		if config.MCPServers == nil {
			*issues = append(*issues, "Clé 'mcpServers' manquante")
			conformant = false
		}
	case ProviderOpenCode:
		if config.MCP == nil {
			*issues = append(*issues, "Clé 'mcp' manquante")
			conformant = false
		}
	}

	return conformant
}

// hasHolowServer vérifie si holow-mcp est déjà configuré
func hasHolowServer(provider MCPProvider, config *MCPConfigFile) bool {
	var servers map[string]MCPServerConfig

	switch provider {
	case ProviderClaudeCode, ProviderGeminiCLI:
		servers = config.MCPServers
	case ProviderOpenCode:
		servers = config.MCP
	}

	if servers == nil {
		return false
	}

	for name := range servers {
		if strings.Contains(strings.ToLower(name), "holow") {
			return true
		}
	}

	return false
}

// GenerateHolowMCPConfig génère la configuration holow-mcp pour un provider
func GenerateHolowMCPConfig(provider MCPProvider, holowPath string) MCPServerConfig {
	switch provider {
	case ProviderClaudeCode:
		return MCPServerConfig{
			Type:    "stdio",
			Command: filepath.Join(holowPath, "holow-mcp"),
			Args:    []string{"-path", holowPath},
			Env:     map[string]string{},
		}
	case ProviderGeminiCLI:
		return MCPServerConfig{
			Command: filepath.Join(holowPath, "holow-mcp"),
			Args:    []string{"-path", holowPath},
			Env:     map[string]string{},
		}
	case ProviderOpenCode:
		enabled := true
		return MCPServerConfig{
			Type:    "local",
			Command: filepath.Join(holowPath, "holow-mcp"),
			Args:    []string{"-path", holowPath},
			Env:     map[string]string{},
			Enabled: &enabled,
		}
	}
	return MCPServerConfig{}
}

// AddHolowToConfig ajoute holow-mcp à une configuration existante
func AddHolowToConfig(provider MCPProvider, config *MCPConfigFile, holowPath string) {
	holowConfig := GenerateHolowMCPConfig(provider, holowPath)

	switch provider {
	case ProviderClaudeCode, ProviderGeminiCLI:
		if config.MCPServers == nil {
			config.MCPServers = make(map[string]MCPServerConfig)
		}
		config.MCPServers["holow-mcp"] = holowConfig
	case ProviderOpenCode:
		if config.MCP == nil {
			config.MCP = make(map[string]MCPServerConfig)
		}
		config.MCP["holow-mcp"] = holowConfig
	}
}

// CreateDefaultConfig crée une configuration par défaut pour un provider
func CreateDefaultConfig(provider MCPProvider, holowPath string) *MCPConfigFile {
	config := &MCPConfigFile{}
	AddHolowToConfig(provider, config, holowPath)
	return config
}

// SaveMCPConfig sauvegarde une configuration MCP
func SaveMCPConfig(path string, config *MCPConfigFile) error {
	// Créer le dossier parent si nécessaire
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("impossible de créer le dossier: %w", err)
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("erreur sérialisation: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("erreur écriture: %w", err)
	}

	return nil
}

// GetDefaultConfigPath retourne le chemin de config par défaut pour un provider
func GetDefaultConfigPath(provider MCPProvider) string {
	home, _ := os.UserHomeDir()

	switch provider {
	case ProviderClaudeCode:
		return filepath.Join(home, ".mcp.json")
	case ProviderGeminiCLI:
		return filepath.Join(home, ".gemini", "settings.json")
	case ProviderOpenCode:
		return filepath.Join(home, ".config", "opencode", "opencode.json")
	}
	return ""
}

// RunMCPConfigSetup exécute le setup interactif des configs MCP
func RunMCPConfigSetup(reader *bufio.Reader, holowPath string) error {
	fmt.Println("\n--- Configuration MCP pour AI Clients ---")

	providers := []struct {
		Provider    MCPProvider
		Name        string
		Description string
	}{
		{ProviderClaudeCode, "Claude Code", "Anthropic Claude Code CLI"},
		{ProviderGeminiCLI, "Gemini CLI", "Google Gemini CLI"},
		{ProviderOpenCode, "OpenCode", "OpenCode AI Terminal"},
	}

	for _, p := range providers {
		fmt.Printf("\n[%s]\n", p.Name)

		// Détecter config existante
		info := DetectProviderConfig(p.Provider)

		if info.Exists {
			fmt.Printf("  Config trouvée: %s\n", info.ConfigPath)

			if !info.IsConformant {
				fmt.Println("  [!] Configuration non conforme:")
				for _, issue := range info.Issues {
					fmt.Printf("      - %s\n", issue)
				}

				if promptYesNo(reader, "  Corriger la configuration?", true) {
					// Créer une config conforme avec l'existante
					config := CreateDefaultConfig(p.Provider, holowPath)

					// Merger avec l'existante si possible
					if info.Config != nil {
						mergeConfigs(p.Provider, config, info.Config)
					}

					if err := SaveMCPConfig(info.ConfigPath, config); err != nil {
						fmt.Printf("  [X] Erreur: %v\n", err)
					} else {
						fmt.Println("  [OK] Configuration corrigée")
					}
				}
			} else if info.HasHolow {
				fmt.Println("  [OK] holow-mcp déjà configuré")
			} else {
				if promptYesNo(reader, "  Ajouter holow-mcp à la configuration?", true) {
					AddHolowToConfig(p.Provider, info.Config, holowPath)
					if err := SaveMCPConfig(info.ConfigPath, info.Config); err != nil {
						fmt.Printf("  [X] Erreur: %v\n", err)
					} else {
						fmt.Println("  [OK] holow-mcp ajouté")
					}
				}
			}
		} else {
			fmt.Println("  Aucune configuration trouvée")

			if promptYesNo(reader, fmt.Sprintf("  Créer la configuration %s?", p.Name), false) {
				configPath := GetDefaultConfigPath(p.Provider)
				config := CreateDefaultConfig(p.Provider, holowPath)

				if err := SaveMCPConfig(configPath, config); err != nil {
					fmt.Printf("  [X] Erreur: %v\n", err)
				} else {
					fmt.Printf("  [OK] Configuration créée: %s\n", configPath)
				}
			}
		}
	}

	return nil
}

// mergeConfigs fusionne deux configurations en préservant les serveurs existants
func mergeConfigs(provider MCPProvider, dest, src *MCPConfigFile) {
	switch provider {
	case ProviderClaudeCode, ProviderGeminiCLI:
		if src.MCPServers != nil {
			for name, server := range src.MCPServers {
				if name != "holow-mcp" {
					dest.MCPServers[name] = server
				}
			}
		}
	case ProviderOpenCode:
		if src.MCP != nil {
			for name, server := range src.MCP {
				if name != "holow-mcp" {
					dest.MCP[name] = server
				}
			}
		}
	}
}

// PrintMCPConfigStatus affiche le statut des configurations MCP
func PrintMCPConfigStatus() {
	fmt.Println("\n--- Statut des configurations MCP ---")

	providers := []struct {
		Provider MCPProvider
		Name     string
	}{
		{ProviderClaudeCode, "Claude Code"},
		{ProviderGeminiCLI, "Gemini CLI"},
		{ProviderOpenCode, "OpenCode"},
	}

	for _, p := range providers {
		info := DetectProviderConfig(p.Provider)

		status := "❌ Non configuré"
		if info.Exists {
			if info.HasHolow {
				status = "✅ holow-mcp configuré"
			} else if info.IsConformant {
				status = "⚠️  Config existe, holow-mcp absent"
			} else {
				status = "⚠️  Config non conforme"
			}
		}

		fmt.Printf("  %s: %s\n", p.Name, status)
		if info.Exists {
			fmt.Printf("    Fichier: %s\n", info.ConfigPath)
		}
	}
}

// GenerateMCPConfigDocs génère la documentation des configs
func GenerateMCPConfigDocs(holowPath string) string {
	var sb strings.Builder

	sb.WriteString("# Configuration MCP pour HOLOW-MCP\n\n")

	// Claude Code
	sb.WriteString("## Claude Code\n\n")
	sb.WriteString("Fichier: `~/.mcp.json` ou `.mcp.json` (projet)\n\n")
	sb.WriteString("```json\n")
	config := CreateDefaultConfig(ProviderClaudeCode, holowPath)
	data, _ := json.MarshalIndent(config, "", "  ")
	sb.Write(data)
	sb.WriteString("\n```\n\n")

	// Gemini CLI
	sb.WriteString("## Gemini CLI\n\n")
	sb.WriteString("Fichier: `~/.gemini/settings.json`\n\n")
	sb.WriteString("```json\n")
	config = CreateDefaultConfig(ProviderGeminiCLI, holowPath)
	data, _ = json.MarshalIndent(config, "", "  ")
	sb.Write(data)
	sb.WriteString("\n```\n\n")

	// OpenCode
	sb.WriteString("## OpenCode\n\n")
	sb.WriteString("Fichier: `~/.config/opencode/opencode.json`\n\n")
	sb.WriteString("```json\n")
	config = CreateDefaultConfig(ProviderOpenCode, holowPath)
	data, _ = json.MarshalIndent(config, "", "  ")
	sb.Write(data)
	sb.WriteString("\n```\n")

	return sb.String()
}
