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
	"strconv"
	"strings"
	"time"

	"github.com/gaiver-it/caleope/internal/audit"
	"github.com/gaiver-it/caleope/internal/backup"
	"github.com/gaiver-it/caleope/internal/docker"
	"github.com/gaiver-it/caleope/internal/events"
	"github.com/gaiver-it/caleope/internal/install"
	"github.com/gaiver-it/caleope/internal/license"
	"github.com/gaiver-it/caleope/internal/metrics"
	"github.com/gaiver-it/caleope/internal/network"
	"github.com/gaiver-it/caleope/internal/runtime"
	"github.com/gaiver-it/caleope/internal/scheduler"
	"github.com/gaiver-it/caleope/internal/secrets"
	"github.com/gaiver-it/caleope/internal/shares"
	"github.com/gaiver-it/caleope/internal/store"
	"github.com/gaiver-it/caleope/internal/vms"
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
	sh         *shares.Manager
	vm         *vms.Manager
	sched      *scheduler.Scheduler
	baseDir    string
	token      string
	lic        *license.Manager
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
	sh *shares.Manager,
	vm *vms.Manager,
	baseDir string,
	lic *license.Manager,
) *Server {
	s := &Server{
		socketPath: socketPath,
		rt:         rt,
		st:         st,
		installer:  installer,
		bkp:        bkp,
		dc:         dc,
		col:        col,
		emitter:    emitter,
		net:        net,
		sh:         sh,
		vm:         vm,
		baseDir:    baseDir,
		token:      loadOrCreateToken(baseDir),
		lic:        lic,
	}
	// Le scheduler est injecté après construction (le daemon appelle scheduler.New(baseDir, server))
	return s
}

// SetScheduler injecte le scheduler dans le server (appelé par le daemon après construction).
func (s *Server) SetScheduler(sched *scheduler.Scheduler) {
	s.sched = sched
}

// ── Implémentation de scheduler.Runner ────────────────────────────────────────

func (s *Server) RunBackup(app string, scope types.BackupScope) error {
	if app == "" {
		_, errs := s.bkp.BackupAll(scope)
		if len(errs) > 0 {
			msgs := make([]string, len(errs))
			for i, e := range errs {
				msgs[i] = e.Error()
			}
			return fmt.Errorf("%d erreur(s): %s", len(errs), strings.Join(msgs, "; "))
		}
		return nil
	}
	_, err := s.bkp.BackupWithScope(app, scope)
	return err
}

func (s *Server) RunUpgrade() error {
	_, err := s.handleUpgrade(map[string]string{})
	return err
}

