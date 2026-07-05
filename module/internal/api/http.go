// internal/api/http.go
//
// 🌐 L'API REST HTTP — exposée sur :8080
//
// Routeur compatible Go 1.18+ : pas de préfixe de méthode dans les patterns,
// dispatch manuel sur r.Method pour chaque groupe de routes.
//
// AUTHENTIFICATION : token Bearer dans <baseDir>/core/daemon/api-token
//
// ROUTES :
//   GET    /api/v1/ping
//   GET    /api/v1/apps
//   GET    /api/v1/apps/{id}
//   POST   /api/v1/apps/{id}/install       body JSON: {"domain":"...","channel":"stable"}
//   POST   /api/v1/apps/{id}/reconfigure   body JSON: {"params":{...}}
//   DELETE /api/v1/apps/{id}               ?keep_data=true
//   GET    /api/v1/apps/{id}/logs          ?tail=100
//   GET    /api/v1/apps/{id}/logs/stream   SSE — tail=N
//   POST   /api/v1/apps/{id}/start
//   POST   /api/v1/apps/{id}/stop
//   POST   /api/v1/apps/{id}/restart
//   POST   /api/v1/apps/{id}/backup
//   POST   /api/v1/apps/{id}/restore       ?timestamp=...
//   GET    /api/v1/apps/{id}/backups
//   GET    /api/v1/stats                   ?disk=true
//   GET    /api/v1/store                   ?q=terme
//   GET    /api/v1/store/{id}
//   POST   /api/v1/upgrade                 ?check=true
//   POST   /api/v1/update
//   GET    /api/v1/token                   (localhost seulement)
//   GET    /api/v1/system                  hostname, uptime, OS, CPU, RAM, disque
//   GET    /api/v1/containers              stats CPU/RAM de tous les conteneurs (docker stats)
//   GET    /api/v1/tasks
//   POST   /api/v1/tasks                   body JSON: Task
//   DELETE /api/v1/tasks/{id}
//   POST   /api/v1/tasks/{id}/run          exécution immédiate
//   PATCH  /api/v1/tasks/{id}/toggle       body JSON: {"enabled": true|false}
//   GET    /api/v1/events                  ?app=...&type=...&limit=...
//   GET    /api/v1/secrets
//   POST   /api/v1/secrets                 body JSON: {"password":"..."}
//   POST   /api/v1/secrets/{app}
//   GET    /api/v1/audit                   ?n=50
//   GET    /api/v1/locations
//   POST   /api/v1/locations
//   DELETE /api/v1/locations/{name}
//   POST   /api/v1/locations/{name}/mount
//   POST   /api/v1/locations/{name}/unmount

package api

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gaiver-it/caleope/pkg/types"
	"github.com/gaiver-it/caleope/pkg/version"
)

// loadOrCreateToken charge le token depuis le fichier, ou en génère un nouveau.
func loadOrCreateToken(baseDir string) string {
	tokenPath := filepath.Join(baseDir, "core", "daemon", "api-token")

	if data, err := os.ReadFile(tokenPath); err == nil {
		token := strings.TrimSpace(string(data))
		if len(token) >= 32 {
			return token
		}
	}

	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%x", b)
	}
	token := fmt.Sprintf("%x", b)

	_ = os.MkdirAll(filepath.Dir(tokenPath), 0755)
	_ = os.WriteFile(tokenPath, []byte(token+"\n"), 0600)

	fmt.Printf("✓ Token API généré → %s\n", tokenPath)
	return token
}

