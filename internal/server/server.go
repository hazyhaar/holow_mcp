// Package server implémente le serveur MCP JSON-RPC
package server

import (
	"bufio"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/horos/holow-mcp/internal/brainloop"
	"github.com/horos/holow-mcp/internal/chromium"
	"github.com/horos/holow-mcp/internal/circuit"
	"github.com/horos/holow-mcp/internal/database"
	"github.com/horos/holow-mcp/internal/discovery"
	"github.com/horos/holow-mcp/internal/initcli"
	"github.com/horos/holow-mcp/internal/observability"
	"github.com/horos/holow-mcp/internal/tools"
)

// Server représente le serveur MCP HOLOW
type Server struct {
	db         *database.Manager
	cdpManager *chromium.CDPManager
	tools      *tools.Manager
	circuits   *circuit.Manager
	metrics    *observability.Collector
	alerts     *observability.AlertChecker
	browser    *chromium.ToolsManager
	brainloop  *brainloop.ToolsManager
	appConfig  *initcli.AppConfig

	stdin  io.Reader
	stdout io.Writer

	basePath          string
	requestsProcessed int64
	requestsFailed    int64

	shutdownChan chan struct{}
	wg           sync.WaitGroup
}

// JSONRPCRequest représente une requête JSON-RPC
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse représente une réponse JSON-RPC
type JSONRPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *RPCError   `json:"error,omitempty"`
}

// RPCError représente une erreur JSON-RPC
type RPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// NewServer crée un nouveau serveur MCP
func NewServer(basePath string) (*Server, error) {
	// Étape 1: Créer le CDPManager avec db = nil (sera configuré après)
	cdpMgr := chromium.NewCDPManager(nil)

	// Étape 2: Créer le callback CDP qui enregistre les fonctions
	// Avec modernc.org/sqlite, les fonctions sont gérées globalement
	cdpCallback := func(db *sql.DB) error {
		return chromium.RegisterCDPFunctions(db, cdpMgr)
	}

	// Étape 3: Créer le database.Manager avec le callback
	db, err := database.NewManager(basePath, cdpCallback)
	if err != nil {
		return nil, fmt.Errorf("failed to create database manager: %w", err)
	}

	// Étape 4: Configurer le CDPManager avec la base LifecycleTools maintenant ouverte
	cdpMgr.SetDB(db.LifecycleTools)

	// Étape 5: Récupération et migrations au boot
	schemasPath := filepath.Join(basePath, "schemas")
	if _, err := os.Stat(schemasPath); os.IsNotExist(err) {
		// Fallback: chercher dans le répertoire de l'exécutable
		if execPath, err := os.Executable(); err == nil {
			schemasPath = filepath.Join(filepath.Dir(execPath), "..", "..", "schemas")
		}
	}
	if err := db.RecoverAndMigrate(schemasPath); err != nil {
		fmt.Fprintf(os.Stderr, "[warn] recovery/migration: %v\n", err)
	}

	// Découverte système au démarrage
	disco := discovery.New(db.LifecycleCore)
	if err := disco.Run(); err != nil {
		// Log mais ne bloque pas - chromium sera indisponible
		fmt.Fprintf(os.Stderr, "discovery warning: %v\n", err)
	}

	// Configuration Chromium depuis Discovery
	browserCfg := &chromium.ToolsConfig{
		ChromePath:  disco.GetChromiumPath(),
		UserDataDir: disco.GetUserDataDir(),
		DefaultPort: disco.GetDefaultPort(),
	}

	// Créer brainloop avec accès aux DBs
	brainloopMgr := brainloop.NewToolsManager()
	brainloopMgr.SetToolsDB(db.LifecycleTools)
	brainloopMgr.SetExecDB(db.LifecycleExec)

	return &Server{
		db:           db,
		cdpManager:   cdpMgr,
		tools:        tools.NewManager(db.LifecycleTools),
		circuits:     circuit.NewManager(db.LifecycleExec),
		metrics:      observability.NewCollector(db.LifecycleCore, db.Metadata, db.Output),
		alerts:       observability.NewAlertChecker(db.Metadata, db.Output),
		browser:      chromium.NewToolsManager(browserCfg),
		brainloop:    brainloopMgr,
		basePath:     basePath,
		stdin:        os.Stdin,
		stdout:       os.Stdout,
		shutdownChan: make(chan struct{}),
	}, nil
}

