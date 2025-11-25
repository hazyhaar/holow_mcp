// Package observability gère les métriques et l'observabilité
package observability

import (
	"database/sql"
	"os"
	"runtime"
	"sync"
	"time"
)

// Collector collecte et persiste les métriques système
type Collector struct {
	db         *sql.DB
	metadataDB *sql.DB
	outputDB   *sql.DB
	stopChan   chan struct{}

	// Métriques en mémoire pour batch write
	latencies []float64
	mu        sync.Mutex
}

// NewCollector crée un nouveau collecteur de métriques
func NewCollector(lifecycleDB, metadataDB, outputDB *sql.DB) *Collector {
	return &Collector{
		db:         lifecycleDB,
		metadataDB: metadataDB,
		outputDB:   outputDB,
		stopChan:   make(chan struct{}),
		latencies:  make([]float64, 0, 1000),
	}
}

// Start démarre la collecte de métriques
func (c *Collector) Start(interval time.Duration) {
	go c.collectLoop(interval)
}

// collectLoop collecte les métriques à intervalle régulier
func (c *Collector) collectLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopChan:
			return
		case <-ticker.C:
			c.collectSystemMetrics()
		}
	}
}

// collectSystemMetrics collecte les métriques système Go
func (c *Collector) collectSystemMetrics() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// Calculer percentiles si on a des latences
	c.mu.Lock()
	p50, p95, p99 := c.calculatePercentiles()
	c.latencies = c.latencies[:0] // Reset
	c.mu.Unlock()

	// Persister en base
	c.metadataDB.Exec(`
		INSERT INTO system_metrics
		(cpu_percent, memory_used_mb, heap_alloc_mb, heap_sys_mb,
		 goroutines, gc_pause_ms, p50_latency_ms, p95_latency_ms, p99_latency_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		0, // CPU percent (nécessite cgo pour être précis)
		float64(m.Alloc)/1024/1024,
		float64(m.HeapAlloc)/1024/1024,
		float64(m.HeapSys)/1024/1024,
		runtime.NumGoroutine(),
		float64(m.PauseNs[(m.NumGC+255)%256])/1e6, // Dernière pause GC en ms
		p50, p95, p99)
}

// calculatePercentiles calcule les percentiles des latences
func (c *Collector) calculatePercentiles() (p50, p95, p99 float64) {
	if len(c.latencies) == 0 {
		return 0, 0, 0
	}

	// Tri simple pour calcul percentiles
	sorted := make([]float64, len(c.latencies))
	copy(sorted, c.latencies)

	// Bubble sort (suffisant pour ~1000 éléments)
	for i := 0; i < len(sorted)-1; i++ {
		for j := 0; j < len(sorted)-i-1; j++ {
			if sorted[j] > sorted[j+1] {
				sorted[j], sorted[j+1] = sorted[j+1], sorted[j]
			}
		}
	}

	n := len(sorted)
	p50 = sorted[n*50/100]
	p95 = sorted[n*95/100]
	if n > 100 {
		p99 = sorted[n*99/100]
	} else {
		p99 = sorted[n-1]
	}

	return p50, p95, p99
}

// RecordLatency enregistre une latence pour calcul percentiles
// Limite à 10000 entrées pour éviter fuite mémoire
func (c *Collector) RecordLatency(latencyMs float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Si le slice atteint la limite, supprimer les plus anciennes
	const maxLatencies = 10000
	if len(c.latencies) >= maxLatencies {
		// Garder la moitié la plus récente
		copy(c.latencies, c.latencies[maxLatencies/2:])
		c.latencies = c.latencies[:maxLatencies/2]
	}

	c.latencies = append(c.latencies, latencyMs)
}

// RecordMetric enregistre une métrique custom
func (c *Collector) RecordMetric(name, metricType string, value float64, labels map[string]string) error {
	labelsJSON := "{}"
	if labels != nil {
		// Simple JSON encoding
		labelsJSON = "{"
		first := true
		for k, v := range labels {
			if !first {
				labelsJSON += ","
			}
			labelsJSON += `"` + k + `":"` + v + `"`
			first = false
		}
		labelsJSON += "}"
	}

	_, err := c.outputDB.Exec(`
		INSERT INTO metrics_realtime (metric_name, metric_type, value, labels)
		VALUES (?, ?, ?, ?)`,
		name, metricType, value, labelsJSON)
	return err
}

// UpdateHeartbeat met à jour le heartbeat
func (c *Collector) UpdateHeartbeat(status string, requestsProcessed, requestsFailed, toolsLoaded int) error {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	_, err := c.outputDB.Exec(`
		INSERT OR REPLACE INTO heartbeat
		(id, status, pid, started_at, last_heartbeat_at, requests_processed,
		 requests_failed, tools_loaded, memory_mb, goroutines)
		VALUES (1, ?, ?,
		        COALESCE((SELECT started_at FROM heartbeat WHERE id = 1), strftime('%s', 'now')),
		        strftime('%s', 'now'), ?, ?, ?, ?, ?)`,
		status, os.Getpid(), requestsProcessed, requestsFailed, toolsLoaded,
		int(m.Alloc/1024/1024), runtime.NumGoroutine())
	return err
}

// Log enregistre un log structuré
func (c *Collector) Log(level, message, logger string, traceID string, fields map[string]interface{}) {
	fieldsJSON := "{}"
	if fields != nil {
		// Simple JSON encoding
		fieldsJSON = "{"
		first := true
		for k, v := range fields {
			if !first {
				fieldsJSON += ","
			}
			fieldsJSON += `"` + k + `":`
			switch val := v.(type) {
			case string:
				fieldsJSON += `"` + val + `"`
			case int, int64, float64:
				fieldsJSON += string(rune(val.(int)))
			default:
				fieldsJSON += `"` + string(rune(val.(int))) + `"`
			}
			first = false
		}
		fieldsJSON += "}"
	}

	c.db.Exec(`
		INSERT INTO telemetry_logs (level, message, logger, trace_id, fields)
		VALUES (?, ?, ?, ?, ?)`,
		level, message, logger, traceID, fieldsJSON)
}

// RecordSecurityEvent enregistre un événement de sécurité
func (c *Collector) RecordSecurityEvent(eventType, severity, sourceIP, userID, details string) {
	c.db.Exec(`
		INSERT INTO telemetry_security_events
		(event_type, severity, source_ip, user_id, details)
		VALUES (?, ?, ?, ?, ?)`,
		eventType, severity, sourceIP, userID, details)
}

// CheckPoisonPill vérifie si le shutdown est demandé
func (c *Collector) CheckPoisonPill() (bool, string) {
	var triggered int
	var reason sql.NullString

	err := c.metadataDB.QueryRow(`
		SELECT triggered, reason FROM poisonpill WHERE id = 1`).Scan(&triggered, &reason)
	if err != nil {
		return false, ""
	}

	if triggered == 1 {
		return true, reason.String
	}
	return false, ""
}

// TriggerPoisonPill déclenche le shutdown gracieux
func (c *Collector) TriggerPoisonPill(reason, triggeredBy string) error {
	_, err := c.metadataDB.Exec(`
		UPDATE poisonpill
		SET triggered = 1, reason = ?, triggered_by = ?,
		    triggered_at = strftime('%s', 'now')
		WHERE id = 1`,
		reason, triggeredBy)
	return err
}

// Stop arrête le collecteur
func (c *Collector) Stop() {
	close(c.stopChan)
}

// AlertChecker vérifie les règles d'alerte
type AlertChecker struct {
	metadataDB *sql.DB
	outputDB   *sql.DB
}

// NewAlertChecker crée un nouveau vérificateur d'alertes
func NewAlertChecker(metadataDB, outputDB *sql.DB) *AlertChecker {
	return &AlertChecker{
		metadataDB: metadataDB,
		outputDB:   outputDB,
	}
}

// CheckAlerts vérifie toutes les règles d'alerte actives
func (a *AlertChecker) CheckAlerts() error {
	rows, err := a.metadataDB.Query(`
		SELECT id, name, metric_name, condition, threshold, severity,
		       duration_seconds, cooldown_seconds, last_triggered_at
		FROM alert_rules
		WHERE enabled = 1`)
	if err != nil {
		return err
	}
	defer rows.Close()

	now := time.Now().Unix()

	for rows.Next() {
		var id int
		var name, metricName, condition, severity string
		var threshold float64
		var durationSeconds, cooldownSeconds int
		var lastTriggered sql.NullInt64

		err := rows.Scan(&id, &name, &metricName, &condition, &threshold,
			&severity, &durationSeconds, &cooldownSeconds, &lastTriggered)
		if err != nil {
			continue
		}

		// Vérifier cooldown
		if lastTriggered.Valid && now-lastTriggered.Int64 < int64(cooldownSeconds) {
			continue
		}

		// Récupérer valeur métrique
		var value float64
		err = a.outputDB.QueryRow(`
			SELECT value FROM metrics_realtime
			WHERE metric_name = ?
			ORDER BY created_at DESC LIMIT 1`, metricName).Scan(&value)
		if err != nil {
			continue
		}

		// Évaluer condition
		triggered := false
		switch condition {
		case "gt":
			triggered = value > threshold
		case "lt":
			triggered = value < threshold
		case "eq":
			triggered = value == threshold
		case "ne":
			triggered = value != threshold
		}

		if triggered {
			// Créer alerte
			a.outputDB.Exec(`
				INSERT INTO alert_events
				(alert_rule_id, severity, title, message, metric_name, metric_value, threshold_value)
				VALUES (?, ?, ?, ?, ?, ?, ?)`,
				id, severity, name,
				metricName+" "+condition+" "+string(rune(int(threshold))),
				metricName, value, threshold)

			// Mettre à jour last_triggered_at
			a.metadataDB.Exec(`
				UPDATE alert_rules SET last_triggered_at = strftime('%s', 'now')
				WHERE id = ?`, id)
		}
	}

	return nil
}
