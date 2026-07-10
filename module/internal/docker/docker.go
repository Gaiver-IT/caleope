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
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
// LogsStream lance docker compose logs --follow et envoie chaque ligne dans ch.
// Ferme ch quand le process se termine ou quand done est fermé.
func (c *Client) LogsStream(composeDir string, tail int, ch chan<- string, done <-chan struct{}) {
	tailStr := fmt.Sprintf("%d", tail)
	cmd := exec.Command("docker", "compose",
		"--file", filepath.Join(composeDir, "compose.yml"),
		"--env-file", filepath.Join(composeDir, "app.env"),
		"logs", "--follow", "--tail", tailStr,
	)
	cmd.Dir = composeDir

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		close(ch)
		return
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		close(ch)
		return
	}

	go func() {
		<-done
		_ = cmd.Process.Kill()
	}()

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		select {
		case ch <- scanner.Text():
		case <-done:
			_ = cmd.Process.Kill()
			close(ch)
			return
		}
	}
	close(ch)
	_ = cmd.Wait()
}

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

// PruneStaleProjectContainers supprime les containers NON-DÉMARRÉS (created / exited
// / dead) d'un projet Compose, SANS toucher aux containers en cours d'exécution.
//
// Appelé avant `up` lors d'un (ré)install : un container du projet resté d'un run
// précédent (ex: un `up` interrompu qui laisse les containers en état "created")
// bloque le prochain `up` par conflit de nom (`container_name` fixe → "name already
// in use"). On lève ce conflit sans jamais couper un service VIVANT — typiquement un
// AzuraCast externe encore Up, qui diffuse le flux, ne doit pas être arrêté.
func (c *Client) PruneStaleProjectContainers(project string) {
	// Filtres status répétés = OR (created OU exited OU dead) ; le label projet = AND.
	// Les containers "running"/"restarting"/"paused" sont donc épargnés.
	listCmd := exec.Command("docker", "ps", "-aq",
		"--filter", "label=com.docker.compose.project="+project,
		"--filter", "status=created",
		"--filter", "status=exited",
		"--filter", "status=dead",
	)
	out, err := listCmd.Output()
	if err != nil || strings.TrimSpace(string(out)) == "" {
		return // rien à nettoyer
	}
	ids := strings.Fields(strings.TrimSpace(string(out)))
	rmArgs := append([]string{"rm", "-f"}, ids...)
	rmCmd := exec.Command("docker", rmArgs...)
	rmCmd.Stdout = os.Stdout
	rmCmd.Stderr = os.Stderr
	_ = rmCmd.Run()
}

// ─────────────────────────────────────────────
// PRIORITÉ RESSOURCES — « apps prioritaires »
// ─────────────────────────────────────────────
//
// Sur un serveur partagé (RAM/CPU limités), toutes les apps ne se valent pas :
// l'antenne radio ne doit JAMAIS souffrir d'un batch d'analyse. Caleope traduit un
// tier de priorité en deux réglages Docker/kernel appliqués aux containers de l'app :
//   - cpu_shares    : poids CPU relatif (défaut Docker 1024) — qui passe devant sous charge CPU ;
//   - oom_score_adj : priorité de sacrifice sous pression RAM (-1000 = jamais tué → +1000 = tué en premier).
//
// Deux niveaux, du plus spécifique au plus général :
//   1. label compose "caleope.priority" sur un service → override fin (un seul container) ;
//   2. sinon, le champ "priority" du manifeste de l'app  → défaut pour toute l'app.
// Le tier "normal" (ou absent/inconnu) = on ne touche à rien (défauts Docker préservés,
// y compris un cpu_shares posé explicitement dans le compose).

// priorityTier traduit un nom de tier en réglages concrets.
// Le bool = false pour "normal"/absent/inconnu (= ne rien appliquer).
func priorityTier(name string) (cpuShares, oomAdj int, apply bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "critical":
		return 2048, -800, true // antenne / vital : CPU prioritaire, protégé de l'OOM-killer
	case "background":
		return 256, 600, true // batch sacrifiable : peu de CPU, tué en premier si la RAM manque
	default:
		return 0, 0, false // "normal", "" ou inconnu : défauts Docker
	}
}