// NewServerWithConfig crée un nouveau serveur MCP avec une configuration
func NewServerWithConfig(basePath string, appConfig *initcli.AppConfig) (*Server, error) {
	srv, err := NewServer(basePath)
	if err != nil {
		return nil, err
	}

	srv.appConfig = appConfig
	srv.basePath = basePath

	return srv, nil
}

// Start démarre le serveur MCP
func (s *Server) Start(ctx context.Context) error {
	// Démarrer les composants
	if err := s.tools.Start(2 * time.Second); err != nil {
		return fmt.Errorf("failed to start tools manager: %w", err)
	}

	if err := s.circuits.LoadAll(); err != nil {
		return fmt.Errorf("failed to load circuit breakers: %w", err)
	}

	s.metrics.Start(5 * time.Second)

	// Heartbeat initial
	s.metrics.UpdateHeartbeat("running",
		int(atomic.LoadInt64(&s.requestsProcessed)),
		int(atomic.LoadInt64(&s.requestsFailed)),
		s.tools.Count())

	// Goroutine heartbeat
	go s.heartbeatLoop()

	// Goroutine vérification poison pill
	go s.poisonPillLoop()

	// Goroutine traitement commandes CDP en arrière-plan
	go s.cdpProcessLoop()

	// Gestion signaux
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		<-sigChan
		s.Shutdown()
	}()

	// Boucle principale stdin
	return s.readLoop(ctx)
}

// readLoop lit les requêtes JSON-RPC depuis stdin
func (s *Server) readLoop(ctx context.Context) error {
	scanner := bufio.NewScanner(s.stdin)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB buffer

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.shutdownChan:
			return nil
		default:
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		s.wg.Add(1)
		go func(data []byte) {
			defer s.wg.Done()
			s.handleRequest(data)
		}(line)
	}

	// Attendre que toutes les requêtes soient traitées
	s.wg.Wait()

	return scanner.Err()
}

// handleRequest traite une requête JSON-RPC
func (s *Server) handleRequest(data []byte) {
	start := time.Now()

	var req JSONRPCRequest
	if err := json.Unmarshal(data, &req); err != nil {
		s.sendError(nil, -32700, "Parse error", err.Error())
		return
	}

	// Méthodes MCP standard exclues de l'idempotence (doivent toujours retourner l'état actuel)
	skipIdempotence := map[string]bool{
		"initialize":     true,
		"tools/list":     true,
		"resources/list": true,
		"prompts/list":   true,
		"ping":           true,
	}

	// Calculer hash pour idempotence
	hash := s.hashRequest(req.Method, req.Params)

	// Vérifier idempotence uniquement pour tools/call et autres méthodes mutatives
	if !skipIdempotence[req.Method] {
		processed, err := s.db.CheckProcessed(hash)
		if err != nil {
			s.sendError(req.ID, -32603, "Internal error", err.Error())
			return
		}

		if processed {
			// Retourner résultat existant
			s.sendResult(req.ID, map[string]interface{}{
				"cached":  true,
				"message": "Request already processed",
			})
			return
		}
	}

	// Router la requête
	var result interface{}
	var rpcErr *RPCError

	switch req.Method {
	case "initialize":
		result, rpcErr = s.handleInitialize(req.Params)
	case "tools/list":
		result, rpcErr = s.handleToolsList()
	case "tools/call":
		result, rpcErr = s.handleToolsCall(req.Params, hash)
	case "resources/list":
		result, rpcErr = s.handleResourcesList()
	case "prompts/list":
		result, rpcErr = s.handlePromptsList()
	default:
		rpcErr = &RPCError{Code: -32601, Message: "Method not found"}
	}

	// Enregistrer latence
	latencyMs := float64(time.Since(start).Milliseconds())
	s.metrics.RecordLatency(latencyMs)

	if rpcErr != nil {
		atomic.AddInt64(&s.requestsFailed, 1)
		s.sendError(req.ID, rpcErr.Code, rpcErr.Message, rpcErr.Data)
		s.db.MarkProcessed(hash, fmt.Sprintf("%v", req.ID), req.Method, "failed", "", int64(latencyMs))
		return
	}

	atomic.AddInt64(&s.requestsProcessed, 1)

	// Calculer hash résultat
	resultJSON, _ := json.Marshal(result)
	resultHash := sha256.Sum256(resultJSON)
	resultHashStr := hex.EncodeToString(resultHash[:])

	// Marquer comme traité
	s.db.MarkProcessed(hash, fmt.Sprintf("%v", req.ID), req.Method, "success", resultHashStr, int64(latencyMs))

	s.sendResult(req.ID, result)
}

