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
//   POST   /api/v1/apps/{id}/install    body JSON: {"domain":"...","channel":"stable"}
//   DELETE /api/v1/apps/{id}            ?keep_data=true
//   GET    /api/v1/apps/{id}/logs       ?tail=100
//   POST   /api/v1/apps/{id}/start
//   POST   /api/v1/apps/{id}/stop
//   POST   /api/v1/apps/{id}/restart
//   POST   /api/v1/apps/{id}/backup
//   POST   /api/v1/apps/{id}/restore    ?timestamp=...
//   GET    /api/v1/apps/{id}/backups
//   GET    /api/v1/stats                ?disk=true
//   GET    /api/v1/store                ?q=terme
//   POST   /api/v1/upgrade              ?check=true
//   GET    /api/v1/token               (localhost seulement)

package api

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"

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

	// POST /api/v1/upgrade
	mux.Handle("/api/v1/upgrade", s.auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			s.httpError(w, "méthode non autorisée", http.StatusMethodNotAllowed)
			return
		}
		s.httpUpgradeApp(w, r)
	})))

	// GET /api/v1/events?app=jellyfin&limit=50&type=app.installed
	mux.Handle("/api/v1/events", s.auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.httpError(w, "méthode non autorisée", http.StatusMethodNotAllowed)
			return
		}
		s.httpEvents(w, r)
	})))

	// GET /api/v1/locations — liste
	// POST /api/v1/locations — ajouter
	mux.Handle("/api/v1/locations", s.auth(http.HandlerFunc(s.routeLocations)))

	// /api/v1/locations/{name}[/action]
	mux.Handle("/api/v1/locations/", s.auth(http.HandlerFunc(s.routeLocation)))

	addr := fmt.Sprintf(":%d", port)
	fmt.Printf("✓ API REST sur %s\n", addr)
	return http.ListenAndServe(addr, mux)
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

	key := r.Method + " " + action
	switch key {
	case "GET ":
		s.httpInfoByID(w, r, id)
	case "DELETE ":
		s.httpRemoveByID(w, r, id)
	case "GET logs":
		s.httpLogsByID(w, r, id)
	case "POST install":
		s.httpInstallByID(w, r, id)
	case "POST start":
		s.httpStartByID(w, r, id)
	case "POST stop":
		s.httpStopByID(w, r, id)
	case "POST restart":
		s.httpRestartByID(w, r, id)
	case "POST backup":
		s.httpBackupByID(w, r, id)
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
	s.httpOK(w, map[string]string{
		"status":     "ok",
		"version":    version.Version,
		"commit":     version.Commit,
		"domain":     cfg.Domain,
		"proxy_mode": cfg.ProxyMode,
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
	var body map[string]string
	if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
		for k, v := range body {
			args[k] = v
		}
	}
	data, err := s.handleInstall(args)
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
