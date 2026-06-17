// internal/backup/backup.go
//
// 💾 SYSTÈME DE SAUVEGARDE
//
// Sauvegarde les données et la configuration d'une app installée.
// Format : backups/<app>/<timestamp>/{data.tar.gz, config.tar.gz, manifest.json}
//
// STRATÉGIE : stop → tar → start
// On arrête les containers avant de sauvegarder pour éviter la corruption
// des fichiers (ex: base de données en écriture). Plus lent mais sûr.

package backup

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"

	"github.com/gaiver-it/caleope/internal/docker"
	"github.com/gaiver-it/caleope/internal/runtime"
	"github.com/gaiver-it/caleope/pkg/types"
	"github.com/gaiver-it/caleope/pkg/version"
)

// Manager orchestre les opérations de backup et restore.
type Manager struct {
	rt      *runtime.Manager
	docker  *docker.Client
	baseDir string
}

func NewManager(rt *runtime.Manager, dc *docker.Client, baseDir string) *Manager {
	return &Manager{rt: rt, docker: dc, baseDir: baseDir}
}

// ─────────────────────────────────────────────
// BACKUP
// ─────────────────────────────────────────────

// Backup crée une sauvegarde complète d'une application.
// Retourne le chemin du dossier de backup créé.
func (m *Manager) Backup(appID string) (string, error) {
	app, err := m.rt.GetApp(appID)
	if err != nil {
		return "", fmt.Errorf("application '%s' non trouvée: %w", appID, err)
	}

	now := time.Now()
	timestamp := now.Format("2006-01-02T15-04-05")
	backupDir := filepath.Join(m.baseDir, "backups", appID, timestamp)
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return "", fmt.Errorf("création dossier backup: %w", err)
	}

	// Arrêter les containers pour garantir la cohérence des données
	fmt.Println("  [1/4] Arrêt des containers...")
	if err := m.docker.Stop(app.ComposeDir); err != nil {
		_ = os.RemoveAll(backupDir)
		return "", fmt.Errorf("arrêt containers: %w", err)
	}

	// Toujours redémarrer, même en cas d'erreur
	defer func() {
		fmt.Println("  → Redémarrage des containers...")
		_ = m.docker.Start(app.ComposeDir)
	}()

	manifest := types.BackupManifest{
		App:            appID,
		AppName:        app.Name,
		Timestamp:      now, // même instant que le nom de dossier
		CaleopeVersion: version.Version,
	}

	// Sauvegarder app-data/<app>/
	fmt.Println("  [2/4] Sauvegarde des données...")
	dataDir := filepath.Join(m.baseDir, "app-data", appID)
	if _, err := os.Stat(dataDir); err == nil {
		if err := tarGz(dataDir, filepath.Join(backupDir, "data.tar.gz")); err != nil {
			return "", fmt.Errorf("backup data: %w", err)
		}
		manifest.HasData = true
	}

	// Sauvegarder app-config/<app>/
	fmt.Println("  [3/4] Sauvegarde de la configuration...")
	configDir := filepath.Join(m.baseDir, "app-config", appID)
	if _, err := os.Stat(configDir); err == nil {
		if err := tarGz(configDir, filepath.Join(backupDir, "config.tar.gz")); err != nil {
			return "", fmt.Errorf("backup config: %w", err)
		}
		manifest.HasConfig = true
	}

	// Écrire le manifest
	fmt.Println("  [4/4] Écriture du manifest...")
	manifestData, _ := json.MarshalIndent(manifest, "", "  ")
	if err := os.WriteFile(filepath.Join(backupDir, "manifest.json"), manifestData, 0644); err != nil {
		return "", fmt.Errorf("écriture manifest: %w", err)
	}

	return backupDir, nil
}

// ─────────────────────────────────────────────
// RESTIC BACKUP
// ─────────────────────────────────────────────

// ResticBackup sauvegarde une application via Restic vers un dépôt distant ou local.
// Le dépôt doit être accessible et les variables RESTIC_PASSWORD / RESTIC_PASSWORD_FILE
// définies dans l'environnement. Retourne l'URL du dépôt.
func (m *Manager) ResticBackup(appID, repo string) (string, error) {
	if repo == "" {
		return "", fmt.Errorf("repo Restic requis (ex: sftp:user@host:/path ou /chemin/local)")
	}

	app, err := m.rt.GetApp(appID)
	if err != nil {
		return "", fmt.Errorf("application '%s' non trouvée: %w", appID, err)
	}

	fmt.Println("  [1/3] Arrêt des containers...")
	if err := m.docker.Stop(app.ComposeDir); err != nil {
		return "", fmt.Errorf("arrêt containers: %w", err)
	}
	defer func() {
		fmt.Println("  → Redémarrage des containers...")
		_ = m.docker.Start(app.ComposeDir)
	}()

	// Initialiser le dépôt si nécessaire (idempotent si déjà initialisé)
	fmt.Printf("  [2/3] Initialisation du dépôt Restic (%s)...\n", repo)
	initCmd := exec.Command("restic", "-r", repo, "init")
	initCmd.Stdout = os.Stdout
	initCmd.Stderr = os.Stderr
	_ = initCmd.Run() // ignore exit code 1 si le dépôt existe déjà

	dataDir := filepath.Join(m.baseDir, "app-data", appID)
	configDir := filepath.Join(m.baseDir, "app-config", appID)

	fmt.Println("  [3/3] Sauvegarde via Restic...")
	resticArgs := []string{
		"-r", repo,
		"backup",
		"--tag", "caleope",
		"--tag", appID,
	}
	if _, err := os.Stat(dataDir); err == nil {
		resticArgs = append(resticArgs, dataDir)
	}
	if _, err := os.Stat(configDir); err == nil {
		resticArgs = append(resticArgs, configDir)
	}
	if len(resticArgs) == 6 { // seulement les flags, pas de chemin
		return "", fmt.Errorf("aucun répertoire app-data ou app-config trouvé pour '%s'", appID)
	}

	cmd := exec.Command("restic", resticArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("restic backup: %w", err)
	}

	return repo, nil
}

