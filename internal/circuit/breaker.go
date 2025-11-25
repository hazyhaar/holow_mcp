// Package circuit implémente le pattern circuit breaker
package circuit

import (
	"database/sql"
	"fmt"
	"log"
	"sync"
	"time"
)

// execOrLog exécute une requête SQL et log l'erreur si elle échoue
// Utilisé pour les opérations de persistance non critiques
func execOrLog(db *sql.DB, query string, args ...interface{}) {
	_, err := db.Exec(query, args...)
	if err != nil {
		log.Printf("[circuit-breaker] SQL exec error: %v (query: %s)", err, query)
	}
}

// State représente l'état du circuit breaker
type State string

const (
	StateClosed   State = "closed"
	StateOpen     State = "open"
	StateHalfOpen State = "half_open"
)

// Breaker gère un circuit breaker pour un service
type Breaker struct {
	name             string
	state            State
	failureCount     int
	successCount     int
	failureThreshold int
	successThreshold int
	timeoutSeconds   int
	lastStateChange  time.Time
	halfOpenMaxCalls int
	halfOpenCalls    int
	mu               sync.RWMutex
}

// Manager gère tous les circuit breakers
type Manager struct {
	db       *sql.DB
	breakers map[string]*Breaker
	mu       sync.RWMutex
}

// NewManager crée un nouveau gestionnaire de circuit breakers
func NewManager(db *sql.DB) *Manager {
	return &Manager{
		db:       db,
		breakers: make(map[string]*Breaker),
	}
}

// LoadAll charge tous les circuit breakers depuis la base
func (m *Manager) LoadAll() error {
	rows, err := m.db.Query(`
		SELECT name, state, failure_count, success_count,
		       failure_threshold, success_threshold, timeout_seconds,
		       last_state_change_at, half_open_max_calls
		FROM circuit_breakers`)
	if err != nil {
		return err
	}
	defer rows.Close()

	m.mu.Lock()
	defer m.mu.Unlock()

	for rows.Next() {
		var b Breaker
		var stateStr string
		var lastChange int64

		err := rows.Scan(
			&b.name, &stateStr, &b.failureCount, &b.successCount,
			&b.failureThreshold, &b.successThreshold, &b.timeoutSeconds,
			&lastChange, &b.halfOpenMaxCalls)
		if err != nil {
			return err
		}

		b.state = State(stateStr)
		b.lastStateChange = time.Unix(lastChange, 0)
		m.breakers[b.name] = &b
	}

	return nil
}

// Get retourne ou crée un circuit breaker
func (m *Manager) Get(name string) *Breaker {
	m.mu.RLock()
	b, ok := m.breakers[name]
	m.mu.RUnlock()

	if ok {
		return b
	}

	// Créer nouveau circuit breaker avec valeurs par défaut
	m.mu.Lock()
	defer m.mu.Unlock()

	// Double check
	if b, ok := m.breakers[name]; ok {
		return b
	}

	b = &Breaker{
		name:             name,
		state:            StateClosed,
		failureThreshold: 5,
		successThreshold: 3,
		timeoutSeconds:   60,
		lastStateChange:  time.Now(),
		halfOpenMaxCalls: 3,
	}

	// Persister en base
	execOrLog(m.db, `
		INSERT INTO circuit_breakers
		(name, state, failure_count, success_count, failure_threshold,
		 success_threshold, timeout_seconds, last_state_change_at, half_open_max_calls)
		VALUES (?, 'closed', 0, 0, 5, 3, 60, strftime('%s', 'now'), 3)`, name)

	m.breakers[name] = b
	return b
}

// CanExecute vérifie si le circuit permet l'exécution
func (b *Breaker) CanExecute() (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case StateClosed:
		return true, nil

	case StateOpen:
		// Vérifier si timeout écoulé
		if time.Since(b.lastStateChange) > time.Duration(b.timeoutSeconds)*time.Second {
			b.state = StateHalfOpen
			b.successCount = 0
			b.halfOpenCalls = 0
			b.lastStateChange = time.Now()
			return true, nil
		}
		return false, fmt.Errorf("circuit breaker %s is open", b.name)

	case StateHalfOpen:
		if b.halfOpenCalls >= b.halfOpenMaxCalls {
			return false, fmt.Errorf("circuit breaker %s: half-open max calls reached", b.name)
		}
		b.halfOpenCalls++
		return true, nil
	}

	return false, fmt.Errorf("unknown circuit state: %s", b.state)
}

// RecordSuccess enregistre un succès
func (b *Breaker) RecordSuccess(db *sql.DB) {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case StateClosed:
		b.failureCount = 0

	case StateHalfOpen:
		b.successCount++
		if b.successCount >= b.successThreshold {
			// Fermer le circuit
			b.state = StateClosed
			b.failureCount = 0
			b.successCount = 0
			b.lastStateChange = time.Now()
		}
	}

	// Persister en base
	execOrLog(db, `
		UPDATE circuit_breakers
		SET state = ?, failure_count = ?, success_count = ?,
		    last_success_at = strftime('%s', 'now'),
		    last_state_change_at = ?
		WHERE name = ?`,
		string(b.state), b.failureCount, b.successCount,
		b.lastStateChange.Unix(), b.name)
}

// RecordFailure enregistre un échec
func (b *Breaker) RecordFailure(db *sql.DB) {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case StateClosed:
		b.failureCount++
		if b.failureCount >= b.failureThreshold {
			// Ouvrir le circuit
			b.state = StateOpen
			b.lastStateChange = time.Now()
		}

	case StateHalfOpen:
		// Réouvrir le circuit
		b.state = StateOpen
		b.successCount = 0
		b.lastStateChange = time.Now()
	}

	// Persister en base
	execOrLog(db, `
		UPDATE circuit_breakers
		SET state = ?, failure_count = ?, success_count = ?,
		    last_failure_at = strftime('%s', 'now'),
		    last_state_change_at = ?
		WHERE name = ?`,
		string(b.state), b.failureCount, b.successCount,
		b.lastStateChange.Unix(), b.name)
}

// State retourne l'état actuel
func (b *Breaker) State() State {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.state
}

// Reset remet le circuit breaker en état fermé
func (b *Breaker) Reset(db *sql.DB) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.state = StateClosed
	b.failureCount = 0
	b.successCount = 0
	b.lastStateChange = time.Now()

	execOrLog(db, `
		UPDATE circuit_breakers
		SET state = 'closed', failure_count = 0, success_count = 0,
		    last_state_change_at = strftime('%s', 'now')
		WHERE name = ?`, b.name)
}

// Stats retourne les statistiques du circuit breaker
func (b *Breaker) Stats() map[string]interface{} {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return map[string]interface{}{
		"name":             b.name,
		"state":            string(b.state),
		"failure_count":    b.failureCount,
		"success_count":    b.successCount,
		"failure_threshold": b.failureThreshold,
		"success_threshold": b.successThreshold,
		"timeout_seconds":   b.timeoutSeconds,
		"last_state_change": b.lastStateChange.Format(time.RFC3339),
	}
}
