// cmd/caleope-ui/totp.go
//
// Double authentification TOTP (RFC 6238) — sans dépendance externe.
//
// Endpoints exposés :
//   GET  /auth/totp/status   → {enabled, has_secret}
//   POST /auth/totp/setup    → génère un nouveau secret (retourne {secret, uri})
//   POST /auth/totp/enable   {code} → active le 2FA après vérification du code
//   POST /auth/totp/disable  {code} → désactive le 2FA
//   POST /auth/totp          {code} → valide le code lors du login (cookie pending requis)
//
// Flux de login avec TOTP activé :
//   1. POST /auth/login  {password OK}  → {totp_required:true} + cookie caleope-pending (5 min)
//   2. POST /auth/totp   {code}          → vérifie code + set cookie caleope-session

package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ── TOTP config ───────────────────────────────────────────────────────────────

type totpState struct {
	mu      sync.RWMutex
	secret  string
	enabled bool
}

var globalTOTP = &totpState{}

// totpLoad recharge l'état TOTP depuis le disque (idempotent, thread-safe).
func totpLoad(baseDir string) {
	secret, _ := readFile(filepath.Join(baseDir, "data", "ui", "totp.secret"))
	_, errEnabled := os.Stat(filepath.Join(baseDir, "data", "ui", "totp.enabled"))
	secret = strings.TrimSpace(secret)
	globalTOTP.mu.Lock()
	globalTOTP.secret = secret
	globalTOTP.enabled = (errEnabled == nil) && secret != ""
	globalTOTP.mu.Unlock()
}

// totpIsEnabled retourne (enabled, secret) de façon thread-safe.
func totpIsEnabled() (bool, string) {
	globalTOTP.mu.RLock()
	defer globalTOTP.mu.RUnlock()
	return globalTOTP.enabled, globalTOTP.secret
}

// ── TOTP algorithm (RFC 6238 + HOTP RFC 4226) ─────────────────────────────────

func totpGenSecret() (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b), nil
}

func totpHOTP(secret string, counter uint64) (uint32, error) {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(secret))
	if err != nil {
		return 0, fmt.Errorf("secret invalide: %w", err)
	}
	mac := hmac.New(sha1.New, key)
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, counter)
	mac.Write(buf)
	digest := mac.Sum(nil)
	offset := digest[len(digest)-1] & 0x0f
	val := binary.BigEndian.Uint32(digest[offset:offset+4]) & 0x7fffffff
	return val % uint32(math.Pow10(6)), nil
}

// totpVerify valide un code TOTP avec une fenêtre de ±1 période (±30 s).
func totpVerify(secret, code string) bool {
	n, err := strconv.ParseUint(strings.TrimSpace(code), 10, 64)
	if err != nil {
		return false
	}
	ts := uint64(time.Now().Unix()) / 30
	for _, delta := range []uint64{ts - 1, ts, ts + 1} {
		expected, err := totpHOTP(secret, delta)
		if err != nil {
			return false
		}
		if uint32(n) == expected {
			return true
		}
	}
	return false
}

// ── Pending sessions (mot de passe OK, TOTP non encore vérifié) ──────────────

type pendingSessions struct {
	mu   sync.RWMutex
	data map[string]time.Time
}

var pendingStore = &pendingSessions{data: make(map[string]time.Time)}

func (p *pendingSessions) create() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	tok := hex.EncodeToString(b)
	p.mu.Lock()
	p.data[tok] = time.Now().Add(5 * time.Minute)
	p.mu.Unlock()
	return tok
}

func (p *pendingSessions) valid(tok string) bool {
	p.mu.RLock()
	exp, ok := p.data[tok]
	p.mu.RUnlock()
	return ok && time.Now().Before(exp)
}

func (p *pendingSessions) delete(tok string) {
	p.mu.Lock()
	delete(p.data, tok)
	p.mu.Unlock()
}

// ── Handlers ──────────────────────────────────────────────────────────────────

