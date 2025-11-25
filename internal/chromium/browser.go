// Package chromium implémente le contrôle de Chromium via CDP
package chromium

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// Browser représente une instance de Chromium
type Browser struct {
	cmd        *exec.Cmd
	wsURL      string
	conn       *websocket.Conn
	debugPort  int
	userDataDir string

	msgID      int64
	pending    map[int64]chan *Response
	mu         sync.Mutex

	ctx        context.Context
	cancel     context.CancelFunc
}

// Response représente une réponse CDP
type Response struct {
	ID     int64           `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *CDPError       `json:"error"`
}

// CDPError représente une erreur CDP
type CDPError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Event représente un événement CDP
type Event struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

// Config configuration pour lancer Chromium
type Config struct {
	Headless    bool
	DebugPort   int
	UserDataDir string
	WindowSize  string // "1920,1080"
	ExtraArgs   []string
	ChromePath  string // Chemin vers l'exécutable (depuis Discovery)
}

// DefaultConfig retourne la configuration par défaut
func DefaultConfig() *Config {
	return &Config{
		Headless:    true,
		DebugPort:   9222,
		WindowSize:  "1920,1080",
	}
}

// Launch lance une nouvelle instance de Chromium
func Launch(cfg *Config) (*Browser, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	// Utiliser le chemin fourni ou chercher
	chromePath := cfg.ChromePath
	if chromePath == "" {
		chromePath = findChromium()
	}
	if chromePath == "" {
		return nil, fmt.Errorf("chromium not found: set ChromePath in config or install chromium")
	}

	// Créer un répertoire temporaire pour les données utilisateur
	if cfg.UserDataDir == "" {
		tmpDir, err := os.MkdirTemp("", "chromium-holow-*")
		if err != nil {
			return nil, fmt.Errorf("failed to create temp dir: %w", err)
		}
		cfg.UserDataDir = tmpDir
	}

	// Arguments Chromium
	args := []string{
		fmt.Sprintf("--remote-debugging-port=%d", cfg.DebugPort),
		fmt.Sprintf("--user-data-dir=%s", cfg.UserDataDir),
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-background-networking",
		"--disable-client-side-phishing-detection",
		"--disable-default-apps",
		"--disable-extensions",
		"--disable-hang-monitor",
		"--disable-popup-blocking",
		"--disable-prompt-on-repost",
		"--disable-sync",
		"--disable-translate",
		"--metrics-recording-only",
		"--safebrowsing-disable-auto-update",
	}

	if cfg.Headless {
		args = append(args, "--headless=new")
	}

	if cfg.WindowSize != "" {
		args = append(args, fmt.Sprintf("--window-size=%s", cfg.WindowSize))
	}

	args = append(args, cfg.ExtraArgs...)

	// Lancer Chromium
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, chromePath, args...)

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to start chromium: %w", err)
	}

	// Attendre que le port soit disponible
	wsURL, err := waitForDebugger(cfg.DebugPort, 30*time.Second)
	if err != nil {
		cmd.Process.Kill()
		cancel()
		return nil, fmt.Errorf("failed to connect to debugger: %w", err)
	}

	// Connecter via WebSocket
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		cmd.Process.Kill()
		cancel()
		return nil, fmt.Errorf("failed to connect websocket: %w", err)
	}

	b := &Browser{
		cmd:         cmd,
		wsURL:       wsURL,
		conn:        conn,
		debugPort:   cfg.DebugPort,
		userDataDir: cfg.UserDataDir,
		pending:     make(map[int64]chan *Response),
		ctx:         ctx,
		cancel:      cancel,
	}

	// Goroutine pour lire les messages
	go b.readLoop()

	return b, nil
}

// Connect se connecte à une instance Chromium existante
func Connect(debugPort int) (*Browser, error) {
	wsURL, err := getDebuggerURL(debugPort)
	if err != nil {
		return nil, err
	}

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to connect websocket: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	b := &Browser{
		wsURL:     wsURL,
		conn:      conn,
		debugPort: debugPort,
		pending:   make(map[int64]chan *Response),
		ctx:       ctx,
		cancel:    cancel,
	}

	go b.readLoop()

	return b, nil
}

// readLoop lit les messages WebSocket
func (b *Browser) readLoop() {
	for {
		select {
		case <-b.ctx.Done():
			return
		default:
		}

		_, message, err := b.conn.ReadMessage()
		if err != nil {
			return
		}

		// Déterminer si c'est une réponse ou un événement
		var resp Response
		if err := json.Unmarshal(message, &resp); err == nil && resp.ID > 0 {
			b.mu.Lock()
			if ch, ok := b.pending[resp.ID]; ok {
				ch <- &resp
				delete(b.pending, resp.ID)
			}
			b.mu.Unlock()
		}
		// Les événements sont ignorés pour l'instant
	}
}

// Call envoie une commande CDP et attend la réponse
func (b *Browser) Call(method string, params interface{}) (json.RawMessage, error) {
	id := atomic.AddInt64(&b.msgID, 1)

	msg := map[string]interface{}{
		"id":     id,
		"method": method,
	}
	if params != nil {
		msg["params"] = params
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}

	// Créer le canal de réponse
	ch := make(chan *Response, 1)
	b.mu.Lock()
	b.pending[id] = ch
	b.mu.Unlock()

	// Envoyer le message
	if err := b.conn.WriteMessage(websocket.TextMessage, data); err != nil {
		b.mu.Lock()
		delete(b.pending, id)
		b.mu.Unlock()
		return nil, err
	}

	// Attendre la réponse avec timeout
	select {
	case resp := <-ch:
		if resp.Error != nil {
			return nil, fmt.Errorf("CDP error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp.Result, nil
	case <-time.After(30 * time.Second):
		b.mu.Lock()
		delete(b.pending, id)
		b.mu.Unlock()
		return nil, fmt.Errorf("timeout waiting for response")
	case <-b.ctx.Done():
		return nil, b.ctx.Err()
	}
}

// Navigate navigue vers une URL
func (b *Browser) Navigate(url string) error {
	// Activer les événements Page
	b.Call("Page.enable", nil)

	_, err := b.Call("Page.navigate", map[string]string{"url": url})
	if err != nil {
		return err
	}

	// Attendre que la page charge (simple sleep pour éviter complexité événements)
	time.Sleep(2 * time.Second)
	return nil
}

// Screenshot prend une capture d'écran
func (b *Browser) Screenshot(format string, quality int, fullPage bool) ([]byte, error) {
	if format == "" {
		format = "png"
	}

	params := map[string]interface{}{
		"format": format,
	}

	if format == "jpeg" && quality > 0 {
		params["quality"] = quality
	}

	if fullPage {
		params["captureBeyondViewport"] = true
	}

	result, err := b.Call("Page.captureScreenshot", params)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, err
	}

	return base64.StdEncoding.DecodeString(resp.Data)
}

// Evaluate exécute du JavaScript
func (b *Browser) Evaluate(expression string) (interface{}, error) {
	result, err := b.Call("Runtime.evaluate", map[string]interface{}{
		"expression":    expression,
		"returnByValue": true,
	})
	if err != nil {
		return nil, err
	}

	var resp struct {
		Result struct {
			Value interface{} `json:"value"`
			Type  string      `json:"type"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text string `json:"text"`
		} `json:"exceptionDetails"`
	}

	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, err
	}

	if resp.ExceptionDetails != nil {
		return nil, fmt.Errorf("JS error: %s", resp.ExceptionDetails.Text)
	}

	return resp.Result.Value, nil
}

