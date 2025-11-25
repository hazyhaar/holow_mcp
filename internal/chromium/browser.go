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

// cdpDebug active les logs de debug CDP (via env CDP_DEBUG=1)
var cdpDebug = os.Getenv("CDP_DEBUG") == "1"

// cdpLog log un message de debug CDP si activé
func cdpLog(format string, args ...interface{}) {
	if cdpDebug {
		fmt.Fprintf(os.Stderr, "[CDP] "+format+"\n", args...)
	}
}

// Browser représente une instance de Chromium
type Browser struct {
	cmd         *exec.Cmd
	wsURL       string
	conn        *websocket.Conn
	debugPort   int
	userDataDir string

	msgID   int64
	pending map[int64]chan *Response
	mu      sync.Mutex

	// Session CDP pour le target actif (page)
	currentTargetID  string
	currentSessionID string

	// Capture des événements (console, network)
	consoleLogs    []ConsoleLog
	networkReqs    []NetworkRequest
	eventsEnabled  bool
	eventsMu       sync.RWMutex

	ctx    context.Context
	cancel context.CancelFunc
}

// ConsoleLog représente un message console
type ConsoleLog struct {
	Timestamp int64  `json:"timestamp"`
	Level     string `json:"level"`
	Message   string `json:"message"`
	Source    string `json:"source,omitempty"`
	Line      int    `json:"line,omitempty"`
}

