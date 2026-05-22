// internal/api/api.go
//
// 🔌 L'API LOCALE — UNIX socket
//
// Le daemon expose une API sur un socket UNIX (un fichier spécial).
// Le CLI se connecte à ce socket pour envoyer des commandes JSON.
//
// POURQUOI UN SOCKET UNIX plutôt qu'un port TCP ?
// - Pas de conflit de port réseau
// - Sécurité par les permissions de fichier (chmod 660)
// - Plus rapide (pas de couche réseau)
// - Idiomatique pour les daemons Linux (systemd, Docker, etc.)
//
// FLUX D'UNE REQUÊTE :
//   CLI écrit: {"command":"install","args":{"app":"jellyfin"}}
//   daemon lit → traite → écrit: {"success":true,"data":{...}}

package api

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/gaiver-it/caleope/internal/backup"
	"github.com/gaiver-it/caleope/internal/docker"
	"github.com/gaiver-it/caleope/internal/events"
	"github.com/gaiver-it/caleope/internal/install"
	"github.com/gaiver-it/caleope/internal/metrics"
	"github.com/gaiver-it/caleope/internal/network"
	"github.com/gaiver-it/caleope/internal/runtime"
	"github.com/gaiver-it/caleope/internal/store"
	"github.com/gaiver-it/caleope/pkg/types"
	"github.com/gaiver-it/caleope/pkg/version"
)

// SOCKET_PATH est le chemin du fichier socket UNIX.
const SOCKET_PATH = "/run/caleoped.sock"

// Server est le serveur API du daemon.
type Server struct {
	socketPath string
	rt         *runtime.Manager
	st         *store.Store
	installer  *install.Installer
	bkp        *backup.Manager
	dc         *docker.Client
	col        *metrics.Collector
	emitter    *events.Emitter
	net        *network.Manager
	baseDir    string
	token      string
}

func NewServer(
	socketPath string,
	rt *runtime.Manager,
	st *store.Store,
	installer *install.Installer,
	bkp *backup.Manager,
	dc *docker.Client,
	col *metrics.Collector,
	emitter *events.Emitter,
	net *network.Manager,
	baseDir string,
) *Server {
	return &Server{
		socketPath: socketPath,
		rt:         rt,
		st:         st,
		installer:  installer,
		bkp:        bkp,
		dc:         dc,
		col:        col,
		emitter:    emitter,
		net:        net,
		baseDir:    baseDir,
		token:      loadOrCreateToken(baseDir),
	}
}

// ─────────────────────────────────────────────
// DÉMARRAGE DU SERVEUR
// ─────────────────────────────────────────────

// Listen démarre le serveur et écoute les connexions.
// Cette fonction bloque jusqu'à ce que le programme se termine.
func (s *Server) Listen() error {
	// Supprimer le socket existant s'il reste d'un crash précédent
	_ = os.Remove(s.socketPath)

	// Créer le socket UNIX
	// net.Listen("unix", path) = créer un socket UNIX (comme un serveur TCP mais local)
	listener, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return fmt.Errorf("impossible de créer le socket %s: %w", s.socketPath, err)
	}
	defer listener.Close()

	// Sécuriser le socket : seulement root et groupe caleope peuvent écrire
	if err := os.Chmod(s.socketPath, 0660); err != nil {
		return fmt.Errorf("chmod socket: %w", err)
	}

	fmt.Printf("✓ Daemon en écoute sur %s\n", s.socketPath)

	// Boucle principale : accepter les connexions entrantes
	for {
		// Accept() bloque jusqu'à ce qu'un client se connecte
		conn, err := listener.Accept()
		if err != nil {
			// Si le listener est fermé (arrêt du daemon), on sort proprement
			if isClosedError(err) {
				return nil
			}
			fmt.Printf("erreur accept: %v\n", err)
			continue
		}

		// Lancer chaque connexion dans une goroutine séparée
		// "go func()" = lancer en arrière-plan (concurrent)
		// Ainsi, le daemon peut traiter plusieurs requêtes en parallèle
		go s.handleConnection(conn)
	}
}

