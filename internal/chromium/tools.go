// Package chromium - Tools MCP pour le contrôle de Chromium
package chromium

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ToolsManager gère les tools Chromium
type ToolsManager struct {
	browser       *Browser
	mu            sync.Mutex
	screenshotDir string
	chromePath    string // Chemin vers Chromium (depuis Discovery)
	userDataDir   string // Répertoire profil (depuis Discovery)
	defaultPort   int    // Port par défaut (depuis Discovery)
}

// ToolsConfig configuration pour ToolsManager depuis Discovery
type ToolsConfig struct {
	ScreenshotDir string
	ChromePath    string
	UserDataDir   string
	DefaultPort   int
}

// NewToolsManager crée un nouveau gestionnaire de tools Chromium
func NewToolsManager(cfg *ToolsConfig) *ToolsManager {
	if cfg == nil {
		cfg = &ToolsConfig{}
	}

	screenshotDir := cfg.ScreenshotDir
	if screenshotDir == "" {
		screenshotDir = filepath.Join(os.TempDir(), "holow-screenshots")
	}
	os.MkdirAll(screenshotDir, 0755)

	defaultPort := cfg.DefaultPort
	if defaultPort == 0 {
		defaultPort = 9222
	}

	return &ToolsManager{
		screenshotDir: screenshotDir,
		chromePath:    cfg.ChromePath,
		userDataDir:   cfg.UserDataDir,
		defaultPort:   defaultPort,
	}
}

// ToolDefinitions retourne la définition du tool maître browser
// Pattern Progressive Disclosure : 1 tool au lieu de 15
func (m *ToolsManager) ToolDefinitions() []map[string]interface{} {
	return []map[string]interface{}{
		{
			"name":        "browser",
			"description": "Browser automation tool. Actions: launch, connect, navigate, screenshot, evaluate, click, type, wait, get_html, get_url, get_title, cookies, set_cookie, pdf, close, list_actions",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"description": "Action to perform",
						"enum": []string{
							"launch", "connect", "navigate", "screenshot",
							"evaluate", "click", "type", "wait",
							"get_html", "get_url", "get_title",
							"cookies", "set_cookie", "pdf", "close",
							"list_actions",
						},
					},
					"url": map[string]interface{}{
						"type":        "string",
						"description": "URL (for navigate)",
					},
					"selector": map[string]interface{}{
						"type":        "string",
						"description": "CSS selector (for click, type, wait)",
					},
					"text": map[string]interface{}{
						"type":        "string",
						"description": "Text to type",
					},
					"expression": map[string]interface{}{
						"type":        "string",
						"description": "JavaScript expression (for evaluate)",
					},
					"headless": map[string]interface{}{
						"type":        "boolean",
						"default":     true,
						"description": "Headless mode (for launch)",
					},
					"port": map[string]interface{}{
						"type":        "integer",
						"default":     9222,
						"description": "Debug port",
					},
					"timeout": map[string]interface{}{
						"type":        "integer",
						"default":     30,
						"description": "Timeout in seconds (for wait)",
					},
					"format": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"png", "jpeg"},
						"description": "Image format (for screenshot)",
					},
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Save path (for screenshot/pdf)",
					},
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Cookie name (for set_cookie)",
					},
					"value": map[string]interface{}{
						"type":        "string",
						"description": "Cookie value (for set_cookie)",
					},
					"domain": map[string]interface{}{
						"type":        "string",
						"description": "Cookie domain (for set_cookie)",
					},
				},
				"required": []string{"action"},
			},
		},
	}
}

