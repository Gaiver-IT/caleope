// cmd/caleope-ui/oidc.go
//
// Authentification SSO via OIDC (OpenID Connect + PKCE RFC 7636).
//
// Flux :
//   GET /auth/oidc/config    → JSON {enabled, name} — public, pas de session
//   POST /auth/oidc/config   → sauvegarde la config (session requise)
//   GET /auth/oidc/start     → génère state+code_challenge, redirige vers le provider
//   GET /auth/oidc/callback  → échange le code, crée une session Caleope
//
// Config stockée dans {baseDir}/data/ui/oidc.conf :
//   OIDC_PROVIDER_NAME=Authentik
//   OIDC_ISSUER=https://authentik.example.com/application/o/caleope/
//   OIDC_CLIENT_ID=<client-id>
//   OIDC_CLIENT_SECRET=<client-secret>
//   OIDC_REDIRECT_URI=https://... (optionnel, dérivé du Host sinon)

package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ── Config ────────────────────────────────────────────────────────────────────

type oidcCfg struct {
	Enabled      bool
	Issuer       string
	ClientID     string
	ClientSecret string
	Name         string
	RedirectURI  string
}

func oidcLoadConfig(baseDir string) oidcCfg {
	path := filepath.Join(baseDir, "data", "ui", "oidc.conf")
	data, err := os.ReadFile(path)
	if err != nil {
		return oidcCfg{}
	}
	cfg := oidcCfg{Name: "SSO"}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.IndexByte(line, '=')
		if idx < 0 {
			continue
		}
		k, v := strings.TrimSpace(line[:idx]), strings.TrimSpace(line[idx+1:])
		switch k {
		case "OIDC_ISSUER":
			cfg.Issuer = v
		case "OIDC_CLIENT_ID":
			cfg.ClientID = v
		case "OIDC_CLIENT_SECRET":
			cfg.ClientSecret = v
		case "OIDC_PROVIDER_NAME":
			cfg.Name = v
		case "OIDC_REDIRECT_URI":
			cfg.RedirectURI = v
		}
	}
	cfg.Enabled = cfg.Issuer != "" && cfg.ClientID != ""
	return cfg
}

func oidcSaveConfig(baseDir string, cfg oidcCfg) error {
	dir := filepath.Join(baseDir, "data", "ui")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	content := fmt.Sprintf(
		"OIDC_PROVIDER_NAME=%s\nOIDC_ISSUER=%s\nOIDC_CLIENT_ID=%s\nOIDC_CLIENT_SECRET=%s\nOIDC_REDIRECT_URI=%s\n",
		cfg.Name, cfg.Issuer, cfg.ClientID, cfg.ClientSecret, cfg.RedirectURI,
	)
	return os.WriteFile(filepath.Join(dir, "oidc.conf"), []byte(content), 0o600)
}

// ── Discovery ─────────────────────────────────────────────────────────────────

type oidcDiscovery struct {
	mu        sync.Mutex
	issuer    string
	authEp    string
	tokenEp   string
	fetchedAt time.Time
}

var oidcDisc oidcDiscovery