// hashRequest calcule le hash d'une requête pour idempotence
func (s *Server) hashRequest(method string, params json.RawMessage) string {
	data := map[string]interface{}{
		"method": method,
		"params": string(params),
	}
	jsonData, _ := json.Marshal(data)
	hash := sha256.Sum256(jsonData)
	return hex.EncodeToString(hash[:])
}

// handleInitialize traite la requête initialize
func (s *Server) handleInitialize(params json.RawMessage) (interface{}, *RPCError) {
	return map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"serverInfo": map[string]interface{}{
			"name":    "holow-mcp",
			"version": "1.0.0",
		},
		"capabilities": map[string]interface{}{
			"tools":     map[string]interface{}{"listChanged": true},
			"resources": map[string]interface{}{"subscribe": false, "listChanged": false},
			"prompts":   map[string]interface{}{"listChanged": false},
		},
	}, nil
}

// handleToolsList retourne la liste des tools
func (s *Server) handleToolsList() (interface{}, *RPCError) {
	// Combiner les tools codés en dur + les tools SQL dynamiques
	allTools := make([]map[string]interface{}, 0, 20)

	// Tool Browser (16 actions hardcodées)
	browserTools := s.browser.ToolDefinitions()
	allTools = append(allTools, browserTools...)

	// Tool Brainloop (18 actions incluant système)
	brainloopTools := s.brainloop.ToolDefinitions()
	allTools = append(allTools, brainloopTools...)

	// Tools SQL dynamiques (depuis tool_definitions table)
	sqlTools := s.tools.GetAllToolDefinitions()
	for _, tool := range sqlTools {
		allTools = append(allTools, tool.ToMCPSchema())
	}

	return map[string]interface{}{"tools": allTools}, nil
}

// handleToolsCall exécute un tool
func (s *Server) handleToolsCall(params json.RawMessage, requestHash string) (interface{}, *RPCError) {
	var callParams struct {
		Name      string                 `json:"name"`
		Arguments map[string]interface{} `json:"arguments"`
	}

	if err := json.Unmarshal(params, &callParams); err != nil {
		return nil, &RPCError{Code: -32602, Message: "Invalid params", Data: err.Error()}
	}

	// Vérifier si c'est un tool browser
	if chromium.IsBrowserTool(callParams.Name) {
		result, err := s.browser.Execute(callParams.Name, callParams.Arguments)
		if err != nil {
			return nil, &RPCError{Code: -32000, Message: "Browser tool failed", Data: err.Error()}
		}

		resultJSON, _ := json.Marshal(result)
		return map[string]interface{}{
			"content": []map[string]interface{}{
				{
					"type": "text",
					"text": string(resultJSON),
				},
			},
		}, nil
	}

	// Vérifier si c'est un tool brainloop
	if brainloop.IsBrainloopTool(callParams.Name) {
		result, err := s.brainloop.Execute(callParams.Name, callParams.Arguments)
		if err != nil {
			return nil, &RPCError{Code: -32000, Message: "Brainloop tool failed", Data: err.Error()}
		}

		resultJSON, _ := json.Marshal(result)
		return map[string]interface{}{
			"content": []map[string]interface{}{
				{
					"type": "text",
					"text": string(resultJSON),
				},
			},
		}, nil
	}

	// Récupérer le tool personnalisé
	tool, ok := s.tools.Get(callParams.Name)
	if !ok {
		return nil, &RPCError{Code: -32602, Message: "Tool not found", Data: callParams.Name}
	}

	// Vérifier circuit breaker
	breaker := s.circuits.Get(callParams.Name)
	if canExec, err := breaker.CanExecute(); !canExec {
		s.metrics.RecordSecurityEvent("circuit_open", "warning", "", "", err.Error())
		return nil, &RPCError{Code: -32000, Message: "Circuit breaker open", Data: err.Error()}
	}

	// Exécuter le tool
	result, err := s.executeTool(tool, callParams.Arguments)
	if err != nil {
		breaker.RecordFailure(s.db.LifecycleExec)
		return nil, &RPCError{Code: -32000, Message: "Tool execution failed", Data: err.Error()}
	}

	breaker.RecordSuccess(s.db.LifecycleExec)

	// Persister résultat
	resultJSON, _ := json.Marshal(result)
	resultHash := sha256.Sum256(resultJSON)
	resultHashStr := hex.EncodeToString(resultHash[:])

	s.db.Output.Exec(`
		INSERT INTO tool_results (hash, request_id, tool_name, result_json, result_type)
		VALUES (?, ?, ?, ?, 'success')`,
		resultHashStr, requestHash, callParams.Name, string(resultJSON))

	return map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": string(resultJSON),
			},
		},
	}, nil
}