func (s *Server) RunUpdate() error {
	return s.handleUpdate(map[string]string{})
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
	case "license.activate":
		data, err = s.handleLicenseActivate(req.Args)
	case "license.status":
		data, err = s.handleLicenseStatus()
	case "retention.get":
		data = s.bkp.Retention()
	case "retention.set":
		data, err = s.handleRetentionSet(req.Args)
	case "retention.apply":
		data, err = s.handleRetentionApply(req.Args)
	case "license.export":
		data, err = s.handleLicenseExport()
	case "license.import":
		data, err = s.handleLicenseImport(req.Args)
	case "install":
		// ⚠️ NE PAS REMETTRE DE VERROU DE LICENCE ICI.
		//
		// Cette commande était autrefois refusée en l'absence de licence activée.
		// Conséquences constatées, toutes mauvaises :
		//   • l'édition Community, qui est GRATUITE, devenait inutilisable dès que
		//     le jeton manquait — réinstallation, nouveau serveur, ou simple
		//     restauration d'une sauvegarde Caleope (core/license n'y était pas) ;
		//   • le produit gratuit dépendait donc du serveur de licences pour
		//     fonctionner, ce qui interdisait de l'éteindre ;
		//   • et l'utilisateur bloqué n'avait aucun recours en autonomie.
		//
		// La règle est désormais simple et tient en une phrase : la licence ne
		// conditionne QUE les fonctions Pro (cf. vmRequirePro dans vms.go).
		// Installer une application du catalogue est gratuit, pour tout le monde.
		data, err = s.handleInstall(req.Args)
	case "store-params":
		data, err = s.handleStoreParams(req.Args)
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
	case "export":
		data, err = s.handleExport(req.Args)
	case "import":
		err = s.handleImport(req.Args)
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
		data, err = s.handleLocationMount(req.Args)
	case "location-unmount":
		err = s.handleLocationUnmount(req.Args)
	case "location-storage":
		data, err = s.handleLocationStorage(req.Args)
	case "shares-list":
		data, err = s.handleSharesList()
	case "shares-add":
		data, err = s.handleSharesUpsert(req.Args, false)
	case "shares-update":
		data, err = s.handleSharesUpsert(req.Args, true)
	case "shares-remove":
		err = s.handleSharesRemove(req.Args)
	case "shares-ensure-user":
		err = s.handleSharesEnsureUser(req.Args)
	case "shares-set-password":
		err = s.handleSharesSetPassword(req.Args)
	case "vms-list":
		data, err = s.handleVMsList()
	case "vm-create":
		err = s.handleVMCreate(req.Args)
	case "vm-start":
		err = s.handleVMAction(req.Args, "start")
	case "vm-stop":
		err = s.handleVMAction(req.Args, "stop")
	case "vm-force-stop":
		err = s.handleVMAction(req.Args, "force-stop")
	case "vm-delete":
		err = s.handleVMAction(req.Args, "delete")
	case "packs-list":
		data, err = s.handlePacksList()
	case "pack-status":
		data, err = s.handlePackStatus(req.Args["id"])
	case "pack-install":
		data, err = s.handlePackInstall(req.Args)
	case "task-list":
		data, err = s.handleTaskList()
	case "task-add":
		data, err = s.handleTaskAdd(req.Args)
	case "task-remove":
		err = s.handleTaskRemove(req.Args)
	case "task-run":
		err = s.handleTaskRun(req.Args)
	case "task-toggle":
		err = s.handleTaskToggle(req.Args)
	case "configure":
		data, err = s.handleConfigure(req.Args)
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
	case "secrets-show":
		data, err = s.handleSecretsShow(req.Args)
	case "audit-list":
		data, err = s.handleAuditList(req.Args)
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

// peekManifest lit rapidement le app.json d'une app sans déclencher l'installation.
// Retourne nil si l'app est introuvable (pas d'erreur fatale).
func (s *Server) peekManifest(appID string) *types.AppManifest {
	repos, err := s.rt.GetRepos()
	if err != nil {
		return nil
	}
	appDir, _, err := s.st.Resolve(appID, repos)
	if err != nil {
		return nil
	}
	m, _ := s.st.ReadManifest(appDir)
	return m
}

func (s *Server) handleInstall(args map[string]string) (interface{}, error) {
	appID, ok := args["app"]
	if !ok || appID == "" {
		return nil, fmt.Errorf("argument 'app' manquant")
	}

	// Résoudre le domaine :
	// 1. Si l'utilisateur a fourni --domain → on l'utilise tel quel
	// 2. Si le manifest indique use_base_domain → domaine racine (pas appID.domain)
	//    ex: arr-stack → caleope.guernaham.bzh (et non arr-stack.caleope.guernaham.bzh)
	// 3. Sinon → <appID>.<domaine_base> depuis caleope.conf
	domain := args["domain"]
	if domain == "" {
		if m := s.peekManifest(appID); m != nil && m.UseBaseDomain {
			domain = s.rt.BaseDomain()
		} else {
			domain = s.rt.AppDomain(appID)
		}
	}

	// Extraire les paramètres interactifs transmis par le CLI sous la forme param_KEY=VALUE
	params := map[string]string{}
	for k, v := range args {
		if strings.HasPrefix(k, "param_") {
			params[strings.TrimPrefix(k, "param_")] = v
		}
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
		Params: params,
		Force:  args["force"] == "true",
		GPU:    args["gpu"] == "true",
	}

	// Stockage NAS : résoudre le chemin de données avant l'installation
	if storageLocation := args["storage"]; storageLocation != "" {
		opts.StorageLocation = storageLocation
		opts.StorageDataDir = s.net.AppDataDir(storageLocation, appID)
	}

	// Synchroniser le store sur la branche du canal demandé AVANT de résoudre l'app.
	// Sans ça, un `install <app> --alpha` depuis un serveur en canal stable ne trouve
	// jamais les apps alpha : le cache est resté sur la branche main. Best-effort —
	// si le fetch échoue, on continue (Resolve renverra "introuvable" si nécessaire).
	if err := s.handleUpdate(map[string]string{"channel": opts.Channel}); err != nil {
		fmt.Printf("⚠️  Sync store (canal %s) avant install: %v\n", opts.Channel, err)
	}

	if err := s.installer.Install(opts); err != nil {
		return nil, err
	}

	// Lire les notes post-install écrites par setup.sh (credentials, instructions...)
	// setup.sh écrit dans app-config/<app>/post-install.txt
	notesPath := filepath.Join(s.baseDir, "app-config", appID, "post-install.txt")
	if notes, err := os.ReadFile(notesPath); err == nil {
		return map[string]string{"notes": string(notes)}, nil
	}

	return nil, nil
}

// handleStoreParams retourne la liste des params interactifs d'une app (params.json).
// Retourne un tableau vide si l'app n'a pas de params.json.
func (s *Server) handleStoreParams(args map[string]string) (interface{}, error) {
	appID, ok := args["app"]
	if !ok || appID == "" {
		return nil, fmt.Errorf("argument 'app' manquant")
	}
	repos, err := s.rt.GetRepos()
	if err != nil {
		return nil, err
	}
	appDir, _, err := s.st.Resolve(appID, repos)
	if err != nil {
		return nil, err
	}
	params, err := s.st.ReadParams(appDir)
	if err != nil {
		return nil, err
	}
	if params == nil {
		return []interface{}{}, nil // tableau vide plutôt que null
	}
	return params, nil
}

func (s *Server) handleRemove(args map[string]string) error {
	appID, ok := args["app"]
	if !ok || appID == "" {
		return fmt.Errorf("argument 'app' manquant")
	}

	keepData := args["keep_data"] == "true"
	return s.installer.Remove(appID, keepData)
}

// isPlatformService indique si l'id correspond à un service systemd de la plateforme.
func isPlatformService(id string) bool {
	return id == "caleope-ui" || id == "caleoped"
}

// systemServiceStatus retourne "running" si le service systemd est actif, sinon "stopped".
func systemServiceStatus(name string) string {
	out, err := exec.Command("systemctl", "is-active", name).Output()
	if err != nil {
		return "stopped"
	}
	if strings.TrimSpace(string(out)) == "active" {
		return "running"
	}
	return "stopped"
}

func (s *Server) handleList() (interface{}, error) {
	apps, err := s.rt.ListApps()
	if err != nil {
		return nil, err
	}

	services := []map[string]interface{}{
		{"id": "caleope-ui", "name": "Caleope UI", "status": systemServiceStatus("caleope-ui"), "type": "system"},
		{"id": "caleoped", "name": "Caleope Daemon", "status": systemServiceStatus("caleoped"), "type": "system"},
	}

	return map[string]interface{}{
		"apps":     apps,
		"services": services,
	}, nil
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

	tail := 100
	if t := args["tail"]; t != "" {
		if n, err := fmt.Sscanf(t, "%d", &tail); n != 1 || err != nil {
			tail = 100
		}
	}

	if isPlatformService(appID) {
		out, err := exec.Command("journalctl", "-u", appID,
			fmt.Sprintf("-n%d", tail), "--no-pager").Output()
		if err != nil {
			return nil, fmt.Errorf("journalctl %s: %w", appID, err)
		}
		return map[string]string{"logs": string(out)}, nil
	}

	app, err := s.rt.GetApp(appID)
	if err != nil {
		return nil, err
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
	if isPlatformService(appID) {
		return exec.Command("systemctl", "stop", appID).Run()
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
	if isPlatformService(appID) {
		return exec.Command("systemctl", "start", appID).Run()
	}
	app, err := s.rt.GetApp(appID)
	if err != nil {
		return err
	}
	if err := s.dc.Start(app.ComposeDir); err != nil {
		return err
	}
	// Ré-appliquer la priorité ressources : oom_score_adj est posé sur /proc/<pid> et
	// perdu à chaque (re)démarrage de container → on le repose après le start.
	s.dc.ApplyPriorities(appID, app.Priority)
	app.Status = types.StatusRunning
	_ = s.emitter.AppStarted(appID)
	return s.rt.SaveApp(app)
}

func (s *Server) handleRestart(args map[string]string) error {
	appID, ok := args["app"]
	if !ok || appID == "" {
		return fmt.Errorf("argument 'app' manquant")
	}
	if isPlatformService(appID) {
		if appID == "caleoped" {
			// Le daemon redémarre lui-même : on laisse la réponse partir avant de couper.
			go func() {
				time.Sleep(300 * time.Millisecond)
				_ = exec.Command("systemctl", "restart", "caleoped").Run()
			}()
			return nil
		}
		return exec.Command("systemctl", "restart", appID).Run()
	}
	app, err := s.rt.GetApp(appID)
	if err != nil {
		return err
	}
	// docker compose up -d relit les env_file et recrée les containers si nécessaire.
	// Contrairement à stop+start qui garde les variables d'env figées en mémoire,
	// up -d applique les changements de secrets.env sans avoir à réinstaller l'app.
	if err := s.dc.Up(app.ComposeDir); err != nil {
		return err
	}
	// Ré-appliquer la priorité ressources (oom_score_adj perdu à la recréation du container).
	s.dc.ApplyPriorities(appID, app.Priority)
	app.Status = types.StatusRunning
	_ = s.emitter.AppStarted(appID)
	return s.rt.SaveApp(app)
}

// handleConfigure met à jour les secrets d'une app et redémarre la stack.
// args contient "app" + les paires clé=valeur à modifier dans secrets.env.
func (s *Server) handleConfigure(args map[string]string) (interface{}, error) {
	appID, ok := args["app"]
	if !ok || appID == "" {
		return nil, fmt.Errorf("argument 'app' manquant")
	}

	// Retirer "app" de la map : le reste = updates à appliquer
	updates := make(map[string]string, len(args)-1)
	for k, v := range args {
		if k != "app" {
			updates[k] = v
		}
	}

	if len(updates) == 0 {
		return nil, fmt.Errorf("aucune mise à jour fournie")
	}

	if err := s.installer.Reconfigure(appID, updates); err != nil {
		return nil, err
	}

	// Retourner les notes post-install mises à jour (setup.sh les génère à l'install,
	// Reconfigure les patche — on les inclut ici pour que le CLI puisse les afficher).
	result := map[string]string{"message": fmt.Sprintf("'%s' reconfiguré et redémarré", appID)}
	notesPath := filepath.Join(s.baseDir, "app-config", appID, "post-install.txt")
	if notes, readErr := os.ReadFile(notesPath); readErr == nil {
		result["notes"] = string(notes)
	}
	return result, nil
}

func (s *Server) handleBackup(args map[string]string) (interface{}, error) {
	appID, ok := args["app"]
	if !ok || appID == "" {
		return nil, fmt.Errorf("argument 'app' manquant")
	}

	// Backend Restic si demandé
	if args["restic"] == "true" {
		repo := args["repo"]
		if repo == "" {
			return nil, fmt.Errorf("--repo requis avec --restic (ex: sftp:user@host:/path)")
		}
		repoURL, err := s.bkp.ResticBackup(appID, repo, args["restic_password"])
		if err != nil {
			return nil, err
		}
		return map[string]string{
			"repo":    repoURL,
			"message": fmt.Sprintf("Backup Restic de '%s' → %s", appID, repoURL),
		}, nil
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

// handleExport crée une archive d'export auto-suffisante (données + config +
// définition + images). withImages par défaut = true (restore hors-ligne).
func (s *Server) handleExport(args map[string]string) (interface{}, error) {
	appID, ok := args["app"]
	if !ok || appID == "" {
		return nil, fmt.Errorf("argument 'app' manquant")
	}
	withImages := args["no_images"] != "true"
	path, err := s.bkp.Export(appID, args["dest"], withImages)
	if err != nil {
		return nil, err
	}
	return map[string]string{
		"path":    path,
		"message": fmt.Sprintf("Export de '%s' créé : %s", appID, path),
	}, nil
}

// handleImport recrée une app depuis une archive d'export (mode legacy|migrate).
func (s *Server) handleImport(args map[string]string) error {
	archive := args["archive"]
	if archive == "" {
		return fmt.Errorf("argument 'archive' manquant")
	}
	mode := args["mode"]
	if mode == "" {
		mode = "legacy"
	}
	return s.bkp.Import(archive, mode)
}

func (s *Server) handleBackupList(args map[string]string) (interface{}, error) {
	appID, ok := args["app"]
	if !ok || appID == "" {
		return nil, fmt.Errorf("argument 'app' manquant")
	}

	return s.bkp.ListBackups(appID)
}

func (s *Server) handleBackupDelete(args map[string]string) error {
	appID := args["app"]
	dir := args["dir"]
	if appID == "" || dir == "" {
		return fmt.Errorf("arguments 'app' et 'dir' requis")
	}
	return s.bkp.DeleteBackup(appID, dir)
}

func (s *Server) handleUpdate(args map[string]string) error {
	repos, err := s.rt.GetRepos()
	if err != nil {
		return err
	}

	// Si channel=alpha est explicitement demandé, forcer la branche alpha sur tous les repos officiels.
	// Sinon lire le canal depuis caleope.conf pour les repos sans branche explicite.
	channel := ""
	ghToken := ""
	if args != nil {
		channel = args["channel"]
	}
	if cfg, err := s.rt.GetConfig(); err == nil {
		if channel == "" {
			channel = cfg.Channel
		}
		ghToken = cfg.GithubToken
	}

	var syncErr error
	for i := range repos {
		r := &repos[i]
		if r.Branch == "" {
			switch channel {
			case "alpha":
				r.Branch = "alpha"
			default:
				r.Branch = "main"
			}
		}
		// Injecter le token GitHub dans l'URL pour les repos privés
		repoURL := r.URL
		if ghToken != "" && strings.HasPrefix(repoURL, "https://github.com/") {
			repoURL = "https://" + ghToken + "@github.com/" + repoURL[len("https://github.com/"):]
		}
		rCopy := *r
		rCopy.URL = repoURL
		if err := s.st.SyncRepo(&rCopy); err != nil {
			fmt.Printf("⚠️  Erreur sync repo %s: %v\n", r.Name, err)
			syncErr = err
		}
	}
	return syncErr
}

// ─────────────────────────────────────────────
// REGISTRE MIROIR (config UI)
// ─────────────────────────────────────────────

// setConfigKeys upsert des clés dans caleope.conf (remplace la ligne existante
// ou l'ajoute en fin de fichier).
func (s *Server) setConfigKeys(keys map[string]string) error {
	confPath := filepath.Join(s.baseDir, "caleope.conf")
	data, _ := os.ReadFile(confPath)
	lines := strings.Split(string(data), "\n")
	seen := map[string]bool{}
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		for k, v := range keys {
			if strings.HasPrefix(trimmed, k+"=") {
				lines[i] = k + "=" + v
				seen[k] = true
			}
		}
	}
	for k, v := range keys {
		if !seen[k] {
			lines = append(lines, k+"="+v)
		}
	}
	return os.WriteFile(confPath, []byte(strings.Join(lines, "\n")), 0644)
}

// SetRegistry écrit la config du registre dans caleope.conf et tente un docker
// login (best-effort). Un mot de passe vide conserve celui déjà enregistré.
func (s *Server) SetRegistry(registry, user, pass, mode string) error {
	registry = strings.TrimSpace(registry)
	user = strings.TrimSpace(user)
	keys := map[string]string{
		"CALEOPE_REGISTRY":      registry,
		"CALEOPE_REGISTRY_USER": user,
		"CALEOPE_REGISTRY_MODE": strings.TrimSpace(mode),
	}
	if pass != "" {
		keys["CALEOPE_REGISTRY_PASS"] = pass
	}
	if err := s.setConfigKeys(keys); err != nil {
		return err
	}
	if registry != "" && user != "" {
		if pass == "" {
			if cfg, _ := s.rt.GetConfig(); cfg != nil {
				pass = cfg.RegistryPass
			}
		}
		_ = exec.Command("docker", "login", registry, "-u", user, "-p", pass).Run()
	}
	return nil
}

// RegistryStatus retourne la config registre pour l'UI (sans exposer le mot de passe).
func (s *Server) RegistryStatus() map[string]interface{} {
	res := map[string]interface{}{"registry": "", "user": "", "has_pass": false}
	if cfg, _ := s.rt.GetConfig(); cfg != nil {
		res["registry"] = cfg.Registry
		res["user"] = cfg.RegistryUser
		res["has_pass"] = cfg.RegistryPass != ""
		res["mode"] = cfg.RegistryMode
	}
	return res
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
		return nil, fmt.Errorf("argument 'type' manquant (smb, cifs, sftp, local)")
	}

	// Pour le type local, le champ "device" indique le périphérique (/dev/sdb1).
	// On le stocke dans Host pour réutiliser la structure NetworkLocation.
	host := args["host"]
	if locType == types.LocationLocal && args["device"] != "" {
		host = args["device"]
	}

	loc := types.NetworkLocation{
		Name:     name,
		Type:     locType,
		Host:     host,
		Share:    args["share"],
		Username: args["username"],
		Options:  args["options"],
	}

	if err := s.net.Add(loc, args["password"]); err != nil {
		return nil, err
	}

	// Tentative de montage immédiat pour valider les identifiants
	result := map[string]interface{}{
		"message":     fmt.Sprintf("Emplacement '%s' ajouté", name),
		"mount_point": s.net.MountPoint(name),
		"mounted":     false,
	}

	if mountErr := s.net.Mount(name); mountErr != nil {
		// Le montage a échoué — l'emplacement est enregistré mais pas monté
		result["mount_error"] = mountErr.Error()
	} else {
		result["mounted"] = true
		// Créer la structure caleope/ sur le NAS
		if err := s.net.EnsureCaleopeStructure(name); err == nil {
			result["caleope_dir"] = s.net.CaleopeDir(name)
		}
		// Lister les fichiers pour confirmer l'accès
		if files, err := s.net.ListFiles(name, 20); err == nil {
			result["files"] = files
		}
	}

	return result, nil
}

func (s *Server) handleLocationRemove(args map[string]string) error {
	name := args["name"]
	if name == "" {
		return fmt.Errorf("argument 'name' manquant")
	}
	return s.net.Remove(name)
}

func (s *Server) handleLocationMount(args map[string]string) (interface{}, error) {
	name := args["name"]
	if name == "" {
		return nil, fmt.Errorf("argument 'name' manquant")
	}
	if err := s.net.Mount(name); err != nil {
		return nil, err
	}
	// Créer la structure caleope/ si pas encore présente
	_ = s.net.EnsureCaleopeStructure(name)
	result := map[string]interface{}{
		"mounted":     true,
		"mount_point": s.net.MountPoint(name),
		"caleope_dir": s.net.CaleopeDir(name),
	}
	if files, err := s.net.ListFiles(name, 20); err == nil {
		result["files"] = files
	}
	return result, nil
}

func (s *Server) handleLocationUnmount(args map[string]string) error {
	name := args["name"]
	if name == "" {
		return fmt.Errorf("argument 'name' manquant")
	}
	return s.net.Unmount(name)
}

// handleLocationStorage migre les données d'une app vers un emplacement NAS
// (ou les rapatrie en local si location == "local").
// Flux : stop → rsync → symlink → start
func (s *Server) handleLocationStorage(args map[string]string) (interface{}, error) {
	appID := args["app"]
	locationName := args["location"] // "local" pour rapatrier
	if appID == "" {
		return nil, fmt.Errorf("argument 'app' manquant")
	}

	app, err := s.rt.GetApp(appID)
	if err != nil {
		return nil, fmt.Errorf("application '%s' non trouvée", appID)
	}

	localDataDir := filepath.Join(s.baseDir, "app-data", appID)

	// ── Cas info : pas de location fournie → afficher le stockage actuel ──
	if locationName == "" {
		storage := "local"
		dataDir := localDataDir
		if app.StorageLocation != "" {
			storage = app.StorageLocation
			dataDir = s.net.AppDataDir(app.StorageLocation, appID)
		}
		return map[string]string{
			"app":      appID,
			"storage":  storage,
			"data_dir": dataDir,
		}, nil
	}

	// Déterminer source et destination
	var srcDir, dstDir, newSymlinkTarget string
	var newStorageLocation string

	if locationName == "local" {
		// Migration NAS → local
		if app.StorageLocation == "" {
			return nil, fmt.Errorf("'%s' est déjà en stockage local", appID)
		}
		srcDir = s.net.AppDataDir(app.StorageLocation, appID)
		dstDir = localDataDir + ".tmp_migrate"
		newStorageLocation = ""
		newSymlinkTarget = ""
	} else {
		// Migration local → NAS (ou NAS → autre NAS)
		if app.StorageLocation == locationName {
			return nil, fmt.Errorf("'%s' utilise déjà la location '%s'", appID, locationName)
		}
		// Vérifier que le NAS est monté
		nasDataDir := s.net.AppDataDir(locationName, appID)
		if !strings.HasPrefix(nasDataDir, s.net.MountPoint(locationName)) {
			return nil, fmt.Errorf("location '%s' non montée", locationName)
		}
		// Résoudre la vraie source (suit le symlink si nécessaire)
		realSrc, _ := filepath.EvalSymlinks(localDataDir)
		if realSrc == "" {
			realSrc = localDataDir
		}
		srcDir = realSrc
		dstDir = nasDataDir
		newStorageLocation = locationName
		newSymlinkTarget = nasDataDir
		// Créer la structure caleope/ sur le NAS
		_ = s.net.EnsureCaleopeStructure(locationName)
	}

	// ── Stop containers ──
	fmt.Printf("  [1/4] Arrêt de '%s'...\n", appID)
	_ = s.dc.Stop(app.ComposeDir)

	restartOnError := func() {
		fmt.Printf("  → Redémarrage de '%s' après erreur...\n", appID)
		_ = s.dc.Start(app.ComposeDir)
	}

	// ── Rsync ──
	fmt.Printf("  [2/4] Copie des données vers %s...\n", dstDir)
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		restartOnError()
		return nil, fmt.Errorf("impossible de créer la destination: %w", err)
	}
	cmd := exec.Command("rsync", "-a", "--delete", srcDir+"/", dstDir+"/")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		restartOnError()
		return nil, fmt.Errorf("rsync échoué: %w", err)
	}

	// ── Mettre à jour le symlink ──
	fmt.Println("  [3/4] Mise à jour du lien symbolique...")
	_ = os.Remove(localDataDir)
	if newSymlinkTarget != "" {
		// local → NAS : créer symlink
		if err := os.Symlink(newSymlinkTarget, localDataDir); err != nil {
			restartOnError()
			return nil, fmt.Errorf("impossible de créer le symlink: %w", err)
		}
	} else {
		// NAS → local : renommer le dossier temporaire
		if err := os.Rename(dstDir, localDataDir); err != nil {
			restartOnError()
			return nil, fmt.Errorf("impossible de déplacer les données en local: %w", err)
		}
	}

	// ── Mettre à jour le runtime ──
	app.StorageLocation = newStorageLocation
	_ = s.rt.SaveApp(app)

	// ── Redémarrer ──
	fmt.Printf("  [4/4] Redémarrage de '%s'...\n", appID)
	_ = s.dc.Start(app.ComposeDir)

	storageStr := "local"
	if newStorageLocation != "" {
		storageStr = newStorageLocation
	}
	return map[string]string{
		"app":     appID,
		"storage": storageStr,
		"message": fmt.Sprintf("Données de '%s' migrées vers %s", appID, storageStr),
	}, nil
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
	// Canal : arg explicite > caleope.conf > stable
	channel := args["channel"]
	if channel == "" {
		cfg, _ := s.rt.GetConfig()
		channel = cfg.Channel
	}
	if channel == "" {
		channel = "stable"
	}

	// Récupérer le token GitHub si configuré (nécessaire pour les repos privés)
	ghToken := ""
	if cfg, err := s.rt.GetConfig(); err == nil {
		ghToken = cfg.GithubToken
	}
	curlGH := func(url string) ([]byte, error) {
		curlArgs := []string{"-sSL", "-w", "\n%{http_code}"}
		if ghToken != "" {
			curlArgs = append(curlArgs, "-H", "Authorization: token "+ghToken)
		}
		curlArgs = append(curlArgs, url)
		out, err := exec.Command("curl", curlArgs...).Output()
		if err != nil {
			return nil, fmt.Errorf("erreur réseau vers GitHub: %w", err)
		}
		// La dernière ligne contient le code HTTP
		raw := strings.TrimRight(string(out), "\n")
		lastNL := strings.LastIndex(raw, "\n")
		code := raw[lastNL+1:]
		body := []byte(raw[:lastNL])
		if code != "200" {
			switch code {
			case "403":
				return nil, fmt.Errorf("GitHub API: accès refusé (HTTP 403) — rate limit ou repo privé. Configurez un token: caleope configure --gh-token <token>")
			case "404":
				return nil, fmt.Errorf("GitHub API: aucune release trouvée (HTTP 404) — vérifiez le canal (caleope version)")
			default:
				return nil, fmt.Errorf("GitHub API: HTTP %s pour %s", code, url)
			}
		}
		return body, nil
	}

	// Choisir l'endpoint GitHub selon le canal
	// stable → /releases/latest (ignore les pré-releases)
	// alpha  → /releases?per_page=20, filtré sur prerelease:true
	var apiURL string
	if channel == "alpha" {
		apiURL = "https://api.github.com/repos/gaiver-it/caleope/releases?per_page=20"
	} else {
		apiURL = "https://api.github.com/repos/gaiver-it/caleope/releases/latest"
	}

	out, err := curlGH(apiURL)
	if err != nil {
		return nil, fmt.Errorf("impossible de contacter GitHub: %w", err)
	}

	// Parser le JSON de la release (format différent selon le canal)
	type releaseInfo struct {
		TagName    string `json:"tag_name"`
		Name       string `json:"name"`
		HTMLURL    string `json:"html_url"`
		Prerelease bool   `json:"prerelease"`
	}
	var release releaseInfo
	if channel == "alpha" {
		var releases []releaseInfo
		if err := json.Unmarshal(out, &releases); err != nil {
			return nil, fmt.Errorf("réponse GitHub invalide (canal alpha): %w", err)
		}
		found := false
		for _, r := range releases {
			if r.TagName == "alpha-latest" {
				release = r
				found = true
				break
			}
		}
		if !found {
			return map[string]string{
				"status":  "up_to_date",
				"version": version.Version,
				"message": "Aucune release alpha disponible sur GitHub",
			}, nil
		}
	} else {
		if err := json.Unmarshal(out, &release); err != nil {
			return nil, fmt.Errorf("réponse GitHub invalide: %w", err)
		}
	}

	latest := release.TagName
	current := version.Version

	// Pour le canal alpha, comparer par commit (release.Name = "Caleope alpha (abc1234)")
	// car le tag est toujours "alpha-latest" — la version semver n'est pas comparable.
	if channel == "alpha" {
		latestCommit := ""
		if n := release.Name; len(n) > 0 {
			// Extraire le hash entre parenthèses : "Caleope alpha (f085075)" → "f085075"
			if start := strings.LastIndex(n, "("); start >= 0 {
				if end := strings.LastIndex(n, ")"); end > start {
					latestCommit = n[start+1 : end]
				}
			}
		}
		if latestCommit != "" && latestCommit == version.Commit {
			return map[string]string{
				"status":  "up_to_date",
				"version": current,
				"message": "Caleope est déjà à jour",
			}, nil
		}
	} else if latest == current {
		return map[string]string{
			"status":  "up_to_date",
			"version": current,
			"message": "Caleope est déjà à jour",
		}, nil
	}

	// Si --check seulement, ne pas télécharger
	if args["check"] == "true" {
		return map[string]string{
			"status":  "update_available",
			"current": current,
			"latest":  latest,
			"url":     release.HTMLURL,
			"message": fmt.Sprintf("Mise à jour disponible : %s → %s", current, latest),
		}, nil
	}

	// Étapes numérotées pour feedback CLI
	fmt.Printf("[1/8] Vérification de la version sur GitHub...\n")

	// Télécharger et remplacer les binaires
	baseURL := fmt.Sprintf("https://github.com/gaiver-it/caleope/releases/download/%s", latest)

	fmt.Printf("[2/8] Téléchargement des binaires (caleoped, caleope, caleope-ui)...\n")

	for _, bin := range []struct{ name, dest string }{
		{"caleoped-linux-amd64", "/usr/local/bin/caleoped.new"},
		{"caleope-linux-amd64", "/usr/local/bin/caleope.new"},
		{"caleope-ui-linux-amd64", "/usr/local/bin/caleope-ui.new"},
	} {
		dlArgs := []string{"-fsSL"}
		if ghToken != "" {
			dlArgs = append(dlArgs, "-H", "Authorization: token "+ghToken)
		}
		dlArgs = append(dlArgs, fmt.Sprintf("%s/%s", baseURL, bin.name), "-o", bin.dest)
		dlCmd := exec.Command("curl", dlArgs...)
		if err := dlCmd.Run(); err != nil {
			return nil, fmt.Errorf("téléchargement %s échoué: %w", bin.name, err)
		}
		// Rendre exécutable
		if err := exec.Command("chmod", "755", bin.dest).Run(); err != nil {
			return nil, fmt.Errorf("chmod %s: %w", bin.dest, err)
		}
	}

	fmt.Printf("[3/8] Installation des binaires...\n")

	// Remplacer les binaires (move atomique)
	for _, pair := range []struct{ src, dst string }{
		{"/usr/local/bin/caleoped.new", "/usr/local/bin/caleoped"},
		{"/usr/local/bin/caleope.new", "/usr/local/bin/caleope"},
		{"/usr/local/bin/caleope-ui.new", "/usr/local/bin/caleope-ui"},
	} {
		if err := exec.Command("mv", "-f", pair.src, pair.dst).Run(); err != nil {
			return nil, fmt.Errorf("remplacement %s: %w", pair.dst, err)
		}
	}

	// Symlink de compatibilité : caleope-store → caleope
	_ = exec.Command("ln", "-sf", "/usr/local/bin/caleope", "/usr/local/bin/caleope-store").Run()

	fmt.Printf("[4/8] Configuration Traefik / systemd...\n")

	// S'assurer que caleope-ui.service et sa config Traefik sont en place
	s.EnsureUISetup()
	s.EnsureSecurityHeaders()

	fmt.Printf("[5/8] Mise à jour de la configuration...\n")

	// Mettre à jour caleope.conf
	confPath := fmt.Sprintf("%s/caleope.conf", s.baseDir)
	confData, _ := os.ReadFile(confPath)
	newConf := strings.ReplaceAll(string(confData),
		"CALEOPE_VERSION="+current,
		"CALEOPE_VERSION="+latest,
	)
	_ = os.WriteFile(confPath, []byte(newConf), 0644)

	fmt.Printf("✅ Caleope mis à jour vers %s\n", latest)

	fmt.Printf("[6/8] Synchronisation du store...\n")

	// Synchroniser les dépôts du store en même temps que le binaire,
	// pour que les nouvelles définitions d'apps soient disponibles immédiatement.
	if err := s.handleUpdate(nil); err != nil {
		fmt.Printf("⚠️  Sync store partiel : %v\n", err)
	}

	fmt.Printf("[7/8] Vérification des composants core...\n")

	// Installer les composants essentiels s'ils sont absents.
	// Ces apps font partie de l'infrastructure Caleope et doivent être présentes
	// sur toute installation complète.
	s.ensureCoreApps()

	fmt.Printf("[8/8] Redémarrage des services dans 2 secondes...\n")

	// Redémarrer via un script shell indépendant de ce processus.
	// On ne peut pas faire les deux restarts dans une goroutine : quand
	// "systemctl restart caleoped" s'exécute, il tue ce processus et la
	// goroutine meurt avec lui — caleope-ui ne serait jamais redémarré.
	// caleoped d'abord : Requires=caleoped dans caleope-ui.service
	// implique que si caleoped s'arrête, caleope-ui s'arrête aussi.
	// En redémarrant caleoped en premier puis caleope-ui ensuite, on évite
	// que le restart de caleoped ne stoppe caleope-ui sans le relancer.
	_ = exec.Command("bash", "-c",
		"sleep 2 && systemctl restart caleoped && sleep 2 && systemctl restart caleope-ui",
	).Start()

	return map[string]string{
		"status":  "upgraded",
		"from":    current,
		"to":      latest,
		"message": fmt.Sprintf("Mis à jour %s → %s, redémarrage en cours...", current, latest),
	}, nil
}

// ─────────────────────────────────────────────
// SECRETS — liste et déverrouillage HTTP
// ─────────────────────────────────────────────

// handleSecretsList retourne les métadonnées (sans valeurs) de toutes les apps qui ont des secrets.
func (s *Server) handleSecretsList() (interface{}, error) {
	apps, err := s.rt.ListApps()
	if err != nil {
		return nil, err
	}
	type appSecretInfo struct {
		AppID     string `json:"app_id"`
		AppName   string `json:"app_name"`
		KeyCount  int    `json:"key_count"`
		Encrypted bool   `json:"encrypted"`
	}
	var result []appSecretInfo
	enc := secrets.IsSetup(s.baseDir)
	for _, app := range apps {
		configDir := filepath.Join(s.baseDir, "app-config", app.ID)
		data, err := os.ReadFile(filepath.Join(configDir, "secrets.env"))
		if err != nil {
			continue
		}
		count := 0
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") && strings.Contains(line, "=") {
				count++
			}
		}
		if count > 0 {
			result = append(result, appSecretInfo{
				AppID:     app.ID,
				AppName:   app.Name,
				KeyCount:  count,
				Encrypted: enc,
			})
		}
	}
	if result == nil {
		result = []appSecretInfo{}
	}
	return result, nil
}

