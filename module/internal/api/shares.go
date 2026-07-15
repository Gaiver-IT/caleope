// internal/api/shares.go
//
// Handlers des Partages (User Shares) — socket (CLI) + HTTP (UI).
// S'appuie sur s.sh (shares.Manager). Le manager génère smb.conf,
// provisionne les utilisateurs Samba depuis Authentik et gère le mot de
// passe réseau dédié.

package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gaiver-it/caleope/pkg/types"
)

// ─────────────────────────────────────────────
// SOCKET (CLI)
// ─────────────────────────────────────────────

func (s *Server) handleSharesList() (interface{}, error) {
	return s.sh.List()
}

// handleSharesUpsert crée (update=false) ou met à jour (update=true) un partage.
// L'argument "share" contient le JSON complet d'un types.Share.
func (s *Server) handleSharesUpsert(args map[string]string, update bool) (interface{}, error) {
	var sh types.Share
	if err := json.Unmarshal([]byte(args["share"]), &sh); err != nil {
		return nil, fmt.Errorf("champ 'share' JSON invalide: %w", err)
	}
	var err error
	if update {
		err = s.sh.Update(sh)
	} else {
		err = s.sh.Add(sh)
	}
	if err != nil {
		return nil, err
	}
	return map[string]string{"message": fmt.Sprintf("Partage '%s' enregistré", sh.Name)}, nil
}

func (s *Server) handleSharesRemove(args map[string]string) error {
	name := args["name"]
	if name == "" {
		return fmt.Errorf("argument 'name' manquant")
	}
	return s.sh.Remove(name)
}

func (s *Server) handleSharesEnsureUser(args map[string]string) error {
	user := args["username"]
	if user == "" {
		return fmt.Errorf("argument 'username' manquant")
	}
	return s.sh.EnsureUser(user, parseGroups(args["groups"]))
}

func (s *Server) handleSharesSetPassword(args map[string]string) error {
	user, pw := args["username"], args["password"]
	if user == "" || pw == "" {
		return fmt.Errorf("arguments 'username' et 'password' requis")
	}
	if err := s.sh.EnsureUser(user, parseGroups(args["groups"])); err != nil {
		return err
	}
	return s.sh.SetNetworkPassword(user, pw)
}

// parseGroups accepte un tableau JSON ou une liste séparée par des virgules.
func parseGroups(g string) []string {
	g = strings.TrimSpace(g)
	if g == "" {
		return nil
	}
	if strings.HasPrefix(g, "[") {
		var out []string
		_ = json.Unmarshal([]byte(g), &out)
		return out
	}
	parts := strings.Split(g, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

// ─────────────────────────────────────────────
// HTTP (UI)
// ─────────────────────────────────────────────

// routeShares : GET /api/v1/shares (liste) ou POST /api/v1/shares (créer)
func (s *Server) routeShares(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		data, err := s.sh.List()
		if err != nil {
			s.httpError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.httpOK(w, data)
	case http.MethodPost:
		var sh types.Share
		if err := json.NewDecoder(r.Body).Decode(&sh); err != nil {
			s.httpError(w, "body JSON invalide", http.StatusBadRequest)
			return
		}
		if err := s.sh.Add(sh); err != nil {
			s.httpError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.httpOK(w, map[string]string{"message": "Partage créé", "name": sh.Name})
	default:
		s.httpError(w, "méthode non autorisée", http.StatusMethodNotAllowed)
	}
}

// routeShare : /api/v1/shares/{name}
//
//	PUT    → mettre à jour
//	DELETE → supprimer (les données sur disque sont conservées)
func (s *Server) routeShare(w http.ResponseWriter, r *http.Request) {
	name := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/shares/"), "/")
	if name == "" {
		s.httpError(w, "nom de partage manquant", http.StatusBadRequest)
		return
	}
	switch r.Method {
	case http.MethodDelete:
		if err := s.sh.Remove(name); err != nil {
			s.httpError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.httpOK(w, nil)
	case http.MethodPut:
		var sh types.Share
		if err := json.NewDecoder(r.Body).Decode(&sh); err != nil {
			s.httpError(w, "body JSON invalide", http.StatusBadRequest)
			return
		}
		sh.Name = name // le nom vient de l'URL
		if err := s.sh.Update(sh); err != nil {
			s.httpError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.httpOK(w, map[string]string{"message": "Partage mis à jour"})
	default:
		s.httpError(w, "méthode non autorisée", http.StatusMethodNotAllowed)
	}
}

// routeNetworkPassword : POST /api/v1/network-password
// body {username, password, groups?} — pose le mot de passe réseau SMB dédié
// et provisionne l'utilisateur Samba depuis Authentik (username + groupes).
func (s *Server) routeNetworkPassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.httpError(w, "méthode non autorisée", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Username string   `json:"username"`
		Password string   `json:"password"`
		Groups   []string `json:"groups"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Username == "" || body.Password == "" {
		s.httpError(w, "champs 'username' et 'password' requis", http.StatusBadRequest)
		return
	}
	if err := s.sh.EnsureUser(body.Username, body.Groups); err != nil {
		s.httpError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := s.sh.SetNetworkPassword(body.Username, body.Password); err != nil {
		s.httpError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.httpOK(w, map[string]string{"message": "Mot de passe réseau défini"})
}