// handleConnection traite une connexion cliente du début à la fin.
func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close() // Toujours fermer la connexion à la fin

	// Décoder la requête JSON
	var req types.APIRequest
	decoder := json.NewDecoder(conn)
	if err := decoder.Decode(&req); err != nil {
		s.writeError(conn, "requête JSON invalide: "+err.Error())
		return
	}

	// Router vers le bon handler selon la commande
	var (
		data interface{}
		err  error
	)

	switch req.Command {
	case "install":
		data, err = s.handleInstall(req.Args)
	case "remove":
		err = s.handleRemove(req.Args)
	case "list":
		data, err = s.handleList()
	case "info":
		data, err = s.handleInfo(req.Args)
	case "logs":
		data, err = s.handleLogs(req.Args)
	case "search":
		data, err = s.handleSearch(req.Args)
	case "stats":
		data, err = s.handleStats(req.Args)
	case "stop":
		err = s.handleStop(req.Args)
	case "start":
		err = s.handleStart(req.Args)
	case "restart":
		err = s.handleRestart(req.Args)
	case "backup":
		data, err = s.handleBackup(req.Args)
	case "restore":
		err = s.handleRestore(req.Args)
	case "backup-list":
		data, err = s.handleBackupList(req.Args)
	case "update":
		err = s.handleUpdate(req.Args)
	case "upgrade":
		data, err = s.handleUpgrade(req.Args)
	case "events":
		data, err = s.handleEvents(req.Args)
	case "location-list":
		data, err = s.handleLocationList()
	case "location-add":
		data, err = s.handleLocationAdd(req.Args)
	case "location-remove":
		err = s.handleLocationRemove(req.Args)
	case "location-mount":
		err = s.handleLocationMount(req.Args)
	case "location-unmount":
		err = s.handleLocationUnmount(req.Args)
	case "ping":
		cfg, _ := s.rt.GetConfig()
		channel := cfg.Channel
		if channel == "" {
			channel = "stable"
		}
		data = map[string]string{
			"status":     "ok",
			"version":    version.Version,
			"commit":     version.Commit,
			"domain":     cfg.Domain,
			"proxy_mode": cfg.ProxyMode,
			"channel":    channel,
		}
	case "token":
		data = map[string]string{"token": s.token}
	default:
		err = fmt.Errorf("commande inconnue: %s", req.Command)
	}

	// Renvoyer la réponse
	if err != nil {
		s.writeError(conn, err.Error())
	} else {
		s.writeSuccess(conn, data)
	}
}

// ─────────────────────────────────────────────
// HANDLERS — un par commande
// ─────────────────────────────────────────────

func (s *Server) handleInstall(args map[string]string) (interface{}, error) {
	appID, ok := args["app"]
	if !ok || appID == "" {
		return nil, fmt.Errorf("argument 'app' manquant")
	}

	// Résoudre le domaine :
	// 1. Si l'utilisateur a fourni --domain → on l'utilise tel quel
	// 2. Sinon → on construit <appID>.<domaine_base> depuis caleope.conf
	domain := args["domain"]
	if domain == "" {
		domain = s.rt.AppDomain(appID)
	}

	opts := install.InstallOptions{
		AppID:  appID,
		Domain: domain,
		Channel: func() string {
			if c := args["channel"]; c != "" {
				return c
			}
			return "stable"
		}(),
	}

	if err := s.installer.Install(opts); err != nil {
		return nil, err
	}

	// Lire les notes post-install écrites par setup.sh (credentials, instructions...)
	notesPath := filepath.Join(s.baseDir, "apps-installed", appID, "post-install.txt")
	if notes, err := os.ReadFile(notesPath); err == nil {
		return map[string]string{"notes": string(notes)}, nil
	}

	return nil, nil
}

