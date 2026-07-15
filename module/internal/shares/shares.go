// internal/shares/shares.go
//
// 📁 PARTAGES — User Shares (à la Unraid)
//
// Un Partage = un dossier + une politique d'accès (groupes Authentik) +
// les protocoles exposés (SMB pour le lecteur réseau ; le gestionnaire de
// fichiers web tape sur les mêmes dossiers/droits).
//
// Identité : Authentik = source de vérité. L'auth SMB (NTLM) ne peut pas
// utiliser l'outpost LDAP d'Authentik (il faut le hash NT, qu'Authentik ne
// fournit pas — vérifié par spike). Donc Samba garde sa base tdbsam locale,
// et ce manager la PROVISIONNE depuis Authentik : même username, groupes
// Authentik mirrorés en groupes unix → ACL de partage. Le mot de passe
// réseau est un identifiant dédié, posé par l'utilisateur dans l'UI.
//
// Méta stockée dans : runtime/shares/<name>.json
// Dossiers partagés : app-data/_shares/<name>/   (montés /shares dans le conteneur)
// Config Samba      : app-config/caleope-samba/smb.conf (montée /etc/samba/smb.conf)
//
// Le service Samba est un conteneur (app store "caleope-samba", réseau host,
// ports 139/445). Ce manager génère smb.conf, recharge Samba, et provisionne
// les utilisateurs/groupes via `docker exec`.

package shares

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gaiver-it/caleope/pkg/types"
)

// SambaContainer est le nom du conteneur Samba géré par Caleope.
const SambaContainer = "caleope-samba"