// Execute exécute le tool maître browser avec dispatch sur action
func (m *ToolsManager) Execute(toolName string, args map[string]interface{}) (interface{}, error) {
	if toolName != "browser" {
		return nil, fmt.Errorf("unknown tool: %s (expected 'browser')", toolName)
	}

	action, ok := args["action"].(string)
	if !ok {
		return nil, fmt.Errorf("action parameter is required")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	switch action {
	case "launch":
		return m.launch(args)
	case "connect":
		return m.connect(args)
	case "navigate":
		return m.navigate(args)
	case "screenshot":
		return m.screenshot(args)
	case "evaluate":
		return m.evaluate(args)
	case "click":
		return m.click(args)
	case "type":
		return m.typeText(args)
	case "wait":
		return m.wait(args)
	case "get_html":
		return m.getHTML()
	case "get_url":
		return m.getURL()
	case "get_title":
		return m.getTitle()
	case "cookies":
		return m.getCookies()
	case "set_cookie":
		return m.setCookie(args)
	case "pdf":
		return m.pdf(args)
	case "close":
		return m.close()
	case "list_actions":
		return m.listActions()
	default:
		return nil, fmt.Errorf("unknown action: %s", action)
	}
}

// listActions retourne la liste des actions disponibles
func (m *ToolsManager) listActions() (interface{}, error) {
	return map[string]interface{}{
		"actions": []map[string]interface{}{
			{"name": "launch", "description": "Launch new browser instance", "params": []string{"headless", "port"}},
			{"name": "connect", "description": "Connect to existing browser", "params": []string{"port"}},
			{"name": "navigate", "description": "Navigate to URL", "params": []string{"url"}},
			{"name": "screenshot", "description": "Take screenshot", "params": []string{"format", "path"}},
			{"name": "evaluate", "description": "Execute JavaScript", "params": []string{"expression"}},
			{"name": "click", "description": "Click element", "params": []string{"selector"}},
			{"name": "type", "description": "Type text into element", "params": []string{"selector", "text"}},
			{"name": "wait", "description": "Wait for element", "params": []string{"selector", "timeout"}},
			{"name": "get_html", "description": "Get page HTML", "params": []string{}},
			{"name": "get_url", "description": "Get current URL", "params": []string{}},
			{"name": "get_title", "description": "Get page title", "params": []string{}},
			{"name": "cookies", "description": "Get all cookies", "params": []string{}},
			{"name": "set_cookie", "description": "Set a cookie", "params": []string{"name", "value", "domain"}},
			{"name": "pdf", "description": "Generate PDF", "params": []string{"path"}},
			{"name": "close", "description": "Close browser", "params": []string{}},
		},
		"total": 15,
	}, nil
}

func (m *ToolsManager) launch(args map[string]interface{}) (interface{}, error) {
	if m.browser != nil {
		m.browser.Close()
	}

	cfg := DefaultConfig()

	// Utiliser les chemins depuis Discovery
	cfg.ChromePath = m.chromePath
	cfg.UserDataDir = m.userDataDir
	cfg.DebugPort = m.defaultPort

	// Surcharges depuis les arguments
	if headless, ok := args["headless"].(bool); ok {
		cfg.Headless = headless
	}
	if port, ok := args["port"].(float64); ok {
		cfg.DebugPort = int(port)
	}

	browser, err := Launch(cfg)
	if err != nil {
		return nil, err
	}

	m.browser = browser

	return map[string]interface{}{
		"success":    true,
		"message":    "Browser launched",
		"port":       cfg.DebugPort,
		"headless":   cfg.Headless,
		"chromePath": cfg.ChromePath,
	}, nil
}

func (m *ToolsManager) connect(args map[string]interface{}) (interface{}, error) {
	if m.browser != nil {
		m.browser.Close()
	}

	port := 9222
	if p, ok := args["port"].(float64); ok {
		port = int(p)
	}

	browser, err := Connect(port)
	if err != nil {
		return nil, err
	}

	m.browser = browser

	return map[string]interface{}{
		"success": true,
		"message": "Connected to browser",
		"port":    port,
	}, nil
}

func (m *ToolsManager) navigate(args map[string]interface{}) (interface{}, error) {
	if m.browser == nil {
		return nil, fmt.Errorf("browser not started - use action 'launch' first")
	}

	url, ok := args["url"].(string)
	if !ok {
		return nil, fmt.Errorf("url is required for navigate")
	}

	if err := m.browser.Navigate(url); err != nil {
		return nil, err
	}

	time.Sleep(500 * time.Millisecond)
	title, _ := m.browser.GetTitle()

	return map[string]interface{}{
		"success": true,
		"url":     url,
		"title":   title,
	}, nil
}

func (m *ToolsManager) screenshot(args map[string]interface{}) (interface{}, error) {
	if m.browser == nil {
		return nil, fmt.Errorf("browser not started")
	}

	format := "png"
	if f, ok := args["format"].(string); ok {
		format = f
	}

	fullPage := false
	if fp, ok := args["fullPage"].(bool); ok {
		fullPage = fp
	}

	data, err := m.browser.Screenshot(format, 80, fullPage)
	if err != nil {
		return nil, err
	}

	savePath := ""
	if sp, ok := args["path"].(string); ok && sp != "" {
		savePath = sp
	} else {
		savePath = filepath.Join(m.screenshotDir, fmt.Sprintf("screenshot_%d.%s", time.Now().Unix(), format))
	}

	if err := os.WriteFile(savePath, data, 0644); err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"success": true,
		"path":    savePath,
		"format":  format,
		"size":    len(data),
		"base64":  base64.StdEncoding.EncodeToString(data),
	}, nil
}