// ─────────────────────────────────────────────
// RESTORE
// ─────────────────────────────────────────────

// Restore restaure une sauvegarde. Si timestamp est vide, prend la plus récente.
func (m *Manager) Restore(appID, timestamp string) error {
	app, err := m.rt.GetApp(appID)
	if err != nil {
		return fmt.Errorf("application '%s' non trouvée: %w", appID, err)
	}

	// Résoudre le timestamp : dernier backup si non spécifié
	if timestamp == "" {
		latest, err := m.latestBackup(appID)
		if err != nil {
			return err
		}
		timestamp = latest
		fmt.Printf("  → Utilisation du backup : %s\n", timestamp)
	}

	backupDir := filepath.Join(m.baseDir, "backups", appID, timestamp)
	if _, err := os.Stat(backupDir); err != nil {
		return fmt.Errorf("backup '%s' introuvable", timestamp)
	}

	// Lire le manifest
	var manifest types.BackupManifest
	manifestData, err := os.ReadFile(filepath.Join(backupDir, "manifest.json"))
	if err != nil {
		return fmt.Errorf("manifest.json manquant dans le backup: %w", err)
	}
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return fmt.Errorf("manifest.json corrompu: %w", err)
	}

	// Arrêter les containers
	fmt.Println("  [1/4] Arrêt des containers...")
	if err := m.docker.Stop(app.ComposeDir); err != nil {
		return fmt.Errorf("arrêt containers: %w", err)
	}

	defer func() {
		fmt.Println("  → Redémarrage des containers...")
		_ = m.docker.Start(app.ComposeDir)
	}()

	// Restaurer app-data/
	if manifest.HasData {
		fmt.Println("  [2/4] Restauration des données...")
		dataDir := filepath.Join(m.baseDir, "app-data", appID)
		if err := os.RemoveAll(dataDir); err != nil {
			return fmt.Errorf("suppression données actuelles: %w", err)
		}
		if err := extractTarGz(filepath.Join(backupDir, "data.tar.gz"), filepath.Join(m.baseDir, "app-data")); err != nil {
			return fmt.Errorf("restauration data: %w", err)
		}
	} else {
		fmt.Println("  [2/4] Pas de données à restaurer")
	}

	// Restaurer app-config/
	if manifest.HasConfig {
		fmt.Println("  [3/4] Restauration de la configuration...")
		configDir := filepath.Join(m.baseDir, "app-config", appID)
		if err := os.RemoveAll(configDir); err != nil {
			return fmt.Errorf("suppression config actuelle: %w", err)
		}
		if err := extractTarGz(filepath.Join(backupDir, "config.tar.gz"), filepath.Join(m.baseDir, "app-config")); err != nil {
			return fmt.Errorf("restauration config: %w", err)
		}
	} else {
		fmt.Println("  [3/4] Pas de configuration à restaurer")
	}

	fmt.Println("  [4/4] Restauration terminée")
	return nil
}

// ─────────────────────────────────────────────
// LIST
// ─────────────────────────────────────────────

// ListBackups retourne les manifests de tous les backups d'une app, triés du plus récent au plus ancien.
func (m *Manager) ListBackups(appID string) ([]types.BackupManifest, error) {
	backupsDir := filepath.Join(m.baseDir, "backups", appID)
	entries, err := os.ReadDir(backupsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []types.BackupManifest{}, nil
		}
		return nil, fmt.Errorf("lecture dossier backups: %w", err)
	}

	var manifests []types.BackupManifest
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		manifestPath := filepath.Join(backupsDir, entry.Name(), "manifest.json")
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			continue
		}
		var bm types.BackupManifest
		if err := json.Unmarshal(data, &bm); err == nil {
			manifests = append(manifests, bm)
		}
	}

	// Tri du plus récent au plus ancien
	sort.Slice(manifests, func(i, j int) bool {
		return manifests[i].Timestamp.After(manifests[j].Timestamp)
	})

	return manifests, nil
}

// ─────────────────────────────────────────────
// HELPERS
// ─────────────────────────────────────────────

// tarGz crée une archive tar.gz d'un dossier.
// tar -czf dst -C parent(src) basename(src)
func tarGz(src, dst string) error {
	cmd := exec.Command("tar", "-czf", dst,
		"-C", filepath.Dir(src),
		filepath.Base(src),
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("tar -czf %s: %w", dst, err)
	}
	return nil
}

// extractTarGz extrait une archive tar.gz dans un dossier parent.
// Le dossier d'origine (ex: "nextcloud") est recréé à l'intérieur de parent.
func extractTarGz(src, parent string) error {
	if err := os.MkdirAll(parent, 0755); err != nil {
		return err
	}
	cmd := exec.Command("tar", "-xzf", src, "-C", parent)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("tar -xzf %s: %w", src, err)
	}
	return nil
}

// latestBackup retourne le timestamp du backup le plus récent.
func (m *Manager) latestBackup(appID string) (string, error) {
	manifests, err := m.ListBackups(appID)
	if err != nil {
		return "", err
	}
	if len(manifests) == 0 {
		return "", fmt.Errorf("aucun backup trouvé pour '%s'", appID)
	}
	return manifests[0].Timestamp.Format("2006-01-02T15-04-05"), nil
}
