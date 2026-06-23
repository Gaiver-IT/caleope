// cmd/caleope-ui/main.go
//
// Caleope UI Server — sert l'interface web et proxie vers caleoped.
//
// Architecture :
//   Browser → :8766 (caleope-ui) → :8765 (caleoped REST API)
//
// Auth :
//   POST /auth/login  { password }  → session cookie
//   POST /auth/logout               → supprime la session
//   GET  /api/*       (requires session) → proxy vers caleoped avec Bearer token
//   GET  /            → SPA (index.html)

package main

import (
	"crypto/rand"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gaiver-it/caleope/pkg/version"
)

//go:embed web
var webFiles embed.FS

// ── Sessions ────────────────────────────────────────────────────────────────

type sessions struct {
	mu   sync.RWMutex
	data map[string]time.Time
}

// vwSessionCache : cookie VW_ADMIN mis en cache pour éviter le rate-limit (Max-Age=1200s)
type vwSessionCache struct {
	mu      sync.Mutex
	cookie  string
	expires time.Time
}

var vwCache = &vwSessionCache{}

func newSessions() *sessions { return &sessions{data: make(map[string]time.Time)} }

func (s *sessions) create() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	tok := hex.EncodeToString(b)
	s.mu.Lock()
	s.data[tok] = time.Now().Add(24 * time.Hour)
	s.mu.Unlock()
	return tok
}

func (s *sessions) valid(tok string) bool {
	s.mu.RLock()
	exp, ok := s.data[tok]
	s.mu.RUnlock()
	return ok && time.Now().Before(exp)
}

