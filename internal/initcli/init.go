// Package initcli implémente le CLI d'initialisation interactif pour HOLOW-MCP
package initcli

import (
	"bufio"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
)

// Config représente la configuration d'initialisation
type Config struct {
	BasePath      string
	CredentialsDB string
	Providers     map[string]string // provider -> api_key (non chiffré en mémoire)
}

// Provider représente un fournisseur d'API
type Provider struct {
	Name        string
	EnvVar      string
	Description string
}

var defaultProviders = []Provider{
	{"claude", "ANTHROPIC_API_KEY", "Claude (Anthropic)"},
	{"gemini", "GOOGLE_API_KEY", "Gemini (Google)"},
	{"cerebras", "CEREBRAS_API_KEY", "Cerebras"},
	{"github", "GITHUB_TOKEN", "GitHub"},
}

// Run exécute le CLI d'initialisation interactif
func Run() (*Config, error) {
	reader := bufio.NewReader(os.Stdin)

	printBanner()

	// Étape 1: Détecter installation existante
	defaultPath := getDefaultBasePath()
	existing := detectExistingInstall(defaultPath)

	var config *Config

	if existing != nil {
		fmt.Printf("\n[!] Installation existante détectée dans: %s\n", existing.BasePath)
		fmt.Println("    1. Connecter aux bases existantes")
		fmt.Println("    2. Purger et réinstaller")
		fmt.Println("    3. Annuler")

		choice := promptChoice(reader, "Choix", []string{"1", "2", "3"}, "1")

		switch choice {
		case "1":
			// Vérifier connexion
			if err := testConnection(existing); err != nil {
				fmt.Printf("\n[X] Connexion échouée: %v\n", err)
				if promptYesNo(reader, "Purger et réinstaller?", false) {
					purgeInstall(existing.BasePath)
					config = &Config{BasePath: existing.BasePath, Providers: make(map[string]string)}
				} else {
					return nil, fmt.Errorf("connexion impossible")
				}
			} else {
				fmt.Println("\n[OK] Connexion réussie")
				config = existing
			}
		case "2":
			purgeInstall(existing.BasePath)
			config = &Config{BasePath: existing.BasePath, Providers: make(map[string]string)}
		case "3":
			return nil, fmt.Errorf("annulé par l'utilisateur")
		}
	} else {
		config = &Config{Providers: make(map[string]string)}
	}

	// Étape 2: Chemin d'installation (si nouveau)
	if config.BasePath == "" {
		fmt.Printf("\n[?] Chemin d'installation [%s]: ", defaultPath)
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)
		if input == "" {
			input = defaultPath
		}

		// Valider le chemin
		if err := validatePath(input); err != nil {
			return nil, fmt.Errorf("chemin invalide: %w", err)
		}
		config.BasePath = input
	}

	// Étape 3: Nom de la base credentials
	if config.CredentialsDB == "" {
		config.CredentialsDB = "credentials"
	}

	// Étape 4: Setup credentials
	fmt.Println("\n--- Configuration des API Keys ---")
	for _, p := range defaultProviders {
		setupProvider(reader, config, p)
	}

	// Étape 5: Créer les bases si nécessaire
	if existing == nil {
		fmt.Println("\n[*] Création des bases de données...")
		if err := createCredentialsDB(config); err != nil {
			return nil, fmt.Errorf("erreur création credentials DB: %w", err)
		}
		fmt.Println("[OK] Base credentials créée")
	}

	// Sauvegarder les credentials
	if len(config.Providers) > 0 {
		fmt.Println("\n[*] Sauvegarde des credentials...")
		if err := saveCredentials(config); err != nil {
			return nil, fmt.Errorf("erreur sauvegarde credentials: %w", err)
		}
		fmt.Println("[OK] Credentials sauvegardées")
	}

	// Étape 6: Configuration MCP pour les AI clients
	if promptYesNo(reader, "\nConfigurer les AI clients (Claude Code, Gemini CLI, OpenCode)?", true) {
		if err := RunMCPConfigSetup(reader, config.BasePath); err != nil {
			fmt.Printf("\n[!] Erreur configuration MCP: %v\n", err)
		}
	}

	// Résumé
	printSummary(config)

	return config, nil
}