// handleSecretsReveal déchiffre et retourne les secrets d'une ou toutes les apps.
// args["app"] peut être vide (toutes les apps) ou préciser une app.
// args["password"] est obligatoire.
func (s *Server) handleSecretsReveal(args map[string]string) (interface{}, error) {
	password := args["password"]
	if password == "" {
		return nil, fmt.Errorf("argument 'password' manquant")
	}

	var dek []byte
	var unlockErr error
	if secrets.IsSetup(s.baseDir) {
		dek, unlockErr = secrets.UnlockDEK(s.baseDir, password)
		if unlockErr != nil {
			audit.Log(audit.ActionSecretsShow, args["app"], "DENIED:wrong-password")
			return nil, fmt.Errorf("mot de passe incorrect")
		}
	}

	apps, err := s.rt.ListApps()
	if err != nil {
		return nil, err
	}

	type appSecrets struct {
		AppID   string            `json:"app_id"`
		AppName string            `json:"app_name"`
		Vars    map[string]string `json:"vars"`
	}
	var result []appSecrets

	for _, app := range apps {
		// Si une app spécifique est demandée, ignorer les autres
		if target := args["app"]; target != "" && target != app.ID {
			continue
		}
		configDir := filepath.Join(s.baseDir, "app-config", app.ID)
		var plaintext string
		if secrets.IsSetup(s.baseDir) {
			plaintext, err = secrets.ShowSecrets(configDir, dek)
			if err != nil {
				continue
			}
		} else {
			data, readErr := os.ReadFile(filepath.Join(configDir, "secrets.env"))
			if readErr != nil {
				continue
			}
			plaintext = string(data)
		}
		vars := map[string]string{}
		for _, line := range strings.Split(plaintext, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if parts := strings.SplitN(line, "=", 2); len(parts) == 2 {
				vars[parts[0]] = parts[1]
			}
		}
		if len(vars) > 0 {
			result = append(result, appSecrets{AppID: app.ID, AppName: app.Name, Vars: vars})
			audit.Log(audit.ActionSecretsShow, app.ID, "OK:http")
		}
	}
	if result == nil {
		result = []appSecrets{}
	}
	return result, nil
}

