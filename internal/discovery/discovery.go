// Package discovery détecte les ressources système au démarrage
package discovery

import (
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// ConfigKey représente une clé de configuration système
const (
	KeyChromiumPath    = "system.chromium.path"
	KeyChromiumFound   = "system.chromium.found"
	KeyTempDir         = "system.temp.dir"
	KeyUserDataDir     = "system.chromium.user_data_dir"
	KeyDefaultPort     = "system.chromium.default_port"
	KeySQLite3Path     = "system.sqlite3.path"
	KeyGitPath         = "system.git.path"
	KeyPlatform        = "system.platform"
	KeyArch            = "system.arch"
	KeyDiscoveredAt    = "system.discovered_at"
)

// Discovery gère la détection des ressources système
type Discovery struct {
	db *sql.DB
}

// New crée une nouvelle instance de Discovery
func New(db *sql.DB) *Discovery {
	return &Discovery{db: db}
}

// Run exécute la découverte complète et stocke dans config
func (d *Discovery) Run() error {
	discoveries := make(map[string]string)

	// Plateforme et architecture
	discoveries[KeyPlatform] = runtime.GOOS
	discoveries[KeyArch] = runtime.GOARCH
	discoveries[KeyDiscoveredAt] = time.Now().UTC().Format(time.RFC3339)

	// Chromium/Chrome
	chromePath := d.findChromium()
	if chromePath != "" {
		discoveries[KeyChromiumPath] = chromePath
		discoveries[KeyChromiumFound] = "true"
	} else {
		discoveries[KeyChromiumPath] = ""
		discoveries[KeyChromiumFound] = "false"
	}

	// Répertoire temporaire pour Chromium
	tempDir := d.setupTempDir()
	discoveries[KeyTempDir] = tempDir
	discoveries[KeyUserDataDir] = filepath.Join(tempDir, "chromium-profile")

	// Port par défaut
	discoveries[KeyDefaultPort] = "9222"

	// Outils système optionnels
	if sqlite3Path := d.findExecutable("sqlite3"); sqlite3Path != "" {
		discoveries[KeySQLite3Path] = sqlite3Path
	}
	if gitPath := d.findExecutable("git"); gitPath != "" {
		discoveries[KeyGitPath] = gitPath
	}

	// Stocker en base
	return d.storeConfig(discoveries)
}

// findChromium recherche le chemin vers Chromium/Chrome
func (d *Discovery) findChromium() string {
	var candidates []string

	switch runtime.GOOS {
	case "darwin":
		candidates = []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/Applications/Google Chrome Canary.app/Contents/MacOS/Google Chrome Canary",
			"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
		}
		// Aussi chercher dans le home
		if home, err := os.UserHomeDir(); err == nil {
			candidates = append(candidates,
				filepath.Join(home, "Applications/Google Chrome.app/Contents/MacOS/Google Chrome"),
				filepath.Join(home, "Applications/Chromium.app/Contents/MacOS/Chromium"),
			)
		}

	case "linux":
		candidates = []string{
			"/usr/bin/chromium",
			"/usr/bin/chromium-browser",
			"/usr/bin/google-chrome",
			"/usr/bin/google-chrome-stable",
			"/snap/bin/chromium",
			"/usr/bin/brave-browser",
		}
		// Chercher dans PATH aussi
		pathCandidates := []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable"}
		for _, name := range pathCandidates {
			if path, err := exec.LookPath(name); err == nil {
				candidates = append([]string{path}, candidates...)
			}
		}

	case "windows":
		programFiles := os.Getenv("PROGRAMFILES")
		programFilesX86 := os.Getenv("PROGRAMFILES(X86)")
		localAppData := os.Getenv("LOCALAPPDATA")

		candidates = []string{
			filepath.Join(programFiles, "Google/Chrome/Application/chrome.exe"),
			filepath.Join(programFilesX86, "Google/Chrome/Application/chrome.exe"),
			filepath.Join(localAppData, "Google/Chrome/Application/chrome.exe"),
			filepath.Join(programFiles, "Chromium/Application/chrome.exe"),
			filepath.Join(localAppData, "Chromium/Application/chrome.exe"),
		}
	}

	// Tester chaque candidat
	for _, path := range candidates {
		if path == "" {
			continue
		}
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}

	return ""
}

