// internal/network/network.go
//
// 🌐 EMPLACEMENTS RÉSEAU — SMB/CIFS et SFTP
//
// Permet de monter des partages réseau et de les rendre disponibles
// aux apps Caleope (bibliothèques médias, cibles de backup, etc.).
//
// Méta stockée dans : runtime/locations/<name>.json
// Credentials dans  : runtime/locations/<name>.secret  (chmod 600, root)
// Point de montage  : /opt/gaiver-it/caleope/mounts/<name>/
//
// Prérequis système :
//   - SMB/CIFS : apt install cifs-utils
//   - SFTP     : apt install sshfs

package network

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gaiver-it/caleope/pkg/types"
)

// Manager gère les emplacements réseau.
type Manager struct {
	baseDir string
	mu      sync.Mutex
}

func NewManager(baseDir string) *Manager {
	return &Manager{baseDir: baseDir}
}

// ─────────────────────────────────────────────
// CHEMINS
// ─────────────────────────────────────────────

func (m *Manager) locationsDir() string {
	return filepath.Join(m.baseDir, "runtime", "locations")
}

func (m *Manager) locationFile(name string) string {
	return filepath.Join(m.locationsDir(), name+".json")
}

func (m *Manager) secretFile(name string) string {
	return filepath.Join(m.locationsDir(), name+".secret")
}

func (m *Manager) MountPoint(name string) string {
	return filepath.Join(m.baseDir, "mounts", name)
}

// ─────────────────────────────────────────────
// CRUD
// ─────────────────────────────────────────────

// Add enregistre un nouvel emplacement réseau.
// Le mot de passe (password) est stocké dans un fichier séparé chmod 600.
func (m *Manager) Add(loc types.NetworkLocation, password string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := os.MkdirAll(m.locationsDir(), 0700); err != nil {
		return fmt.Errorf("impossible de créer le dossier locations: %w", err)
	}

	// Vérifier que le nom n'existe pas déjà
	if _, err := os.Stat(m.locationFile(loc.Name)); err == nil {
		return fmt.Errorf("un emplacement '%s' existe déjà", loc.Name)
	}

	// Créer le point de montage
	mountPoint := m.MountPoint(loc.Name)
	if err := os.MkdirAll(mountPoint, 0755); err != nil {
		return fmt.Errorf("impossible de créer le point de montage: %w", err)
	}

	loc.MountPoint = mountPoint
	loc.AddedAt = time.Now().UTC()
	loc.Mounted = false

	// Écrire les métadonnées
	data, err := json.MarshalIndent(loc, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(m.locationFile(loc.Name), data, 0600); err != nil {
		return fmt.Errorf("écriture metadata: %w", err)
	}

	// Écrire le mot de passe si fourni
	if password != "" {
		if err := os.WriteFile(m.secretFile(loc.Name), []byte(password), 0600); err != nil {
			return fmt.Errorf("écriture credentials: %w", err)
		}
	}

	return nil
}

// Remove supprime un emplacement (le démonte d'abord si monté).
func (m *Manager) Remove(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	loc, err := m.readLocation(name)
	if err != nil {
		return err
	}

	// Démonter si monté
	if loc.Mounted {
		if err := m.unmountLocked(loc); err != nil {
			return fmt.Errorf("impossible de démonter avant suppression: %w", err)
		}
	}

	// Supprimer les fichiers
	_ = os.Remove(m.locationFile(name))
	_ = os.Remove(m.secretFile(name))

	// Supprimer le point de montage s'il est vide
	_ = os.Remove(m.MountPoint(name))

	return nil
}

// List retourne tous les emplacements enregistrés.
func (m *Manager) List() ([]types.NetworkLocation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	files, _ := filepath.Glob(filepath.Join(m.locationsDir(), "*.json"))
	var locs []types.NetworkLocation
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var loc types.NetworkLocation
		if err := json.Unmarshal(data, &loc); err != nil {
			continue
		}
		// Vérifier l'état de montage réel
		loc.Mounted = m.isMounted(loc.MountPoint)
		locs = append(locs, loc)
	}
	return locs, nil
}

// ─────────────────────────────────────────────
// MONTAGE / DÉMONTAGE
// ─────────────────────────────────────────────

// Mount monte l'emplacement réseau.
func (m *Manager) Mount(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	loc, err := m.readLocation(name)
	if err != nil {
		return err
	}

	if m.isMounted(loc.MountPoint) {
		return fmt.Errorf("'%s' est déjà monté sur %s", name, loc.MountPoint)
	}

	// Vérifier que le point de montage existe
	if err := os.MkdirAll(loc.MountPoint, 0755); err != nil {
		return fmt.Errorf("point de montage: %w", err)
	}

	// Lire le mot de passe si disponible
	password := ""
	if data, err := os.ReadFile(m.secretFile(name)); err == nil {
		password = strings.TrimSpace(string(data))
	}

	// Monter selon le type
	var mountErr error
	switch loc.Type {
	case types.LocationSMB, types.LocationCIFS:
		mountErr = m.mountSMB(loc, password)
	case types.LocationSFTP:
		mountErr = m.mountSFTP(loc, password)
	default:
		return fmt.Errorf("type d'emplacement inconnu: %s", loc.Type)
	}

	if mountErr != nil {
		return mountErr
	}

	// Mettre à jour l'état
	loc.Mounted = true
	return m.saveLocation(loc)
}

