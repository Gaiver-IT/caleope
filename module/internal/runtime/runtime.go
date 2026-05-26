// internal/runtime/runtime.go
//
// 📁 LE RUNTIME — état persistant du système
//
// Le runtime c'est la "mémoire" de Caleope sur disque.
// Quand le daemon redémarre, il relit ces fichiers JSON pour
// retrouver l'état de toutes les apps installées.
//
// CONCEPT GO : les "méthodes sur struct"
// En Go, on attache des fonctions à des structs avec la syntaxe :
//   func (m *Manager) MaFonction() { ... }
// Le (m *Manager) s'appelle le "receiver" — c'est comme "self" en Python.
// Le * signifie qu'on passe un pointeur (on modifie l'original, pas une copie).

package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gaiver-it/caleope/pkg/types"
)

// BASE_DIR est le répertoire racine de Caleope.
// On le définit ici pour pouvoir le changer facilement (ex: pour les tests).
const BASE_DIR = "/opt/gaiver-it/caleope"

// Manager gère l'état persistant du runtime.
// Il expose des méthodes pour lire/écrire les apps, ports, repos.
type Manager struct {
	baseDir string

	// sync.RWMutex = verrou de lecture/écriture.
	// En Go, le daemon est concurrent (plusieurs requêtes en même temps).
	// Sans verrou, deux goroutines pourraient écrire le même fichier simultanément → corruption.
	// RWMutex : plusieurs lecteurs simultanés OK, mais un seul écrivain à la fois.
	mu sync.RWMutex
}

// NewManager crée un Manager. C'est le constructeur Go (par convention: "New<Type>").
func NewManager(baseDir string) *Manager {
	return &Manager{baseDir: baseDir}
}

// ─────────────────────────────────────────────
// CHEMINS — helpers pour construire les paths
// ─────────────────────────────────────────────

func (m *Manager) appsDir() string {
	return filepath.Join(m.baseDir, "runtime", "apps")
}

func (m *Manager) appFile(id string) string {
	return filepath.Join(m.appsDir(), id+".json")
}

func (m *Manager) portsFile() string {
	return filepath.Join(m.baseDir, "runtime", "ports.json")
}

func (m *Manager) reposFile() string {
	return filepath.Join(m.baseDir, "runtime", "repos.json")
}

func (m *Manager) eventsDir() string {
	return filepath.Join(m.baseDir, "runtime", "events")
}

// ─────────────────────────────────────────────
// INIT — créer la structure de dossiers
// ─────────────────────────────────────────────

// Init crée tous les dossiers nécessaires au runtime.
// os.MkdirAll = mkdir -p en bash (crée tous les parents si besoin).
func (m *Manager) Init() error {
	dirs := []string{
		m.appsDir(),
		m.eventsDir(),
		filepath.Join(m.baseDir, "apps-store"),
		filepath.Join(m.baseDir, "apps-installed"),
		filepath.Join(m.baseDir, "app-config"),
		filepath.Join(m.baseDir, "app-data"),
		filepath.Join(m.baseDir, "backups"),
		filepath.Join(m.baseDir, "logs"),
		filepath.Join(m.baseDir, "core", "cache"),
	}

	for _, dir := range dirs {
		// 0755 = permissions Unix : propriétaire peut tout faire, autres peuvent lire/entrer
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("impossible de créer %s: %w", dir, err)
		}
	}

	// Initialiser ports.json s'il n'existe pas
	if _, err := os.Stat(m.portsFile()); os.IsNotExist(err) {
		if err := m.writePorts(map[string]int{}); err != nil {
			return err
		}
	}

	// Initialiser repos.json s'il n'existe pas
	if _, err := os.Stat(m.reposFile()); os.IsNotExist(err) {
		defaultRepos := []types.Repo{
			{
				Name:     "official",
				URL:      "https://github.com/gaiver-it/caleope-store",
				Trust:    types.TrustOfficial,
				LocalDir: filepath.Join(m.baseDir, "core", "cache", "official"),
				LastSync: time.Time{},
			},
		}
		if err := m.writeRepos(defaultRepos); err != nil {
			return err
		}
	}

	return nil
}