func (m *ToolsManager) evaluate(args map[string]interface{}) (interface{}, error) {
	if m.browser == nil {
		return nil, fmt.Errorf("browser not started")
	}

	expr, ok := args["expression"].(string)
	if !ok {
		return nil, fmt.Errorf("expression is required for evaluate")
	}

	result, err := m.browser.Evaluate(expr)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"success": true,
		"result":  result,
	}, nil
}

func (m *ToolsManager) click(args map[string]interface{}) (interface{}, error) {
	if m.browser == nil {
		return nil, fmt.Errorf("browser not started")
	}

	selector, ok := args["selector"].(string)
	if !ok {
		return nil, fmt.Errorf("selector is required for click")
	}

	if err := m.browser.Click(selector); err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"success":  true,
		"selector": selector,
	}, nil
}

func (m *ToolsManager) typeText(args map[string]interface{}) (interface{}, error) {
	if m.browser == nil {
		return nil, fmt.Errorf("browser not started")
	}

	selector, ok := args["selector"].(string)
	if !ok {
		return nil, fmt.Errorf("selector is required for type")
	}

	text, ok := args["text"].(string)
	if !ok {
		return nil, fmt.Errorf("text is required for type")
	}

	if err := m.browser.Type(selector, text); err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"success":  true,
		"selector": selector,
		"length":   len(text),
	}, nil
}

func (m *ToolsManager) wait(args map[string]interface{}) (interface{}, error) {
	if m.browser == nil {
		return nil, fmt.Errorf("browser not started")
	}

	selector, ok := args["selector"].(string)
	if !ok {
		return nil, fmt.Errorf("selector is required for wait")
	}

	timeout := 30 * time.Second
	if t, ok := args["timeout"].(float64); ok {
		timeout = time.Duration(t) * time.Second
	}

	if err := m.browser.WaitForSelector(selector, timeout); err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"success":  true,
		"selector": selector,
	}, nil
}

func (m *ToolsManager) getHTML() (interface{}, error) {
	if m.browser == nil {
		return nil, fmt.Errorf("browser not started")
	}

	html, err := m.browser.GetHTML()
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"success": true,
		"html":    html,
		"length":  len(html),
	}, nil
}

func (m *ToolsManager) getURL() (interface{}, error) {
	if m.browser == nil {
		return nil, fmt.Errorf("browser not started")
	}

	url, err := m.browser.GetURL()
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"success": true,
		"url":     url,
	}, nil
}

func (m *ToolsManager) getTitle() (interface{}, error) {
	if m.browser == nil {
		return nil, fmt.Errorf("browser not started")
	}

	title, err := m.browser.GetTitle()
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"success": true,
		"title":   title,
	}, nil
}

func (m *ToolsManager) getCookies() (interface{}, error) {
	if m.browser == nil {
		return nil, fmt.Errorf("browser not started")
	}

	cookies, err := m.browser.GetCookies()
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"success": true,
		"cookies": cookies,
		"count":   len(cookies),
	}, nil
}

func (m *ToolsManager) setCookie(args map[string]interface{}) (interface{}, error) {
	if m.browser == nil {
		return nil, fmt.Errorf("browser not started")
	}

	name, _ := args["name"].(string)
	value, _ := args["value"].(string)
	domain, _ := args["domain"].(string)
	path := "/"
	if p, ok := args["path"].(string); ok {
		path = p
	}

	if err := m.browser.SetCookie(name, value, domain, path); err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"success": true,
		"name":    name,
		"domain":  domain,
	}, nil
}

func (m *ToolsManager) pdf(args map[string]interface{}) (interface{}, error) {
	if m.browser == nil {
		return nil, fmt.Errorf("browser not started")
	}

	data, err := m.browser.PDF()
	if err != nil {
		return nil, err
	}

	savePath := ""
	if sp, ok := args["path"].(string); ok && sp != "" {
		savePath = sp
	} else {
		savePath = filepath.Join(m.screenshotDir, fmt.Sprintf("page_%d.pdf", time.Now().Unix()))
	}

	if err := os.WriteFile(savePath, data, 0644); err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"success": true,
		"path":    savePath,
		"size":    len(data),
	}, nil
}

func (m *ToolsManager) close() (interface{}, error) {
	if m.browser == nil {
		return map[string]interface{}{
			"success": true,
			"message": "No browser to close",
		}, nil
	}

	if err := m.browser.Close(); err != nil {
		return nil, err
	}

	m.browser = nil

	return map[string]interface{}{
		"success": true,
		"message": "Browser closed",
	}, nil
}

// IsBrowserTool vérifie si c'est le tool maître browser
func IsBrowserTool(name string) bool {
	return name == "browser"
}
