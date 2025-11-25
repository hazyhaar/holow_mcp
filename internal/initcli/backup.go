// Package initcli - Fonctions de backup pour HOLOW-MCP
package initcli

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// BackupConfig configuration pour le backup
type BackupConfig struct {
	BasePath   string
	BackupDir  string
	MaxBackups int
}

// CreateBackup crée un backup tar.gz de toutes les bases
func CreateBackup(config *BackupConfig) (string, error) {
	// Créer le dossier de backup si nécessaire
	backupDir := config.BackupDir
	if backupDir == "" {
		backupDir = filepath.Join(config.BasePath, "backups")
	}

	if err := os.MkdirAll(backupDir, 0700); err != nil {
		return "", fmt.Errorf("impossible de créer le dossier backup: %w", err)
	}

	// Nom du fichier backup avec timestamp
	timestamp := time.Now().Format("20060102-150405")
	backupFile := filepath.Join(backupDir, fmt.Sprintf("holow-mcp-backup-%s.tar.gz", timestamp))

	// Créer le fichier tar.gz
	file, err := os.Create(backupFile)
	if err != nil {
		return "", fmt.Errorf("impossible de créer le fichier backup: %w", err)
	}
	defer file.Close()

	gzWriter := gzip.NewWriter(file)
	defer gzWriter.Close()

	tarWriter := tar.NewWriter(gzWriter)
	defer tarWriter.Close()

	// Trouver tous les fichiers .db
	dbFiles, err := filepath.Glob(filepath.Join(config.BasePath, "*.db"))
	if err != nil {
		return "", err
	}

	for _, dbFile := range dbFiles {
		if err := addFileToTar(tarWriter, dbFile, filepath.Base(dbFile)); err != nil {
			return "", fmt.Errorf("erreur ajout %s: %w", dbFile, err)
		}
	}

	// Nettoyer les vieux backups si nécessaire
	if config.MaxBackups > 0 {
		cleanOldBackups(backupDir, config.MaxBackups)
	}

	return backupFile, nil
}

func addFileToTar(tw *tar.Writer, filePath, name string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return err
	}

	header := &tar.Header{
		Name:    name,
		Size:    stat.Size(),
		Mode:    int64(stat.Mode()),
		ModTime: stat.ModTime(),
	}

	if err := tw.WriteHeader(header); err != nil {
		return err
	}

	_, err = io.Copy(tw, file)
	return err
}

func cleanOldBackups(backupDir string, maxBackups int) {
	files, err := filepath.Glob(filepath.Join(backupDir, "holow-mcp-backup-*.tar.gz"))
	if err != nil {
		return
	}

	if len(files) <= maxBackups {
		return
	}

	// Trier par date (le nom contient le timestamp)
	// Les plus vieux sont en premier alphabétiquement
	toDelete := len(files) - maxBackups
	for i := 0; i < toDelete; i++ {
		os.Remove(files[i])
	}
}

// RestoreBackup restaure un backup tar.gz
func RestoreBackup(backupFile, destPath string) error {
	file, err := os.Open(backupFile)
	if err != nil {
		return err
	}
	defer file.Close()

	gzReader, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		destFile := filepath.Join(destPath, header.Name)

		// Vérifier que le chemin est sûr (pas de path traversal)
		if !isSubPath(destPath, destFile) {
			return fmt.Errorf("chemin dangereux dans l'archive: %s", header.Name)
		}

		outFile, err := os.Create(destFile)
		if err != nil {
			return err
		}

		if _, err := io.Copy(outFile, tarReader); err != nil {
			outFile.Close()
			return err
		}

		outFile.Close()

		// Restaurer les permissions
		os.Chmod(destFile, os.FileMode(header.Mode))
	}

	return nil
}

func isSubPath(parent, child string) bool {
	absParent, _ := filepath.Abs(parent)
	absChild, _ := filepath.Abs(child)
	return len(absChild) >= len(absParent) && absChild[:len(absParent)] == absParent
}

// ListBackups liste les backups disponibles
func ListBackups(basePath string) ([]BackupInfo, error) {
	backupDir := filepath.Join(basePath, "backups")
	files, err := filepath.Glob(filepath.Join(backupDir, "holow-mcp-backup-*.tar.gz"))
	if err != nil {
		return nil, err
	}

	var backups []BackupInfo
	for _, f := range files {
		stat, err := os.Stat(f)
		if err != nil {
			continue
		}

		backups = append(backups, BackupInfo{
			Path:    f,
			Name:    filepath.Base(f),
			Size:    stat.Size(),
			ModTime: stat.ModTime(),
		})
	}

	return backups, nil
}

// BackupInfo informations sur un backup
type BackupInfo struct {
	Path    string
	Name    string
	Size    int64
	ModTime time.Time
}
