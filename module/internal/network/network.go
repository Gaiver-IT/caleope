// internal/network/network.go
//
// 🌐 EMPLACEMENTS RÉSEAU — SMB/CIFS, NFS et SFTP
//
// Permet de monter des partages réseau et de les rendre disponibles
// aux apps Caleope (bibliothèques médias, cibles de backup, etc.).
//
// Méta stockée dans : runtime/locations/<name>.json
// Credentials dans  : runtime/locations/<name>.secret  (chmod 600, root)
// Point de montage  : /opt/gaiver-it/caleope/mounts/<name>/
// fstab             : ligne ajoutée au montage, retirée à la suppression
//                     marquée par "# caleope:<name>" pour identification
//
// Prérequis système :
//   - SMB/CIFS : apt install cifs-utils
//   - NFS      : apt install nfs-common
//   - SFTP     : apt install sshfs

package network

import (
	"bufio"
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

// CaleopeDir retourne le dossier caleope/ à la racine du NAS.
func (m *Manager) CaleopeDir(name string) string {
	return filepath.Join(m.MountPoint(name), "caleope")
}

// AppDataDir retourne le chemin de données d'une app sur le NAS.
// Structure : <nas_mount>/caleope/app-data/<appID>/
func (m *Manager) AppDataDir(name, appID string) string {
	return filepath.Join(m.CaleopeDir(name), "app-data", appID)
}

// EnsureCaleopeStructure crée la structure caleope/ sur le NAS si elle n'existe pas.
// Appelé automatiquement après un montage réussi.
func (m *Manager) EnsureCaleopeStructure(name string) error {
	dirs := []string{
		filepath.Join(m.CaleopeDir(name), "app-data"),
		filepath.Join(m.CaleopeDir(name), "backups"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("impossible de créer %s sur le NAS: %w", d, err)
		}
	}
	return nil
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

	// Retirer l'entrée de /etc/fstab
	if err := m.removeFstabEntry(name); err != nil {
		fmt.Fprintf(os.Stderr, "avertissement: impossible de nettoyer /etc/fstab: %v\n", err)
	}

	// Supprimer les fichiers (métadonnées, secret, credentials CIFS)
	_ = os.Remove(m.locationFile(name))
	_ = os.Remove(m.secretFile(name))
	_ = os.Remove(m.credentialsFile(name))

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
	case types.LocationNFS:
		mountErr = m.mountNFS(loc)
	case types.LocationSFTP:
		mountErr = m.mountSFTP(loc, password)
	case types.LocationLocal:
		mountErr = m.mountLocal(loc)
	default:
		return fmt.Errorf("type d'emplacement inconnu: %s", loc.Type)
	}

	if mountErr != nil {
		return mountErr
	}

	// Pour SMB : écrire le fichier credentials (utilisé par la ligne fstab)
	if (loc.Type == types.LocationSMB || loc.Type == types.LocationCIFS) && password != "" {
		if err := m.writeCIFSCredentials(loc, password); err != nil {
			// Non bloquant : le montage a réussi, on log juste l'avertissement
			fmt.Fprintf(os.Stderr, "avertissement: impossible d'écrire le fichier credentials CIFS: %v\n", err)
		}
	}

	// Ajouter (ou mettre à jour) l'entrée dans /etc/fstab
	if err := m.addFstabEntry(loc); err != nil {
		// Non bloquant : le montage a réussi, on log juste l'avertissement
		fmt.Fprintf(os.Stderr, "avertissement: impossible de mettre à jour /etc/fstab: %v\n", err)
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
// GESTION /etc/fstab
// ─────────────────────────────────────────────

const fstabPath = "/etc/fstab"

// fstabMarker retourne le marqueur de fin de ligne identifiant l'entrée Caleope.
func fstabMarker(name string) string {
	return fmt.Sprintf("# caleope:%s", name)
}

// optionsNFS retourne les options de montage NFS de Caleope.
//
// SOURCE UNIQUE, volontairement : elle sert au montage à chaud (mountNFS) ET à
// la ligne /etc/fstab. Les deux avaient divergé, si bien qu'un durcissement
// s'évaporait au redémarrage suivant.
//
// ⚠️ `hard` — ne pas remettre `soft`. Avec `soft`, une écriture qui expire rend
// une erreur que la plupart des programmes ignorent : la donnée est perdue en
// silence, le fichier gardant sa taille avec des zéros à la place. Constaté chez
// un utilisateur du 18/07 au 03/08/2026 (30 à 60 Go troués, détectés six semaines
// plus tard). Avec `hard`, un NAS malade fait attendre au lieu de faire perdre.
// Qui veut `soft` le demande explicitement : loc.Options est ajouté après et gagne.
func optionsNFS() []string {
	return []string{
		"vers=3",
		"rw",
		"hard",
		"proto=tcp",
		"mountproto=tcp",
		"timeo=50",
		"retrans=3",
		"retry=0",
	}
}

// fstabLine construit la ligne fstab pour un emplacement.
// Pour SMB : utilise un fichier credentials séparé (pas le mdp en clair).
// Pour NFS : pas de credentials, auth par IP côté serveur.
func (m *Manager) fstabLine(loc types.NetworkLocation) string {
	marker := fstabMarker(loc.Name)
	switch loc.Type {
	case types.LocationSMB, types.LocationCIFS:
		unc := fmt.Sprintf("//%s/%s", loc.Host, strings.TrimPrefix(loc.Share, "/"))
		opts := "iocharset=utf8,file_mode=0777,dir_mode=0777,nounix,nomapposix,_netdev"
		// Fichier credentials : username=xxx\npassword=xxx (chmod 600)
		opts += fmt.Sprintf(",credentials=%s", m.credentialsFile(loc.Name))
		if loc.Options != "" {
			opts += "," + loc.Options
		}
		return fmt.Sprintf("%s\t%s\tcifs\t%s\t0 0\t%s", unc, loc.MountPoint, opts, marker)
	case types.LocationNFS:
		export := fmt.Sprintf("%s:%s", loc.Host, "/"+strings.TrimPrefix(loc.Share, "/"))
		// ⚠️ MÊME liste que le montage à chaud (optionsNFS), sinon le durcissement
		// disparaît au premier redémarrage sans que personne ne s'en aperçoive.
		// C'est exactement ce qui était arrivé : le correctif v0.7.8 (proto=tcp,
		// mountproto=tcp, timeo, retrans, retry) ne vivait que dans mountNFS,
		// et fstab remontait le NAS en « vers=3,rw,soft,_netdev » après chaque boot.
		opts := strings.Join(append(optionsNFS(), "_netdev"), ",")
		if loc.Options != "" {
			opts += "," + loc.Options
		}
		return fmt.Sprintf("%s\t%s\tnfs\t%s\t0 0\t%s", export, loc.MountPoint, opts, marker)
	default:
		// SFTP via sshfs : pas de support fstab standard
		return ""
	}
}

// credentialsFile retourne le chemin du fichier credentials CIFS (format mount.cifs).
func (m *Manager) credentialsFile(name string) string {
	return filepath.Join(m.locationsDir(), name+".credentials")
}

// writeCIFSCredentials crée/met à jour le fichier credentials CIFS (chmod 600).
func (m *Manager) writeCIFSCredentials(loc types.NetworkLocation, password string) error {
	var sb strings.Builder
	if loc.Username != "" {
		sb.WriteString("username=" + loc.Username + "\n")
	}
	if password != "" {
		sb.WriteString("password=" + password + "\n")
	}
	return os.WriteFile(m.credentialsFile(loc.Name), []byte(sb.String()), 0600)
}

// addFstabEntry ajoute (ou met à jour) l'entrée fstab de l'emplacement.
// Idempotent : si le marqueur existe déjà, la ligne est remplacée.
func (m *Manager) addFstabEntry(loc types.NetworkLocation) error {
	line := m.fstabLine(loc)
	if line == "" {
		return nil // type sans support fstab (SFTP)
	}
	marker := fstabMarker(loc.Name)

	data, err := os.ReadFile(fstabPath)
	if err != nil {
		return fmt.Errorf("lecture fstab: %w", err)
	}

	var lines []string
	found := false
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		l := scanner.Text()
		if strings.Contains(l, marker) {
			lines = append(lines, line) // remplace
			found = true
		} else {
			lines = append(lines, l)
		}
	}
	if !found {
		lines = append(lines, line)
	}

	content := strings.Join(lines, "\n")
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return os.WriteFile(fstabPath, []byte(content), 0644)
}

// removeFstabEntry retire l'entrée fstab de l'emplacement (identifiée par le marqueur).
func (m *Manager) removeFstabEntry(name string) error {
	marker := fstabMarker(name)

	data, err := os.ReadFile(fstabPath)
	if err != nil {
		return fmt.Errorf("lecture fstab: %w", err)
	}

	var lines []string
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		l := scanner.Text()
		if !strings.Contains(l, marker) {
			lines = append(lines, l)
		}
	}

	content := strings.Join(lines, "\n")
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return os.WriteFile(fstabPath, []byte(content), 0644)
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
	// - file_mode/dir_mode=0777 : tous les conteneurs Docker peuvent lire/écrire
	// - nounix : désactive les extensions Unix (incompatibles avec certains NAS)
	// - nomapposix : désactive le mapping SID→POSIX qui cause des ACL Windows
	//   héritées bloquant la création de sous-répertoires imbriqués
	options := []string{
		"iocharset=utf8",
		"file_mode=0777",
		"dir_mode=0777",
		"nounix",
		"nomapposix",
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
// MONTAGE NFS
// ─────────────────────────────────────────────

func (m *Manager) mountNFS(loc types.NetworkLocation) error {
	// Vérifier que nfs-common est installé
	if _, err := exec.LookPath("mount.nfs"); err != nil {
		return fmt.Errorf("nfs-common non installé — installe-le avec : apt install nfs-common")
	}

	// Construire le chemin NFS : host:/export/path
	// loc.Share doit déjà contenir le chemin d'export (ex: /export/nas)
	export := fmt.Sprintf("%s:%s", loc.Host, "/"+strings.TrimPrefix(loc.Share, "/"))

	// Options de montage NFS
	// - vers=3       : NFSv3 (compatible avec la majorité des NAS)
	// - rw           : lecture/écriture
	// - hard         : ⚠️ NE JAMAIS REMETTRE `soft` PAR DÉFAUT. Voir plus bas.
	// - proto=tcp    : transport NFS sur TCP
	// - mountproto=tcp : requête MOUNT vers mountd sur TCP au lieu d'UDP (défaut).
	//   Sans ça, un NAS dont le mountd ne répond pas en UDP (pare-feu, backend
	//   distant lent type storage box / mergerfs) fige le montage indéfiniment.
	// - timeo=50,retrans=3 : 5 s par tentative, 3 tentatives avant de réessayer.
	// - retry=0      : mount.nfs abandonne tout de suite au lieu de reboucler 2 min.
	//
	// ⚠️ POURQUOI `hard` ET PAS `soft` — corrigé le 2026-08-12, ne pas défaire.
	// Avec `soft`, une écriture qui expire rend une ERREUR à l'application. Or
	// d'innombrables programmes (et `cp`, et les copies d'import) ne vérifient pas
	// le retour de write()/close() : la donnée est alors perdue EN SILENCE et le
	// fichier garde sa taille, rempli de zéros à l'emplacement manquant.
	// C'est exactement ce qui s'est produit chez un utilisateur entre le 18/07 et
	// le 03/08/2026 : 30 à 60 Go de fichiers troués de blocs nuls de 1 Mio, sans
	// une seule erreur visible, découverts six semaines plus tard.
	// Avec `hard`, l'opération est retentée jusqu'à ce que le serveur réponde :
	// un NAS malade fait ATTENDRE, il ne fait plus PERDRE. C'est le compromis que
	// recommande la documentation NFS, et le seul acceptable pour des données.
	// L'utilisateur qui veut `soft` peut toujours le demander : loc.Options est
	// concaténé APRÈS et gagne (`caleope location add … --options soft`).
	options := optionsNFS()
	if loc.Options != "" {
		options = append(options, loc.Options)
	}

	cmd := exec.Command("mount", "-t", "nfs", export, loc.MountPoint,
		"-o", strings.Join(options, ","))
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("montage NFS échoué: %s", strings.TrimSpace(string(out)))
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

// ─────────────────────────────────────────────
// MONTAGE LOCAL (disque interne/externe)
// ─────────────────────────────────────────────

// mountLocal monte un disque local. Le chemin du périphérique est stocké dans Host (ex: /dev/sdb1).
func (m *Manager) mountLocal(loc types.NetworkLocation) error {
	device := loc.Host
	if device == "" {
		return fmt.Errorf("chemin du périphérique manquant (Host)")
	}
	if _, err := os.Stat(device); err != nil {
		return fmt.Errorf("périphérique introuvable : %s", device)
	}

	out, err := exec.Command("mount", "-t", "auto", device, loc.MountPoint).CombinedOutput()
	if err != nil {
		return fmt.Errorf("montage échoué : %s", strings.TrimSpace(string(out)))
	}
	return nil
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