// NetworkRequest représente une requête réseau
type NetworkRequest struct {
	RequestID string `json:"requestId"`
	Timestamp int64  `json:"timestamp"`
	URL       string `json:"url"`
	Method    string `json:"method"`
	Status    int    `json:"status,omitempty"`
	MimeType  string `json:"mimeType,omitempty"`
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

	cdpLog("Launch(headless=%v, port=%d)", cfg.Headless, cfg.DebugPort)

	// Utiliser le chemin fourni ou chercher
	chromePath := cfg.ChromePath
	if chromePath == "" {
		chromePath = findChromium()
	}
	if chromePath == "" {
		cdpLog("ERROR: chromium not found")
		return nil, fmt.Errorf("chromium not found: set ChromePath in config or install chromium")
	}
	cdpLog("Using chromium: %s", chromePath)

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
	cdpLog("Connect(port=%d)", debugPort)

	wsURL, err := getDebuggerURL(debugPort)
	if err != nil {
		return nil, err
	}

	cdpLog("Dialing WebSocket: %s", wsURL)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		cdpLog("WebSocket ERROR: %v", err)
		return nil, fmt.Errorf("failed to connect websocket: %w", err)
	}
	cdpLog("WebSocket connected")

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
		var msg struct {
			ID     int64           `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			Result json.RawMessage `json:"result"`
			Error  *CDPError       `json:"error"`
		}

		if err := json.Unmarshal(message, &msg); err != nil {
			continue
		}

		// C'est une réponse (a un ID)
		if msg.ID > 0 {
			b.mu.Lock()
			if ch, ok := b.pending[msg.ID]; ok {
				ch <- &Response{ID: msg.ID, Result: msg.Result, Error: msg.Error}
				delete(b.pending, msg.ID)
			}
			b.mu.Unlock()
			continue
		}

		// C'est un événement (a un Method)
		if msg.Method != "" {
			b.handleEvent(msg.Method, msg.Params)
		}
	}
}

// handleEvent traite les événements CDP
func (b *Browser) handleEvent(method string, params json.RawMessage) {
	switch method {
	case "Runtime.consoleAPICalled":
		b.handleConsoleEvent(params)
	case "Network.requestWillBeSent":
		b.handleNetworkRequest(params)
	case "Network.responseReceived":
		b.handleNetworkResponse(params)
	}
}

// handleConsoleEvent capture les logs console
func (b *Browser) handleConsoleEvent(params json.RawMessage) {
	var event struct {
		Type string `json:"type"`
		Args []struct {
			Type  string `json:"type"`
			Value string `json:"value"`
		} `json:"args"`
		Timestamp float64 `json:"timestamp"`
		StackTrace *struct {
			CallFrames []struct {
				URL        string `json:"url"`
				LineNumber int    `json:"lineNumber"`
			} `json:"callFrames"`
		} `json:"stackTrace"`
	}

	if err := json.Unmarshal(params, &event); err != nil {
		return
	}

	// Construire le message
	var message string
	for _, arg := range event.Args {
		if message != "" {
			message += " "
		}
		message += arg.Value
	}

	log := ConsoleLog{
		Timestamp: int64(event.Timestamp),
		Level:     event.Type,
		Message:   message,
	}

	if event.StackTrace != nil && len(event.StackTrace.CallFrames) > 0 {
		log.Source = event.StackTrace.CallFrames[0].URL
		log.Line = event.StackTrace.CallFrames[0].LineNumber
	}

	cdpLog("Console[%s]: %s", log.Level, log.Message)

	b.eventsMu.Lock()
	b.consoleLogs = append(b.consoleLogs, log)
	// Garder max 1000 logs
	if len(b.consoleLogs) > 1000 {
		b.consoleLogs = b.consoleLogs[len(b.consoleLogs)-1000:]
	}
	b.eventsMu.Unlock()
}

// handleNetworkRequest capture les requêtes réseau
func (b *Browser) handleNetworkRequest(params json.RawMessage) {
	var event struct {
		RequestID string `json:"requestId"`
		Request   struct {
			URL    string `json:"url"`
			Method string `json:"method"`
		} `json:"request"`
		Timestamp float64 `json:"timestamp"`
	}

	if err := json.Unmarshal(params, &event); err != nil {
		return
	}

	req := NetworkRequest{
		RequestID: event.RequestID,
		Timestamp: int64(event.Timestamp * 1000),
		URL:       event.Request.URL,
		Method:    event.Request.Method,
	}

	cdpLog("Network[%s] %s %s", req.RequestID[:8], req.Method, req.URL)

	b.eventsMu.Lock()
	b.networkReqs = append(b.networkReqs, req)
	// Garder max 500 requêtes
	if len(b.networkReqs) > 500 {
		b.networkReqs = b.networkReqs[len(b.networkReqs)-500:]
	}
	b.eventsMu.Unlock()
}

// handleNetworkResponse met à jour la requête avec le status
func (b *Browser) handleNetworkResponse(params json.RawMessage) {
	var event struct {
		RequestID string `json:"requestId"`
		Response  struct {
			Status   int    `json:"status"`
			MimeType string `json:"mimeType"`
		} `json:"response"`
	}

	if err := json.Unmarshal(params, &event); err != nil {
		return
	}

	b.eventsMu.Lock()
	for i := len(b.networkReqs) - 1; i >= 0; i-- {
		if b.networkReqs[i].RequestID == event.RequestID {
			b.networkReqs[i].Status = event.Response.Status
			b.networkReqs[i].MimeType = event.Response.MimeType
			break
		}
	}
	b.eventsMu.Unlock()
}

// EnableMonitoring active la capture des événements console et network
func (b *Browser) EnableMonitoring() error {
	cdpLog("EnableMonitoring()")

	// Activer Runtime pour les logs console
	if _, err := b.Call("Runtime.enable", nil); err != nil {
		return fmt.Errorf("Runtime.enable failed: %w", err)
	}

	// Activer Network pour les requêtes
	if _, err := b.Call("Network.enable", nil); err != nil {
		return fmt.Errorf("Network.enable failed: %w", err)
	}

	b.eventsMu.Lock()
	b.eventsEnabled = true
	b.eventsMu.Unlock()

	cdpLog("Monitoring enabled (Runtime + Network)")
	return nil
}

// GetConsoleLogs retourne les logs console capturés
func (b *Browser) GetConsoleLogs(clear bool) []ConsoleLog {
	b.eventsMu.Lock()
	defer b.eventsMu.Unlock()

	logs := make([]ConsoleLog, len(b.consoleLogs))
	copy(logs, b.consoleLogs)

	if clear {
		b.consoleLogs = nil
	}

	return logs
}

// GetNetworkRequests retourne les requêtes réseau capturées
func (b *Browser) GetNetworkRequests(clear bool) []NetworkRequest {
	b.eventsMu.Lock()
	defer b.eventsMu.Unlock()

	reqs := make([]NetworkRequest, len(b.networkReqs))
	copy(reqs, b.networkReqs)

	if clear {
		b.networkReqs = nil
	}

	return reqs
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

	cdpLog("Call[%d] %s params=%s", id, method, string(data))

	// Créer le canal de réponse
	ch := make(chan *Response, 1)
	b.mu.Lock()
	b.pending[id] = ch
	b.mu.Unlock()

	// Envoyer le message
	if err := b.conn.WriteMessage(websocket.TextMessage, data); err != nil {
		cdpLog("Call[%d] SEND ERROR: %v", id, err)
		b.mu.Lock()
		delete(b.pending, id)
		b.mu.Unlock()
		return nil, err
	}

	// Attendre la réponse avec timeout
	select {
	case resp := <-ch:
		if resp.Error != nil {
			cdpLog("Call[%d] CDP ERROR: %d %s", id, resp.Error.Code, resp.Error.Message)
			return nil, fmt.Errorf("CDP error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		cdpLog("Call[%d] OK result=%d bytes", id, len(resp.Result))
		return resp.Result, nil
	case <-time.After(30 * time.Second):
		cdpLog("Call[%d] TIMEOUT", id)
		b.mu.Lock()
		delete(b.pending, id)
		b.mu.Unlock()
		return nil, fmt.Errorf("timeout waiting for response")
	case <-b.ctx.Done():
		cdpLog("Call[%d] CANCELLED", id)
		return nil, b.ctx.Err()
	}
}

// TargetInfo représente les informations d'un target CDP
type TargetInfo struct {
	TargetID string `json:"targetId"`
	Type     string `json:"type"`
	Title    string `json:"title"`
	URL      string `json:"url"`
}

// GetTargets retourne la liste de tous les targets
func (b *Browser) GetTargets() ([]TargetInfo, error) {
	result, err := b.Call("Target.getTargets", nil)
	if err != nil {
		return nil, err
	}

	var resp struct {
		TargetInfos []TargetInfo `json:"targetInfos"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse targets: %w", err)
	}

	return resp.TargetInfos, nil
}