// GetHTML retourne le HTML de la page
func (b *Browser) GetHTML() (string, error) {
	result, err := b.Call("DOM.getDocument", map[string]interface{}{
		"depth": -1,
	})
	if err != nil {
		return "", err
	}

	var doc struct {
		Root struct {
			NodeID int `json:"nodeId"`
		} `json:"root"`
	}
	if err := json.Unmarshal(result, &doc); err != nil {
		return "", err
	}

	result, err = b.Call("DOM.getOuterHTML", map[string]int{
		"nodeId": doc.Root.NodeID,
	})
	if err != nil {
		return "", err
	}

	var html struct {
		OuterHTML string `json:"outerHTML"`
	}
	if err := json.Unmarshal(result, &html); err != nil {
		return "", err
	}

	return html.OuterHTML, nil
}

// escapeJSString échappe une chaîne pour insertion sécurisée dans du JavaScript
// Protège contre les injections XSS en échappant tous les caractères dangereux
func escapeJSString(s string) string {
	var result strings.Builder
	result.Grow(len(s) + 10) // Pré-allouer un peu plus

	for _, r := range s {
		switch r {
		case '\\':
			result.WriteString("\\\\")
		case '\'':
			result.WriteString("\\'")
		case '"':
			result.WriteString("\\\"")
		case '\n':
			result.WriteString("\\n")
		case '\r':
			result.WriteString("\\r")
		case '\t':
			result.WriteString("\\t")
		case '<':
			result.WriteString("\\x3c") // Évite </script> injection
		case '>':
			result.WriteString("\\x3e")
		case '\u2028': // Line separator
			result.WriteString("\\u2028")
		case '\u2029': // Paragraph separator
			result.WriteString("\\u2029")
		default:
			if r < 32 || r == 127 {
				// Caractères de contrôle: encoder en hex
				result.WriteString(fmt.Sprintf("\\x%02x", r))
			} else {
				result.WriteRune(r)
			}
		}
	}
	return result.String()
}