// findExecutable recherche un exécutable dans PATH
func (d *Discovery) findExecutable(name string) string {
	path, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	return path
}

// setupTempDir crée et retourne le répertoire temporaire
func (d *Discovery) setupTempDir() string {
	// Préférer un répertoire dédié
	baseDir := os.TempDir()
	mcpDir := filepath.Join(baseDir, "holow-mcp")

	if err := os.MkdirAll(mcpDir, 0755); err != nil {
		return baseDir
	}

	return mcpDir
}

// storeConfig stocke les découvertes dans la table config
func (d *Discovery) storeConfig(discoveries map[string]string) error {
	// Utiliser une transaction
	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Préparer l'upsert
	stmt, err := tx.Prepare(`
		INSERT INTO config (key, value, description, updated_at)
		VALUES (?, ?, ?, strftime('%s', 'now'))
		ON CONFLICT(key) DO UPDATE SET
			value = excluded.value,
			updated_at = excluded.updated_at
	`)
	if err != nil {
		return fmt.Errorf("prepare statement: %w", err)
	}
	defer stmt.Close()

	// Descriptions pour chaque clé
	descriptions := map[string]string{
		KeyChromiumPath:  "Chemin vers l'exécutable Chromium/Chrome",
		KeyChromiumFound: "Chromium détecté sur le système",
		KeyTempDir:       "Répertoire temporaire MCP",
		KeyUserDataDir:   "Répertoire profil Chromium",
		KeyDefaultPort:   "Port par défaut débogueur Chrome",
		KeySQLite3Path:   "Chemin vers sqlite3 CLI",
		KeyGitPath:       "Chemin vers git",
		KeyPlatform:      "Système d'exploitation",
		KeyArch:          "Architecture processeur",
		KeyDiscoveredAt:  "Date de dernière découverte",
	}

	// Insérer chaque découverte
	for key, value := range discoveries {
		desc := descriptions[key]
		if desc == "" {
			desc = "Auto-discovered"
		}
		if _, err := stmt.Exec(key, value, desc); err != nil {
			return fmt.Errorf("insert %s: %w", key, err)
		}
	}

	return tx.Commit()
}

// Get récupère une valeur de configuration
func (d *Discovery) Get(key string) (string, error) {
	var value string
	err := d.db.QueryRow("SELECT value FROM config WHERE key = ?", key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

// GetWithDefault récupère une valeur avec fallback
func (d *Discovery) GetWithDefault(key, defaultValue string) string {
	value, err := d.Get(key)
	if err != nil || value == "" {
		return defaultValue
	}
	return value
}

// GetChromiumPath retourne le chemin Chromium découvert
func (d *Discovery) GetChromiumPath() string {
	return d.GetWithDefault(KeyChromiumPath, "")
}

// GetUserDataDir retourne le répertoire profil Chromium
func (d *Discovery) GetUserDataDir() string {
	return d.GetWithDefault(KeyUserDataDir, filepath.Join(os.TempDir(), "holow-mcp", "chromium-profile"))
}

// GetDefaultPort retourne le port par défaut
func (d *Discovery) GetDefaultPort() int {
	portStr := d.GetWithDefault(KeyDefaultPort, "9222")
	var port int
	fmt.Sscanf(portStr, "%d", &port)
	if port == 0 {
		return 9222
	}
	return port
}

// IsChromiumAvailable vérifie si Chromium est disponible
func (d *Discovery) IsChromiumAvailable() bool {
	value := d.GetWithDefault(KeyChromiumFound, "false")
	return strings.ToLower(value) == "true"
}
