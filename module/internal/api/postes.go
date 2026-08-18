package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gaiver-it/caleope/internal/postes"
)

// Routes des postes nomades.
//
// ⚠️ DEUX PUBLICS, DEUX AUTHENTIFICATIONS — c'est le point délicat de ce fichier.
//
//   - L'ADMINISTRATION (créer un profil, fabriquer un code d'appairage, lister
//     les machines) passe par le jeton d'admin habituel, donc par s.auth.
//   - LE POSTE lui-même n'a pas ce jeton, et ne doit surtout pas l'avoir : un
//     portable qui traîne dans un train ne peut pas porter la clé du serveur.
//     Il s'authentifie avec SA clé, obtenue à l'appairage, qui ne donne accès
//     qu'à sa propre configuration.
//
// L'appairage est le seul point ouvert sans authentification préalable : c'est
// justement son rôle. Il est tenu par un code à usage unique, périmé en deux
// heures, qui ne donne rien d'autre que le droit de devenir une machine.

// routePostesProfils : GET (liste) / POST (créer ou remplacer) — ADMIN
func (s *Server) routePostesProfils(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.httpOK(w, s.postes.ListerProfils())
	case http.MethodPost:
		var p postes.Profil
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			s.httpError(w, "body JSON invalide", http.StatusBadRequest)
			return
		}
		if err := s.postes.EnregistrerProfil(p); err != nil {
			s.httpError(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.httpOK(w, map[string]string{"message": "Profil enregistré", "nom": p.Nom})
	default:
		s.httpError(w, "méthode non autorisée", http.StatusMethodNotAllowed)
	}
}

// routePostesProfil : DELETE /api/v1/postes/profils/{nom} — ADMIN
func (s *Server) routePostesProfil(w http.ResponseWriter, r *http.Request) {
	nom := strings.TrimPrefix(r.URL.Path, "/api/v1/postes/profils/")
	if nom == "" {
		s.httpError(w, "nom de profil manquant", http.StatusBadRequest)
		return
	}
	if r.Method != http.MethodDelete {
		s.httpError(w, "méthode non autorisée", http.StatusMethodNotAllowed)
		return
	}
	if err := s.postes.SupprimerProfil(nom); err != nil {
		s.httpError(w, err.Error(), http.StatusConflict)
		return
	}
	s.httpOK(w, map[string]string{"message": "Profil supprimé"})
}

// routePostesMachines : GET (liste) — ADMIN
func (s *Server) routePostesMachines(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.httpError(w, "méthode non autorisée", http.StatusMethodNotAllowed)
		return
	}
	s.httpOK(w, s.postes.ListerMachines())
}

// routePostesMachine : DELETE /api/v1/postes/machines/{empreinte} — ADMIN
// Oublier une machine RÉVOQUE sa clé : elle ne tirera plus rien.
func (s *Server) routePostesMachine(w http.ResponseWriter, r *http.Request) {
	emp := strings.TrimPrefix(r.URL.Path, "/api/v1/postes/machines/")
	if r.Method != http.MethodDelete || emp == "" {
		s.httpError(w, "requête invalide", http.StatusBadRequest)
		return
	}
	if err := s.postes.OublierMachine(emp); err != nil {
		s.httpError(w, err.Error(), http.StatusNotFound)
		return
	}
	s.httpOK(w, map[string]string{"message": "Machine oubliée, sa clé est révoquée"})
}

// routePostesJeton : POST /api/v1/postes/jeton {profil} — ADMIN
// Rend le code court à recopier dans l'exécutable, sur la machine à appairer.
func (s *Server) routePostesJeton(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.httpError(w, "méthode non autorisée", http.StatusMethodNotAllowed)
		return
	}
	var corps struct {
		Profil string `json:"profil"`
	}
	if err := json.NewDecoder(r.Body).Decode(&corps); err != nil {
		s.httpError(w, "body JSON invalide", http.StatusBadRequest)
		return
	}
	code, exp, err := s.postes.CreerJeton(corps.Profil)
	if err != nil {
		s.httpError(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.httpOK(w, map[string]interface{}{"jeton": code, "expire": exp})
}

// routePostesAppairage : POST /api/v1/postes/appairage — LE POSTE, sans auth
//
// Seul point ouvert. Ce qu'il rend (la clé de machine) ne vaut que pour la
// configuration de cette machine-là.
func (s *Server) routePostesAppairage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.httpError(w, "méthode non autorisée", http.StatusMethodNotAllowed)
		return
	}
	var corps struct {
		Jeton   string `json:"jeton"`
		Machine string `json:"machine"`
		Systeme string `json:"systeme"`
	}
	if err := json.NewDecoder(r.Body).Decode(&corps); err != nil {
		s.httpError(w, "body JSON invalide", http.StatusBadRequest)
		return
	}
	m, err := s.postes.Appairer(corps.Jeton, corps.Machine, corps.Systeme)
	if err != nil {
		// Message volontairement identique pour toutes les causes de refus :
		// distinguer « jeton inconnu » de « jeton périmé » aiderait surtout
		// quelqu'un qui essaie des codes au hasard.
		s.httpError(w, "appairage refusé", http.StatusUnauthorized)
		return
	}
	s.httpOK(w, map[string]interface{}{
		"cle":     m.Cle,
		"profil":  m.Profil,
		"machine": m.Nom,
	})
}

// cleDeLaMachine extrait la clé de l'en-tête « Authorization: Machine <clé> ».
func cleDeLaMachine(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Machine ") {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(h, "Machine "))
}

// routePostesProfilDuPoste : GET /api/v1/postes/ma-conf — LE POSTE, avec sa clé
func (s *Server) routePostesProfilDuPoste(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.httpError(w, "méthode non autorisée", http.StatusMethodNotAllowed)
		return
	}
	cle := cleDeLaMachine(r)
	if cle == "" {
		s.httpError(w, "clé de machine requise", http.StatusUnauthorized)
		return
	}
	m, p, err := s.postes.ProfilDeLaMachine(cle)
	if err != nil {
		s.httpError(w, "machine inconnue", http.StatusUnauthorized)
		return
	}
	s.httpOK(w, map[string]interface{}{"machine": m.Nom, "profil": p})
}

// routePostesRapport : POST /api/v1/postes/rapport — LE POSTE, avec sa clé
// Le poste dit ce qu'il a constaté chez lui ; c'est ce qui permet à l'interface
// d'afficher « à jour » ou « 3 logiciels manquants » sans aller le sonder.
func (s *Server) routePostesRapport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.httpError(w, "méthode non autorisée", http.StatusMethodNotAllowed)
		return
	}
	cle := cleDeLaMachine(r)
	if cle == "" {
		s.httpError(w, "clé de machine requise", http.StatusUnauthorized)
		return
	}
	var corps struct {
		Manquants int `json:"manquants"`
	}
	if err := json.NewDecoder(r.Body).Decode(&corps); err != nil {
		s.httpError(w, "body JSON invalide", http.StatusBadRequest)
		return
	}
	if err := s.postes.Rapporter(cle, corps.Manquants); err != nil {
		s.httpError(w, "machine inconnue", http.StatusUnauthorized)
		return
	}
	s.httpOK(w, map[string]string{"message": "rapport enregistré"})
}
