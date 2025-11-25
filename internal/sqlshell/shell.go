// Package sqlshell fournit un shell SQL interactif compatible ncruces WASM
package sqlshell

import (
	"bufio"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ncruces/go-sqlite3/driver"
)

// Shell représente un shell SQL interactif
type Shell struct {
	basePath string
	db       *sql.DB
	dbName   string
	out      io.Writer
}

// New crée un nouveau shell SQL
func New(basePath string) *Shell {
	return &Shell{
		basePath: basePath,
		out:      os.Stdout,
	}
}

// Run exécute une requête unique et affiche le résultat
func (s *Shell) Run(dbName, query string) error {
	if err := s.openDB(dbName); err != nil {
		return err
	}
	defer s.closeDB()

	return s.execAndPrint(query)
}

// Interactive démarre le mode REPL interactif
func (s *Shell) Interactive() error {
	fmt.Fprintln(s.out, "HOLOW-MCP SQL Shell (ncruces WASM compatible)")
	fmt.Fprintln(s.out, "Type .help for commands, .quit to exit")
	fmt.Fprintln(s.out, "")

	// Lister les bases disponibles
	s.listDatabases()

	reader := bufio.NewReader(os.Stdin)
	var multiline strings.Builder

	for {
		prompt := "sql> "
		if multiline.Len() > 0 {
			prompt = "...> "
		}
		fmt.Fprint(s.out, prompt)

		line, err := reader.ReadString('\n')
		if err == io.EOF {
			fmt.Fprintln(s.out, "\nBye!")
			return nil
		}
		if err != nil {
			return err
		}

		line = strings.TrimSpace(line)

		// Commandes spéciales
		if strings.HasPrefix(line, ".") && multiline.Len() == 0 {
			if s.handleCommand(line) {
				continue
			}
			return nil // .quit
		}

		// Accumuler les lignes multiline
		multiline.WriteString(line)
		multiline.WriteString(" ")

		// Vérifier si la requête est complète (se termine par ;)
		query := strings.TrimSpace(multiline.String())
		if !strings.HasSuffix(query, ";") {
			continue
		}

		// Exécuter la requête
		if s.db != nil {
			if err := s.execAndPrint(query); err != nil {
				fmt.Fprintf(s.out, "Error: %v\n", err)
			}
		} else {
			fmt.Fprintln(s.out, "No database open. Use .open <dbname>")
		}

		multiline.Reset()
	}
}

func (s *Shell) handleCommand(cmd string) bool {
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return true
	}

	switch parts[0] {
	case ".quit", ".exit", ".q":
		return false

	case ".help", ".h":
		fmt.Fprintln(s.out, "Commands:")
		fmt.Fprintln(s.out, "  .open <db>    Open database (e.g., .open lifecycle-tools)")
		fmt.Fprintln(s.out, "  .tables       List tables in current database")
		fmt.Fprintln(s.out, "  .schema [t]   Show schema (optionally for table t)")
		fmt.Fprintln(s.out, "  .databases    List available databases")
		fmt.Fprintln(s.out, "  .quit         Exit shell")

	case ".open":
		if len(parts) < 2 {
			fmt.Fprintln(s.out, "Usage: .open <dbname>")
			return true
		}
		if err := s.openDB(parts[1]); err != nil {
			fmt.Fprintf(s.out, "Error: %v\n", err)
		} else {
			fmt.Fprintf(s.out, "Opened %s\n", s.dbName)
		}

	case ".tables":
		if s.db == nil {
			fmt.Fprintln(s.out, "No database open")
			return true
		}
		s.execAndPrint("SELECT name FROM sqlite_master WHERE type='table' ORDER BY name;")

	case ".schema":
		if s.db == nil {
			fmt.Fprintln(s.out, "No database open")
			return true
		}
		if len(parts) > 1 {
			s.execAndPrint(fmt.Sprintf("SELECT sql FROM sqlite_master WHERE name='%s';", parts[1]))
		} else {
			s.execAndPrint("SELECT sql FROM sqlite_master WHERE type='table' ORDER BY name;")
		}

	case ".databases", ".dbs":
		s.listDatabases()

	default:
		fmt.Fprintf(s.out, "Unknown command: %s\n", parts[0])
	}

	return true
}

func (s *Shell) listDatabases() {
	fmt.Fprintln(s.out, "Available databases:")
	dbs := []string{
		"input",
		"lifecycle-tools",
		"lifecycle-execution",
		"lifecycle-core",
		"output",
		"metadata",
	}
	for _, db := range dbs {
		path := filepath.Join(s.basePath, fmt.Sprintf("holow-mcp.%s.db", db))
		if _, err := os.Stat(path); err == nil {
			marker := "  "
			if s.dbName == db {
				marker = "* "
			}
			fmt.Fprintf(s.out, "%s%s\n", marker, db)
		}
	}
	fmt.Fprintln(s.out, "")
}

func (s *Shell) openDB(name string) error {
	s.closeDB()

	// Normaliser le nom
	name = strings.TrimSuffix(name, ".db")
	name = strings.TrimPrefix(name, "holow-mcp.")

	path := filepath.Join(s.basePath, fmt.Sprintf("holow-mcp.%s.db", name))

	// Vérifier que le fichier existe
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("database not found: %s", path)
	}

	// Ouvrir avec ncruces driver (compatible WASM)
	db, err := driver.Open(path, nil)
	if err != nil {
		return fmt.Errorf("failed to open: %w", err)
	}

	// Appliquer les pragmas HOROS
	pragmas := []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA busy_timeout = 5000",
	}
	for _, pragma := range pragmas {
		db.Exec(pragma)
	}

	s.db = db
	s.dbName = name
	return nil
}

func (s *Shell) closeDB() {
	if s.db != nil {
		s.db.Close()
		s.db = nil
		s.dbName = ""
	}
}

func (s *Shell) execAndPrint(query string) error {
	rows, err := s.db.Query(query)
	if err != nil {
		return err
	}
	defer rows.Close()

	// Colonnes
	cols, err := rows.Columns()
	if err != nil {
		return err
	}

	if len(cols) == 0 {
		fmt.Fprintln(s.out, "OK")
		return nil
	}

	// Header
	fmt.Fprintln(s.out, strings.Join(cols, " | "))
	fmt.Fprintln(s.out, strings.Repeat("-", len(strings.Join(cols, " | "))))

	// Rows
	values := make([]interface{}, len(cols))
	valuePtrs := make([]interface{}, len(cols))
	for i := range values {
		valuePtrs[i] = &values[i]
	}

	count := 0
	for rows.Next() {
		if err := rows.Scan(valuePtrs...); err != nil {
			return err
		}

		var row []string
		for _, v := range values {
			if v == nil {
				row = append(row, "NULL")
			} else {
				row = append(row, fmt.Sprintf("%v", v))
			}
		}
		fmt.Fprintln(s.out, strings.Join(row, " | "))
		count++
	}

	fmt.Fprintf(s.out, "(%d rows)\n", count)
	return nil
}