func printBanner() {
	fmt.Println(`
╔═══════════════════════════════════════════════════════════╗
║                    HOLOW-MCP INIT                         ║
║              Configuration interactive                     ║
╚═══════════════════════════════════════════════════════════╝`)
}

func getDefaultBasePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "/tmp/holow-mcp"
	}
	return filepath.Join(home, ".holow-mcp")
}

func detectExistingInstall(basePath string) *Config {
	// Vérifier si le dossier existe avec des bases
	dbFiles := []string{
		"holow-mcp.lifecycle-core.db",
		"holow-mcp.lifecycle-tools.db",
	}

	for _, dbFile := range dbFiles {
		path := filepath.Join(basePath, dbFile)
		if _, err := os.Stat(path); err == nil {
			return &Config{
				BasePath:      basePath,
				CredentialsDB: "credentials",
				Providers:     make(map[string]string),
			}
		}
	}
	return nil
}

func testConnection(config *Config) error {
	dbPath := filepath.Join(config.BasePath, "holow-mcp.lifecycle-core.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	return db.Ping()
}

func purgeInstall(basePath string) {
	fmt.Printf("[*] Purge de %s...\n", basePath)

	// Supprimer les fichiers .db
	files, _ := filepath.Glob(filepath.Join(basePath, "*.db"))
	for _, f := range files {
		os.Remove(f)
		fmt.Printf("    Supprimé: %s\n", filepath.Base(f))
	}

	// Supprimer les fichiers -wal et -shm
	walFiles, _ := filepath.Glob(filepath.Join(basePath, "*.db-*"))
	for _, f := range walFiles {
		os.Remove(f)
	}
}

func validatePath(path string) error {
	// Créer le dossier si nécessaire
	if err := os.MkdirAll(path, 0700); err != nil {
		return fmt.Errorf("impossible de créer le dossier: %w", err)
	}

	// Vérifier les permissions
	testFile := filepath.Join(path, ".test")
	if err := os.WriteFile(testFile, []byte("test"), 0600); err != nil {
		return fmt.Errorf("pas de permission d'écriture: %w", err)
	}
	os.Remove(testFile)

	// Vérifier que ce n'est pas un chemin dangereux
	absPath, _ := filepath.Abs(path)
	dangerous := []string{"/tmp", "/var/tmp", "/dev", "/proc", "/sys"}
	for _, d := range dangerous {
		if absPath == d {
			return fmt.Errorf("chemin non sécurisé: %s", absPath)
		}
	}

	return nil
}

func setupProvider(reader *bufio.Reader, config *Config, p Provider) {
	// Vérifier variable d'environnement
	if envVal := os.Getenv(p.EnvVar); envVal != "" {
		fmt.Printf("\n[%s] Trouvé dans $%s\n", p.Description, p.EnvVar)
		if promptYesNo(reader, fmt.Sprintf("Utiliser cette clé pour %s?", p.Name), true) {
			config.Providers[p.Name] = envVal
			return
		}
	}

	// Demander à l'utilisateur
	if promptYesNo(reader, fmt.Sprintf("Configurer %s?", p.Description), false) {
		fmt.Printf("  Clé API %s: ", p.Name)
		key, _ := reader.ReadString('\n')
		key = strings.TrimSpace(key)
		if key != "" {
			config.Providers[p.Name] = key
			fmt.Printf("  [OK] %s configuré\n", p.Description)
		}
	}
}

func promptYesNo(reader *bufio.Reader, prompt string, defaultYes bool) bool {
	defaultStr := "o/N"
	if defaultYes {
		defaultStr = "O/n"
	}

	fmt.Printf("[?] %s [%s]: ", prompt, defaultStr)
	input, _ := reader.ReadString('\n')
	input = strings.ToLower(strings.TrimSpace(input))

	if input == "" {
		return defaultYes
	}
	return input == "o" || input == "oui" || input == "y" || input == "yes"
}

func promptChoice(reader *bufio.Reader, prompt string, choices []string, defaultChoice string) string {
	fmt.Printf("[?] %s [%s]: ", prompt, defaultChoice)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)

	if input == "" {
		return defaultChoice
	}

	for _, c := range choices {
		if input == c {
			return c
		}
	}
	return defaultChoice
}