// handleSecretsShow déchiffre et retourne les secrets d'une app.
// Le mot de passe est demandé à chaque appel (pas de cache de session).
func (s *Server) handleSecretsShow(args map[string]string) (interface{}, error) {
	appID := args["app"]
	if appID == "" {
		return nil, fmt.Errorf("argument 'app' manquant")
	}
	password := args["password"]
	if password == "" {
		return nil, fmt.Errorf("argument 'password' manquant")
	}

	if !secrets.IsSetup(s.baseDir) {
		// Pas de chiffrement configuré : lire secrets.env directement
		configDir := filepath.Join(s.baseDir, "app-config", appID)
		plain, err := os.ReadFile(filepath.Join(configDir, "secrets.env"))
		if err != nil {
			return nil, fmt.Errorf("secrets.env introuvable pour '%s'", appID)
		}
		audit.Log(audit.ActionSecretsShow, appID, "OK:no-encryption")
		return map[string]string{"secrets": string(plain), "encrypted": "false"}, nil
	}

	dek, err := secrets.UnlockDEK(s.baseDir, password)
	if err != nil {
		audit.Log(audit.ActionSecretsShow, appID, "DENIED:wrong-password")
		return nil, fmt.Errorf("mot de passe incorrect")
	}

	configDir := filepath.Join(s.baseDir, "app-config", appID)
	plaintext, err := secrets.ShowSecrets(configDir, dek)
	if err != nil {
		return nil, fmt.Errorf("déchiffrement secrets '%s': %w", appID, err)
	}

	audit.Log(audit.ActionSecretsShow, appID, "OK")
	return map[string]string{"secrets": plaintext, "encrypted": "true"}, nil
}