func (d *oidcDiscovery) endpoints(issuer string) (authEp, tokenEp string, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.issuer == issuer && time.Since(d.fetchedAt) < time.Hour {
		return d.authEp, d.tokenEp, nil
	}
	discURL := strings.TrimRight(issuer, "/") + "/.well-known/openid-configuration"
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(discURL) //#nosec G107 — URL depuis config admin
	if err != nil {
		return "", "", fmt.Errorf("OIDC discovery: %w", err)
	}
	defer resp.Body.Close()
	var doc struct {
		AuthEndpoint  string `json:"authorization_endpoint"`
		TokenEndpoint string `json:"token_endpoint"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return "", "", fmt.Errorf("OIDC discovery decode: %w", err)
	}
	if doc.AuthEndpoint == "" || doc.TokenEndpoint == "" {
		return "", "", fmt.Errorf("OIDC discovery: endpoints manquants")
	}
	d.issuer = issuer
	d.authEp = doc.AuthEndpoint
	d.tokenEp = doc.TokenEndpoint
	d.fetchedAt = time.Now()
	return doc.AuthEndpoint, doc.TokenEndpoint, nil
}

// ── State store (PKCE) ────────────────────────────────────────────────────────

type oidcPending struct {
	CodeVerifier string
	ExpiresAt    time.Time
}

var oidcStates = struct {
	mu   sync.Mutex
	data map[string]oidcPending
}{data: make(map[string]oidcPending)}

func oidcPutState(state, verifier string) {
	oidcStates.mu.Lock()
	defer oidcStates.mu.Unlock()
	now := time.Now()
	for k, v := range oidcStates.data {
		if now.After(v.ExpiresAt) {
			delete(oidcStates.data, k)
		}
	}
	oidcStates.data[state] = oidcPending{CodeVerifier: verifier, ExpiresAt: now.Add(5 * time.Minute)}
}

func oidcPopState(state string) (oidcPending, bool) {
	oidcStates.mu.Lock()
	defer oidcStates.mu.Unlock()
	p, ok := oidcStates.data[state]
	if ok {
		delete(oidcStates.data, state)
	}
	return p, ok
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func oidcRedirectURI(cfg oidcCfg, r *http.Request) string {
	if cfg.RedirectURI != "" {
		return cfg.RedirectURI
	}
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return scheme + "://" + r.Host + "/auth/oidc/callback"
}

func randBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	_, err := rand.Read(b)
	return b, err
}

// ── Handlers ──────────────────────────────────────────────────────────────────

// ── Auto-configuration depuis Authentik ───────────────────────────────────────

// tryAutoConfigAuthentik tente de créer automatiquement une application OIDC
// dans Authentik pour Caleope UI, en utilisant le token bootstrap Authentik.
// Appelée si OIDC n'est pas encore configuré mais Authentik est installé.
func tryAutoConfigAuthentik(baseDir string, r *http.Request) {
	// Vérifier Authentik installé
	if _, err := os.Stat(filepath.Join(baseDir, "apps-installed", "authentik")); err != nil {
		return
	}
	akSecrets := filepath.Join(baseDir, "app-config", "authentik", "secrets.env")
	token := readEnvKey(akSecrets, "AUTHENTIK_BOOTSTRAP_TOKEN")
	akDomain := readEnvKey(akSecrets, "AUTHENTIK_DOMAIN")
	if token == "" {
		return
	}
	if akDomain == "" {
		// Dériver depuis CALEOPE_DOMAIN si disponible
		caleoCfg := filepath.Join(baseDir, "caleope.conf")
		baseDomain := readEnvKey(caleoCfg, "CALEOPE_DOMAIN")
		if baseDomain != "" {
			akDomain = "authentik." + baseDomain
		}
	}
	if akDomain == "" {
		return
	}

	apiBase := "https://" + akDomain + "/api/v3"
	hdr := map[string]string{
		"Authorization": "Bearer " + token,
		"Content-Type":  "application/json",
	}
	client := &http.Client{Timeout: 10 * time.Second}

	doJSON := func(method, url string, body interface{}) (map[string]interface{}, int, error) {
		var bodyBytes []byte
		if body != nil {
			b, err := json.Marshal(body)
			if err != nil {
				return nil, 0, err
			}
			bodyBytes = b
		}
		req, err := http.NewRequest(method, url, strings.NewReader(string(bodyBytes)))
		if err != nil {
			return nil, 0, err
		}
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, 0, err
		}
		defer resp.Body.Close()
		var result map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&result)
		return result, resp.StatusCode, nil
	}

	// Vérifier si l'application caleope-ui existe déjà
	existing, status, err := doJSON("GET", apiBase+"/core/applications/?slug=caleope-ui", nil)
	if err != nil || status != 200 {
		return
	}
	results, _ := existing["results"].([]interface{})
	if len(results) > 0 {
		// App déjà créée — récupérer les credentials si on les a pas
		// (on ne peut pas récupérer le client_secret après création, skip)
		return
	}

	// Récupérer le flow d'authentification par défaut
	flows, status, err := doJSON("GET", apiBase+"/flows/instances/?designation=authentication&page_size=1", nil)
	if err != nil || status != 200 {
		return
	}
	flowResults, _ := flows["results"].([]interface{})
	if len(flowResults) == 0 {
		return
	}
	flowMap, _ := flowResults[0].(map[string]interface{})
	flowUUID, _ := flowMap["pk"].(string)
	if flowUUID == "" {
		return
	}

	// Déterminer le redirect_uri (scheme + host de la requête courante)
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	redirectURI := scheme + "://" + r.Host + "/auth/oidc/callback"

	// Créer le provider OAuth2/OIDC
	provResp, status, err := doJSON("POST", apiBase+"/providers/oauth2/", map[string]interface{}{
		"name":                   "Caleope UI",
		"authorization_flow":     flowUUID,
		"client_type":            "confidential",
		"sub_mode":               "hashed_user_id",
		"redirect_uris":          redirectURI + "\n" + strings.Replace(redirectURI, "http://", "https://", 1),
		"access_code_validity":   "minutes=1",
		"access_token_validity":  "hours=1",
		"refresh_token_validity": "days=30",
		"issuer_mode":            "global",
		"signing_key":            nil,
	})
	if err != nil || status != 201 {
		return
	}
	provPK, _ := provResp["pk"].(float64)
	clientID, _ := provResp["client_id"].(string)
	clientSecret, _ := provResp["client_secret"].(string)
	if clientID == "" || clientSecret == "" {
		return
	}

	// Créer l'application
	_, _, _ = doJSON("POST", apiBase+"/core/applications/", map[string]interface{}{
		"name":             "Caleope UI",
		"slug":             "caleope-ui",
		"provider":         int(provPK),
		"meta_launch_url":  scheme + "://" + r.Host,
		"meta_description": "Interface d'administration Caleope",
	})

	// Sauvegarder la config OIDC
	issuer := "https://" + akDomain + "/application/o/caleope-ui/"
	cfg := oidcCfg{
		Issuer:       issuer,
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Name:         "Connexion Authentik",
		RedirectURI:  redirectURI,
	}
	_ = oidcSaveConfig(baseDir, cfg)
}

// GET /auth/oidc/config — public
func handleOidcConfig(w http.ResponseWriter, r *http.Request, baseDir string) {
	cfg := oidcLoadConfig(baseDir)

	// Auto-configuration depuis Authentik si OIDC non encore configuré
	if !cfg.Enabled {
		tryAutoConfigAuthentik(baseDir, r)
		cfg = oidcLoadConfig(baseDir) // re-lire après tentative
	}

	name := cfg.Name
	if name == "" {
		name = "Connexion SSO"
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"enabled": cfg.Enabled,
		"name":    name,
	})
}

// POST /auth/oidc/config — session requise
func handleOidcSave(w http.ResponseWriter, r *http.Request, baseDir string) {
	var body struct {
		Issuer       string `json:"issuer"`
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
		Name         string `json:"name"`
		RedirectURI  string `json:"redirect_uri"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonErr(w, http.StatusBadRequest, "JSON invalide")
		return
	}
	name := body.Name
	if name == "" {
		name = "SSO"
	}
	cfg := oidcCfg{
		Issuer:       body.Issuer,
		ClientID:     body.ClientID,
		ClientSecret: body.ClientSecret,
		Name:         name,
		RedirectURI:  body.RedirectURI,
	}
	if err := oidcSaveConfig(baseDir, cfg); err != nil {
		jsonErr(w, http.StatusInternalServerError, "Impossible de sauvegarder: "+err.Error())
		return
	}
	// Invalider le cache discovery
	oidcDisc.mu.Lock()
	oidcDisc.issuer = ""
	oidcDisc.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// GET /auth/oidc/start
func handleOidcStart(w http.ResponseWriter, r *http.Request, baseDir string) {
	cfg := oidcLoadConfig(baseDir)
	if !cfg.Enabled {
		http.Error(w, "OIDC non configuré", http.StatusNotFound)
		return
	}

	authEp, _, err := oidcDisc.endpoints(cfg.Issuer)
	if err != nil {
		http.Error(w, "Erreur découverte OIDC: "+err.Error(), http.StatusBadGateway)
		return
	}

	// code_verifier (43 chars minimum RFC 7636)
	vb, err := randBytes(32)
	if err != nil {
		http.Error(w, "erreur interne", http.StatusInternalServerError)
		return
	}
	codeVerifier := base64.RawURLEncoding.EncodeToString(vb)

	// code_challenge = BASE64URL(SHA256(code_verifier))
	h := sha256.Sum256([]byte(codeVerifier))
	codeChallenge := base64.RawURLEncoding.EncodeToString(h[:])

	// state
	sb, err := randBytes(16)
	if err != nil {
		http.Error(w, "erreur interne", http.StatusInternalServerError)
		return
	}
	state := base64.RawURLEncoding.EncodeToString(sb)

	oidcPutState(state, codeVerifier)

	authURL, _ := url.Parse(authEp)
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", cfg.ClientID)
	q.Set("redirect_uri", oidcRedirectURI(cfg, r))
	q.Set("scope", "openid profile email")
	q.Set("state", state)
	q.Set("code_challenge", codeChallenge)
	q.Set("code_challenge_method", "S256")
	authURL.RawQuery = q.Encode()

	http.Redirect(w, r, authURL.String(), http.StatusFound)
}

// GET /auth/oidc/callback
func handleOidcCallback(w http.ResponseWriter, r *http.Request, baseDir string, store *sessions) {
	cfg := oidcLoadConfig(baseDir)
	if !cfg.Enabled {
		http.Error(w, "OIDC non configuré", http.StatusNotFound)
		return
	}

	if errParam := r.URL.Query().Get("error"); errParam != "" {
		http.Redirect(w, r, "/?oidc_error="+url.QueryEscape(errParam), http.StatusFound)
		return
	}

	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")
	if state == "" || code == "" {
		http.Redirect(w, r, "/?oidc_error=missing_params", http.StatusFound)
		return
	}

	pending, ok := oidcPopState(state)
	if !ok || time.Now().After(pending.ExpiresAt) {
		http.Redirect(w, r, "/?oidc_error=invalid_state", http.StatusFound)
		return
	}

	_, tokenEp, err := oidcDisc.endpoints(cfg.Issuer)
	if err != nil {
		http.Redirect(w, r, "/?oidc_error=discovery_failed", http.StatusFound)
		return
	}

	// Échanger le code contre un token
	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("client_id", cfg.ClientID)
	data.Set("client_secret", cfg.ClientSecret)
	data.Set("code", code)
	data.Set("code_verifier", pending.CodeVerifier)
	data.Set("redirect_uri", oidcRedirectURI(cfg, r))

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.PostForm(tokenEp, data) //#nosec G107 — URL découverte depuis config admin
	if err != nil {
		http.Redirect(w, r, "/?oidc_error=token_request_failed", http.StatusFound)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		http.Redirect(w, r, "/?oidc_error=token_exchange_failed", http.StatusFound)
		return
	}

	var tokenResult map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResult); err != nil {
		http.Redirect(w, r, "/?oidc_error=token_decode_failed", http.StatusFound)
		return
	}
	if _, hasToken := tokenResult["access_token"]; !hasToken {
		http.Redirect(w, r, "/?oidc_error=no_access_token", http.StatusFound)
		return
	}

	// Créer une session Caleope
	tok := store.create()
	http.SetCookie(w, &http.Cookie{
		Name:     "caleope-session",
		Value:    tok,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode, // Lax requis : redirect cross-site depuis le provider
		MaxAge:   86400,
	})
	http.Redirect(w, r, "/", http.StatusFound)
}