// nameRe valide les noms de partage (évite l'injection dans smb.conf / chemins).
var nameRe = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,32}$`)

// Manager gère les partages (User Shares) et le service Samba.
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

func (m *Manager) sharesDir() string {
	return filepath.Join(m.baseDir, "runtime", "shares")
}

func (m *Manager) shareFile(name string) string {
	return filepath.Join(m.sharesDir(), name+".json")
}

// SharesRoot est la racine des dossiers partagés sur l'hôte.
func (m *Manager) SharesRoot() string {
	return filepath.Join(m.baseDir, "app-data", "_shares")
}

// DefaultPath retourne le chemin hôte par défaut d'un partage.
func (m *Manager) DefaultPath(name string) string {
	return filepath.Join(m.SharesRoot(), name)
}

// smbConfPath est le smb.conf généré, monté dans le conteneur Samba.
func (m *Manager) smbConfPath() string {
	return filepath.Join(m.baseDir, "app-config", "caleope-samba", "smb.conf")
}

// containerSharePath traduit un chemin hôte en chemin conteneur (/shares/<name>).
// La racine SharesRoot() est montée sur /shares dans le conteneur.
func (m *Manager) containerSharePath(s types.Share) string {
	base := m.SharesRoot()
	if rel, err := filepath.Rel(base, s.Path); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.Join("/shares", rel)
	}
	// Fallback : partage hors racine standard → même nom
	return filepath.Join("/shares", s.Name)
}

// ─────────────────────────────────────────────
// CRUD
// ─────────────────────────────────────────────

// Add crée un partage : valide, crée le dossier, écrit la méta, régénère Samba.
func (m *Manager) Add(s types.Share) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !nameRe.MatchString(s.Name) {
		return fmt.Errorf("nom de partage invalide (a-z, 0-9, - et _, 1-32 caractères)")
	}
	if _, err := os.Stat(m.shareFile(s.Name)); err == nil {
		return fmt.Errorf("un partage '%s' existe déjà", s.Name)
	}

	if s.Path == "" {
		s.Path = m.DefaultPath(s.Name)
	}
	if err := os.MkdirAll(s.Path, 0o2775); err != nil { // setgid pour ownership cohérent
		return fmt.Errorf("création du dossier partagé: %w", err)
	}
	s.CreatedAt = time.Now().UTC()

	if err := m.writeShare(s); err != nil {
		return err
	}
	return m.applyLocked()
}

// Update remplace la configuration d'un partage existant.
func (m *Manager) Update(s types.Share) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !nameRe.MatchString(s.Name) {
		return fmt.Errorf("nom de partage invalide")
	}
	old, err := m.readShare(s.Name)
	if err != nil {
		return err
	}
	if s.Path == "" {
		s.Path = old.Path
	}
	s.CreatedAt = old.CreatedAt
	if err := m.writeShare(s); err != nil {
		return err
	}
	return m.applyLocked()
}

// Remove supprime un partage (les données sur disque sont CONSERVÉES).
func (m *Manager) Remove(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, err := m.readShare(name); err != nil {
		return err
	}
	if err := os.Remove(m.shareFile(name)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return m.applyLocked()
}

// List retourne tous les partages.
func (m *Manager) List() ([]types.Share, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.listLocked()
}

func (m *Manager) listLocked() ([]types.Share, error) {
	files, _ := filepath.Glob(filepath.Join(m.sharesDir(), "*.json"))
	var out []types.Share
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var s types.Share
		if err := json.Unmarshal(data, &s); err != nil {
			continue
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Get retourne un partage par nom.
func (m *Manager) Get(name string) (types.Share, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.readShare(name)
}

func (m *Manager) readShare(name string) (types.Share, error) {
	data, err := os.ReadFile(m.shareFile(name))
	if err != nil {
		if os.IsNotExist(err) {
			return types.Share{}, fmt.Errorf("partage '%s' introuvable", name)
		}
		return types.Share{}, err
	}
	var s types.Share
	if err := json.Unmarshal(data, &s); err != nil {
		return types.Share{}, err
	}
	return s, nil
}

func (m *Manager) writeShare(s types.Share) error {
	if err := os.MkdirAll(m.sharesDir(), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.shareFile(s.Name), data, 0o600)
}

// ─────────────────────────────────────────────
// GÉNÉRATION smb.conf + RECHARGE
// ─────────────────────────────────────────────

// applyLocked régénère smb.conf depuis tous les partages et recharge Samba.
func (m *Manager) applyLocked() error {
	shares, err := m.listLocked()
	if err != nil {
		return err
	}
	conf := renderSMBConf(shares, m.containerSharePath)
	if err := os.MkdirAll(filepath.Dir(m.smbConfPath()), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(m.smbConfPath(), []byte(conf), 0o644); err != nil {
		return fmt.Errorf("écriture smb.conf: %w", err)
	}
	// Recharge le conteneur s'il tourne (non bloquant sinon : la conf sera lue au prochain démarrage).
	_ = m.reload()
	return nil
}

// renderSMBConf construit le smb.conf complet.
// pathFn traduit un partage en chemin conteneur.
func renderSMBConf(shares []types.Share, pathFn func(types.Share) string) string {
	var b strings.Builder
	b.WriteString("# Généré par Caleope — ne pas éditer à la main\n")
	b.WriteString("[global]\n")
	b.WriteString("   workgroup = CALEOPE\n")
	b.WriteString("   server string = Caleope\n")
	b.WriteString("   security = user\n")
	b.WriteString("   map to guest = never\n")
	b.WriteString("   server min protocol = SMB2\n")
	b.WriteString("   passdb backend = tdbsam\n")
	b.WriteString("   load printers = no\n")
	b.WriteString("   disable spoolss = yes\n")
	b.WriteString("   veto files = /.DS_Store/._*/.Trashes/Thumbs.db/desktop.ini/\n")
	b.WriteString("   delete veto files = yes\n")

	for _, s := range shares {
		if !s.SMBEnabled {
			continue
		}
		valid := groupList(s.ACL, nil)          // tous les groupes ont accès (lecture au moins)
		write := groupList(s.ACL, &accessWrite) // seuls les groupes RW peuvent écrire
		b.WriteString("\n[" + s.Name + "]\n")
		if s.Comment != "" {
			b.WriteString("   comment = " + sanitize(s.Comment) + "\n")
		}
		b.WriteString("   path = " + pathFn(s) + "\n")
		b.WriteString("   browseable = yes\n")
		b.WriteString("   read only = yes\n") // RO par défaut ; write list ouvre l'écriture
		if len(valid) > 0 {
			b.WriteString("   valid users = " + strings.Join(valid, " ") + "\n")
		}
		if len(write) > 0 {
			b.WriteString("   write list = " + strings.Join(write, " ") + "\n")
		}
		b.WriteString("   create mask = 0664\n")
		b.WriteString("   directory mask = 0775\n")
		b.WriteString("   force create mode = 0664\n")
		b.WriteString("   force directory mode = 02775\n")
	}
	return b.String()
}

var accessWrite = types.AccessWrite

// groupList retourne les groupes de l'ACL au format "@groupe".
// Si onlyAccess != nil, ne garde que les groupes ayant ce droit.
func groupList(acl []types.ShareGroupACL, onlyAccess *types.ShareAccess) []string {
	var out []string
	for _, a := range acl {
		if onlyAccess != nil && a.Access != *onlyAccess {
			continue
		}
		if g := strings.TrimSpace(a.Group); g != "" {
			out = append(out, "@"+g)
		}
	}
	sort.Strings(out)
	return out
}

// sanitize retire les caractères de saut de ligne d'une valeur libre.
func sanitize(v string) string {
	v = strings.ReplaceAll(v, "\n", " ")
	v = strings.ReplaceAll(v, "\r", " ")
	return strings.TrimSpace(v)
}

// reload recharge la config Samba dans le conteneur (si présent).
func (m *Manager) reload() error {
	if !containerRunning(SambaContainer) {
		return nil
	}
	out, err := exec.Command("docker", "exec", SambaContainer,
		"smbcontrol", "smbd", "reload-config").CombinedOutput()
	if err != nil {
		return fmt.Errorf("reload samba: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// ─────────────────────────────────────────────
// PROVISIONNEMENT DEPUIS AUTHENTIK
// ─────────────────────────────────────────────

// EnsureUser provisionne un utilisateur réseau dans Samba : crée l'utilisateur
// unix + les groupes (mirroir d'Authentik). N'affecte PAS le mot de passe.
func (m *Manager) EnsureUser(username string, groups []string) error {
	if !nameRe.MatchString(username) {
		return fmt.Errorf("username invalide")
	}
	if !containerRunning(SambaContainer) {
		return fmt.Errorf("le service Samba (%s) n'est pas démarré", SambaContainer)
	}
	var sb strings.Builder
	sb.WriteString("set -e\n")
	// Groupes (mirroir Authentik)
	for _, g := range groups {
		if !nameRe.MatchString(g) {
			continue
		}
		sb.WriteString(fmt.Sprintf("getent group %q >/dev/null 2>&1 || addgroup %q 2>/dev/null || groupadd %q\n", g, g, g))
	}
	// Utilisateur
	sb.WriteString(fmt.Sprintf("id %q >/dev/null 2>&1 || adduser -D -H %q 2>/dev/null || useradd -M %q\n", username, username, username))
	// Appartenance aux groupes
	for _, g := range groups {
		if !nameRe.MatchString(g) {
			continue
		}
		sb.WriteString(fmt.Sprintf("adduser %q %q 2>/dev/null || usermod -aG %q %q\n", username, g, g, username))
	}
	return dockerSh(SambaContainer, sb.String())
}

// SetNetworkPassword pose (ou change) le mot de passe réseau SMB dédié.
// L'utilisateur unix doit exister (EnsureUser d'abord).
func (m *Manager) SetNetworkPassword(username, password string) error {
	if !nameRe.MatchString(username) {
		return fmt.Errorf("username invalide")
	}
	if password == "" {
		return fmt.Errorf("mot de passe vide")
	}
	if !containerRunning(SambaContainer) {
		return fmt.Errorf("le service Samba (%s) n'est pas démarré", SambaContainer)
	}
	cmd := exec.Command("docker", "exec", "-i", SambaContainer, "smbpasswd", "-a", "-s", username)
	cmd.Stdin = strings.NewReader(password + "\n" + password + "\n")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("smbpasswd: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// ─────────────────────────────────────────────
// HELPERS DOCKER
// ─────────────────────────────────────────────

func containerRunning(name string) bool {
	out, err := exec.Command("docker", "inspect", "-f", "{{.State.Running}}", name).Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}

// dockerSh exécute un script shell dans un conteneur.
func dockerSh(container, script string) error {
	out, err := exec.Command("docker", "exec", container, "sh", "-c", script).CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker exec %s: %s", container, strings.TrimSpace(string(out)))
	}
	return nil
}