// CreateTarget crée un nouveau target (page) et retourne son ID
func (b *Browser) CreateTarget(url string) (string, error) {
	if url == "" {
		url = "about:blank"
	}

	result, err := b.Call("Target.createTarget", map[string]interface{}{
		"url": url,
	})
	if err != nil {
		return "", err
	}

	var resp struct {
		TargetID string `json:"targetId"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return "", fmt.Errorf("failed to parse target ID: %w", err)
	}

	return resp.TargetID, nil
}

// AttachToTarget s'attache à un target et retourne le sessionId
func (b *Browser) AttachToTarget(targetID string) (string, error) {
	result, err := b.Call("Target.attachToTarget", map[string]interface{}{
		"targetId": targetID,
		"flatten":  true, // Mode flat = les messages utilisent sessionId au lieu d'être wrappés
	})
	if err != nil {
		return "", err
	}

	var resp struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return "", fmt.Errorf("failed to parse session ID: %w", err)
	}

	b.mu.Lock()
	b.currentTargetID = targetID
	b.currentSessionID = resp.SessionID
	b.mu.Unlock()

	return resp.SessionID, nil
}

// CloseTarget ferme un target
func (b *Browser) CloseTarget(targetID string) error {
	_, err := b.Call("Target.closeTarget", map[string]interface{}{
		"targetId": targetID,
	})
	return err
}

// CallWithSession envoie une commande CDP avec un sessionId spécifique
func (b *Browser) CallWithSession(sessionID, method string, params interface{}) (json.RawMessage, error) {
	id := atomic.AddInt64(&b.msgID, 1)

	msg := map[string]interface{}{
		"id":        id,
		"method":    method,
		"sessionId": sessionID,
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

// EnsurePageSession s'assure qu'une session page est active
// Si aucune session n'existe, crée une page et s'y attache
func (b *Browser) EnsurePageSession() (string, error) {
	b.mu.Lock()
	sessionID := b.currentSessionID
	b.mu.Unlock()

	if sessionID != "" {
		return sessionID, nil
	}

	// Chercher une page existante
	targets, err := b.GetTargets()
	if err != nil {
		return "", fmt.Errorf("failed to get targets: %w", err)
	}

	var pageTargetID string
	for _, t := range targets {
		if t.Type == "page" {
			pageTargetID = t.TargetID
			break
		}
	}

	// Créer une page si aucune n'existe
	if pageTargetID == "" {
		pageTargetID, err = b.CreateTarget("about:blank")
		if err != nil {
			return "", fmt.Errorf("failed to create page: %w", err)
		}
	}

	// S'attacher au target
	sessionID, err = b.AttachToTarget(pageTargetID)
	if err != nil {
		return "", fmt.Errorf("failed to attach to target: %w", err)
	}

	return sessionID, nil
}

// GetCurrentSession retourne le sessionId actuel
func (b *Browser) GetCurrentSession() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.currentSessionID
}

// GetCurrentTargetID retourne l'ID du target actuel
func (b *Browser) GetCurrentTargetID() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.currentTargetID
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

// getDebuggerURL récupère l'URL WebSocket d'une PAGE (pas du browser)
// Important: Les commandes Page.* ne fonctionnent qu'au niveau page, pas browser
func getDebuggerURL(port int) (string, error) {
	url := fmt.Sprintf("http://127.0.0.1:%d/json", port)
	cdpLog("GET %s", url)

	resp, err := http.Get(url)
	if err != nil {
		cdpLog("ERROR: %v", err)
		return "", err
	}
	defer resp.Body.Close()

	var pages []struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
		Type                 string `json:"type"`
		URL                  string `json:"url"`
		Title                string `json:"title"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&pages); err != nil {
		cdpLog("ERROR decode: %v", err)
		return "", err
	}

	cdpLog("Found %d targets:", len(pages))
	for i, p := range pages {
		cdpLog("  [%d] type=%s url=%s ws=%s", i, p.Type, p.URL, p.WebSocketDebuggerURL != "")
	}

	// Chercher une page de type "page"
	for _, p := range pages {
		if p.Type == "page" && p.WebSocketDebuggerURL != "" {
			cdpLog("Selected page: %s", p.URL)
			return p.WebSocketDebuggerURL, nil
		}
	}

	// Fallback: prendre n'importe quelle cible avec une URL WebSocket
	for _, p := range pages {
		if p.WebSocketDebuggerURL != "" {
			cdpLog("Fallback target: type=%s url=%s", p.Type, p.URL)
			return p.WebSocketDebuggerURL, nil
		}
	}

	cdpLog("ERROR: no page available")
	return "", fmt.Errorf("no page available - browser may have no tabs open")
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