// ─────────────────────────────────────────────
// AUDIT — lecture du journal
// ─────────────────────────────────────────────

// handleAuditList retourne les dernières lignes du journal d'audit.
func (s *Server) handleAuditList(args map[string]string) (interface{}, error) {
	n := 50 // défaut : 50 dernières lignes
	if nStr, ok := args["n"]; ok && nStr != "" {
		fmt.Sscanf(nStr, "%d", &n)
	}
	lines, err := audit.Read(n)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"lines": lines, "count": len(lines)}, nil
}

// ─────────────────────────────────────────────
// TÂCHES PLANIFIÉES
// ─────────────────────────────────────────────

func (s *Server) handleTaskList() (interface{}, error) {
	if s.sched == nil {
		return []types.Task{}, nil
	}
	return s.sched.Load()
}

func (s *Server) handleTaskAdd(args map[string]string) (interface{}, error) {
	if s.sched == nil {
		return nil, fmt.Errorf("scheduler non initialisé")
	}
	taskJSON, ok := args["task"]
	if !ok || taskJSON == "" {
		return nil, fmt.Errorf("argument 'task' (JSON) manquant")
	}
	var t types.Task
	if err := json.Unmarshal([]byte(taskJSON), &t); err != nil {
		return nil, fmt.Errorf("JSON tâche invalide : %w", err)
	}
	if err := s.sched.Add(t); err != nil {
		return nil, err
	}
	return t, nil
}