func (s *Server) handleRemove(args map[string]string) error {
	appID, ok := args["app"]
	if !ok || appID == "" {
		return fmt.Errorf("argument 'app' manquant")
	}

	keepData := args["keep_data"] == "true"
	return s.installer.Remove(appID, keepData)
}

func (s *Server) handleList() (interface{}, error) {
	apps, err := s.rt.ListApps()
	if err != nil {
		return nil, err
	}
	return apps, nil
}

func (s *Server) handleInfo(args map[string]string) (interface{}, error) {
	appID, ok := args["app"]
	if !ok || appID == "" {
		return nil, fmt.Errorf("argument 'app' manquant")
	}
	return s.rt.GetApp(appID)
}

func (s *Server) handleLogs(args map[string]string) (interface{}, error) {
	appID, ok := args["app"]
	if !ok || appID == "" {
		return nil, fmt.Errorf("argument 'app' manquant")
	}

	app, err := s.rt.GetApp(appID)
	if err != nil {
		return nil, err
	}

	tail := 100
	if t := args["tail"]; t != "" {
		if n, err := fmt.Sscanf(t, "%d", &tail); n != 1 || err != nil {
			tail = 100
		}
	}

	logs, err := s.dc.Logs(app.ComposeDir, tail)
	if err != nil {
		return nil, fmt.Errorf("impossible de lire les logs: %w", err)
	}

	return map[string]string{"logs": logs}, nil
}

func (s *Server) handleSearch(args map[string]string) (interface{}, error) {
	term := args["term"]
	repos, err := s.rt.GetRepos()
	if err != nil {
		return nil, err
	}
	return s.st.Search(term, repos)
}

func (s *Server) handleStats(args map[string]string) (interface{}, error) {
	withDisk := args["disk"] == "true"
	return s.col.Collect(withDisk)
}

func (s *Server) handleStop(args map[string]string) error {
	appID, ok := args["app"]
	if !ok || appID == "" {
		return fmt.Errorf("argument 'app' manquant")
	}
	app, err := s.rt.GetApp(appID)
	if err != nil {
		return err
	}
	if err := s.dc.Stop(app.ComposeDir); err != nil {
		return err
	}
	app.Status = types.StatusStopped
	_ = s.emitter.AppStopped(appID)
	return s.rt.SaveApp(app)
}

func (s *Server) handleStart(args map[string]string) error {
	appID, ok := args["app"]
	if !ok || appID == "" {
		return fmt.Errorf("argument 'app' manquant")
	}
	app, err := s.rt.GetApp(appID)
	if err != nil {
		return err
	}
	if err := s.dc.Start(app.ComposeDir); err != nil {
		return err
	}
	app.Status = types.StatusRunning
	_ = s.emitter.AppStarted(appID)
	return s.rt.SaveApp(app)
}

func (s *Server) handleRestart(args map[string]string) error {
	if err := s.handleStop(args); err != nil {
		return err
	}
	return s.handleStart(args)
}

func (s *Server) handleBackup(args map[string]string) (interface{}, error) {
	appID, ok := args["app"]
	if !ok || appID == "" {
		return nil, fmt.Errorf("argument 'app' manquant")
	}

	backupDir, err := s.bkp.Backup(appID)
	if err != nil {
		return nil, err
	}

	return map[string]string{
		"backup_dir": backupDir,
		"message":    fmt.Sprintf("Backup de '%s' créé dans %s", appID, backupDir),
	}, nil
}

func (s *Server) handleRestore(args map[string]string) error {
	appID, ok := args["app"]
	if !ok || appID == "" {
		return fmt.Errorf("argument 'app' manquant")
	}

	return s.bkp.Restore(appID, args["backup"])
}

func (s *Server) handleBackupList(args map[string]string) (interface{}, error) {
	appID, ok := args["app"]
	if !ok || appID == "" {
		return nil, fmt.Errorf("argument 'app' manquant")
	}

	return s.bkp.ListBackups(appID)
}