// executeTool exécute les steps d'un tool
func (s *Server) executeTool(tool *tools.Tool, args map[string]interface{}) (interface{}, error) {
	if len(tool.Steps) == 0 {
		return map[string]interface{}{
			"message": "Tool executed (no steps defined)",
			"tool":    tool.Name,
			"args":    args,
		}, nil
	}

	// Exécuter chaque step
	var lastResult interface{}
	for _, step := range tool.Steps {
		// Substituer les paramètres dans le template SQL
		sql := s.substituteParams(step.SQLTemplate, args)

		var err error
		var result interface{}

		switch step.StepType {
		case "validate":
			// Les validations utilisent RAISE pour échouer
			_, err = s.db.LifecycleTools.Exec(sql)
			if err != nil {
				return nil, fmt.Errorf("validation failed at step %s: %w", step.Name, err)
			}
			result = map[string]interface{}{"validated": true}

		case "sql":
			// Exécuter et récupérer résultat
			result, err = s.executeSQL(sql)
			if err != nil {
				return nil, fmt.Errorf("SQL execution failed at step %s: %w", step.Name, err)
			}

		case "attach":
			// ATTACH temporaire
			result = map[string]interface{}{"attached": true}

		case "transform":
			// Transformation de données
			result = map[string]interface{}{"transformed": true}

		default:
			return nil, fmt.Errorf("unknown step type: %s", step.StepType)
		}

		lastResult = result
	}

	return lastResult, nil
}

// sanitizeSQLValue échappe une valeur pour insertion sécurisée dans SQL
// Protège contre les injections SQL en échappant les guillemets simples
// Note: SQLite n'utilise PAS backslash comme caractère d'échappement
func sanitizeSQLValue(value string) string {
	// Supprimer les NULL bytes qui peuvent tronquer les chaînes
	value = strings.ReplaceAll(value, "\x00", "")

	// Échapper les guillemets simples (standard SQL: ' -> '')
	// Note: Ne PAS échapper les backslashes - SQLite ne les interprète pas
	value = strings.ReplaceAll(value, "'", "''")

	// Supprimer les caractères de contrôle dangereux (sauf newline et tab)
	var sanitized strings.Builder
	for _, r := range value {
		// Autoriser: printable ASCII, newline, tab, et caractères Unicode valides
		if r == '\n' || r == '\t' || r == '\r' || (r >= 32 && r < 127) || r >= 128 {
			sanitized.WriteRune(r)
		}
	}

	return sanitized.String()
}

// escapeJSONValue échappe une valeur pour insertion dans une chaîne JSON
// Utilisé quand le placeholder est à l'intérieur d'un json_object() ou d'une chaîne JSON
func escapeJSONValue(value string) string {
	var result strings.Builder
	result.Grow(len(value) + 10)

	for _, r := range value {
		switch r {
		case '\\':
			result.WriteString("\\\\")
		case '"':
			result.WriteString("\\\"")
		case '\n':
			result.WriteString("\\n")
		case '\r':
			result.WriteString("\\r")
		case '\t':
			result.WriteString("\\t")
		case '\x00':
			// Ignorer les NULL bytes
		default:
			if r < 32 {
				// Caractères de contrôle: encoder en unicode escape
				result.WriteString(fmt.Sprintf("\\u%04x", r))
			} else {
				result.WriteRune(r)
			}
		}
	}

	return result.String()
}

