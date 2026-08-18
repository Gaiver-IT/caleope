package api

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gaiver-it/caleope/pkg/version"
)

// Téléchargement des clients depuis l'interface.
//
// POURQUOI passer par le serveur plutôt que par un lien vers GitHub : parce que
// l'utilisateur est déjà connecté à SON Caleope. Lui demander d'aller chercher
// un fichier ailleurs, de choisir la bonne architecture parmi douze, puis de
// revenir, c'est perdre la moitié des gens en route. Le serveur sait quelle
// version il fait tourner ; il sert le bon fichier.
//
// Le fichier est mis en cache après le premier téléchargement : les postes
// suivants n'attendent plus le réseau, et une installation coupée d'Internet
// reste possible une fois le premier client récupéré.

// Client : une cible téléchargeable.
type Client struct {
	Cible       string `json:"cible"`
	Nom         string `json:"nom"`
	Fichier     string `json:"fichier"`
	Interface   string `json:"interface"` // "fenêtre" ou "ligne de commande"
	Instruction string `json:"instruction,omitempty"`
}

// clientsConnus : ce que la chaîne de fabrication publie réellement. Toute
// entrée ici DOIT correspondre à un fichier de la release, sinon le bouton
// mène à une erreur — on préfère ne pas proposer une cible qu'en proposer une
// qui échoue.
func clientsConnus() []Client {
	return []Client{
		{Cible: "macos-arm64", Nom: "macOS (Apple Silicon)", Fichier: "Poste-macos-arm64.app.zip", Interface: "fenêtre",
			Instruction: "Décompresse, puis au PREMIER lancement : clic droit sur Poste.app → Ouvrir. " +
				"Un double-clic direct est refusé par macOS tant que l'application n'est pas certifiée par Apple."},
		{Cible: "macos-amd64", Nom: "macOS (Intel)", Fichier: "Poste-macos-amd64.app.zip", Interface: "fenêtre",
			Instruction: "Décompresse, puis au PREMIER lancement : clic droit sur Poste.app → Ouvrir."},
		{Cible: "linux-amd64", Nom: "Linux / Ubuntu", Fichier: "poste-bureau-linux-amd64", Interface: "fenêtre",
			Instruction: "Rends-le exécutable (chmod +x) puis lance-le. Le raccourci poste.desktop l'ajoute au menu."},
		{Cible: "windows-amd64", Nom: "Windows", Fichier: "poste-bureau-windows-amd64.exe", Interface: "fenêtre",
			Instruction: "Double-clic. Windows peut demander une confirmation au premier lancement."},
		{Cible: "cli-linux-amd64", Nom: "Linux — ligne de commande", Fichier: "poste-linux-amd64", Interface: "ligne de commande"},
		{Cible: "cli-macos-arm64", Nom: "macOS — ligne de commande", Fichier: "poste-macos-arm64", Interface: "ligne de commande"},
	}
}

// routePostesClients : GET /api/v1/postes/clients — la liste, pour l'interface.
func (s *Server) routePostesClients(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.httpError(w, "méthode non autorisée", http.StatusMethodNotAllowed)
		return
	}
	s.httpOK(w, map[string]interface{}{
		"version": version.Version,
		"clients": clientsConnus(),
	})
}

// routePostesTelecharger : GET /api/v1/postes/clients/{cible}
func (s *Server) routePostesTelecharger(w http.ResponseWriter, r *http.Request) {
	cible := strings.TrimPrefix(r.URL.Path, "/api/v1/postes/clients/")
	var c *Client
	for _, x := range clientsConnus() {
		if x.Cible == cible {
			cp := x
			c = &cp
			break
		}
	}
	if c == nil {
		s.httpError(w, "cible inconnue", http.StatusNotFound)
		return
	}

	chemin, err := s.fichierClient(*c)
	if err != nil {
		s.httpError(w, err.Error(), http.StatusBadGateway)
		return
	}
	f, err := os.Open(chemin)
	if err != nil {
		s.httpError(w, "fichier illisible", http.StatusInternalServerError)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", c.Fichier))
	http.ServeContent(w, r, c.Fichier, time.Time{}, f)
}

// fichierClient rend le chemin local du fichier, en le récupérant la première fois.
func (s *Server) fichierClient(c Client) (string, error) {
	cache := filepath.Join(s.baseDir, "core", "clients", version.Version)
	chemin := filepath.Join(cache, c.Fichier)
	if st, err := os.Stat(chemin); err == nil && st.Size() > 0 {
		return chemin, nil
	}
	if err := os.MkdirAll(cache, 0o755); err != nil {
		return "", err
	}
	url := fmt.Sprintf("https://github.com/Gaiver-IT/caleope/releases/download/%s/%s", version.Version, c.Fichier)
	cl := &http.Client{Timeout: 5 * time.Minute}
	rep, err := cl.Get(url)
	if err != nil {
		return "", fmt.Errorf("téléchargement impossible : %w", err)
	}
	defer rep.Body.Close()
	if rep.StatusCode != http.StatusOK {
		return "", fmt.Errorf("le fichier %s n'existe pas pour la version %s", c.Fichier, version.Version)
	}
	// Écriture par fichier temporaire puis renommage : sans ça, une coupure
	// laisse un fichier tronqué dans le cache, et TOUS les téléchargements
	// suivants servent une application incomplète sans jamais réessayer.
	tmp := chemin + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(out, rep.Body); err != nil {
		out.Close()
		os.Remove(tmp)
		return "", err
	}
	out.Close()
	return chemin, os.Rename(tmp, chemin)
}