func (s *Server) handleUpdate(args map[string]string) error {
	repos, err := s.rt.GetRepos()
	if err != nil {
		return err
	}
	var syncErr error
	for i := range repos {
		if err := s.st.SyncRepo(&repos[i]); err != nil {
			fmt.Printf("⚠️  Erreur sync repo %s: %v\n", repos[i].Name, err)
			syncErr = err
		}
	}
	return syncErr
}

// ─────────────────────────────────────────────
// EVENTS
// ─────────────────────────────────────────────

func (s *Server) handleEvents(args map[string]string) (interface{}, error) {
	filter := events.EventFilter{
		App:  args["app"],
		Type: args["type"],
	}
	if n := args["limit"]; n != "" {
		fmt.Sscanf(n, "%d", &filter.Limit)
	}
	return s.emitter.Read(filter)
}

// ─────────────────────────────────────────────
// NETWORK LOCATIONS
// ─────────────────────────────────────────────

func (s *Server) handleLocationList() (interface{}, error) {
	return s.net.List()
}

func (s *Server) handleLocationAdd(args map[string]string) (interface{}, error) {
	name := args["name"]
	if name == "" {
		return nil, fmt.Errorf("argument 'name' manquant")
	}
	locType := types.NetworkLocationType(args["type"])
	if locType == "" {
		return nil, fmt.Errorf("argument 'type' manquant (smb, cifs, sftp)")
	}

	loc := types.NetworkLocation{
		Name:     name,
		Type:     locType,
		Host:     args["host"],
		Share:    args["share"],
		Username: args["username"],
		Options:  args["options"],
	}

	if err := s.net.Add(loc, args["password"]); err != nil {
		return nil, err
	}
	return map[string]string{
		"message":     fmt.Sprintf("Emplacement '%s' ajouté", name),
		"mount_point": s.net.MountPoint(name),
	}, nil
}

func (s *Server) handleLocationRemove(args map[string]string) error {
	name := args["name"]
	if name == "" {
		return fmt.Errorf("argument 'name' manquant")
	}
	return s.net.Remove(name)
}

func (s *Server) handleLocationMount(args map[string]string) error {
	name := args["name"]
	if name == "" {
		return fmt.Errorf("argument 'name' manquant")
	}
	return s.net.Mount(name)
}

func (s *Server) handleLocationUnmount(args map[string]string) error {
	name := args["name"]
	if name == "" {
		return fmt.Errorf("argument 'name' manquant")
	}
	return s.net.Unmount(name)
}

// ─────────────────────────────────────────────
// HELPERS — écriture des réponses
// ─────────────────────────────────────────────

func (s *Server) writeSuccess(conn net.Conn, data interface{}) {
	resp := types.APIResponse{Success: true, Data: data}
	_ = json.NewEncoder(conn).Encode(resp)
}

func (s *Server) writeError(conn net.Conn, errMsg string) {
	resp := types.APIResponse{Success: false, Error: errMsg}
	_ = json.NewEncoder(conn).Encode(resp)
}

// isClosedError vérifie si une erreur est due à la fermeture du listener.
func isClosedError(err error) bool {
	return err != nil && (err.Error() == "use of closed network connection" ||
		os.IsNotExist(err))
}