// ApplyPriorities applique la priorité ressources aux containers d'un projet Compose,
// à appeler APRÈS le `up`. appPriority = tier du manifeste ; un label "caleope.priority"
// sur un service prime dessus. Idempotent, best-effort : toute erreur est loggée sur
// stderr et n'interrompt jamais l'install.
func (c *Client) ApplyPriorities(project, appPriority string) {
	listCmd := exec.Command("docker", "ps", "-q",
		"--filter", "label=com.docker.compose.project="+project,
	)
	out, err := listCmd.Output()
	if err != nil || strings.TrimSpace(string(out)) == "" {
		return // aucun container en cours : rien à prioriser
	}
	for _, id := range strings.Fields(strings.TrimSpace(string(out))) {
		// Tier effectif : label du service > priorité de l'app.
		tierName := appPriority
		if lbl := c.containerLabel(id, "caleope.priority"); lbl != "" {
			tierName = lbl
		}
		cpuShares, oomAdj, apply := priorityTier(tierName)
		if !apply {
			continue // "normal" : on laisse les défauts Docker (et un cpu_shares compose éventuel)
		}
		// 1. cpu_shares — modifiable à chaud via docker update.
		upd := exec.Command("docker", "update", "--cpu-shares", strconv.Itoa(cpuShares), id)
		if o, e := upd.CombinedOutput(); e != nil {
			fmt.Fprintf(os.Stderr, "[priority] cpu_shares %s (%s): %v — %s\n", id, tierName, e, strings.TrimSpace(string(o)))
		}
		// 2. oom_score_adj — non modifiable par docker update ni exprimable en compose ;
		//    on l'écrit directement dans /proc/<pid>/oom_score_adj (le daemon tourne en root).
		if pid := c.containerPID(id); pid > 0 {
			p := "/proc/" + strconv.Itoa(pid) + "/oom_score_adj"
			if e := os.WriteFile(p, []byte(strconv.Itoa(oomAdj)), 0644); e != nil {
				fmt.Fprintf(os.Stderr, "[priority] oom_score_adj %s (%s): %v\n", id, tierName, e)
			}
		}
	}
}

// containerLabel renvoie la valeur d'un label d'un container ("" si absent).
func (c *Client) containerLabel(id, label string) string {
	out, err := exec.Command("docker", "inspect", "-f",
		fmt.Sprintf("{{ index .Config.Labels %q }}", label), id).Output()
	if err != nil {
		return ""
	}
	v := strings.TrimSpace(string(out))
	if v == "<no value>" {
		return ""
	}
	return v
}

// containerPID renvoie le PID hôte du process principal d'un container (0 si introuvable).
func (c *Client) containerPID(id string) int {
	out, err := exec.Command("docker", "inspect", "-f", "{{ .State.Pid }}", id).Output()
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return 0
	}
	return pid
}

// ─────────────────────────────────────────────
// IMAGES — save / load (pour l'export auto-suffisant)
// ─────────────────────────────────────────────

// SaveImages exporte une ou plusieurs images dans un tar (docker save -o dst img...).
// Utilisé par `caleope export` pour embarquer les images (restore hors-ligne).
func (c *Client) SaveImages(images []string, dst string) error {
	if len(images) == 0 {
		return fmt.Errorf("aucune image à sauvegarder")
	}
	args := append([]string{"save", "-o", dst}, images...)
	cmd := exec.Command("docker", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker save: %w\n%s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// LoadImages charge les images depuis un tar (docker load -i src).
func (c *Client) LoadImages(src string) error {
	cmd := exec.Command("docker", "load", "-i", src)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker load: %w\n%s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// ImageExists indique si une image est présente localement.
func (c *Client) ImageExists(ref string) bool {
	return exec.Command("docker", "image", "inspect", ref).Run() == nil
}

// ExportImages écrit chaque image dans imagesDir (un .tar par image, format
// docker-archive). Préfère skopeo (copie depuis le registre → couches complètes,
// fiable avec le store containerd où `docker save` n'exporte que les manifests).
// Retombe sur `docker save` si skopeo est absent (store classique).
func (c *Client) ExportImages(refs []string, imagesDir string) error {
	if err := os.MkdirAll(imagesDir, 0755); err != nil {
		return err
	}
	useSkopeo := exec.Command("skopeo", "--version").Run() == nil
	for _, ref := range refs {
		dst := filepath.Join(imagesDir, sanitizeRef(ref)+".tar")
		if useSkopeo {
			cmd := exec.Command("skopeo", "copy", "--quiet",
				"--src-tls-verify=false",
				"docker://"+ref, "docker-archive:"+dst+":"+ref)
			var e bytes.Buffer
			cmd.Stderr = &e
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("skopeo copy %s: %w\n%s", ref, err, strings.TrimSpace(e.String()))
			}
			continue
		}
		if !c.ImageExists(ref) {
			if err := c.PullImage(ref); err != nil {
				return err
			}
		}
		if err := c.SaveImages([]string{ref}, dst); err != nil {
			return err
		}
	}
	return nil
}

// LoadImagesDir charge tous les .tar d'un dossier (docker load).
func (c *Client) LoadImagesDir(imagesDir string) error {
	tars, _ := filepath.Glob(filepath.Join(imagesDir, "*.tar"))
	for _, t := range tars {
		if err := c.LoadImages(t); err != nil {
			return err
		}
	}
	return nil
}

func sanitizeRef(ref string) string {
	return strings.NewReplacer("/", "_", ":", "_", "@", "_").Replace(ref)
}

// PullImage tire une image précise (docker pull <ref>).
func (c *Client) PullImage(ref string) error {
	cmd := exec.Command("docker", "pull", ref)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker pull %s: %w\n%s", ref, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// TagImage crée un alias local (docker tag src dst).
func (c *Client) TagImage(src, dst string) error {
	return exec.Command("docker", "tag", src, dst).Run()
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