// handleTOTPStatus : GET /auth/totp/status
func handleTOTPStatus(w http.ResponseWriter, r *http.Request, baseDir string) {
	totpLoad(baseDir)
	enabled, secret := totpIsEnabled()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"enabled":    enabled,
		"has_secret": secret != "",
	})
}

// handleTOTPSetup : POST /auth/totp/setup — génère un nouveau secret (pas encore activé)
func handleTOTPSetup(w http.ResponseWriter, r *http.Request, baseDir string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	secret, err := totpGenSecret()
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "erreur génération: "+err.Error())
		return
	}
	dir := filepath.Join(baseDir, "data", "ui")
	_ = os.MkdirAll(dir, 0700)
	if err := os.WriteFile(filepath.Join(dir, "totp.secret"), []byte(secret), 0600); err != nil {
		jsonErr(w, http.StatusInternalServerError, "erreur sauvegarde: "+err.Error())
		return
	}
	globalTOTP.mu.Lock()
	globalTOTP.secret = secret
	globalTOTP.mu.Unlock()

	uri := fmt.Sprintf("otpauth://totp/Caleope:admin?secret=%s&issuer=Caleope&algorithm=SHA1&digits=6&period=30",
		url.QueryEscape(secret))
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"secret": secret, "uri": uri})
}

// handleTOTPEnable : POST /auth/totp/enable {code} — active le 2FA
func handleTOTPEnable(w http.ResponseWriter, r *http.Request, baseDir string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Code string `json:"code"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	globalTOTP.mu.RLock()
	secret := globalTOTP.secret
	globalTOTP.mu.RUnlock()

	if secret == "" {
		jsonErr(w, http.StatusBadRequest, "secret non généré — appeler /auth/totp/setup d'abord")
		return
	}
	if !totpVerify(secret, body.Code) {
		jsonErr(w, http.StatusUnauthorized, "code TOTP invalide")
		return
	}
	if err := os.WriteFile(filepath.Join(baseDir, "data", "ui", "totp.enabled"), []byte("1"), 0600); err != nil {
		jsonErr(w, http.StatusInternalServerError, "erreur activation: "+err.Error())
		return
	}
	globalTOTP.mu.Lock()
	globalTOTP.enabled = true
	globalTOTP.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleTOTPDisable : POST /auth/totp/disable {code} — désactive le 2FA
func handleTOTPDisable(w http.ResponseWriter, r *http.Request, baseDir string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Code string `json:"code"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	globalTOTP.mu.RLock()
	secret := globalTOTP.secret
	enabled := globalTOTP.enabled
	globalTOTP.mu.RUnlock()

	if enabled && !totpVerify(secret, body.Code) {
		jsonErr(w, http.StatusUnauthorized, "code TOTP invalide")
		return
	}
	_ = os.Remove(filepath.Join(baseDir, "data", "ui", "totp.enabled"))
	_ = os.Remove(filepath.Join(baseDir, "data", "ui", "totp.secret"))
	globalTOTP.mu.Lock()
	globalTOTP.enabled = false
	globalTOTP.secret = ""
	globalTOTP.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleTOTPVerify : POST /auth/totp {code}
// Appelé après que le mot de passe a été validé (cookie caleope-pending requis).
// En cas de succès, set le cookie de session et invalide le cookie pending.
func handleTOTPVerify(w http.ResponseWriter, r *http.Request, baseDir string, sessionStore *sessions) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	c, err := r.Cookie("caleope-pending")
	if err != nil || !pendingStore.valid(c.Value) {
		jsonErr(w, http.StatusUnauthorized, "session expirée — reconnectez-vous")
		return
	}
	var body struct {
		Code string `json:"code"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	totpLoad(baseDir)
	_, secret := totpIsEnabled()
	if !totpVerify(secret, body.Code) {
		jsonErr(w, http.StatusUnauthorized, "code TOTP invalide")
		return
	}
	pendingStore.delete(c.Value)
	// Effacer le cookie pending
	http.SetCookie(w, &http.Cookie{Name: "caleope-pending", MaxAge: -1, Path: "/"})
	// Créer la session
	tok := sessionStore.create()
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
}