// validateCSSSelector valide qu'un sélecteur CSS ne contient pas de caractères dangereux
func validateCSSSelector(selector string) error {
	if len(selector) == 0 {
		return fmt.Errorf("selector cannot be empty")
	}
	if len(selector) > 1024 {
		return fmt.Errorf("selector too long (max 1024 characters)")
	}
	// Vérifier les caractères interdits qui pourraient casser la syntaxe JS
	for _, r := range selector {
		if r < 32 && r != '\t' {
			return fmt.Errorf("selector contains invalid control character")
		}
	}
	return nil
}

// Click clique sur un élément par sélecteur CSS
func (b *Browser) Click(selector string) error {
	if err := validateCSSSelector(selector); err != nil {
		return fmt.Errorf("invalid selector: %w", err)
	}
	// Trouver l'élément avec sélecteur échappé
	escaped := escapeJSString(selector)
	_, err := b.Evaluate(fmt.Sprintf(`document.querySelector('%s').click()`, escaped))
	return err
}

// Type tape du texte dans un élément
func (b *Browser) Type(selector, text string) error {
	if err := validateCSSSelector(selector); err != nil {
		return fmt.Errorf("invalid selector: %w", err)
	}
	// Focus sur l'élément avec sélecteur échappé
	escaped := escapeJSString(selector)
	_, err := b.Evaluate(fmt.Sprintf(`document.querySelector('%s').focus()`, escaped))
	if err != nil {
		return err
	}

	// Envoyer les caractères
	for _, char := range text {
		_, err = b.Call("Input.dispatchKeyEvent", map[string]interface{}{
			"type": "char",
			"text": string(char),
		})
		if err != nil {
			return err
		}
	}

	return nil
}

// WaitForSelector attend qu'un élément soit présent
func (b *Browser) WaitForSelector(selector string, timeout time.Duration) error {
	if err := validateCSSSelector(selector); err != nil {
		return fmt.Errorf("invalid selector: %w", err)
	}
	escaped := escapeJSString(selector)
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		result, err := b.Evaluate(fmt.Sprintf(`document.querySelector('%s') !== null`, escaped))
		if err != nil {
			return err
		}

		if found, ok := result.(bool); ok && found {
			return nil
		}

		time.Sleep(100 * time.Millisecond)
	}

	return fmt.Errorf("timeout waiting for selector: %s", selector)
}