func (s *Server) handleTaskRemove(args map[string]string) error {
	if s.sched == nil {
		return fmt.Errorf("scheduler non initialisé")
	}
	id := args["id"]
	if id == "" {
		return fmt.Errorf("argument 'id' manquant")
	}
	return s.sched.Remove(id)
}

func (s *Server) handleTaskRun(args map[string]string) error {
	if s.sched == nil {
		return fmt.Errorf("scheduler non initialisé")
	}
	id := args["id"]
	if id == "" {
		return fmt.Errorf("argument 'id' manquant")
	}
	return s.sched.RunNow(id)
}

func (s *Server) handleTaskToggle(args map[string]string) error {
	if s.sched == nil {
		return fmt.Errorf("scheduler non initialisé")
	}
	id := args["id"]
	if id == "" {
		return fmt.Errorf("argument 'id' manquant")
	}
	enabled := args["enabled"] == "true"
	return s.sched.Toggle(id, enabled)
}

// EnsureUISetup garantit que caleope-ui est correctement configuré :
// service systemd + config Traefik dynamic. Idempotent.
// Appelé au démarrage du daemon ET pendant un upgrade.
func (s *Server) EnsureUISetup() {
	// 1. Service systemd
	const servicePath = "/etc/systemd/system/caleope-ui.service"
	const serviceContent = `[Unit]
Description=Caleope UI Server
After=network.target caleoped.service
Requires=caleoped.service

[Service]
Type=simple
User=root
ExecStart=/usr/local/bin/caleope-ui \
    --base-dir /opt/gaiver-it/caleope \
    --daemon   http://127.0.0.1:8765 \
    --port     8766
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
`
	_ = os.WriteFile(servicePath, []byte(serviceContent), 0644)
	_ = exec.Command("systemctl", "daemon-reload").Run()
	_ = exec.Command("systemctl", "enable", "caleope-ui").Run()

	// 2. Config Traefik dynamic — route ui.<domain> → localhost:8766
	domain := s.rt.AppDomain("ui")
	if domain == "" {
		return // pas de domaine configuré, on skip
	}
	// Traefik tourne dans Docker — utiliser l'IP de la gateway de caleope-public
	// pour joindre caleope-ui (service systemd sur le host, pas dans Docker).
	// Fallback : IP principale de l'hôte.
	hostIP := getDockerGateway("caleope-public")
	if hostIP == "" {
		hostIP = getHostIP()
	}
	traefikDir := filepath.Join(s.baseDir, "data", "traefik", "dynamic")
	if err := os.MkdirAll(traefikDir, 0755); err != nil {
		return
	}

	// Adapter la config selon le proxy mode choisi à l'installation :
	// - traefik : Traefik gère Let's Encrypt → certResolver + redirect HTTP→HTTPS
	// - npm     : NPM gère le TLS en amont → Traefik ne voit que du HTTP (web seulement)
	// - standalone : HTTP seul, pas de TLS (LAN/offline)
	cfg, _ := s.rt.GetConfig()
	proxyMode := ""
	if cfg != nil {
		proxyMode = cfg.ProxyMode
	}

	var traefikConf string
	switch proxyMode {
	case "traefik":
		traefikConf = fmt.Sprintf(`http:
  routers:
    caleope-ui:
      rule: "Host(`+"`%s`"+`)"
      entryPoints:
        - websecure
      tls:
        certResolver: letsencrypt
      service: caleope-ui
    caleope-ui-http:
      rule: "Host(`+"`%s`"+`)"
      entryPoints:
        - web
      middlewares:
        - redirect-to-https
      service: caleope-ui

  services:
    caleope-ui:
      loadBalancer:
        servers:
          - url: "http://%s:8766"

  middlewares:
    redirect-to-https:
      redirectScheme:
        scheme: https
        permanent: true
`, domain, domain, hostIP)
	default: // npm, standalone, ou non configuré → HTTP seul via web
		traefikConf = fmt.Sprintf(`http:
  routers:
    caleope-ui:
      rule: "Host(`+"`%s`"+`)"
      entryPoints:
        - web
      service: caleope-ui

  services:
    caleope-ui:
      loadBalancer:
        servers:
          - url: "http://%s:8766"
`, domain, hostIP)
	}
	_ = os.WriteFile(filepath.Join(traefikDir, "caleope-ui.yml"), []byte(traefikConf), 0644)

	// 3. UFW — ouvrir port 8766 depuis les bridges Docker vers caleope-ui (service host).
	// UFW avec policy DROP bloque le trafic container → service systemd.
	// On autorise le /16 de la gateway caleope-public ET le réseau par défaut de Traefik.
	if hostIP != "" {
		parts := strings.Split(hostIP, ".")
		if len(parts) == 4 {
			subnet := fmt.Sprintf("%s.%s.0.0/16", parts[0], parts[1])
			_ = exec.Command("ufw", "allow", "from", subnet, "to", "any", "port", "8766",
				"comment", "caleope-ui Traefik bridge").Run()
		}
	}
	// Autoriser aussi le réseau par défaut de Traefik (peut différer de caleope-public).
	if dfltGW := getTraefikDefaultGateway(); dfltGW != "" && dfltGW != hostIP {
		parts := strings.Split(dfltGW, ".")
		if len(parts) == 4 {
			subnet := fmt.Sprintf("%s.%s.0.0/16", parts[0], parts[1])
			_ = exec.Command("ufw", "allow", "from", subnet, "to", "any", "port", "8766",
				"comment", "caleope-ui Traefik default-gw").Run()
		}
	}
}