// validateParamKey vérifie qu'un nom de paramètre est valide
func validateParamKey(key string) bool {
	if len(key) == 0 || len(key) > 64 {
		return false
	}
	for i, r := range key {
		if i == 0 {
			// Premier caractère: lettre ou underscore
			if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_') {
				return false
			}
		} else {
			// Caractères suivants: lettre, chiffre ou underscore
			if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_') {
				return false
			}
		}
	}
	return true
}

// isInJavaScriptContext vérifie si un placeholder est dans un contexte JavaScript/JSON
// (par ex. inside json_object('expression', '...{{param}}...'))
func isInJavaScriptContext(template, placeholder string) bool {
	idx := strings.Index(template, placeholder)
	if idx == -1 {
		return false
	}

	// Regarder le contexte avant le placeholder (max 200 caractères)
	lookback := 200
	if idx < lookback {
		lookback = idx
	}
	context := strings.ToLower(template[idx-lookback : idx])

	// Indicateurs de contexte JavaScript/JSON
	jsIndicators := []string{
		"expression",
		"document.",
		"window.",
		"json.stringify",
		".queryselector",
		".click()",
		".focus()",
		".value",
		"innertext",
		"innerhtml",
	}

	for _, indicator := range jsIndicators {
		if strings.Contains(context, indicator) {
			return true
		}
	}

	return false
}

// substituteParams remplace les {{param}} par leurs valeurs de façon sécurisée
func (s *Server) substituteParams(template string, args map[string]interface{}) string {
	result := template
	for key, value := range args {
		// Valider le nom du paramètre
		if !validateParamKey(key) {
			continue
		}

		placeholder := "{{" + key + "}}"
		var strValue string
		switch v := value.(type) {
		case string:
			strValue = v
		case float64:
			strValue = fmt.Sprintf("%v", v)
		case int:
			strValue = fmt.Sprintf("%d", v)
		case int64:
			strValue = fmt.Sprintf("%d", v)
		case bool:
			if v {
				strValue = "1"
			} else {
				strValue = "0"
			}
		case nil:
			strValue = ""
		default:
			jsonBytes, err := json.Marshal(v)
			if err != nil {
				continue // Ignorer les valeurs non sérialisables
			}
			strValue = string(jsonBytes)
		}

		// Limiter la longueur des valeurs pour éviter les attaques DoS
		const maxValueLen = 65536 // 64KB max par valeur
		if len(strValue) > maxValueLen {
			strValue = strValue[:maxValueLen]
		}

		// Déterminer le type d'échappement nécessaire
		if isInJavaScriptContext(result, placeholder) {
			// Contexte JavaScript: échapper pour JS d'abord, puis SQL
			strValue = escapeJSONValue(strValue)
		}

		// Toujours appliquer l'échappement SQL (guillemets simples)
		strValue = sanitizeSQLValue(strValue)

		result = strings.ReplaceAll(result, placeholder, strValue)
	}

	// Remplacer les placeholders non fournis par des chaînes vides
	for {
		start := strings.Index(result, "{{")
		if start == -1 {
			break
		}
		end := strings.Index(result[start:], "}}")
		if end == -1 {
			break
		}
		result = result[:start] + result[start+end+2:]
	}

	return result
}

