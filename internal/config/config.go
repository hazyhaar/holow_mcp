// Package config gère la configuration du serveur
package config

import (
	"database/sql"
	"strconv"
)

// Config représente la configuration du serveur
type Config struct {
	ServerName            string
	ServerVersion         string
	PollingIntervalMs     int
	HeartbeatIntervalSecs int
	ShutdownTimeoutSecs   int
	CacheDefaultTTLSecs   int
	RetryMaxAttempts      int
	CircuitBreakerThreshold int
}

// Load charge la configuration depuis la base
func Load(db *sql.DB) (*Config, error) {
	cfg := &Config{
		// Valeurs par défaut
		ServerName:            "holow-mcp",
		ServerVersion:         "1.0.0",
		PollingIntervalMs:     2000,
		HeartbeatIntervalSecs: 15,
		ShutdownTimeoutSecs:   60,
		CacheDefaultTTLSecs:   3600,
		RetryMaxAttempts:      3,
		CircuitBreakerThreshold: 5,
	}

	rows, err := db.Query(`SELECT key, value FROM config`)
	if err != nil {
		return cfg, err
	}
	defer rows.Close()

	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			continue
		}

		switch key {
		case "server.name":
			cfg.ServerName = value
		case "server.version":
			cfg.ServerVersion = value
		case "polling.interval_ms":
			cfg.PollingIntervalMs, _ = strconv.Atoi(value)
		case "heartbeat.interval_seconds":
			cfg.HeartbeatIntervalSecs, _ = strconv.Atoi(value)
		case "shutdown.timeout_seconds":
			cfg.ShutdownTimeoutSecs, _ = strconv.Atoi(value)
		case "cache.default_ttl_seconds":
			cfg.CacheDefaultTTLSecs, _ = strconv.Atoi(value)
		case "retry.max_attempts":
			cfg.RetryMaxAttempts, _ = strconv.Atoi(value)
		case "circuit_breaker.failure_threshold":
			cfg.CircuitBreakerThreshold, _ = strconv.Atoi(value)
		}
	}

	return cfg, nil
}

// Save sauvegarde une valeur de configuration
func Save(db *sql.DB, key, value string) error {
	_, err := db.Exec(`
		UPDATE config SET value = ?, updated_at = strftime('%s', 'now')
		WHERE key = ?`, value, key)
	return err
}

// Get récupère une valeur de configuration
func Get(db *sql.DB, key string) (string, error) {
	var value string
	err := db.QueryRow(`SELECT value FROM config WHERE key = ?`, key).Scan(&value)
	return value, err
}

// GetInt récupère une valeur entière de configuration
func GetInt(db *sql.DB, key string) (int, error) {
	value, err := Get(db, key)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(value)
}
