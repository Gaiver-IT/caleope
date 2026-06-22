// internal/docker/docker.go
//
// 🐳 LE PILOTE DOCKER
//
// Caleope n'utilise PAS le SDK Docker directement.
// Il appelle docker compose en sous-processus — exactement comme tu le ferais
// en ligne de commande. C'est volontaire : ça reste transparent et debuggable.
//
// CONCEPT GO : os/exec
// Le package os/exec permet de lancer des commandes système.
// exec.Command("docker", "compose", "up", "-d") = docker compose up -d
// cmd.CombinedOutput() = récupère stdout + stderr dans un []byte

package docker

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Client est le pilote Docker de Caleope.
type Client struct{}

func NewClient() *Client {
	return &Client{}
}

// ─────────────────────────────────────────────
// COMPOSE — opérations sur une stack
// ─────────────────────────────────────────────

// Up lance une stack Docker Compose (docker compose up -d).
// composeDir = dossier contenant compose.yml et app.env
func (c *Client) Up(composeDir string) error {
	// Toutes les variables de app.env doivent être dans l'environnement du processus :
	// - COMPOSE_PROFILES : pour la sélection des profils (Docker Compose v2.23+)
	// - Autres vars (ARR_VPN_TYPE, etc.) : pour la substitution YAML (${VAR})
	// Docker Compose lit d'abord le process env, puis .env (symlink → app.env), puis env_file.
	extraEnv := allAppEnvVars(filepath.Join(composeDir, "app.env"))
	return c.runComposeEnv(composeDir, extraEnv, "up", "--detach", "--remove-orphans")
}

// Down arrête et supprime les containers d'une stack.
func (c *Client) Down(composeDir string) error {
	return c.runCompose(composeDir, "down")
}

// Stop arrête les containers sans les supprimer.
func (c *Client) Stop(composeDir string) error {
	return c.runCompose(composeDir, "stop")
}

// Start redémarre des containers arrêtés.
func (c *Client) Start(composeDir string) error {
	return c.runCompose(composeDir, "start")
}

// Pull télécharge les images sans démarrer les containers.
func (c *Client) Pull(composeDir string) error {
	return c.runCompose(composeDir, "pull")
}

// Logs retourne les logs d'une stack (les 100 dernières lignes).
func (c *Client) Logs(composeDir string, tail int) (string, error) {
	tailStr := fmt.Sprintf("%d", tail)
	cmd := exec.Command("docker", "compose",
		"--file", filepath.Join(composeDir, "compose.yml"),
		"--env-file", filepath.Join(composeDir, "app.env"),
		"logs", "--tail", tailStr,
	)
	cmd.Dir = composeDir

	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker compose logs: %w\n%s", err, string(out))
	}
	return string(out), nil
}

// IsRunning vérifie si les containers d'une stack sont en cours d'exécution.
func (c *Client) IsRunning(composeDir string) (bool, error) {
	cmd := exec.Command("docker", "compose",
		"--file", filepath.Join(composeDir, "compose.yml"),
		"--env-file", filepath.Join(composeDir, "app.env"),
		"ps", "--services", "--filter", "status=running",
	)
	cmd.Dir = composeDir

	out, err := cmd.Output()
	if err != nil {
		return false, nil // Si erreur, on considère que ça ne tourne pas
	}

	// Si la sortie n'est pas vide = au moins un service tourne
	return strings.TrimSpace(string(out)) != "", nil
}

// RunOneOff exécute docker compose run --rm <service> pour lancer un container
// one-shot post-démarrage (ex: bootstrap de connexions inter-services).
// Les COMPOSE_PROFILES sont lus depuis app.env pour activer les bons profils.
func (c *Client) RunOneOff(composeDir, service string) error {
	extraEnv := allAppEnvVars(filepath.Join(composeDir, "app.env"))
	return c.runComposeEnv(composeDir, extraEnv, "run", "--rm", service)
}

// runCompose est le helper interne qui exécute docker compose.
func (c *Client) runCompose(composeDir string, args ...string) error {
	return c.runComposeEnv(composeDir, nil, args...)
}