func (s *Server) handleUpgrade(args map[string]string) (interface{}, error) {
	// Déterminer le canal (stable ou alpha) depuis caleope.conf
	cfg, _ := s.rt.GetConfig()
	channel := cfg.Channel
	if channel == "" {
		channel = "stable"
	}

	// Choisir l'endpoint GitHub selon le canal
	// stable → /releases/latest (ignore les pré-releases)
	// alpha  → /releases?per_page=1 (inclut les pré-releases, plus récent en premier)
	var apiURL string
	if channel == "alpha" {
		apiURL = "https://api.github.com/repos/gaiver-it/caleope/releases?per_page=1"
	} else {
		apiURL = "https://api.github.com/repos/gaiver-it/caleope/releases/latest"
	}

	cmd := exec.Command("curl", "-fsSL", apiURL)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("impossible de contacter GitHub: %w", err)
	}

	// Parser le JSON de la release (format différent selon le canal)
	type releaseInfo struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}
	var release releaseInfo
	if channel == "alpha" {
		var releases []releaseInfo
		if err := json.Unmarshal(out, &releases); err != nil || len(releases) == 0 {
			return nil, fmt.Errorf("réponse GitHub invalide (canal alpha): %w", err)
		}
		release = releases[0]
	} else {
		if err := json.Unmarshal(out, &release); err != nil {
			return nil, fmt.Errorf("réponse GitHub invalide: %w", err)
		}
	}

	latest := release.TagName
	current := version.Version

	// Comparer les versions
	if latest == current {
		return map[string]string{
			"status":  "up_to_date",
			"version": current,
			"message": "Caleope est déjà à jour",
		}, nil
	}

	// Si --check seulement, ne pas télécharger
	if args["check"] == "true" {
		return map[string]string{
			"status":   "update_available",
			"current":  current,
			"latest":   latest,
			"url":      release.HTMLURL,
			"message":  fmt.Sprintf("Mise à jour disponible : %s → %s", current, latest),
		}, nil
	}

	// Télécharger et remplacer les binaires
	baseURL := fmt.Sprintf("https://github.com/gaiver-it/caleope/releases/download/%s", latest)

	fmt.Printf("→ Téléchargement de Caleope %s...\n", latest)

	for _, bin := range []struct{ name, dest string }{
		{"caleoped-linux-amd64", "/usr/local/bin/caleoped.new"},
		{"caleope-linux-amd64", "/usr/local/bin/caleope.new"},
	} {
		dlCmd := exec.Command("curl", "-fsSL",
			fmt.Sprintf("%s/%s", baseURL, bin.name),
			"-o", bin.dest,
		)
		if err := dlCmd.Run(); err != nil {
			return nil, fmt.Errorf("téléchargement %s échoué: %w", bin.name, err)
		}
		// Rendre exécutable
		if err := exec.Command("chmod", "755", bin.dest).Run(); err != nil {
			return nil, fmt.Errorf("chmod %s: %w", bin.dest, err)
		}
	}

	// Remplacer les binaires (move atomique)
	for _, pair := range []struct{ src, dst string }{
		{"/usr/local/bin/caleoped.new", "/usr/local/bin/caleoped"},
		{"/usr/local/bin/caleope.new", "/usr/local/bin/caleope"},
	} {
		if err := exec.Command("mv", "-f", pair.src, pair.dst).Run(); err != nil {
			return nil, fmt.Errorf("remplacement %s: %w", pair.dst, err)
		}
	}

	// Symlink de compatibilité : caleope-store → caleope
	_ = exec.Command("ln", "-sf", "/usr/local/bin/caleope", "/usr/local/bin/caleope-store").Run()

	// Mettre à jour caleope.conf
	confPath := fmt.Sprintf("%s/caleope.conf", s.baseDir)
	confData, _ := os.ReadFile(confPath)
	newConf := strings.ReplaceAll(string(confData),
		"CALEOPE_VERSION="+current,
		"CALEOPE_VERSION="+latest,
	)
	_ = os.WriteFile(confPath, []byte(newConf), 0644)

	fmt.Printf("✅ Caleope mis à jour vers %s\n", latest)
	fmt.Println("→ Redémarrage du daemon dans 2 secondes...")

	// Redémarrer le daemon via systemd (en arrière-plan)
	go func() {
		time.Sleep(2 * time.Second)
		_ = exec.Command("systemctl", "restart", "caleoped").Run()
	}()

	return map[string]string{
		"status":  "upgraded",
		"from":    current,
		"to":      latest,
		"message": fmt.Sprintf("Mis à jour %s → %s, redémarrage en cours...", current, latest),
	}, nil
}