func (s *sessions) delete(tok string) {
	s.mu.Lock()
	delete(s.data, tok)
	s.mu.Unlock()
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func jsonErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func readFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// readEnvKey extrait la valeur d'une clé dans un fichier clé=valeur (format .env).
func readEnvKey(path, key string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	prefix := key + "="
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	return ""
}

// ── Main ─────────────────────────────────────────────────────────────────────

func main() {
	port    := flag.Int("port",     8766,                    "Port de l'interface web")
	daemon  := flag.String("daemon", "http://127.0.0.1:8765", "URL du daemon caleoped")
	baseDir := flag.String("base-dir", "/opt/gaiver-it/caleope", "Répertoire base Caleope")
	flag.Parse()

	// Token daemon (obligatoire)
	tokenPath := filepath.Join(*baseDir, "core", "daemon", "api-token")
	daemonToken, err := readFile(tokenPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Token daemon introuvable (%s): %v\n", tokenPath, err)
		fmt.Fprintf(os.Stderr, "   Assurez-vous que caleoped tourne et que le fichier existe.\n")
		os.Exit(1)
	}

	// Mot de passe UI — dans core/daemon/ui-password, sinon = token daemon
	uiPassword := daemonToken
	if pw, err := readFile(filepath.Join(*baseDir, "core", "daemon", "ui-password")); err == nil && pw != "" {
		uiPassword = pw
	}

	store := newSessions()

	// Répertoire pour le logo custom
	logoDir  := filepath.Join(*baseDir, "data", "ui")
	logoBase := filepath.Join(logoDir, "logo")

	// ── Proxy vers caleoped ────────────────────────────────────────────────
	target, _ := url.Parse(*daemon)
	proxy := httputil.NewSingleHostReverseProxy(target)
	orig := proxy.Director
	proxy.Director = func(r *http.Request) {
		orig(r)
		r.Header.Set("Authorization", "Bearer "+daemonToken)
		r.Header.Del("Cookie")
		r.Host = target.Host
	}
	proxy.ModifyResponse = func(r *http.Response) error {
		r.Header.Set("Access-Control-Allow-Origin", "*")
		return nil
	}

	// ── Fichiers statiques (embedded) ──────────────────────────────────────
	webFS, _ := fs.Sub(webFiles, "web")
	fileServer := http.FileServer(http.FS(webFS))

	// ── Auth middleware ────────────────────────────────────────────────────
	requireSession := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			c, err := r.Cookie("caleope-session")
			if err != nil || !store.valid(c.Value) {
				jsonErr(w, http.StatusUnauthorized, "not authenticated")
				return
			}
			next(w, r)
		}
	}

	// ── Routes ────────────────────────────────────────────────────────────
	mux := http.NewServeMux()

	// Logo custom : GET sert le logo uploadé ou le SVG embarqué par défaut
	mux.HandleFunc("/ui/logo", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		exts := []struct{ ext, ct string }{
			{".png", "image/png"}, {".jpg", "image/jpeg"},
			{".webp", "image/webp"}, {".svg", "image/svg+xml"},
		}
		for _, e := range exts {
			f, err := os.Open(logoBase + e.ext)
			if err != nil {
				continue
			}
			defer f.Close()
			w.Header().Set("Content-Type", e.ct)
			_, _ = io.Copy(w, f)
			return
		}
		// Fallback : SVG embarqué
		data, err := webFiles.ReadFile("web/img/logo.svg")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/svg+xml")
		_, _ = w.Write(data)
	})

	// Upload logo (session requise)
	mux.HandleFunc("/ui/logo/upload", requireSession(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := r.ParseMultipartForm(5 << 20); err != nil {
			jsonErr(w, http.StatusBadRequest, "fichier trop grand (max 5 Mo)")
			return
		}
		file, header, err := r.FormFile("logo")
		if err != nil {
			jsonErr(w, http.StatusBadRequest, "champ 'logo' manquant")
			return
		}
		defer file.Close()

		// Détecter le type réel
		buf := make([]byte, 512)
		n, _ := file.Read(buf)
		ct := http.DetectContentType(buf[:n])
		var ext string
		switch {
		case strings.HasPrefix(ct, "image/png"):
			ext = ".png"
		case strings.HasPrefix(ct, "image/jpeg"):
			ext = ".jpg"
		case strings.HasPrefix(ct, "image/webp"):
			ext = ".webp"
		case strings.ToLower(filepath.Ext(header.Filename)) == ".svg":
			ext = ".svg"
		default:
			jsonErr(w, http.StatusBadRequest, "format non supporté — utilisez PNG, JPG, SVG ou WebP")
			return
		}

		if err := os.MkdirAll(logoDir, 0o755); err != nil {
			jsonErr(w, http.StatusInternalServerError, "impossible de créer le répertoire")
			return
		}
		// Supprimer les anciens logos
		for _, e := range []string{".png", ".jpg", ".webp", ".svg"} {
			_ = os.Remove(logoBase + e)
		}
		dst, err := os.Create(logoBase + ext)
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "impossible d'écrire le fichier")
			return
		}
		defer dst.Close()
		_, _ = dst.Write(buf[:n])
		_, _ = io.Copy(dst, file)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))

	// Réinitialiser le logo (session requise)
	mux.HandleFunc("/ui/logo/reset", requireSession(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		for _, e := range []string{".png", ".jpg", ".webp", ".svg"} {
			_ = os.Remove(logoBase + e)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))

	// ── Proxy vers les APIs des apps installées ──────────────────────────────
	// GET|POST /ui/proxy/{appid}/{path...}
	//
	// Supporte plusieurs modes d'auth et de résolution d'adresse :
	//   - Bearer / X-Api-Key : token statique dans secrets.env
	//   - Basic : username + password encodés en base64
	//   - GitToken : Authorization: token TOKEN (Gitea API)
	//   - VaultwardenAdmin : POST /admin cookie flow
	//   - portName : port hôte dans runtime/apps/{appid}.json
	//   - containerName + containerPort : résolution IP Docker dynamique
	type appProxyCfg struct {
		tokenKey      string // clé token dans secrets.env (Bearer/X-Api-Key)
		basicUserKey  string // clé username pour Basic auth
		basicPassKey  string // clé password pour Basic auth
		authScheme    string // "Bearer", "X-Api-Key", "Basic", "GitToken", "VaultwardenAdmin"
		portName      string // port nommé dans runtime JSON (si pas containerName)
		secretsApp    string // app dont lire les secrets (si différent de appID)
		containerName string // nom du conteneur Docker à résoudre
		containerPort int    // port du conteneur (si containerName défini)
		hostOverride  string // Host header override (ex: nextcloud.domain.com)
		extraHeaders  map[string]string // headers supplémentaires à ajouter
	}

	// Lire le domaine depuis caleope.conf pour le Host header Nextcloud
	caleopeDomain := readEnvKey(filepath.Join(*baseDir, "caleope.conf"), "CALEOPE_DOMAIN")

	appProxyMap := map[string]appProxyCfg{
		// Apps avec token hôte mappé
		"authentik": {tokenKey: "AUTHENTIK_BOOTSTRAP_TOKEN", authScheme: "Bearer",    portName: "web"},
		"azuracast": {tokenKey: "AZURACAST_API_KEY",         authScheme: "X-Api-Key", portName: "web"},

		// Nextcloud — OCS API : Basic auth + Host header requis (trusted_domains)
		"nextcloud": {basicUserKey: "NEXTCLOUD_ADMIN_USER", basicPassKey: "NEXTCLOUD_ADMIN_PASSWORD",
			authScheme: "Basic", containerName: "nextcloud", containerPort: 80,
			hostOverride: "nextcloud." + caleopeDomain,
			extraHeaders: map[string]string{"OCS-APIRequest": "true"}},

		// Gitea — REST API (token API généré via gitea admin)
		"gitea": {tokenKey: "GITEA_API_TOKEN", authScheme: "GitToken",
			containerName: "gitea", containerPort: 3000},

		// Vaultwarden — admin panel (session cookie via POST /admin)
		"vaultwarden": {tokenKey: "_ADMIN_TOKEN_PLAIN", authScheme: "VaultwardenAdmin",
			containerName: "vaultwarden", containerPort: 80},

		// Arr-stack : chaque service via IP Docker
		"arr-sonarr":  {tokenKey: "ARR_API_SONARR",  authScheme: "X-Api-Key", secretsApp: "arr-stack", containerName: "sonarr",  containerPort: 8989},
		"arr-radarr":  {tokenKey: "ARR_API_RADARR",  authScheme: "X-Api-Key", secretsApp: "arr-stack", containerName: "radarr",  containerPort: 7878},
		"arr-lidarr":  {tokenKey: "ARR_API_LIDARR",  authScheme: "X-Api-Key", secretsApp: "arr-stack", containerName: "lidarr",  containerPort: 8686},
		"arr-prowlarr":{tokenKey: "ARR_API_PROWLARR", authScheme: "X-Api-Key", secretsApp: "arr-stack", containerName: "prowlarr",containerPort: 9696},
	}

	// resolveDockerIP récupère l'IP d'un conteneur Docker via `docker inspect`.
	resolveDockerIP := func(containerName string) string {
		out, err := exec.Command("docker", "inspect",
			"--format", "{{range .NetworkSettings.Networks}}{{.IPAddress}}\n{{end}}",
			containerName,
		).Output()
		if err != nil {
			return ""
		}
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				return line
			}
		}
		return ""
	}

	// readAppPort lit le port hôte d'une app depuis runtime/apps/{appid}.json.
	readAppPort := func(appID, portName string) int {
		data, err := os.ReadFile(filepath.Join(*baseDir, "runtime", "apps", appID+".json"))
		if err != nil {
			return 0
		}
		var app struct {
			Ports []struct {
				Name string `json:"name"`
				Host int    `json:"host"`
			} `json:"ports"`
		}
		if err := json.Unmarshal(data, &app); err != nil {
			return 0
		}
		for _, p := range app.Ports {
			if p.Name == portName && p.Host > 0 {
				return p.Host
			}
		}
		if len(app.Ports) > 0 {
			return app.Ports[0].Host
		}
		return 0
	}

	mux.HandleFunc("/ui/proxy/", requireSession(func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/ui/proxy/")
		slash := strings.IndexByte(rest, '/')
		if slash < 0 {
			jsonErr(w, http.StatusBadRequest, "format: /ui/proxy/{appid}/{path}")
			return
		}
		appID   := rest[:slash]
		apiPath := rest[slash:]

		cfg, ok := appProxyMap[appID]
		if !ok {
			jsonErr(w, http.StatusBadRequest, "app non supportée pour le proxy: "+appID)
			return
		}

		// Résoudre l'app cible (secrets + adresse)
		secretsAppID := cfg.secretsApp
		if secretsAppID == "" {
			secretsAppID = appID
		}
		secretsPath := filepath.Join(*baseDir, "app-config", secretsAppID, "secrets.env")

		// Résoudre l'adresse cible
		var targetBase string
		if cfg.containerName != "" {
			ip := resolveDockerIP(cfg.containerName)
			if ip == "" {
				jsonErr(w, http.StatusServiceUnavailable, appID+": conteneur introuvable")
				return
			}
			targetBase = fmt.Sprintf("http://%s:%d", ip, cfg.containerPort)
		} else {
			port := readAppPort(secretsAppID, cfg.portName)
			if port == 0 {
				jsonErr(w, http.StatusServiceUnavailable, appID+": port hôte introuvable")
				return
			}
			targetBase = fmt.Sprintf("http://localhost:%d", port)
		}

		targetURL := targetBase + apiPath
		if r.URL.RawQuery != "" {
			targetURL += "?" + r.URL.RawQuery
		}

		client := &http.Client{Timeout: 30 * time.Second}

		// VaultwardenAdmin : session cookie flow avec cache (évite le rate-limit 429)
		// Le cookie VW_ADMIN a un Max-Age de 1200s (20 min) — on le réutilise jusqu'à expiry.
		if cfg.authScheme == "VaultwardenAdmin" {
			adminToken := readEnvKey(secretsPath, cfg.tokenKey)
			if adminToken == "" {
				jsonErr(w, http.StatusServiceUnavailable, appID+": token non disponible")
				return
			}

			// Récupérer ou rafraîchir le cookie en cache
			vwCache.mu.Lock()
			vwCookie := vwCache.cookie
			if vwCookie == "" || time.Now().After(vwCache.expires) {
				formData := strings.NewReader("token=" + url.QueryEscape(adminToken))
				authReq, _ := http.NewRequest(http.MethodPost, targetBase+"/admin", formData)
				authReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				noRedir := &http.Client{
					Timeout: 15 * time.Second,
					CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
						return http.ErrUseLastResponse
					},
				}
				authResp, err := noRedir.Do(authReq)
				if err == nil || authResp != nil {
					if authResp != nil {
						for _, c := range authResp.Cookies() {
							if c.Name == "VW_ADMIN" {
								vwCookie = c.Value
								vwCache.cookie = vwCookie
								vwCache.expires = time.Now().Add(18 * time.Minute)
								break
							}
						}
						authResp.Body.Close()
					}
				}
			}
			vwCache.mu.Unlock()

			if vwCookie == "" {
				jsonErr(w, http.StatusServiceUnavailable, "vaultwarden: cookie VW_ADMIN absent (rate-limited ou token invalide)")
				return
			}

			outReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, r.Body)
			if err != nil {
				jsonErr(w, http.StatusInternalServerError, err.Error())
				return
			}
			outReq.Header.Set("Cookie", "VW_ADMIN="+vwCookie)
			outReq.Header.Set("Accept", "application/json, text/html")
			resp, err := client.Do(outReq)
			if err != nil {
				jsonErr(w, http.StatusBadGateway, "proxy "+appID+": "+err.Error())
				return
			}
			defer resp.Body.Close()
			ct := resp.Header.Get("Content-Type")
			w.Header().Set("Content-Type", ct)
			w.Header().Set("X-Proxy-App", appID)
			w.WriteHeader(resp.StatusCode)
			_, _ = io.Copy(w, resp.Body)
			return
		}

		// Construire le header d'authentification (autres schemes)
		var authHeader, authValue string
		switch cfg.authScheme {
		case "Basic":
			user := readEnvKey(secretsPath, cfg.basicUserKey)
			pass := readEnvKey(secretsPath, cfg.basicPassKey)
			if user == "" || pass == "" {
				jsonErr(w, http.StatusServiceUnavailable, appID+": credentials non disponibles")
				return
			}
			creds := base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
			authHeader, authValue = "Authorization", "Basic "+creds
		case "GitToken":
			token := readEnvKey(secretsPath, cfg.tokenKey)
			if token == "" {
				jsonErr(w, http.StatusServiceUnavailable, appID+": token non disponible")
				return
			}
			authHeader, authValue = "Authorization", "token "+token
		default:
			token := readEnvKey(secretsPath, cfg.tokenKey)
			if token == "" {
				jsonErr(w, http.StatusServiceUnavailable, appID+": token non disponible")
				return
			}
			switch cfg.authScheme {
			case "Bearer":
				authHeader, authValue = "Authorization", "Bearer "+token
			default:
				authHeader, authValue = cfg.authScheme, token
			}
		}

		outReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, r.Body)
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		if ct := r.Header.Get("Content-Type"); ct != "" {
			outReq.Header.Set("Content-Type", ct)
		}
		outReq.Header.Set(authHeader, authValue)
		outReq.Header.Set("Accept", "application/json")
		// Host override (ex: Nextcloud trusted_domains)
		if cfg.hostOverride != "" {
			outReq.Host = cfg.hostOverride
		}
		// Headers supplémentaires (ex: OCS-APIRequest pour Nextcloud)
		for k, v := range cfg.extraHeaders {
			outReq.Header.Set(k, v)
		}

		resp, err := client.Do(outReq)
		if err != nil {
			jsonErr(w, http.StatusBadGateway, "proxy "+appID+": "+err.Error())
			return
		}
		defer resp.Body.Close()

		w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
		w.Header().Set("X-Proxy-App", appID)
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	}))

	// Test de connectivité NAS (session requise)
	mux.HandleFunc("/ui/location/test", requireSession(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Type string `json:"type"`
			Host string `json:"host"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Host == "" {
			jsonErr(w, http.StatusBadRequest, "host requis")
			return
		}
		port := "2049" // NFS
		if req.Type == "smb" {
			port = "445"
		}
		addr := net.JoinHostPort(req.Host, port)
		start := time.Now()
		conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
		latency := time.Since(start).Milliseconds()
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"reachable":  false,
				"error":      err.Error(),
				"latency_ms": latency,
			})
			return
		}
		conn.Close()
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"reachable":  true,
			"latency_ms": latency,
		})
	}))

	// Login
	mux.HandleFunc("/auth/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			Password string `json:"password"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Password != uiPassword {
			jsonErr(w, http.StatusUnauthorized, "invalid credentials")
			return
		}
		tok := store.create()
		http.SetCookie(w, &http.Cookie{
			Name:     "caleope-session",
			Value:    tok,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
			MaxAge:   86400,
		})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	// Logout
	mux.HandleFunc("/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("caleope-session"); err == nil {
			store.delete(c.Value)
		}
		http.SetCookie(w, &http.Cookie{Name: "caleope-session", MaxAge: -1, Path: "/"})
		w.WriteHeader(http.StatusOK)
	})

	// Vérifier session (utilisé par le frontend au chargement)
	mux.HandleFunc("/auth/check", func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("caleope-session")
		if err != nil || !store.valid(c.Value) {
			jsonErr(w, http.StatusUnauthorized, "not authenticated")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	// ── Terminal WebSocket (session requise) ──────────────────────────────────
	mux.HandleFunc("/ws/terminal", requireSession(func(w http.ResponseWriter, r *http.Request) {
		handleTerminal(w, r)
	}))

	// ── API système (session requise) ─────────────────────────────────────────
	mux.HandleFunc("/sys/services", requireSession(func(w http.ResponseWriter, r *http.Request) {
		handleSysServices(w, r)
	}))
	mux.HandleFunc("/sys/services/", requireSession(func(w http.ResponseWriter, r *http.Request) {
		handleSysServices(w, r)
	}))
	mux.HandleFunc("/sys/network", requireSession(func(w http.ResponseWriter, r *http.Request) {
		handleSysNetwork(w, r)
	}))
	mux.HandleFunc("/sys/storage", requireSession(func(w http.ResponseWriter, r *http.Request) {
		handleSysStorage(w, r)
	}))
	mux.HandleFunc("/sys/journal", requireSession(func(w http.ResponseWriter, r *http.Request) {
		handleSysJournal(w, r)
	}))

	// API proxy (session requise) — logs en streaming aussi
	mux.HandleFunc("/api/", requireSession(func(w http.ResponseWriter, r *http.Request) {
		// SSE : désactiver le buffering
		if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
			w.Header().Set("X-Accel-Buffering", "no")
		}
		proxy.ServeHTTP(w, r)
	}))

	// SPA fallback
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		f, err := webFS.Open(path)
		if err == nil {
			_ = f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}
		// Toute route inconnue → index.html (SPA routing)
		idx, err := webFiles.Open("web/index.html")
		if err != nil {
			http.Error(w, "not found", 404)
			return
		}
		defer idx.Close()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.Copy(w, idx)
	})

	addr := fmt.Sprintf(":%d", *port)
	ver := version.Version
	if ver == "" || ver == "dev" {
		ver = "dev"
	}
	fmt.Printf("╔══════════════════════════════════════╗\n")
	fmt.Printf("║  Caleope UI  — %-22s║\n", ver)
	fmt.Printf("╚══════════════════════════════════════╝\n")
	fmt.Printf("  Interface : http://0.0.0.0%s\n", addr)
	fmt.Printf("  Daemon    : %s\n\n", *daemon)

	if err := http.ListenAndServe(addr, mux); err != nil {
		fmt.Fprintf(os.Stderr, "❌ %v\n", err)
		os.Exit(1)
	}
}
