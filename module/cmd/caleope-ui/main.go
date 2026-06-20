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
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

//go:embed web
var webFiles embed.FS

// ── Sessions ────────────────────────────────────────────────────────────────

type sessions struct {
	mu   sync.RWMutex
	data map[string]time.Time
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
	fmt.Printf("╔══════════════════════════════════════╗\n")
	fmt.Printf("║       Caleope UI  — v0.4.18          ║\n")
	fmt.Printf("╚══════════════════════════════════════╝\n")
	fmt.Printf("  Interface : http://0.0.0.0%s\n", addr)
	fmt.Printf("  Daemon    : %s\n\n", *daemon)

	if err := http.ListenAndServe(addr, mux); err != nil {
		fmt.Fprintf(os.Stderr, "❌ %v\n", err)
		os.Exit(1)
	}
}