// executeSQL exécute une requête SQL et retourne le résultat
func (s *Server) executeSQL(sql string) (interface{}, error) {
	trimmed := strings.TrimSpace(sql)
	isSelect := strings.HasPrefix(strings.ToUpper(trimmed), "SELECT")

	if isSelect {
		rows, err := s.db.LifecycleTools.Query(sql)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		columns, err := rows.Columns()
		if err != nil {
			return nil, err
		}

		var results []map[string]interface{}
		for rows.Next() {
			values := make([]interface{}, len(columns))
			valuePtrs := make([]interface{}, len(columns))
			for i := range values {
				valuePtrs[i] = &values[i]
			}

			if err := rows.Scan(valuePtrs...); err != nil {
				return nil, err
			}

			row := make(map[string]interface{})
			for i, col := range columns {
				val := values[i]
				if b, ok := val.([]byte); ok {
					row[col] = string(b)
				} else {
					row[col] = val
				}
			}
			results = append(results, row)
		}

		// Si un seul résultat avec une seule colonne JSON, parser
		if len(results) == 1 && len(columns) == 1 {
			val := results[0][columns[0]]
			if str, ok := val.(string); ok && len(str) > 0 && (str[0] == '{' || str[0] == '[') {
				var parsed interface{}
				if json.Unmarshal([]byte(str), &parsed) == nil {
					return parsed, nil
				}
			}
			return val, nil
		}

		return results, nil
	}

	// Exécution (INSERT, UPDATE, DELETE)
	result, err := s.db.LifecycleTools.Exec(sql)
	if err != nil {
		return nil, err
	}

	rowsAffected, _ := result.RowsAffected()
	lastID, _ := result.LastInsertId()

	return map[string]interface{}{
		"rows_affected":  rowsAffected,
		"last_insert_id": lastID,
	}, nil
}

// handleResourcesList retourne la liste des ressources
func (s *Server) handleResourcesList() (interface{}, *RPCError) {
	return map[string]interface{}{"resources": []interface{}{}}, nil
}

// handlePromptsList retourne la liste des prompts
func (s *Server) handlePromptsList() (interface{}, *RPCError) {
	return map[string]interface{}{"prompts": []interface{}{}}, nil
}

// sendResult envoie une réponse succès
func (s *Server) sendResult(id interface{}, result interface{}) {
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
	s.send(resp)
}

// sendError envoie une réponse erreur
func (s *Server) sendError(id interface{}, code int, message string, data interface{}) {
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &RPCError{
			Code:    code,
			Message: message,
			Data:    data,
		},
	}
	s.send(resp)
}

// send envoie une réponse JSON-RPC
func (s *Server) send(resp JSONRPCResponse) {
	data, err := json.Marshal(resp)
	if err != nil {
		return
	}
	fmt.Fprintln(s.stdout, string(data))
}

// heartbeatLoop envoie un heartbeat toutes les 15 secondes
func (s *Server) heartbeatLoop() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.shutdownChan:
			return
		case <-ticker.C:
			s.metrics.UpdateHeartbeat("running",
				int(atomic.LoadInt64(&s.requestsProcessed)),
				int(atomic.LoadInt64(&s.requestsFailed)),
				s.tools.Count())
		}
	}
}

// poisonPillLoop vérifie la table poisonpill
func (s *Server) poisonPillLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.shutdownChan:
			return
		case <-ticker.C:
			if triggered, reason := s.metrics.CheckPoisonPill(); triggered {
				fmt.Fprintf(os.Stderr, "Poison pill triggered: %s\n", reason)
				s.Shutdown()
				return
			}
		}
	}
}

// cdpProcessLoop traite les commandes CDP en attente toutes les 100ms
func (s *Server) cdpProcessLoop() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-s.shutdownChan:
			return
		case <-ticker.C:
			if err := s.cdpManager.ProcessPendingCommands(); err != nil {
				// Log l'erreur mais continue (ne fait pas tomber le serveur)
				fmt.Fprintf(os.Stderr, "CDP process error: %v\n", err)
			}
		}
	}
}