func createCredentialsDB(config *Config) error {
	dbPath := filepath.Join(config.BasePath, fmt.Sprintf("holow-mcp.%s.db", config.CredentialsDB))

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	// Créer le schéma
	schema := `
	-- Table de métadonnées pour le chiffrement
	CREATE TABLE IF NOT EXISTS encryption_meta (
		id INTEGER PRIMARY KEY CHECK(id = 1),
		salt BLOB NOT NULL,
		created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
	);

	-- Table des credentials
	CREATE TABLE IF NOT EXISTS credentials (
		id INTEGER PRIMARY KEY,
		provider TEXT NOT NULL UNIQUE,
		api_key_encrypted BLOB NOT NULL,
		iv BLOB NOT NULL,
		key_hint TEXT,
		created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
		updated_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
	);

	-- Table de configuration par provider
	CREATE TABLE IF NOT EXISTS provider_config (
		provider TEXT PRIMARY KEY,
		base_url TEXT,
		model_default TEXT,
		enabled INTEGER DEFAULT 1,
		config_json TEXT
	);
	`

	if _, err := db.Exec(schema); err != nil {
		return err
	}

	// Générer et stocker le sel
	salt := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		return err
	}

	_, err = db.Exec(`INSERT OR IGNORE INTO encryption_meta (id, salt) VALUES (1, ?)`, salt)
	return err
}

func saveCredentials(config *Config) error {
	dbPath := filepath.Join(config.BasePath, fmt.Sprintf("holow-mcp.%s.db", config.CredentialsDB))

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	// Récupérer le sel
	var salt []byte
	err = db.QueryRow(`SELECT salt FROM encryption_meta WHERE id = 1`).Scan(&salt)
	if err != nil {
		return fmt.Errorf("sel non trouvé: %w", err)
	}

	// Dériver la clé de chiffrement
	key := deriveKey(config.BasePath, config.CredentialsDB, salt)

	// Sauvegarder chaque credential
	for provider, apiKey := range config.Providers {
		encrypted, iv, err := encrypt([]byte(apiKey), key)
		if err != nil {
			return fmt.Errorf("chiffrement échoué pour %s: %w", provider, err)
		}

		// Hint: 4 derniers caractères
		hint := ""
		if len(apiKey) > 4 {
			hint = "..." + apiKey[len(apiKey)-4:]
		}

		_, err = db.Exec(`
			INSERT OR REPLACE INTO credentials (provider, api_key_encrypted, iv, key_hint, updated_at)
			VALUES (?, ?, ?, ?, strftime('%s', 'now'))
		`, provider, encrypted, iv, hint)

		if err != nil {
			return fmt.Errorf("sauvegarde échouée pour %s: %w", provider, err)
		}
	}

	return nil
}

// deriveKey dérive une clé AES-256 à partir du chemin et du nom de la base
func deriveKey(basePath, dbName string, salt []byte) []byte {
	input := fmt.Sprintf("%s:%s", basePath, dbName)
	hash := sha256.New()
	hash.Write([]byte(input))
	hash.Write(salt)
	return hash.Sum(nil) // 32 bytes = AES-256
}

// encrypt chiffre des données avec AES-256-GCM
func encrypt(plaintext, key []byte) (ciphertext, iv []byte, err error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, err
	}

	iv = make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return nil, nil, err
	}

	ciphertext = gcm.Seal(nil, iv, plaintext, nil)
	return ciphertext, iv, nil
}

