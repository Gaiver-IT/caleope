package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gaiver-it/caleope/internal/instances"
)

// Routes de gestion des serveurs de jeu.
//
// Toutes derrière le jeton d'admin : la console d'un serveur permet d'en
// prendre le contrôle (op, whitelist, arrêt), ce n'est pas une page publique.

// appDeLaRequete extrait l'identifiant d'app d'un chemin
// /api/v1/jeux/{app}/quelquechose. L'identifiant peut contenir un « @ ».
func appDeLaRequete(chemin, suffixe string) string {
	reste := strings.TrimPrefix(chemin, "/api/v1/jeux/")
	reste = strings.TrimSuffix(reste, suffixe)
	return strings.Trim(reste, "/")
}

// routeJeux : GET /api/v1/jeux — les serveurs installés.
func (s *Server) routeJeux(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.httpError(w, "méthode non autorisée", http.StatusMethodNotAllowed)
		return
	}
	apps, err := s.rt.ListApps()
	if err != nil {
		s.httpError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	type serveur struct {
		ID       string `json:"id"`
		Instance string `json:"instance,omitempty"`
		Statut   string `json:"statut"`
		Port     int    `json:"port,omitempty"`
	}
	out := []serveur{}
	for _, a := range apps {
		paquet, inst := instances.Decouper(a.ID)
		// Seuls les paquets de jeu connus : la page ne doit pas proposer une
		// console à Nextcloud.
		if paquet != "minecraft" {
			continue
		}
		sv := serveur{ID: a.ID, Instance: inst, Statut: string(a.Status)}
		for _, p := range a.Ports {
			if p.Name == "jeu" {
				sv.Port = p.Host
			}
		}
		out = append(out, sv)
	}
	s.httpOK(w, out)
}

// routeJeuConsole : POST /api/v1/jeux/{app}/console {commande}
func (s *Server) routeJeuConsole(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.httpError(w, "méthode non autorisée", http.StatusMethodNotAllowed)
		return
	}
	app := appDeLaRequete(r.URL.Path, "/console")
	var corps struct {
		Commande string `json:"commande"`
	}
	if err := json.NewDecoder(r.Body).Decode(&corps); err != nil {
		s.httpError(w, "body JSON invalide", http.StatusBadRequest)
		return
	}
	sortie, err := s.jeux.Console(app, corps.Commande)
	if err != nil && sortie == "" {
		s.httpError(w, err.Error(), http.StatusBadGateway)
		return
	}
	// Une commande refusée PAR LE JEU (« Unknown command ») n'est pas une panne
	// de Caleope : on rend la réponse telle quelle, c'est ce que l'utilisateur
	// veut lire.
	s.httpOK(w, map[string]string{"sortie": sortie})
}

// routeJeuProprietes : GET / PUT /api/v1/jeux/{app}/proprietes
func (s *Server) routeJeuProprietes(w http.ResponseWriter, r *http.Request) {
	app := appDeLaRequete(r.URL.Path, "/proprietes")
	switch r.Method {
	case http.MethodGet:
		p, err := s.jeux.Proprietes(app)
		if err != nil {
			s.httpError(w, err.Error(), http.StatusNotFound)
			return
		}
		s.httpOK(w, p)
	case http.MethodPut:
		var changements map[string]string
		if err := json.NewDecoder(r.Body).Decode(&changements); err != nil {
			s.httpError(w, "body JSON invalide", http.StatusBadRequest)
			return
		}
		if err := s.jeux.EcrireProprietes(app, changements); err != nil {
			s.httpError(w, err.Error(), http.StatusBadRequest)
			return
		}
		// On le dit franchement : le fichier est lu au démarrage du serveur.
		s.httpOK(w, map[string]string{
			"message": "Enregistré — le serveur doit redémarrer pour que ça prenne effet.",
		})
	default:
		s.httpError(w, "méthode non autorisée", http.StatusMethodNotAllowed)
	}
}

// routeJeuMods : GET (liste) / POST (ajouter) / DELETE (retirer)
func (s *Server) routeJeuMods(w http.ResponseWriter, r *http.Request) {
	app := appDeLaRequete(r.URL.Path, "/mods")
	switch r.Method {
	case http.MethodGet:
		l, err := s.jeux.Mods(app)
		if err != nil {
			s.httpError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.httpOK(w, l)
	case http.MethodPost, http.MethodDelete:
		var corps struct {
			Projet string `json:"projet"`
		}
		if err := json.NewDecoder(r.Body).Decode(&corps); err != nil {
			s.httpError(w, "body JSON invalide", http.StatusBadRequest)
			return
		}
		var err error
		if r.Method == http.MethodPost {
			err = s.jeux.AjouterMod(app, corps.Projet)
		} else {
			err = s.jeux.RetirerMod(app, corps.Projet)
		}
		if err != nil {
			s.httpError(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.httpOK(w, map[string]string{
			"message": "Liste mise à jour — le mod sera téléchargé au prochain démarrage du serveur.",
		})
	default:
		s.httpError(w, "méthode non autorisée", http.StatusMethodNotAllowed)
	}
}

// routeJeuxRecherche : GET /api/v1/jeux-recherche?q=…&moteur=…&version=…
func (s *Server) routeJeuxRecherche(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.httpError(w, "méthode non autorisée", http.StatusMethodNotAllowed)
		return
	}
	q := r.URL.Query()
	res, err := s.jeux.ChercherModrinth(q.Get("q"), q.Get("moteur"), q.Get("version"))
	if err != nil {
		s.httpError(w, err.Error(), http.StatusBadGateway)
		return
	}
	s.httpOK(w, res)
}