// runComposeEnv exécute docker compose avec des variables d'environnement supplémentaires.
// extraEnv = variables qui surchargent l'environnement courant (ex: "COMPOSE_PROFILES=vpn,novpn").
// Si compose.override.yml existe dans composeDir (GPU overlay, etc.), il est inclus automatiquement.
func (c *Client) runComposeEnv(composeDir string, extraEnv []string, args ...string) error {
	baseArgs := []string{
		"compose",
		"--file", filepath.Join(composeDir, "compose.yml"),
		"--env-file", filepath.Join(composeDir, "app.env"),
	}

	// GPU / overlay : inclure compose.override.yml si présent
	overridePath := filepath.Join(composeDir, "compose.override.yml")
	if _, err := os.Stat(overridePath); err == nil {
		baseArgs = append(baseArgs, "--file", overridePath)
	}

	fullArgs := append(baseArgs, args...)
	cmd := exec.Command("docker", fullArgs...)
	cmd.Dir = composeDir

	// stderr capturé ET renvoyé vers os.Stderr (daemon journal) :
	// si la commande échoue, le message d'erreur est inclus dans l'erreur Go
	// et remonte jusqu'au CLI → l'utilisateur voit ce qui s'est passé.
	var stderrBuf bytes.Buffer
	cmd.Stdout = os.Stdout
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderrBuf)

	if extraEnv != nil {
		cmd.Env = append(os.Environ(), extraEnv...)
	}

	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderrBuf.String())
		if detail != "" {
			return fmt.Errorf("docker compose %s: %w\n%s", strings.Join(args, " "), err, detail)
		}
		return fmt.Errorf("docker compose %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

// allAppEnvVars lit toutes les variables de app.env et les retourne comme slice
// d'environnement pour le processus docker compose.
// Cela permet à docker compose d'utiliser ces variables pour :
// - La sélection de profils (COMPOSE_PROFILES)
// - La substitution YAML (${ARR_VPN_TYPE}, ${ARR_VPN_WG_PRIVATE_KEY}, etc.)
// Retourne nil si le fichier est introuvable ou vide.
func allAppEnvVars(appEnvPath string) []string {
	data, err := os.ReadFile(appEnvPath)
	if err != nil {
		return nil
	}
	var vars []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.Contains(line, "=") {
			vars = append(vars, line)
		}
	}
	return vars
}

// composeProfilesEnv est conservé pour compatibilité interne (utilisé dans DownAllProfiles).
// Préférer allAppEnvVars pour les nouveaux usages.
func composeProfilesEnv(appEnvPath string) []string {
	data, err := os.ReadFile(appEnvPath)
	if err != nil {
		return nil
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "COMPOSE_PROFILES=") {
			val := strings.TrimPrefix(line, "COMPOSE_PROFILES=")
			val = strings.TrimSpace(val)
			if val != "" {
				return []string{"COMPOSE_PROFILES=" + val}
			}
		}
	}
	return nil
}

// DownAllProfiles arrête TOUS les containers d'un projet Docker Compose,
// quelle que soit leur appartenance à un profil.
// Passer allProfiles = union de tous les profils possibles (ex: "novpn,vpn,jellyfin").
// Utilisé avant un changement de profil pour éviter les conteneurs orphelins.
func (c *Client) DownAllProfiles(composeDir, allProfiles string) {
	// On surcharge COMPOSE_PROFILES pour que docker compose voie TOUS les services
	// et puisse arrêter et supprimer tous les containers du projet.
	_ = c.runComposeEnv(composeDir,
		[]string{"COMPOSE_PROFILES=" + allProfiles},
		"down", "--remove-orphans",
	)
}

// ForceRemoveProjectContainers supprime de force tous les containers d'un projet
// Compose en filtrant par le label com.docker.compose.project=<project>.
// Utilisé en fallback quand le compose.yml n'est plus disponible (ex: après
// un rollback d'installation échouée) mais que des containers orphelins subsistent.
func (c *Client) ForceRemoveProjectContainers(project string) {
	// Lister tous les containers (y compris arrêtés) du projet
	listCmd := exec.Command("docker", "ps", "-aq",
		"--filter", "label=com.docker.compose.project="+project,
	)
	out, err := listCmd.Output()
	if err != nil || strings.TrimSpace(string(out)) == "" {
		return // Aucun container à supprimer
	}

	// docker rm -f <id1> <id2> ...
	ids := strings.Fields(strings.TrimSpace(string(out)))
	rmArgs := append([]string{"rm", "-f"}, ids...)
	rmCmd := exec.Command("docker", rmArgs...)
	rmCmd.Stdout = os.Stdout
	rmCmd.Stderr = os.Stderr
	_ = rmCmd.Run()
}

// ─────────────────────────────────────────────
// RÉSEAUX — création des réseaux Caleope
// ─────────────────────────────────────────────

// EnsureNetworks crée les réseaux Docker de Caleope s'ils n'existent pas.
// docker network create --driver bridge --ignore-errors caleope-public
func (c *Client) EnsureNetworks() error {
	networks := []string{"caleope-public", "caleope-internal"}

	for _, network := range networks {
		// On essaie de créer, on ignore si ça existe déjà
		cmd := exec.Command("docker", "network", "create",
			"--driver", "bridge",
			network,
		)
		// On ignore l'erreur si le réseau existe déjà (exit code != 0)
		_ = cmd.Run()
	}

	return nil
}

// CheckDockerAvailable vérifie que Docker est installé et accessible.
func CheckDockerAvailable() error {
	cmd := exec.Command("docker", "version", "--format", "{{.Server.Version}}")
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("docker non disponible: %w", err)
	}
	fmt.Printf("✓ Docker version: %s\n", strings.TrimSpace(string(out)))
	return nil
}