// getHostIP retourne l'IP principale de l'hôte (interface sortante).
func getHostIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "127.0.0.1"
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String()
}

// getDockerGateway retourne l'IP de la gateway d'un réseau Docker.
// Cette IP est toujours joignable depuis les containers sur ce réseau (dont Traefik).
// Elle correspond à l'interface virtuelle du host sur ce bridge Docker.
func getDockerGateway(network string) string {
	out, err := exec.Command("docker", "network", "inspect", network,
		"--format", "{{range .IPAM.Config}}{{.Gateway}}{{end}}").Output()
	if err != nil {
		return ""
	}
	ip := strings.TrimSpace(string(out))
	if ip == "" || strings.Contains(ip, "error") {
		return ""
	}
	return ip
}

// getTraefikDefaultGateway retourne l'IP de la default gateway vue depuis le container Traefik.
// Utile pour autoriser les réseaux Docker supplémentaires que Traefik utilise via UFW.
func getTraefikDefaultGateway() string {
	out, err := exec.Command("docker", "exec", "traefik",
		"sh", "-c", "ip route show default | awk '{print $3}'").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// ─────────────────────────────────────────────
// SYSTEM INFO
// ─────────────────────────────────────────────

// handleSystemInfo retourne les informations système du serveur hôte.
func (s *Server) handleSystemInfo() (map[string]interface{}, error) {
	hostname, _ := os.Hostname()

	// Uptime depuis /proc/uptime
	var uptimeSec float64
	if raw, err := os.ReadFile("/proc/uptime"); err == nil {
		fmt.Sscanf(strings.TrimSpace(string(raw)), "%f", &uptimeSec)
	}
	days := int(uptimeSec / 86400)
	hours := int(uptimeSec/3600) % 24
	mins := int(uptimeSec/60) % 60

	// OS depuis /etc/os-release
	osName := ""
	if raw, err := os.ReadFile("/etc/os-release"); err == nil {
		for _, line := range strings.Split(string(raw), "\n") {
			if strings.HasPrefix(line, "PRETTY_NAME=") {
				osName = strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), `"`)
				break
			}
		}
	}

	// Nombre de CPU depuis /proc/cpuinfo
	cpuCount := 0
	if raw, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		cpuCount = strings.Count(string(raw), "processor\t:")
	}

	// Version du kernel
	kernel := ""
	if out, err := exec.Command("uname", "-r").Output(); err == nil {
		kernel = strings.TrimSpace(string(out))
	}

	// RAM depuis /proc/meminfo
	var memTotal, memAvailable uint64
	if raw, err := os.ReadFile("/proc/meminfo"); err == nil {
		for _, line := range strings.Split(string(raw), "\n") {
			var val uint64
			if strings.HasPrefix(line, "MemTotal:") {
				fmt.Sscanf(strings.TrimPrefix(line, "MemTotal:"), "%d", &val)
				memTotal = val * 1024
			} else if strings.HasPrefix(line, "MemAvailable:") {
				fmt.Sscanf(strings.TrimPrefix(line, "MemAvailable:"), "%d", &val)
				memAvailable = val * 1024
			}
		}
	}

	// Disque depuis syscall.Statfs sur le répertoire base
	var diskTotal, diskFree uint64
	if stat, err := os.Stat(s.baseDir); err == nil && stat.IsDir() {
		out, err2 := exec.Command("df", "-B1", "--output=size,avail", s.baseDir).Output()
		if err2 == nil {
			lines := strings.Split(strings.TrimSpace(string(out)), "\n")
			if len(lines) >= 2 {
				fmt.Sscanf(strings.TrimSpace(lines[1]), "%d %d", &diskTotal, &diskFree)
			}
		}
	}

	// Load average depuis /proc/loadavg
	var load1, load5, load15 float64
	if raw, err := os.ReadFile("/proc/loadavg"); err == nil {
		fmt.Sscanf(strings.TrimSpace(string(raw)), "%f %f %f", &load1, &load5, &load15)
	}

	return map[string]interface{}{
		"hostname":       hostname,
		"uptime_seconds": uptimeSec,
		"uptime":         fmt.Sprintf("%dd %dh %dm", days, hours, mins),
		"os":             osName,
		"cpu_count":      cpuCount,
		"kernel":         kernel,
		"mem_total":      memTotal,
		"mem_available":  memAvailable,
		"mem_used":       memTotal - memAvailable,
		"disk_total":     diskTotal,
		"disk_free":      diskFree,
		"disk_used":      diskTotal - diskFree,
		"load_avg_1":     load1,
		"load_avg_5":     load5,
		"load_avg_15":    load15,
	}, nil
}