// GetCookies retourne les cookies
func (b *Browser) GetCookies() ([]map[string]interface{}, error) {
	result, err := b.Call("Network.getCookies", nil)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Cookies []map[string]interface{} `json:"cookies"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, err
	}

	return resp.Cookies, nil
}

// SetCookie définit un cookie
func (b *Browser) SetCookie(name, value, domain, path string) error {
	_, err := b.Call("Network.setCookie", map[string]string{
		"name":   name,
		"value":  value,
		"domain": domain,
		"path":   path,
	})
	return err
}

// GetURL retourne l'URL actuelle
func (b *Browser) GetURL() (string, error) {
	result, err := b.Evaluate("window.location.href")
	if err != nil {
		return "", err
	}
	// Vérification de type sécurisée pour éviter les panics
	url, ok := result.(string)
	if !ok {
		return "", fmt.Errorf("unexpected result type for URL: %T", result)
	}
	return url, nil
}

// GetTitle retourne le titre de la page
func (b *Browser) GetTitle() (string, error) {
	result, err := b.Evaluate("document.title")
	if err != nil {
		return "", err
	}
	// Vérification de type sécurisée pour éviter les panics
	title, ok := result.(string)
	if !ok {
		return "", fmt.Errorf("unexpected result type for title: %T", result)
	}
	return title, nil
}

// Close ferme le navigateur
func (b *Browser) Close() error {
	b.cancel()

	if b.conn != nil {
		b.conn.Close()
	}

	if b.cmd != nil && b.cmd.Process != nil {
		b.cmd.Process.Kill()
	}

	// Nettoyer le répertoire temporaire
	if b.userDataDir != "" {
		os.RemoveAll(b.userDataDir)
	}

	return nil
}

// findChromium trouve l'exécutable Chromium
func findChromium() string {
	paths := []string{
		"/usr/bin/chromium",
		"/usr/bin/chromium-browser",
		"/usr/bin/google-chrome",
		"/usr/bin/google-chrome-stable",
		"/snap/bin/chromium",
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/Applications/Chromium.app/Contents/MacOS/Chromium",
	}

	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	// Chercher dans PATH
	if path, err := exec.LookPath("chromium"); err == nil {
		return path
	}
	if path, err := exec.LookPath("chromium-browser"); err == nil {
		return path
	}
	if path, err := exec.LookPath("google-chrome"); err == nil {
		return path
	}

	return ""
}

// waitForDebugger attend que le débogueur soit disponible
func waitForDebugger(port int, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		// Vérifier si le port est ouvert
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
		if err == nil {
			conn.Close()
			return getDebuggerURL(port)
		}

		time.Sleep(100 * time.Millisecond)
	}

	return "", fmt.Errorf("timeout waiting for debugger on port %d", port)
}

// getDebuggerURL récupère l'URL WebSocket du débogueur
func getDebuggerURL(port int) (string, error) {
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/json/version", port))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var info struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return "", err
	}

	if info.WebSocketDebuggerURL == "" {
		return "", fmt.Errorf("no WebSocket URL in response")
	}

	return info.WebSocketDebuggerURL, nil
}

// SaveScreenshot sauvegarde une capture d'écran dans un fichier
func (b *Browser) SaveScreenshot(path string) error {
	format := "png"
	if filepath.Ext(path) == ".jpg" || filepath.Ext(path) == ".jpeg" {
		format = "jpeg"
	}

	data, err := b.Screenshot(format, 80, false)
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// PDF génère un PDF de la page
func (b *Browser) PDF() ([]byte, error) {
	result, err := b.Call("Page.printToPDF", map[string]interface{}{
		"printBackground": true,
	})
	if err != nil {
		return nil, err
	}

	var resp struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, err
	}

	return base64.StdEncoding.DecodeString(resp.Data)
}