// ─────────────────────────────────────────────
// APPS — lire et écrire l'état des apps
// ─────────────────────────────────────────────

// SaveApp écrit l'état d'une app dans runtime/apps/<id>.json.
// CONCEPT : encoding/json est la bibliothèque standard Go pour JSON.
// json.MarshalIndent = convertit une struct Go en JSON formaté (indenté pour lisibilité).
func (m *Manager) SaveApp(app *types.RuntimeApp) error {
	m.mu.Lock()         // On prend le verrou en écriture
	defer m.mu.Unlock() // defer = exécuté à la fin de la fonction, quoi qu'il arrive

	app.UpdatedAt = time.Now()

	data, err := json.MarshalIndent(app, "", "  ")
	if err != nil {
		return fmt.Errorf("erreur sérialisation app %s: %w", app.ID, err)
	}

	// os.WriteFile = écrire un fichier en une fois (crée ou remplace)
	return os.WriteFile(m.appFile(app.ID), data, 0644)
}

// GetApp lit l'état d'une app depuis runtime/apps/<id>.json.
func (m *Manager) GetApp(id string) (*types.RuntimeApp, error) {
	m.mu.RLock()         // Verrou de lecture (plusieurs lecteurs simultanés OK)
	defer m.mu.RUnlock()

	data, err := os.ReadFile(m.appFile(id))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("app '%s' non installée", id)
		}
		return nil, err
	}

	var app types.RuntimeApp
	// json.Unmarshal = convertit du JSON en struct Go
	if err := json.Unmarshal(data, &app); err != nil {
		return nil, fmt.Errorf("erreur lecture runtime app %s: %w", id, err)
	}

	return &app, nil
}

// ListApps retourne toutes les apps installées.
// On lit tous les fichiers .json dans runtime/apps/.
func (m *Manager) ListApps() ([]*types.RuntimeApp, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// filepath.Glob = liste les fichiers qui correspondent au pattern
	files, err := filepath.Glob(filepath.Join(m.appsDir(), "*.json"))
	if err != nil {
		return nil, err
	}

	// make([]T, 0) = créer une slice (tableau dynamique) vide
	apps := make([]*types.RuntimeApp, 0, len(files))

	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			continue // On saute les fichiers illisibles sans planter
		}

		var app types.RuntimeApp
		if err := json.Unmarshal(data, &app); err != nil {
			continue
		}

		// append = ajouter un élément à une slice (comme .append() en Python)
		apps = append(apps, &app)
	}

	return apps, nil
}

// RemoveApp supprime le fichier runtime d'une app.
func (m *Manager) RemoveApp(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	err := os.Remove(m.appFile(id))
	if os.IsNotExist(err) {
		return fmt.Errorf("app '%s' non installée", id)
	}
	return err
}

// ─────────────────────────────────────────────
// PORTS — allocation dynamique
// ─────────────────────────────────────────────

// AllocatePort trouve un port libre entre min et max,
// l'enregistre dans ports.json et le retourne.
// C'est comme ça que deux apps n'ont jamais le même port hôte.
func (m *Manager) AllocatePort(appID string, min, max int) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	ports, err := m.readPorts()
	if err != nil {
		return 0, err
	}

	// Construire un set des ports déjà utilisés
	// map[int]bool = dictionnaire Go (clé: port, valeur: true si utilisé)
	used := make(map[int]bool)
	for _, p := range ports {
		used[p] = true
	}

	// Trouver le premier port libre
	for port := min; port <= max; port++ {
		if !used[port] {
			ports[appID] = port
			if err := m.writePorts(ports); err != nil {
				return 0, err
			}
			return port, nil
		}
	}

	return 0, fmt.Errorf("aucun port disponible entre %d et %d", min, max)
}