// ─────────────────────────────────────────────
// RECONFIGURE
// ─────────────────────────────────────────────

// handleReconfigure relance setup.sh sur une app déjà installée pour appliquer
// de nouveaux paramètres sans réinstaller (pas de suppression des données).
func (s *Server) handleReconfigure(args map[string]string) (interface{}, error) {
	appID := args["app"]
	if appID == "" {
		return nil, fmt.Errorf("argument 'app' manquant")
	}
	app, err := s.rt.GetApp(appID)
	if err != nil {
		return nil, fmt.Errorf("application '%s' non trouvée", appID)
	}

	// Extraire les nouveaux paramètres (format param_KEY=VALUE)
	params := map[string]string{}
	for k, v := range args {
		if strings.HasPrefix(k, "param_") {
			params[strings.TrimPrefix(k, "param_")] = v
		}
	}

	opts := install.InstallOptions{
		AppID:   appID,
		Domain:  app.Domain,
		Channel: app.Channel,
		Params:  params,
		Force:   true,
	}

	if err := s.installer.Install(opts); err != nil {
		audit.Log(audit.ActionConfigure, appID, "ERROR:"+err.Error())
		return nil, fmt.Errorf("reconfiguration '%s': %w", appID, err)
	}
	audit.Log(audit.ActionConfigure, appID, "OK")
	return map[string]string{"status": "reconfigured", "app": appID}, nil
}

// EnsureSecurityHeaders écrit un middleware Traefik avec les en-têtes de sécurité HTTP.
// Le middleware "secure-headers" est disponible pour toutes les apps qui l'activent.
// Idempotent — peut être appelé à chaque démarrage.
func (s *Server) EnsureSecurityHeaders() {
	traefikDir := filepath.Join(s.baseDir, "data", "traefik", "dynamic")
	if err := os.MkdirAll(traefikDir, 0755); err != nil {
		return
	}
	content := `http:
  middlewares:
    secure-headers:
      headers:
        stsSeconds: 31536000
        stsIncludeSubdomains: true
        stsPreload: true
        forceSTSHeader: true
        frameDeny: true
        contentTypeNosniff: true
        browserXssFilter: true
        referrerPolicy: "strict-origin-when-cross-origin"
        permissionsPolicy: "camera=(), microphone=(), geolocation=()"
        customResponseHeaders:
          X-Powered-By: ""
          Server: ""
`
	_ = os.WriteFile(filepath.Join(traefikDir, "security-headers.yml"), []byte(content), 0644)
}

// EnsureAppPriorities ré-applique la priorité ressources (cpu_shares + oom_score_adj) à
// toutes les apps installées, au démarrage du daemon. Indispensable après un reboot hôte :
// les containers redémarrent via la restart-policy Docker sans passer par caleope, donc
// l'oom_score_adj posé sur /proc/<pid> est perdu et doit être reposé. Appelée pour chaque
// app (ApplyPriorities lit aussi les labels "caleope.priority" par service) ; no-op quand
// tout est en tier "normal".
func (s *Server) EnsureAppPriorities() {
	apps, err := s.rt.ListApps()
	if err != nil {
		return
	}
	for _, app := range apps {
		s.dc.ApplyPriorities(app.ID, app.Priority)
	}
}

// coreApps liste les composants essentiels installés automatiquement sur toute instance Caleope.
// Chaque entrée contient l'ID de l'app et son domaine (vide = auto-dérivé depuis la config).
var coreApps = []struct {
	id     string
	domain string // vide = s.rt.AppDomain(id)
}{
	{"crowdsec", ""},
	{"authentik", ""},
}

// ensureCoreApps installe silencieusement les composants core manquants.
// Appelé à chaque upgrade — idempotent si l'app est déjà installée.
func (s *Server) ensureCoreApps() {
	for _, app := range coreApps {
		if _, err := s.rt.GetApp(app.id); err == nil {
			continue // déjà installée
		}
		domain := app.domain
		if domain == "" {
			if m := s.peekManifest(app.id); m != nil && m.UseBaseDomain {
				domain = s.rt.BaseDomain()
			} else {
				domain = s.rt.AppDomain(app.id)
			}
		}
		fmt.Printf("→ Installation du composant core : %s...\n", app.id)
		if err := s.installer.Install(install.InstallOptions{
			AppID:   app.id,
			Domain:  domain,
			Channel: "stable",
		}); err != nil {
			fmt.Printf("⚠️  %s : installation échouée : %v\n", app.id, err)
		} else {
			fmt.Printf("✅ %s installé\n", app.id)
		}
	}
}

func (s *Server) handleLicenseActivate(args map[string]string) (interface{}, error) {
	key, ok := args["license_key"]
	if !ok || key == "" {
		return nil, fmt.Errorf("argument 'license_key' manquant")
	}
	if err := s.lic.Activate(key); err != nil {
		return nil, err
	}
	st := s.lic.Status()
	return map[string]interface{}{
		"activated": true,
		"edition":   st.Edition,
		"message":   fmt.Sprintf("Licence %s activée avec succès", strings.ToUpper(st.Edition)),
	}, nil
}

// handleRetentionSet enregistre la politique de rétention des sauvegardes.
func (s *Server) handleRetentionSet(args map[string]string) (interface{}, error) {
	p := s.bkp.Retention()
	if v, ok := args["keep_last"]; ok {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("keep_last doit être un entier positif ou nul")
		}
		p.KeepLast = n
	}
	if v, ok := args["keep_days"]; ok {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("keep_days doit être un entier positif ou nul")
		}
		p.KeepDays = n
	}
	if err := s.bkp.SetRetention(p); err != nil {
		return nil, err
	}
	return p, nil
}

// handleRetentionApply purge immédiatement les sauvegardes hors politique.
// Utile pour récupérer de la place sans attendre la prochaine sauvegarde.
func (s *Server) handleRetentionApply(args map[string]string) (interface{}, error) {
	appID, ok := args["app"]
	if !ok || appID == "" {
		return nil, fmt.Errorf("argument 'app' manquant")
	}
	deleted, err := s.bkp.ApplyRetention(appID)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"deleted": deleted, "count": len(deleted)}, nil
}

// handleLicenseExport renvoie le jeton signé pour que l'utilisateur le mette à
// l'abri. C'est son filet de récupération : il fonctionne hors ligne et ne
// dépend pas du serveur de licences.
func (s *Server) handleLicenseExport() (interface{}, error) {
	token, err := s.lic.ExportToken()
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"token": token,
		"path":  s.lic.TokenPath(),
	}, nil
}

// handleLicenseImport réinstalle un jeton conservé par l'utilisateur, sans
// aucun échange réseau. Permet de récupérer sa licence sur une nouvelle machine
// même si le serveur de licences est indisponible.
func (s *Server) handleLicenseImport(args map[string]string) (interface{}, error) {
	token, ok := args["token"]
	if !ok || strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("argument 'token' manquant")
	}
	payload, err := s.lic.ImportToken(token)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"activated": true,
		"edition":   payload.Edition,
		"message":   fmt.Sprintf("Licence %s restaurée depuis votre sauvegarde", strings.ToUpper(payload.Edition)),
	}, nil
}

func (s *Server) handleLicenseStatus() (interface{}, error) {
	st := s.lic.Status()
	return map[string]interface{}{
		"activated":    st.Activated,
		"edition":      st.Edition,
		"license_key":  st.LicenseKey,
		"machine_hash": st.MachineHash,
		"issued_at":    st.IssuedAt,
	}, nil
}