// decrypt déchiffre des données avec AES-256-GCM
func decrypt(ciphertext, key, iv []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	return gcm.Open(nil, iv, ciphertext, nil)
}

func printSummary(config *Config) {
	fmt.Println(`
╔═══════════════════════════════════════════════════════════╗
║                       RÉSUMÉ                              ║
╚═══════════════════════════════════════════════════════════╝`)
	fmt.Printf("  Chemin: %s\n", config.BasePath)
	fmt.Printf("  Base credentials: holow-mcp.%s.db\n", config.CredentialsDB)
	fmt.Println("\n  Providers configurés:")
	if len(config.Providers) == 0 {
		fmt.Println("    (aucun)")
	}
	for provider := range config.Providers {
		fmt.Printf("    - %s\n", provider)
	}
	fmt.Println("\n[OK] Initialisation terminée!")
	fmt.Println("     Lancez: holow-mcp -path " + config.BasePath)
}

// GetCredential récupère une clé API déchiffrée
func GetCredential(basePath, credentialsDB, provider string) (string, error) {
	dbPath := filepath.Join(basePath, fmt.Sprintf("holow-mcp.%s.db", credentialsDB))

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return "", err
	}
	defer db.Close()

	// Récupérer le sel
	var salt []byte
	err = db.QueryRow(`SELECT salt FROM encryption_meta WHERE id = 1`).Scan(&salt)
	if err != nil {
		return "", fmt.Errorf("sel non trouvé: %w", err)
	}

	// Récupérer le credential chiffré
	var encrypted, iv []byte
	err = db.QueryRow(`
		SELECT api_key_encrypted, iv FROM credentials WHERE provider = ?
	`, provider).Scan(&encrypted, &iv)
	if err != nil {
		return "", fmt.Errorf("credential non trouvé: %w", err)
	}

	// Dériver la clé et déchiffrer
	key := deriveKey(basePath, credentialsDB, salt)
	plaintext, err := decrypt(encrypted, key, iv)
	if err != nil {
		return "", fmt.Errorf("déchiffrement échoué: %w", err)
	}

	return string(plaintext), nil
}

// ListProviders liste les providers configurés
func ListProviders(basePath, credentialsDB string) ([]string, error) {
	dbPath := filepath.Join(basePath, fmt.Sprintf("holow-mcp.%s.db", credentialsDB))

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query(`SELECT provider FROM credentials`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var providers []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err == nil {
			providers = append(providers, p)
		}
	}

	return providers, nil
}

// CredentialHint retourne un hint pour une clé API (pour affichage)
func CredentialHint(basePath, credentialsDB, provider string) string {
	dbPath := filepath.Join(basePath, fmt.Sprintf("holow-mcp.%s.db", credentialsDB))

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return ""
	}
	defer db.Close()

	var hint string
	db.QueryRow(`SELECT key_hint FROM credentials WHERE provider = ?`, provider).Scan(&hint)
	return hint
}

// ExportConfig exporte la configuration (sans les clés) pour debug
func ExportConfig(config *Config) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("BasePath: %s\n", config.BasePath))
	sb.WriteString(fmt.Sprintf("CredentialsDB: %s\n", config.CredentialsDB))
	sb.WriteString("Providers:\n")
	for p := range config.Providers {
		sb.WriteString(fmt.Sprintf("  - %s\n", p))
	}
	return sb.String()
}

// KeyFingerprint retourne une empreinte de la clé de chiffrement (pour vérification)
func KeyFingerprint(basePath, credentialsDB string) string {
	dbPath := filepath.Join(basePath, fmt.Sprintf("holow-mcp.%s.db", credentialsDB))

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return ""
	}
	defer db.Close()

	var salt []byte
	err = db.QueryRow(`SELECT salt FROM encryption_meta WHERE id = 1`).Scan(&salt)
	if err != nil {
		return ""
	}

	key := deriveKey(basePath, credentialsDB, salt)
	hash := sha256.Sum256(key)
	return hex.EncodeToString(hash[:8]) // 16 premiers caractères hex
}