// ReleasePort libère le port d'une app dans ports.json.
func (m *Manager) ReleasePort(appID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ports, err := m.readPorts()
	if err != nil {
		return err
	}

	// delete = supprimer une clé d'une map Go
	delete(ports, appID)
	return m.writePorts(ports)
}

func (m *Manager) readPorts() (map[string]int, error) {
	data, err := os.ReadFile(m.portsFile())
	if err != nil {
		return map[string]int{}, nil
	}

	var ports map[string]int
	if err := json.Unmarshal(data, &ports); err != nil {
		return nil, err
	}
	return ports, nil
}

func (m *Manager) writePorts(ports map[string]int) error {
	data, err := json.MarshalIndent(ports, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.portsFile(), data, 0644)
}

// ─────────────────────────────────────────────
// REPOS
// ─────────────────────────────────────────────

func (m *Manager) GetRepos() ([]types.Repo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.readRepos()
}

func (m *Manager) readRepos() ([]types.Repo, error) {
	data, err := os.ReadFile(m.reposFile())
	if err != nil {
		return nil, err
	}
	var repos []types.Repo
	if err := json.Unmarshal(data, &repos); err != nil {
		return nil, err
	}
	return repos, nil
}

func (m *Manager) writeRepos(repos []types.Repo) error {
	data, err := json.MarshalIndent(repos, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.reposFile(), data, 0644)
}

// ─────────────────────────────────────────────
// CONFIG — lecture de caleope.conf
// ─────────────────────────────────────────────

// Config contient la configuration persistante de Caleope.
// Elle est lue depuis caleope.conf (fichier clé=valeur généré à l'install).
type Config struct {
	Domain    string // domaine de base (ex: caleope-redberry.guernaham.bzh)
	ProxyMode string // "npm" ou "traefik"
	Email     string // email Let's Encrypt
	Version   string
	Channel   string // "stable" ou "alpha"
}

// GetConfig lit et parse caleope.conf.
func (m *Manager) GetConfig() (*Config, error) {
	confPath := filepath.Join(m.baseDir, "caleope.conf")
	data, err := os.ReadFile(confPath)
	if err != nil {
		// Si le fichier n'existe pas, on retourne une config vide (pas d'erreur fatale)
		if os.IsNotExist(err) {
			return &Config{}, nil
		}
		return nil, fmt.Errorf("lecture caleope.conf: %w", err)
	}

	cfg := &Config{}
	// Parser le format clé=valeur ligne par ligne
	for _, line := range strings.Split(string(data), "\n") {
		// Ignorer les commentaires et lignes vides
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		switch key {
		case "CALEOPE_DOMAIN":
			cfg.Domain = val
		case "CALEOPE_PROXY_MODE":
			cfg.ProxyMode = val
		case "CALEOPE_EMAIL":
			cfg.Email = val
		case "CALEOPE_VERSION":
			cfg.Version = val
		case "CALEOPE_CHANNEL":
			cfg.Channel = val
		}
	}
	return cfg, nil
}

// AppDomain construit le domaine complet d'une app.
// Ex: "jellyfin" + "caleope.guernaham.bzh" → "jellyfin.caleope.guernaham.bzh"
func (m *Manager) AppDomain(appID string) string {
	cfg, err := m.GetConfig()
	if err != nil || cfg.Domain == "" {
		return ""
	}
	return appID + "." + cfg.Domain
}

// BaseDomain retourne le domaine racine de l'installation (sans préfixe d'app).
// Utilisé par les apps multi-services comme arr-stack dont chaque service
// occupe déjà son propre sous-domaine (radarr.domain, prowlarr.domain…).
func (m *Manager) BaseDomain() string {
	cfg, err := m.GetConfig()
	if err != nil || cfg.Domain == "" {
		return ""
	}
	return cfg.Domain
}