// StartHTTP démarre le serveur HTTP REST sur le port donné.
// Bloquant — à appeler dans une goroutine.
// Compatible Go 1.18+ : pas de préfixe de méthode dans les patterns ServeMux.
func (s *Server) StartHTTP(port int) error {
	mux := http.NewServeMux()

	// Endpoint public — pas d'auth
	mux.HandleFunc("/api/v1/ping", s.httpPing)

	// Token — localhost seulement
	mux.Handle("/api/v1/token", s.localOnly(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.httpError(w, "méthode non autorisée", http.StatusMethodNotAllowed)
			return
		}
		s.httpToken(w, r)
	})))

	// GET /api/v1/apps (liste)
	mux.Handle("/api/v1/apps", s.auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.httpError(w, "méthode non autorisée", http.StatusMethodNotAllowed)
			return
		}
		s.httpListApps(w, r)
	})))

	// /api/v1/apps/{id}[/action] — dispatcher central
	mux.Handle("/api/v1/apps/", s.auth(http.HandlerFunc(s.routeApp)))

	// GET /api/v1/stats
	mux.Handle("/api/v1/stats", s.auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.httpError(w, "méthode non autorisée", http.StatusMethodNotAllowed)
			return
		}
		s.httpStats(w, r)
	})))

	// GET /api/v1/store
	mux.Handle("/api/v1/store", s.auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.httpError(w, "méthode non autorisée", http.StatusMethodNotAllowed)
			return
		}
		s.httpStore(w, r)
	})))

	// GET /api/v1/store/{id} — params d'une app spécifique
	mux.Handle("/api/v1/store/", s.auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.httpError(w, "méthode non autorisée", http.StatusMethodNotAllowed)
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/api/v1/store/")
		if id == "" {
			s.httpError(w, "id application manquant", http.StatusBadRequest)
			return
		}
		s.httpStoreApp(w, r, id)
	})))

	// POST /api/v1/upgrade
	mux.Handle("/api/v1/upgrade", s.auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			s.httpError(w, "méthode non autorisée", http.StatusMethodNotAllowed)
			return
		}
		s.httpUpgradeApp(w, r)
	})))

	// POST /api/v1/update — synchroniser le store (git pull sur les dépôts)
	mux.Handle("/api/v1/update", s.auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			s.httpError(w, "méthode non supportée", http.StatusMethodNotAllowed)
			return
		}
		if err := s.handleUpdate(map[string]string{}); err != nil {
			s.httpError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.httpOK(w, map[string]string{"status": "ok", "message": "Store synchronisé"})
	})))

	// GET /api/v1/events?app=jellyfin&limit=50&type=app.installed
	mux.Handle("/api/v1/events", s.auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.httpError(w, "méthode non autorisée", http.StatusMethodNotAllowed)
			return
		}
		s.httpEvents(w, r)
	})))

	// GET  /api/v1/secrets         — liste des apps avec secrets (métadonnées)
	// POST /api/v1/secrets         — déverrouiller + retourner toutes les valeurs
	mux.Handle("/api/v1/secrets", s.auth(http.HandlerFunc(s.routeSecrets)))
	// POST /api/v1/secrets/{app}   — déverrouiller + retourner les valeurs d'une app
	mux.Handle("/api/v1/secrets/", s.auth(http.HandlerFunc(s.routeSecretsApp)))

	// GET /api/v1/audit
	mux.Handle("/api/v1/audit", s.auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.httpError(w, "méthode non autorisée", http.StatusMethodNotAllowed)
			return
		}
		args := map[string]string{"n": r.URL.Query().Get("n")}
		data, err := s.handleAuditList(args)
		if err != nil {
			s.httpError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.httpOK(w, data)
	})))

	// GET /api/v1/locations — liste
	// POST /api/v1/locations — ajouter
	mux.Handle("/api/v1/locations", s.auth(http.HandlerFunc(s.routeLocations)))

	// /api/v1/locations/{name}[/action]
	mux.Handle("/api/v1/locations/", s.auth(http.HandlerFunc(s.routeLocation)))

	// GET /api/v1/tasks — liste des tâches planifiées
	// POST /api/v1/tasks — créer une tâche
	mux.Handle("/api/v1/tasks", s.auth(http.HandlerFunc(s.routeTasks)))

	// /api/v1/tasks/{id}[/action]
	mux.Handle("/api/v1/tasks/", s.auth(http.HandlerFunc(s.routeTask)))

	// GET /api/v1/system — informations système (hostname, uptime, OS, CPU, RAM, disque)
	mux.Handle("/api/v1/system", s.auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.httpError(w, "méthode non autorisée", http.StatusMethodNotAllowed)
			return
		}
		data, err := s.handleSystemInfo()
		if err != nil {
			s.httpError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.httpOK(w, data)
	})))

	// GET /api/v1/containers — stats Docker de tous les conteneurs (docker stats --no-stream)
	mux.Handle("/api/v1/containers", s.auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.httpError(w, "méthode non autorisée", http.StatusMethodNotAllowed)
			return
		}
		out, err := exec.Command("docker", "stats", "--no-stream",
			"--format", `{"name":"{{.Name}}","cpu":"{{.CPUPerc}}","mem":"{{.MemUsage}}","mem_pct":"{{.MemPerc}}","net_io":"{{.NetIO}}","block_io":"{{.BlockIO}}"}`).Output()
		if err != nil {
			s.httpOK(w, map[string]interface{}{"containers": []interface{}{}})
			return
		}
		var containers []map[string]string
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var ct map[string]string
			if err2 := json.Unmarshal([]byte(line), &ct); err2 == nil {
				containers = append(containers, ct)
			}
		}
		if containers == nil {
			containers = []map[string]string{}
		}
		s.httpOK(w, map[string]interface{}{"containers": containers})
	})))

	// GET /api/v1/license — statut licence (public, pas d'auth — besoin pendant setup)
	mux.HandleFunc("/api/v1/license", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.httpError(w, "méthode non autorisée", http.StatusMethodNotAllowed)
			return
		}
		st := s.lic.Status()
		s.httpOK(w, st)
	})

	// POST /api/v1/license/activate — activer la licence (public — besoin pendant setup)
	mux.HandleFunc("/api/v1/license/activate", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			s.httpError(w, "méthode non autorisée", http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			LicenseKey string `json:"license_key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			s.httpError(w, "JSON invalide", http.StatusBadRequest)
			return
		}
		if err := s.lic.Activate(body.LicenseKey); err != nil {
			s.httpError(w, err.Error(), http.StatusBadRequest)
			return
		}
		st := s.lic.Status()
		s.httpOK(w, map[string]interface{}{
			"activated": true,
			"edition":   st.Edition,
			"message":   "Licence activée avec succès",
		})
	})

	// GET/POST /api/v1/registry — lire/écrire la config du registre miroir
	mux.Handle("/api/v1/registry", s.auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			s.httpOK(w, s.RegistryStatus())
		case http.MethodPost:
			var body struct {
				Registry string `json:"registry"`
				User     string `json:"user"`
				Pass     string `json:"pass"`
				Mode     string `json:"mode"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				s.httpError(w, "JSON invalide", http.StatusBadRequest)
				return
			}
			if err := s.SetRegistry(body.Registry, body.User, body.Pass, body.Mode); err != nil {
				s.httpError(w, err.Error(), http.StatusInternalServerError)
				return
			}
			s.httpOK(w, s.RegistryStatus())
		default:
			s.httpError(w, "méthode non autorisée", http.StatusMethodNotAllowed)
		}
	})))

	// POST /api/v1/import — recréer une app depuis une archive d'export (chemin serveur)
	mux.Handle("/api/v1/import", s.auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			s.httpError(w, "méthode non autorisée", http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			Archive string `json:"archive"`
			Mode    string `json:"mode"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			s.httpError(w, "JSON invalide", http.StatusBadRequest)
			return
		}
		if err := s.handleImport(map[string]string{"archive": body.Archive, "mode": body.Mode}); err != nil {
			s.httpError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.httpOK(w, map[string]string{"status": "imported", "archive": body.Archive})
	})))

	// GET/POST/DELETE /api/v1/repos — gérer les dépôts du store
	mux.Handle("/api/v1/repos", s.auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			repos, err := s.rt.GetRepos()
			if err != nil {
				s.httpError(w, err.Error(), http.StatusInternalServerError)
				return
			}
			s.httpOK(w, repos)
		case http.MethodPost:
			var body types.Repo
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				s.httpError(w, "JSON invalide", http.StatusBadRequest)
				return
			}
			if err := s.rt.AddRepo(body); err != nil {
				s.httpError(w, err.Error(), http.StatusBadRequest)
				return
			}
			repos, _ := s.rt.GetRepos()
			s.httpOK(w, repos)
		case http.MethodDelete:
			name := r.URL.Query().Get("name")
			if name == "" {
				s.httpError(w, "nom du dépôt requis", http.StatusBadRequest)
				return
			}
			if err := s.rt.RemoveRepo(name); err != nil {
				s.httpError(w, err.Error(), http.StatusBadRequest)
				return
			}
			repos, _ := s.rt.GetRepos()
			s.httpOK(w, repos)
		default:
			s.httpError(w, "méthode non autorisée", http.StatusMethodNotAllowed)
		}
	})))

	addr := fmt.Sprintf(":%d", port)
	fmt.Printf("✓ API REST sur %s\n", addr)
	return http.ListenAndServe(addr, s.rateLimit(mux))
}

// routeApp dispatche toutes les routes /api/v1/apps/{id}[/action].
func (s *Server) routeApp(w http.ResponseWriter, r *http.Request) {
	// Extraire {id} et {action} depuis le chemin
	// Chemin attendu : /api/v1/apps/{id}  ou  /api/v1/apps/{id}/action
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/apps/")
	parts := strings.SplitN(rest, "/", 2)
	id := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	if id == "" {
		s.httpError(w, "id application manquant", http.StatusBadRequest)
		return
	}

	// DELETE /api/v1/apps/{id}/backups/{dir}
	if r.Method == http.MethodDelete && strings.HasPrefix(action, "backups/") {
		dir := strings.TrimPrefix(action, "backups/")
		if err := s.handleBackupDelete(map[string]string{"app": id, "dir": dir}); err != nil {
			s.httpError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.httpOK(w, map[string]string{"status": "deleted", "app": id, "dir": dir})
		return
	}

	// POST /api/v1/apps/{id}/backups/{timestamp}/restore
	if r.Method == http.MethodPost && strings.HasPrefix(action, "backups/") && strings.HasSuffix(action, "/restore") {
		ts := strings.TrimSuffix(strings.TrimPrefix(action, "backups/"), "/restore")
		if err := s.handleRestore(map[string]string{"app": id, "backup": ts}); err != nil {
			s.httpError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.httpOK(w, map[string]string{"status": "restored", "app": id, "backup": ts})
		return
	}

	key := r.Method + " " + action
	switch key {
	case "GET ":
		s.httpInfoByID(w, r, id)
	case "DELETE ":
		s.httpRemoveByID(w, r, id)
	case "GET logs":
		s.httpLogsByID(w, r, id)
	case "GET logs/stream":
		s.httpLogsStreamByID(w, r, id)
	case "POST install":
		s.httpInstallByID(w, r, id)
	case "POST reconfigure":
		s.httpReconfigureByID(w, r, id)
	case "POST start":
		s.httpStartByID(w, r, id)
	case "POST stop":
		s.httpStopByID(w, r, id)
	case "POST restart":
		s.httpRestartByID(w, r, id)
	case "POST backup", "POST backups":
		s.httpBackupByID(w, r, id)
	case "POST export":
		s.httpExportByID(w, r, id)
	case "POST restore":
		s.httpRestoreByID(w, r, id)
	case "GET backups":
		s.httpBackupListByID(w, r, id)
	default:
		s.httpError(w, fmt.Sprintf("route non trouvée: %s /api/v1/apps/%s/%s", r.Method, id, action), http.StatusNotFound)
	}
}

// ─────────────────────────────────────────────
// MIDDLEWARES
// ─────────────────────────────────────────────

// ─────────────────────────────────────────────
// RATE LIMITING — 60 req/min par IP
// ─────────────────────────────────────────────

const (
	rateLimitMax    = 60
	rateLimitWindow = time.Minute
)

type ipBucket struct {
	mu       sync.Mutex
	tokens   int
	lastFill time.Time
}

var rateBuckets sync.Map // IP → *ipBucket

func (s *Server) rateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip, _, _ := net.SplitHostPort(r.RemoteAddr)

		val, _ := rateBuckets.LoadOrStore(ip, &ipBucket{tokens: rateLimitMax, lastFill: time.Now()})
		bucket := val.(*ipBucket)

		bucket.mu.Lock()
		now := time.Now()
		if now.Sub(bucket.lastFill) >= rateLimitWindow {
			bucket.tokens = rateLimitMax
			bucket.lastFill = now
		}
		if bucket.tokens <= 0 {
			bucket.mu.Unlock()
			w.Header().Set("Retry-After", "60")
			s.httpError(w, "trop de requêtes — réessayer dans 60s", http.StatusTooManyRequests)
			return
		}
		bucket.tokens--
		bucket.mu.Unlock()

		next.ServeHTTP(w, r)
	})
}

func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			s.httpError(w, "authentification requise", http.StatusUnauthorized)
			return
		}
		if strings.TrimPrefix(authHeader, "Bearer ") != s.token {
			s.httpError(w, "token invalide", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) localOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		if host != "127.0.0.1" && host != "::1" {
			s.httpError(w, "accès local uniquement", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ─────────────────────────────────────────────
// HELPERS JSON
// ─────────────────────────────────────────────

func (s *Server) httpOK(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "data": data})
}

func (s *Server) httpError(w http.ResponseWriter, msg string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": msg})
}

// ─────────────────────────────────────────────
// HANDLERS HTTP (sans id dans le path)
// ─────────────────────────────────────────────

func (s *Server) httpPing(w http.ResponseWriter, r *http.Request) {
	cfg, _ := s.rt.GetConfig()
	ver := version.Version
	// Si le binaire a été compilé sans ldflags (dev local), utiliser la version de caleope.conf
	if ver == "dev" && cfg != nil && cfg.Version != "" {
		ver = cfg.Version
		if cfg.Channel != "" {
			ver += "-" + cfg.Channel
		}
	}
	channel := ""
	if cfg != nil {
		channel = cfg.Channel
	}
	licSt := s.lic.Status()
	s.httpOK(w, map[string]interface{}{
		"status":          "ok",
		"version":         ver,
		"commit":          version.Commit,
		"channel":         channel,
		"domain":          cfg.Domain,
		"proxy_mode":      cfg.ProxyMode,
		"license_active":  licSt.Activated,
		"license_edition": licSt.Edition,
	})
}

func (s *Server) httpListApps(w http.ResponseWriter, r *http.Request) {
	data, err := s.handleList()
	if err != nil {
		s.httpError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.httpOK(w, data)
}

func (s *Server) httpStats(w http.ResponseWriter, r *http.Request) {
	args := map[string]string{}
	if r.URL.Query().Get("disk") == "true" {
		args["disk"] = "true"
	}
	data, err := s.handleStats(args)
	if err != nil {
		s.httpError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.httpOK(w, data)
}

func (s *Server) httpStore(w http.ResponseWriter, r *http.Request) {
	data, err := s.handleSearch(map[string]string{"term": r.URL.Query().Get("q")})
	if err != nil {
		s.httpError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.httpOK(w, data)
}

func (s *Server) httpStoreApp(w http.ResponseWriter, r *http.Request, id string) {
	data, err := s.handleStoreParams(map[string]string{"app": id})
	if err != nil {
		s.httpError(w, err.Error(), http.StatusNotFound)
		return
	}
	s.httpOK(w, data)
}

func (s *Server) httpUpgradeApp(w http.ResponseWriter, r *http.Request) {
	args := map[string]string{}
	if r.URL.Query().Get("check") == "true" {
		args["check"] = "true"
	}
	data, err := s.handleUpgrade(args)
	if err != nil {
		s.httpError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.httpOK(w, data)
}

func (s *Server) httpToken(w http.ResponseWriter, r *http.Request) {
	s.httpOK(w, map[string]string{"token": s.token})
}

// ─────────────────────────────────────────────
// HANDLERS HTTP (avec id passé en paramètre)
// ─────────────────────────────────────────────

func (s *Server) httpInfoByID(w http.ResponseWriter, r *http.Request, id string) {
	data, err := s.handleInfo(map[string]string{"app": id})
	if err != nil {
		s.httpError(w, err.Error(), http.StatusNotFound)
		return
	}
	s.httpOK(w, data)
}

func (s *Server) httpInstallByID(w http.ResponseWriter, r *http.Request, id string) {
	args := map[string]string{"app": id}
	// Accepter deux formats :
	//   1. flat map[string]string (CLI interne)
	//   2. {params: {key: value}, domain, channel, async} (UI web)
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err == nil {
		for k, v := range raw {
			switch k {
			case "params":
				// Objet imbriqué : {admin_email: "...", ...} → param_admin_email=...
				var params map[string]interface{}
				if json.Unmarshal(v, &params) == nil {
					for pk, pv := range params {
						if sv, ok := pv.(string); ok {
							args["param_"+pk] = sv
						}
					}
				}
			case "async":
				// ignoré côté daemon (toujours synchrone pour l'instant)
			default:
				// domain, channel, force, gpu, etc.
				var sv string
				if json.Unmarshal(v, &sv) == nil {
					args[k] = sv
				}
			}
		}
	}
	data, err := s.handleInstall(args)
	if err != nil {
		s.httpError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.httpOK(w, data)
}

func (s *Server) httpReconfigureByID(w http.ResponseWriter, r *http.Request, id string) {
	args := map[string]string{"app": id}
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err == nil {
		if paramsRaw, ok := raw["params"]; ok {
			var params map[string]interface{}
			if json.Unmarshal(paramsRaw, &params) == nil {
				for pk, pv := range params {
					if sv, ok := pv.(string); ok {
						args["param_"+pk] = sv
					}
				}
			}
		}
	}
	data, err := s.handleReconfigure(args)
	if err != nil {
		s.httpError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.httpOK(w, data)
}

func (s *Server) httpRemoveByID(w http.ResponseWriter, r *http.Request, id string) {
	args := map[string]string{"app": id}
	if r.URL.Query().Get("keep_data") == "true" {
		args["keep_data"] = "true"
	}
	if err := s.handleRemove(args); err != nil {
		s.httpError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.httpOK(w, nil)
}

// httpLogsStreamByID — SSE endpoint pour les logs en temps réel.
// GET /api/v1/apps/{id}/logs/stream?tail=100
func (s *Server) httpLogsStreamByID(w http.ResponseWriter, r *http.Request, id string) {
	app, err := s.rt.GetApp(id)
	if err != nil {
		s.httpError(w, "application introuvable: "+id, http.StatusNotFound)
		return
	}

	tail := 50
	if t := r.URL.Query().Get("tail"); t != "" {
		if n, err := fmt.Sscanf(t, "%d", &tail); n != 1 || err != nil {
			tail = 50
		}
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		s.httpError(w, "streaming non supporté", http.StatusInternalServerError)
		return
	}

	ch := make(chan string, 64)
	done := make(chan struct{})
	defer close(done)

	go s.dc.LogsStream(app.ComposeDir, tail, ch, done)

	// Envoyer les lignes au format SSE : "data: <line>\n\n"
	for {
		select {
		case line, ok := <-ch:
			if !ok {
				fmt.Fprintf(w, "event: close\ndata: stream terminé\n\n")
				flusher.Flush()
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", line)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (s *Server) httpLogsByID(w http.ResponseWriter, r *http.Request, id string) {
	args := map[string]string{"app": id}
	if tail := r.URL.Query().Get("tail"); tail != "" {
		args["tail"] = tail
	}
	data, err := s.handleLogs(args)
	if err != nil {
		s.httpError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.httpOK(w, data)
}

func (s *Server) httpStartByID(w http.ResponseWriter, r *http.Request, id string) {
	if err := s.handleStart(map[string]string{"app": id}); err != nil {
		s.httpError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.httpOK(w, map[string]string{"status": "started"})
}

func (s *Server) httpStopByID(w http.ResponseWriter, r *http.Request, id string) {
	if err := s.handleStop(map[string]string{"app": id}); err != nil {
		s.httpError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.httpOK(w, map[string]string{"status": "stopped"})
}

func (s *Server) httpRestartByID(w http.ResponseWriter, r *http.Request, id string) {
	if err := s.handleRestart(map[string]string{"app": id}); err != nil {
		s.httpError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.httpOK(w, map[string]string{"status": "restarted"})
}

func (s *Server) httpBackupByID(w http.ResponseWriter, r *http.Request, id string) {
	data, err := s.handleBackup(map[string]string{"app": id})
	if err != nil {
		s.httpError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.httpOK(w, data)
}

// httpExportByID : POST /api/v1/apps/{id}/export?no_images=true
// Crée une archive auto-suffisante et renvoie son chemin serveur.
func (s *Server) httpExportByID(w http.ResponseWriter, r *http.Request, id string) {
	args := map[string]string{"app": id}
	if r.URL.Query().Get("no_images") == "true" {
		args["no_images"] = "true"
	}
	data, err := s.handleExport(args)
	if err != nil {
		s.httpError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.httpOK(w, data)
}

func (s *Server) httpRestoreByID(w http.ResponseWriter, r *http.Request, id string) {
	args := map[string]string{"app": id, "backup": r.URL.Query().Get("timestamp")}
	if err := s.handleRestore(args); err != nil {
		s.httpError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.httpOK(w, nil)
}

func (s *Server) httpBackupListByID(w http.ResponseWriter, r *http.Request, id string) {
	data, err := s.handleBackupList(map[string]string{"app": id})
	if err != nil {
		s.httpError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.httpOK(w, data)
}

// ─────────────────────────────────────────────
// EVENTS
// ─────────────────────────────────────────────

// httpEvents : GET /api/v1/events?app=jellyfin&limit=50&type=app.stopped
func (s *Server) httpEvents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	args := map[string]string{
		"app":   q.Get("app"),
		"type":  q.Get("type"),
		"limit": q.Get("limit"),
	}
	data, err := s.handleEvents(args)
	if err != nil {
		s.httpError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.httpOK(w, data)
}

// ─────────────────────────────────────────────
// LOCATIONS
// ─────────────────────────────────────────────

// routeLocations : GET /api/v1/locations ou POST /api/v1/locations
func (s *Server) routeLocations(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		data, err := s.handleLocationList()
		if err != nil {
			s.httpError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.httpOK(w, data)
	case http.MethodPost:
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			s.httpError(w, "body JSON invalide", http.StatusBadRequest)
			return
		}
		data, err := s.handleLocationAdd(body)
		if err != nil {
			s.httpError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.httpOK(w, data)
	default:
		s.httpError(w, "méthode non autorisée", http.StatusMethodNotAllowed)
	}
}

// ─────────────────────────────────────────────
// SECRETS
// ─────────────────────────────────────────────

// routeSecrets : GET /api/v1/secrets  ou  POST /api/v1/secrets
func (s *Server) routeSecrets(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		data, err := s.handleSecretsList()
		if err != nil {
			s.httpError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.httpOK(w, data)
	case http.MethodPost:
		var body struct {
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Password == "" {
			s.httpError(w, "champ 'password' requis", http.StatusBadRequest)
			return
		}
		data, err := s.handleSecretsReveal(map[string]string{"password": body.Password})
		if err != nil {
			s.httpError(w, err.Error(), http.StatusUnauthorized)
			return
		}
		s.httpOK(w, data)
	default:
		s.httpError(w, "méthode non autorisée", http.StatusMethodNotAllowed)
	}
}

// routeSecretsApp : POST /api/v1/secrets/{app}
func (s *Server) routeSecretsApp(w http.ResponseWriter, r *http.Request) {
	appID := strings.TrimPrefix(r.URL.Path, "/api/v1/secrets/")
	appID = strings.TrimSuffix(appID, "/")
	if appID == "" {
		s.routeSecrets(w, r)
		return
	}
	if r.Method != http.MethodPost {
		s.httpError(w, "méthode non autorisée", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Password == "" {
		s.httpError(w, "champ 'password' requis", http.StatusBadRequest)
		return
	}
	data, err := s.handleSecretsReveal(map[string]string{"password": body.Password, "app": appID})
	if err != nil {
		s.httpError(w, err.Error(), http.StatusUnauthorized)
		return
	}
	s.httpOK(w, data)
}

// routeLocation : /api/v1/locations/{name}[/action]
func (s *Server) routeLocation(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/locations/")
	parts := strings.SplitN(rest, "/", 2)
	name := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}
	if name == "" {
		s.httpError(w, "nom emplacement manquant", http.StatusBadRequest)
		return
	}

	key := r.Method + " " + action
	switch key {
	case "DELETE ":
		if err := s.handleLocationRemove(map[string]string{"name": name}); err != nil {
			s.httpError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.httpOK(w, nil)
	case "POST mount":
		result, err := s.handleLocationMount(map[string]string{"name": name})
		if err != nil {
			s.httpError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.httpOK(w, result)
	case "POST unmount":
		if err := s.handleLocationUnmount(map[string]string{"name": name}); err != nil {
			s.httpError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.httpOK(w, map[string]string{"status": "unmounted"})
	default:
		s.httpError(w, fmt.Sprintf("route non trouvée: %s /api/v1/locations/%s/%s", r.Method, name, action), http.StatusNotFound)
	}
}

// ─────────────────────────────────────────────
// TÂCHES PLANIFIÉES
// ─────────────────────────────────────────────

// routeTasks : GET /api/v1/tasks  —  POST /api/v1/tasks
func (s *Server) routeTasks(w http.ResponseWriter, r *http.Request) {
	if s.sched == nil {
		s.httpError(w, "scheduler non initialisé", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet:
		tasks, err := s.sched.Load()
		if err != nil {
			s.httpError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.httpOK(w, tasks)

	case http.MethodPost:
		var t types.Task
		if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
			s.httpError(w, "corps JSON invalide: "+err.Error(), http.StatusBadRequest)
			return
		}
		if t.ID == "" {
			s.httpError(w, "champ 'id' requis", http.StatusBadRequest)
			return
		}
		if t.Type == "" {
			s.httpError(w, "champ 'type' requis (backup, upgrade, update)", http.StatusBadRequest)
			return
		}
		t.CreatedAt = time.Now()
		t.Enabled = true
		if err := s.sched.Add(t); err != nil {
			s.httpError(w, err.Error(), http.StatusConflict)
			return
		}
		s.httpOK(w, t)

	default:
		s.httpError(w, "méthode non autorisée", http.StatusMethodNotAllowed)
	}
}

// routeTask : DELETE /api/v1/tasks/{id}  —  POST /api/v1/tasks/{id}/run  —  PATCH /api/v1/tasks/{id}/toggle
func (s *Server) routeTask(w http.ResponseWriter, r *http.Request) {
	if s.sched == nil {
		s.httpError(w, "scheduler non initialisé", http.StatusServiceUnavailable)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/tasks/")
	parts := strings.SplitN(rest, "/", 2)
	id := parts[0]
	action := ""
	if len(parts) == 2 {
		action = parts[1]
	}

	switch r.Method + " " + action {
	case "DELETE ":
		if err := s.sched.Remove(id); err != nil {
			s.httpError(w, err.Error(), http.StatusNotFound)
			return
		}
		s.httpOK(w, map[string]string{"status": "deleted", "id": id})

	case "POST run":
		if err := s.sched.RunNow(id); err != nil {
			s.httpError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.httpOK(w, map[string]string{"status": "executed", "id": id})

	case "PATCH toggle":
		var body struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			s.httpError(w, "JSON invalide", http.StatusBadRequest)
			return
		}
		if err := s.sched.Toggle(id, body.Enabled); err != nil {
			s.httpError(w, err.Error(), http.StatusNotFound)
			return
		}
		s.httpOK(w, map[string]string{"status": "updated", "id": id})

	default:
		s.httpError(w, fmt.Sprintf("route non trouvée: %s /api/v1/tasks/%s/%s", r.Method, id, action), http.StatusNotFound)
	}
}