// Unmount démonte l'emplacement réseau.
func (m *Manager) Unmount(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	loc, err := m.readLocation(name)
	if err != nil {
		return err
	}

	return m.unmountLocked(loc)
}

func (m *Manager) unmountLocked(loc types.NetworkLocation) error {
	if !m.isMounted(loc.MountPoint) {
		return fmt.Errorf("'%s' n'est pas monté", loc.Name)
	}

	cmd := exec.Command("umount", loc.MountPoint)
	if out, err := cmd.CombinedOutput(); err != nil {
		// Essayer avec -l (lazy unmount) si forcé
		cmd2 := exec.Command("umount", "-l", loc.MountPoint)
		if _, err2 := cmd2.CombinedOutput(); err2 != nil {
			return fmt.Errorf("démontage échoué: %s", string(out))
		}
	}

	loc.Mounted = false
	return m.saveLocation(loc)
}

// ─────────────────────────────────────────────
// MONTAGE SMB/CIFS
// ─────────────────────────────────────────────

func (m *Manager) mountSMB(loc types.NetworkLocation, password string) error {
	// Vérifier que cifs-utils est installé
	if _, err := exec.LookPath("mount.cifs"); err != nil {
		return fmt.Errorf("cifs-utils non installé — installe-le avec : apt install cifs-utils")
	}

	// Construire l'UNC : //host/share
	unc := fmt.Sprintf("//%s/%s", loc.Host, strings.TrimPrefix(loc.Share, "/"))

	// Options de montage
	options := []string{
		"iocharset=utf8",
		"file_mode=0755",
		"dir_mode=0755",
	}
	if loc.Username != "" {
		options = append(options, "username="+loc.Username)
	}
	if password != "" {
		options = append(options, "password="+password)
	} else {
		options = append(options, "guest")
	}
	if loc.Options != "" {
		options = append(options, loc.Options)
	}

	cmd := exec.Command("mount", "-t", "cifs", unc, loc.MountPoint,
		"-o", strings.Join(options, ","))
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("montage SMB échoué: %s", strings.TrimSpace(string(out)))
	}

	return nil
}

// ─────────────────────────────────────────────
// MONTAGE SFTP
// ─────────────────────────────────────────────

func (m *Manager) mountSFTP(loc types.NetworkLocation, password string) error {
	// Vérifier que sshfs est installé
	if _, err := exec.LookPath("sshfs"); err != nil {
		return fmt.Errorf("sshfs non installé — installe-le avec : apt install sshfs")
	}

	// Format : user@host:/path
	remote := fmt.Sprintf("%s@%s:%s", loc.Username, loc.Host, loc.Share)

	options := []string{
		"allow_other",
		"default_permissions",
		"reconnect",
		"ServerAliveInterval=15",
	}
	if password != "" {
		options = append(options, "password_stdin")
	}
	if loc.Options != "" {
		options = append(options, loc.Options)
	}

	cmd := exec.Command("sshfs", remote, loc.MountPoint,
		"-o", strings.Join(options, ","))

	if password != "" {
		cmd.Stdin = strings.NewReader(password + "\n")
	}

	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("montage SFTP échoué: %s", strings.TrimSpace(string(out)))
	}

	return nil
}

// ─────────────────────────────────────────────
// HELPERS
// ─────────────────────────────────────────────

// isMounted vérifie si un point de montage est actuellement monté.
func (m *Manager) isMounted(mountPoint string) bool {
	data, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return false
	}
	return strings.Contains(string(data), " "+mountPoint+" ")
}

func (m *Manager) readLocation(name string) (types.NetworkLocation, error) {
	data, err := os.ReadFile(m.locationFile(name))
	if err != nil {
		if os.IsNotExist(err) {
			return types.NetworkLocation{}, fmt.Errorf("emplacement '%s' non trouvé", name)
		}
		return types.NetworkLocation{}, err
	}
	var loc types.NetworkLocation
	if err := json.Unmarshal(data, &loc); err != nil {
		return types.NetworkLocation{}, err
	}
	return loc, nil
}

func (m *Manager) saveLocation(loc types.NetworkLocation) error {
	data, err := json.MarshalIndent(loc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.locationFile(loc.Name), data, 0600)
}

// ─────────────────────────────────────────────
// INSPECTION
// ─────────────────────────────────────────────

// ListFiles retourne le contenu (premier niveau) du point de montage.
// Utilisé après un montage réussi pour confirmer que la liaison fonctionne.
// Retourne au maximum maxEntries entrées.
func (m *Manager) ListFiles(name string, maxEntries int) ([]string, error) {
	mountPoint := m.MountPoint(name)

	if !m.isMounted(mountPoint) {
		return nil, fmt.Errorf("'%s' n'est pas monté", name)
	}

	entries, err := os.ReadDir(mountPoint)
	if err != nil {
		return nil, fmt.Errorf("lecture du point de montage: %w", err)
	}

	if maxEntries <= 0 {
		maxEntries = 20
	}

	var files []string
	for i, e := range entries {
		if i >= maxEntries {
			files = append(files, fmt.Sprintf("... (%d fichiers/dossiers supplémentaires)", len(entries)-maxEntries))
			break
		}
		if e.IsDir() {
			files = append(files, e.Name()+"/")
		} else {
			info, err := e.Info()
			if err == nil {
				files = append(files, fmt.Sprintf("%s  (%s)", e.Name(), humanSize(info.Size())))
			} else {
				files = append(files, e.Name())
			}
		}
	}
	return files, nil
}

// humanSize formate une taille en octets de façon lisible.
func humanSize(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