// Shutdown arrête gracieusement le serveur
func (s *Server) Shutdown() {
	close(s.shutdownChan)

	// Mettre à jour heartbeat
	s.metrics.UpdateHeartbeat("shutting_down",
		int(atomic.LoadInt64(&s.requestsProcessed)),
		int(atomic.LoadInt64(&s.requestsFailed)),
		s.tools.Count())

	// Attendre les requêtes en cours (max 60s)
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Toutes les requêtes terminées
	case <-time.After(60 * time.Second):
		fmt.Fprintln(os.Stderr, "Shutdown timeout exceeded, forcing shutdown")
		// La goroutine reste bloquée mais on continue le shutdown
		// Elle sera terminée avec le process
	}

	// Arrêter les composants
	s.tools.Stop()
	s.metrics.Stop()

	// Déconnecter le browser CDP
	if err := s.cdpManager.Disconnect(); err != nil {
		fmt.Fprintf(os.Stderr, "CDP disconnect error: %v\n", err)
	}

	// Heartbeat final AVANT fermeture des bases
	s.db.Output.Exec(`
		UPDATE heartbeat SET status = 'stopped',
		last_heartbeat_at = strftime('%s', 'now')
		WHERE id = 1`)

	// Checkpoint WAL
	s.db.Checkpoint()

	// Backup automatique si configuré
	if s.appConfig != nil && s.appConfig.BackupEnabled {
		fmt.Fprintln(os.Stderr, "Creating backup...")
		backupFile, err := s.appConfig.CreateBackupNow()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Backup error: %v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "Backup created: %s\n", backupFile)
		}
	}

	// Fermer les bases
	s.db.Close()
}

// GetCredential récupère une clé API depuis la configuration
func (s *Server) GetCredential(provider string) (string, error) {
	if s.appConfig == nil {
		return "", fmt.Errorf("configuration non chargée")
	}
	return s.appConfig.GetCredential(provider)
}

// AddRetryJob ajoute un job à la queue de retry
func (s *Server) AddRetryJob(requestID, toolName string, params map[string]interface{}, maxAttempts int) error {
	paramsJSON, _ := json.Marshal(params)

	_, err := s.db.LifecycleExec.Exec(`
		INSERT INTO retry_queue
		(request_id, tool_name, params_json, max_attempts, next_retry_at, backoff_seconds)
		VALUES (?, ?, ?, ?, strftime('%s', 'now') + 2, 2)`,
		requestID, toolName, string(paramsJSON), maxAttempts)

	return err
}

// ProcessRetryQueue traite la queue de retry
func (s *Server) ProcessRetryQueue() error {
	rows, err := s.db.LifecycleExec.Query(`
		SELECT id, request_id, tool_name, params_json, attempt_number, max_attempts, backoff_seconds
		FROM retry_queue
		WHERE status = 'pending' AND next_retry_at <= strftime('%s', 'now')
		LIMIT 10`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var id int
		var requestID, toolName, paramsJSON string
		var attempt, maxAttempts, backoff int

		if err := rows.Scan(&id, &requestID, &toolName, &paramsJSON, &attempt, &maxAttempts, &backoff); err != nil {
			continue
		}

		// Marquer comme processing
		s.db.LifecycleExec.Exec(`UPDATE retry_queue SET status = 'processing' WHERE id = ?`, id)

		// Récupérer tool et exécuter
		tool, ok := s.tools.Get(toolName)
		if !ok {
			s.db.LifecycleExec.Exec(`
				UPDATE retry_queue SET status = 'exhausted', last_error = 'Tool not found'
				WHERE id = ?`, id)
			continue
		}

		var params map[string]interface{}
		json.Unmarshal([]byte(paramsJSON), &params)

		_, err := s.executeTool(tool, params)
		if err != nil {
			// Échec
			if attempt >= maxAttempts {
				// Déplacer vers dead letter queue
				s.db.Output.Exec(`
					INSERT INTO dead_letter_queue
					(request_id, tool_name, params_json, error_message, attempts, first_attempt_at, last_attempt_at)
					VALUES (?, ?, ?, ?, ?, ?, strftime('%s', 'now'))`,
					requestID, toolName, paramsJSON, err.Error(), attempt, 0)

				s.db.LifecycleExec.Exec(`UPDATE retry_queue SET status = 'exhausted' WHERE id = ?`, id)
			} else {
				// Programmer prochain retry (exponential backoff)
				nextBackoff := backoff * 2
				s.db.LifecycleExec.Exec(`
					UPDATE retry_queue
					SET status = 'pending', attempt_number = ?, backoff_seconds = ?,
					    next_retry_at = strftime('%s', 'now') + ?, last_error = ?
					WHERE id = ?`,
					attempt+1, nextBackoff, nextBackoff, err.Error(), id)
			}
		} else {
			// Succès
			s.db.LifecycleExec.Exec(`DELETE FROM retry_queue WHERE id = ?`, id)
		}
	}

	return nil
}
