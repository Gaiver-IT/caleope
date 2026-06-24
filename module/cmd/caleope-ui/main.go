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
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
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

// immichTokenCache : token Immich mis en cache (POST /api/auth/login → Bearer)
type immichTokenCache struct {
	mu      sync.Mutex
	token   string
	expires time.Time
}

var immichCache = &immichTokenCache{}

// glpiSessionCache : session GLPI (GET /apirest.php/initSession → Session-Token)
type glpiSessionCache struct {
	mu      sync.Mutex
	token   string
	expires time.Time
}

var glpiCache = &glpiSessionCache{}

// ghostAdminJWT génère un JWT signé HMAC-SHA256 pour l'Admin API Ghost.
// apiKey format : "keyid:hexsecret" (copié depuis Ghost admin → Intégrations)
func ghostAdminJWT(apiKey string) (string, error) {
	parts := strings.SplitN(apiKey, ":", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("GHOST_ADMIN_API_KEY doit être au format 'id:hexsecret'")
	}
	kid, hexSecret := parts[0], parts[1]
	secret, err := hex.DecodeString(hexSecret)
	if err != nil {
		return "", fmt.Errorf("secret hex invalide: %w", err)
	}
	now := time.Now().Unix()
	hdr := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT","kid":"` + kid + `"}`))
	pld := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"iat":%d,"exp":%d,"aud":"/admin/"}`, now, now+300)))
	msg := hdr + "." + pld
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(msg))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return msg + "." + sig, nil
}

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
	uiPasswordPath := filepath.Join(*baseDir, "core", "daemon", "ui-password")
	uiPassword := daemonToken
	if pw, err := readFile(uiPasswordPath); err == nil && pw != "" {
		uiPassword = pw
	}
	var uiPasswordMu sync.RWMutex

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

		// Grafana — Basic auth (admin user/pass)
		"grafana": {basicUserKey: "GRAFANA_ADMIN_USER", basicPassKey: "GRAFANA_ADMIN_PASSWORD",
			authScheme: "Basic", secretsApp: "prometheus-grafana",
			containerName: "caleope-grafana", containerPort: 3000},

		// Jellyfin — API key (JELLYFIN_API_KEY dans secrets.env)
		"jellyfin": {tokenKey: "JELLYFIN_API_KEY", authScheme: "JellyfinToken",
			containerName: "jellyfin", containerPort: 8096},

		// Immich — login flow avec cache (POST /api/auth/login → Bearer token)
		"immich": {basicUserKey: "IMMICH_ADMIN_EMAIL", basicPassKey: "IMMICH_ADMIN_PASS",
			authScheme: "ImmichLogin",
			containerName: "immich-server", containerPort: 2283},

		// WikiJS — Bearer token (généré dans WikiJS admin → Developer Tools)
		"wikijs": {tokenKey: "WIKIJS_API_TOKEN", authScheme: "Bearer",
			containerName: "wikijs", containerPort: 3000},

		// Ghost — Admin API JWT (GHOST_ADMIN_API_KEY format: "keyid:hexsecret")
		"ghost": {tokenKey: "GHOST_ADMIN_API_KEY", authScheme: "GhostAdmin",
			containerName: "ghost", containerPort: 2368},

		// WordPress — Basic auth (WP_ADMIN_USER / WP_ADMIN_PASS)
		"wordpress": {basicUserKey: "WP_ADMIN_USER", basicPassKey: "WP_ADMIN_PASS",
			authScheme: "Basic", containerName: "wordpress", containerPort: 80},

		// GLPI — session token (Basic auth → initSession → Session-Token header)
		// Nécessite que l'API REST GLPI soit activée et GLPI_ADMIN_USER dans secrets.env
		"glpi": {basicUserKey: "GLPI_ADMIN_USER", basicPassKey: "GLPI_ADMIN_PASSWORD",
			authScheme: "GLPISession", containerName: "glpi", containerPort: 80},

		// Pi-hole — auth en query param (PIHOLE_API_TOKEN = SHA256(SHA256(password)))
		"pihole": {tokenKey: "PIHOLE_API_TOKEN", authScheme: "PiholeQuery",
			containerName: "pihole", containerPort: 80},

		// AdGuard Home — Basic auth (ADGUARD_USERNAME / ADGUARD_PASSWORD)
		"adguard": {basicUserKey: "ADGUARD_USERNAME", basicPassKey: "ADGUARD_PASSWORD",
			authScheme: "Basic", containerName: "adguardhome", containerPort: 3000},

		// Portainer — X-API-Key header (PORTAINER_API_TOKEN)
		"portainer": {tokenKey: "PORTAINER_API_TOKEN", authScheme: "X-Api-Key",
			containerName: "portainer", containerPort: 9000},

		// Uptime Kuma — pas d'auth sur l'API status-page publique
		"uptime-kuma": {containerName: "uptime-kuma", containerPort: 3001},

		// Memos — Bearer token (généré après login)
		"memos": {tokenKey: "MEMOS_API_TOKEN", authScheme: "Bearer",
			containerName: "memos", containerPort: 5230},

		// Linkding — Bearer token (LINKDING_API_TOKEN)
		"linkding": {tokenKey: "LINKDING_API_TOKEN", authScheme: "DRFToken",
			containerName: "linkding", containerPort: 9090},

		// Paperless-NGX — Bearer token (PAPERLESS_API_TOKEN)
		"paperless-ngx": {tokenKey: "PAPERLESS_API_TOKEN", authScheme: "Bearer",
			containerName: "paperless-ngx", containerPort: 8000},

		// FreshRSS — Google Reader API (BasicAuth)
		"freshrss": {basicUserKey: "FRESHRSS_ADMIN_USER", basicPassKey: "FRESHRSS_ADMIN_PASS",
			authScheme: "Basic", containerName: "freshrss", containerPort: 80},

		// Syncthing — sans auth sur GUI (auth gérée dans l'interface)
		"syncthing": {containerName: "syncthing", containerPort: 8384},

		// Stirling PDF — sans auth par défaut
		"stirling-pdf": {containerName: "stirling-pdf", containerPort: 8080},

		// ntfy — notifications push, sans auth sur l'API publique
		"ntfy": {containerName: "ntfy", containerPort: 80},

		// n8n — automation workflows, sans auth (géré par n8n)
		"n8n": {containerName: "n8n", containerPort: 5678},

		// File Browser — sans auth proxy (auth propre à filebrowser)
		"filebrowser": {containerName: "filebrowser", containerPort: 80},

		// Mealie — API JWT, pas de proxy auth (login UI)
		"mealie": {containerName: "mealie", containerPort: 9000},

		// Changedetection.io — sans auth par défaut
		"changedetection": {tokenKey: "CHANGEDETECTION_API_TOKEN", authScheme: "X-Api-Key",
			containerName: "changedetection", containerPort: 5000},

		// Gotify — notifications push, client token dans X-Gotify-Key
		"gotify": {tokenKey: "GOTIFY_CLIENT_TOKEN", authScheme: "X-Gotify-Key",
			containerName: "gotify", containerPort: 80},

		// Homarr — dashboard de liens, sans auth proxy
		"homarr": {containerName: "homarr", containerPort: 7575},

		// WG-Easy — interface web WireGuard
		"wg-easy": {containerName: "wg-easy", containerPort: 51821},

		// CrowdSec — LAPI locale pour consulter alertes/décisions (bouncer API key)
		"crowdsec": {tokenKey: "CROWDSEC_LOCAL_API_KEY", authScheme: "X-Api-Key",
			containerName: "crowdsec", containerPort: 8080},

		// Grocy — gestion inventaire maison, API key dans GROCY-API-KEY header
		"grocy": {tokenKey: "GROCY_API_KEY", authScheme: "GROCY-API-KEY",
			containerName: "grocy", containerPort: 80},

		// Jellyseerr — demandes médias pour Jellyfin
		"jellyseerr": {containerName: "jellyseerr", containerPort: 5055},

		// Home Assistant — domotique
		"home-assistant": {containerName: "home-assistant", containerPort: 8123},

		// Calibre-Web — bibliothèque ebooks
		"calibre-web": {containerName: "calibre-web", containerPort: 8083},

		// Navidrome — serveur de musique
		"navidrome": {containerName: "navidrome", containerPort: 4533},

		// PhotoPrism — gestionnaire de photos
		"photoprism": {containerName: "photoprism", containerPort: 2342},

		// Kavita — lecteur manga/comics/ebooks
		"kavita": {containerName: "kavita", containerPort: 5000},

		// Komga — bibliothèque comics/mangas
		"komga": {containerName: "komga", containerPort: 25600},

		// Code Server — VS Code web
		"code-server": {containerName: "code-server", containerPort: 8080},

		// Scrutiny — monitoring disques SMART
		"scrutiny": {containerName: "scrutiny", containerPort: 8080},
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

		// ImmichLogin : POST /api/auth/login → cache Bearer token (expiry ~24h)
		if cfg.authScheme == "ImmichLogin" {
			email := readEnvKey(secretsPath, cfg.basicUserKey)
			pass  := readEnvKey(secretsPath, cfg.basicPassKey)
			if email == "" || pass == "" {
				jsonErr(w, http.StatusServiceUnavailable, appID+": credentials Immich non disponibles")
				return
			}
			immichCache.mu.Lock()
			immichToken := immichCache.token
			if immichToken == "" || time.Now().After(immichCache.expires) {
				loginBody := strings.NewReader(fmt.Sprintf(`{"email":%q,"password":%q}`, email, pass))
				loginReq, _ := http.NewRequest(http.MethodPost, targetBase+"/api/auth/login", loginBody)
				loginReq.Header.Set("Content-Type", "application/json")
				loginReq.Header.Set("Accept", "application/json")
				loginResp, err2 := (&http.Client{Timeout: 15 * time.Second}).Do(loginReq)
				if err2 == nil && loginResp != nil {
					var loginData struct { AccessToken string `json:"accessToken"` }
					if json.NewDecoder(loginResp.Body).Decode(&loginData) == nil && loginData.AccessToken != "" {
						immichToken = loginData.AccessToken
						immichCache.token = immichToken
						immichCache.expires = time.Now().Add(23 * time.Hour)
					}
					loginResp.Body.Close()
				}
			}
			immichCache.mu.Unlock()
			if immichToken == "" {
				jsonErr(w, http.StatusServiceUnavailable, "immich: authentification échouée")
				return
			}
			outReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, r.Body)
			if err != nil { jsonErr(w, http.StatusInternalServerError, err.Error()); return }
			if ct := r.Header.Get("Content-Type"); ct != "" { outReq.Header.Set("Content-Type", ct) }
			outReq.Header.Set("Authorization", "Bearer "+immichToken)
			outReq.Header.Set("Accept", "application/json")
			resp, err := client.Do(outReq)
			if err != nil { jsonErr(w, http.StatusBadGateway, "proxy "+appID+": "+err.Error()); return }
			defer resp.Body.Close()
			w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
			w.Header().Set("X-Proxy-App", appID)
			w.WriteHeader(resp.StatusCode)
			_, _ = io.Copy(w, resp.Body)
			return
		}

		// GLPISession : GET /apirest.php/initSession (Basic auth) → cache Session-Token
		if cfg.authScheme == "GLPISession" {
			user := readEnvKey(secretsPath, cfg.basicUserKey)
			if user == "" { user = "glpi" } // fallback nom admin par défaut
			pass := readEnvKey(secretsPath, cfg.basicPassKey)
			if pass == "" {
				jsonErr(w, http.StatusServiceUnavailable, appID+": GLPI_ADMIN_PASSWORD non disponible")
				return
			}
			glpiCache.mu.Lock()
			glpiToken := glpiCache.token
			if glpiToken == "" || time.Now().After(glpiCache.expires) {
				creds := base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
				initReq, _ := http.NewRequest(http.MethodGet, targetBase+"/apirest.php/initSession", nil)
				initReq.Header.Set("Authorization", "Basic "+creds)
				initReq.Header.Set("Content-Type", "application/json")
				initResp, err2 := (&http.Client{Timeout: 15 * time.Second}).Do(initReq)
				if err2 == nil && initResp != nil {
					var initData struct { SessionToken string `json:"session_token"` }
					if json.NewDecoder(initResp.Body).Decode(&initData) == nil && initData.SessionToken != "" {
						glpiToken = initData.SessionToken
						glpiCache.token = glpiToken
						glpiCache.expires = time.Now().Add(30 * time.Minute)
					}
					initResp.Body.Close()
				}
			}
			glpiCache.mu.Unlock()
			if glpiToken == "" {
				jsonErr(w, http.StatusServiceUnavailable, "glpi: session non initialisée (API REST activée ?)")
				return
			}
			outReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, r.Body)
			if err != nil { jsonErr(w, http.StatusInternalServerError, err.Error()); return }
			if ct := r.Header.Get("Content-Type"); ct != "" { outReq.Header.Set("Content-Type", ct) }
			outReq.Header.Set("Session-Token", glpiToken)
			outReq.Header.Set("Accept", "application/json")
			resp, err := client.Do(outReq)
			if err != nil { jsonErr(w, http.StatusBadGateway, "proxy "+appID+": "+err.Error()); return }
			defer resp.Body.Close()
			w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
			w.Header().Set("X-Proxy-App", appID)
			w.WriteHeader(resp.StatusCode)
			_, _ = io.Copy(w, resp.Body)
			return
		}

		// PiholeQuery : auth en query param ?auth=TOKEN (Pi-hole v5 API)
		if cfg.authScheme == "PiholeQuery" {
			token := readEnvKey(secretsPath, cfg.tokenKey)
			if token == "" {
				jsonErr(w, http.StatusServiceUnavailable, appID+": PIHOLE_API_TOKEN non disponible")
				return
			}
			if strings.Contains(targetURL, "?") {
				targetURL += "&auth=" + url.QueryEscape(token)
			} else {
				targetURL += "?auth=" + url.QueryEscape(token)
			}
			outReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, r.Body)
			if err != nil { jsonErr(w, http.StatusInternalServerError, err.Error()); return }
			outReq.Header.Set("Accept", "application/json")
			resp, err := client.Do(outReq)
			if err != nil { jsonErr(w, http.StatusBadGateway, "proxy "+appID+": "+err.Error()); return }
			defer resp.Body.Close()
			w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
			w.Header().Set("X-Proxy-App", appID)
			w.WriteHeader(resp.StatusCode)
			_, _ = io.Copy(w, resp.Body)
			return
		}

		// Construire le header d'authentification (autres schemes)
		// Si authScheme est vide, on proxifie sans auth (proxy direct).
		var authHeader, authValue string
		if cfg.authScheme != "" {
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
			case "JellyfinToken":
				token := readEnvKey(secretsPath, cfg.tokenKey)
				if token == "" {
					jsonErr(w, http.StatusServiceUnavailable, appID+": JELLYFIN_API_KEY non disponible — générer via setup.sh")
					return
				}
				authHeader, authValue = "Authorization", `MediaBrowser Token="`+token+`"`
			case "GhostAdmin":
				apiKey := readEnvKey(secretsPath, cfg.tokenKey)
				if apiKey == "" {
					jsonErr(w, http.StatusServiceUnavailable, appID+": GHOST_ADMIN_API_KEY non disponible — créer dans Ghost admin → Intégrations")
					return
				}
				jwt, err := ghostAdminJWT(apiKey)
				if err != nil {
					jsonErr(w, http.StatusServiceUnavailable, "ghost: "+err.Error())
					return
				}
				authHeader, authValue = "Authorization", "Ghost "+jwt
			default:
				token := readEnvKey(secretsPath, cfg.tokenKey)
				if token == "" {
					jsonErr(w, http.StatusServiceUnavailable, appID+": token non disponible")
					return
				}
				switch cfg.authScheme {
				case "Bearer":
					authHeader, authValue = "Authorization", "Bearer "+token
				case "DRFToken":
					authHeader, authValue = "Authorization", "Token "+token
				default:
					authHeader, authValue = cfg.authScheme, token
				}
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
		if authHeader != "" {
			outReq.Header.Set(authHeader, authValue)
		}
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
		uiPasswordMu.RLock()
		currentPw := uiPassword
		uiPasswordMu.RUnlock()
		if body.Password != currentPw {
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

	// Changer mot de passe UI (session requise)
	mux.HandleFunc("/auth/password", requireSession(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			Current string `json:"current"`
			New     string `json:"new"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		uiPasswordMu.RLock()
		currentPw := uiPassword
		uiPasswordMu.RUnlock()
		if body.Current != currentPw {
			jsonErr(w, http.StatusUnauthorized, "mot de passe actuel incorrect")
			return
		}
		if len(body.New) < 8 {
			jsonErr(w, http.StatusBadRequest, "le nouveau mot de passe doit faire au moins 8 caractères")
			return
		}
		if err := os.WriteFile(uiPasswordPath, []byte(body.New), 0600); err != nil {
			jsonErr(w, http.StatusInternalServerError, "erreur écriture: "+err.Error())
			return
		}
		uiPasswordMu.Lock()
		uiPassword = body.New
		uiPasswordMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))

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
	mux.HandleFunc("/sys/ports", requireSession(func(w http.ResponseWriter, r *http.Request) {
		handleSysPorts(w, r)
	}))
	mux.HandleFunc("/sys/netstat", requireSession(func(w http.ResponseWriter, r *http.Request) {
		handleSysNetstat(w, r)
	}))
	mux.HandleFunc("/sys/processes", requireSession(func(w http.ResponseWriter, r *http.Request) {
		handleSysProcesses(w, r)
	}))
	mux.HandleFunc("/sys/docker-inspect/", requireSession(func(w http.ResponseWriter, r *http.Request) {
		handleDockerInspect(w, r)
	}))
	mux.HandleFunc("/sys/firewall", requireSession(func(w http.ResponseWriter, r *http.Request) {
		handleSysFirewall(w, r)
	}))
	mux.HandleFunc("/sys/docker-prune", requireSession(func(w http.ResponseWriter, r *http.Request) {
		handleDockerPrune(w, r)
	}))
	mux.HandleFunc("/sys/docker-stats", requireSession(func(w http.ResponseWriter, r *http.Request) {
		handleDockerStats(w, r)
	}))

	// Notes post-install d'une app
	mux.HandleFunc("/sys/app-notes/", requireSession(func(w http.ResponseWriter, r *http.Request) {
		appID := strings.TrimPrefix(r.URL.Path, "/sys/app-notes/")
		appID = strings.Trim(appID, "/")
		if appID == "" || strings.Contains(appID, "..") {
			http.Error(w, "invalid app id", http.StatusBadRequest)
			return
		}
		notesPath := filepath.Join(*baseDir, "app-config", appID, "post-install.txt")
		data, err := os.ReadFile(notesPath)
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"notes": "", "found": false})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"notes": string(data), "found": true})
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
