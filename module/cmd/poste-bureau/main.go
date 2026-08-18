// poste-bureau — la fenêtre. Ce qu'on double-clique sur son Mac ou son Ubuntu.
//
// # POURQUOI PAS UNE FENÊTRE NATIVE
//
// Une vraie fenêtre en Go (Fyne, GTK) impose CGO et des bibliothèques système
// différentes sur chaque machine. Deux conséquences : on ne compile plus depuis
// un seul poste vers les trois systèmes, et surtout on ne peut PAS vérifier le
// résultat sur une machine sans écran — on livrerait un programme jamais lancé.
//
// Ce binaire sert donc son interface en local et l'ouvre. Il n'a besoin de rien :
// pas de runtime, pas de bibliothèque graphique, pas d'installation préalable.
// Le rendu est entièrement sous notre contrôle, ce qui permet une interface
// réellement soignée plutôt que les boutons génériques d'une boîte à outils.
//
// # CE QUI TIENT LA PORTE
//
// L'interface locale n'est PAS un serveur web : elle n'écoute que sur la boucle
// locale, sur un port tiré au hasard, et exige un jeton engendré au démarrage.
// Sans ces trois précautions, n'importe quel site ouvert dans le navigateur
// pourrait piloter l'installation de logiciels sur la machine.
package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/gaiver-it/caleope/internal/posteclient"
)

//go:embed web/*
var fichiersWeb embed.FS

var jetonLocal string

// garde refuse tout ce qui ne présente pas le jeton de cette session.
func garde(suivant http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("j") != jetonLocal && r.Header.Get("X-Poste-Jeton") != jetonLocal {
			http.Error(w, "jeton absent", http.StatusForbidden)
			return
		}
		suivant(w, r)
	}
}

func repondre(w http.ResponseWriter, v interface{}, err error) {
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"erreur": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(v)
}

// etatComplet rassemble tout ce que la fenêtre affiche en une fois.
type etatComplet struct {
	Connectee bool                      `json:"connectee"`
	Machine   string                    `json:"machine"`
	Systeme   string                    `json:"systeme"`
	Gestion   string                    `json:"gestionnaire"`
	Serveur   string                    `json:"serveur"`
	Profil    string                    `json:"profil"`
	Demandes  int                       `json:"demandes"`
	Manquants []string                  `json:"manquants"`
	Dossiers  []posteclient.EtatDossier `json:"dossiers"`
	Erreur    string                    `json:"erreur,omitempty"`
}

func lireEtat() etatComplet {
	sys, gest := posteclient.Systeme()
	e := etatComplet{Systeme: sys, Gestion: gest, Machine: posteclient.NomMachine()}

	c, err := posteclient.LireConfig()
	if err != nil {
		return e // pas encore connectée : la fenêtre montrera l'écran d'accueil
	}
	e.Connectee, e.Serveur, e.Machine = true, c.Serveur, c.Machine

	p, err := posteclient.TirerProfil(c)
	if err != nil {
		e.Erreur = err.Error()
		return e
	}
	e.Profil, e.Demandes = p.Nom, len(p.Paquets)
	e.Dossiers = posteclient.PreparerDossiers(p)

	inst, err := posteclient.PaquetsInstalles(gest)
	if err != nil {
		e.Erreur = err.Error()
		return e
	}
	e.Manquants = posteclient.Manquants(p, inst)
	posteclient.Rapporter(c, len(e.Manquants))
	return e
}

func main() {
	// Appelée AVEC des arguments, l'application se comporte comme la commande en
	// ligne : c'est ce qui permet à un seul binaire de servir les deux usages, et
	// au lanceur posé dans ~/.local/bin de fonctionner sans second téléchargement.
	if len(os.Args) > 1 {
		enLigneDeCommande(os.Args[1:])
		return
	}
	jetonLocal = fmt.Sprintf("%d-%d", time.Now().UnixNano(), os.Getpid())

	// Port 0 : c'est le système qui en choisit un libre. Un port fixe finirait
	// par entrer en conflit avec autre chose, et surtout serait devinable.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintf(os.Stderr, "impossible d'ouvrir la fenêtre : %v\n", err)
		os.Exit(1)
	}
	adresse := fmt.Sprintf("http://127.0.0.1:%d/?j=%s", ln.Addr().(*net.TCPAddr).Port, jetonLocal)

	mux := http.NewServeMux()

	mux.HandleFunc("/", garde(func(w http.ResponseWriter, r *http.Request) {
		page, _ := fichiersWeb.ReadFile("web/index.html")
		t := template.Must(template.New("p").Parse(string(page)))
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = t.Execute(w, map[string]string{"Jeton": jetonLocal})
	}))

	mux.HandleFunc("/api/etat", garde(func(w http.ResponseWriter, r *http.Request) {
		repondre(w, lireEtat(), nil)
	}))

	mux.HandleFunc("/api/connexion", garde(func(w http.ResponseWriter, r *http.Request) {
		var corps struct{ Invitation string }
		if err := json.NewDecoder(r.Body).Decode(&corps); err != nil {
			repondre(w, nil, fmt.Errorf("requête illisible"))
			return
		}
		// Un seul champ : l'invitation porte l'adresse ET le code. Deux saisies
		// à la main, dont une URL, c'est là qu'on se trompe.
		serveur, code, err := posteclient.LireInvitation(corps.Invitation)
		if err != nil {
			repondre(w, nil, err)
			return
		}
		if _, err := posteclient.Connecter(serveur, code); err != nil {
			repondre(w, nil, err)
			return
		}
		repondre(w, lireEtat(), nil)
	}))

	mux.HandleFunc("/api/appliquer", garde(func(w http.ResponseWriter, r *http.Request) {
		c, err := posteclient.LireConfig()
		if err != nil {
			repondre(w, nil, err)
			return
		}
		p, err := posteclient.TirerProfil(c)
		if err != nil {
			repondre(w, nil, err)
			return
		}
		_, gest := posteclient.Systeme()
		inst, err := posteclient.PaquetsInstalles(gest)
		if err != nil {
			repondre(w, nil, err)
			return
		}
		var echecs []string
		for _, x := range posteclient.Manquants(p, inst) {
			if err := posteclient.Installer(gest, x); err != nil {
				// Un nom peut ne pas exister sur cette distribution : on continue,
				// sinon un seul paquet exotique bloque tous les autres.
				echecs = append(echecs, x)
			}
		}
		// On RE-MESURE au lieu d'annoncer : c'est ce qui distingue un rapport
		// d'une promesse.
		etat := lireEtat()
		repondre(w, map[string]interface{}{"etat": etat, "echecs": echecs}, nil)
	}))

	mux.HandleFunc("/api/commande-terminal", garde(func(w http.ResponseWriter, r *http.Request) {
		chemin, err := installerCommandeTerminal()
		repondre(w, map[string]string{"chemin": chemin}, err)
	}))

	go func() { _ = http.Serve(ln, mux) }()

	fmt.Printf("Poste — fenêtre ouverte sur %s\n", adresse)
	ouvrirNavigateur(adresse)
	select {} // la fenêtre vit tant que le programme vit
}

// ouvrirNavigateur affiche l'interface. Si l'ouverture échoue, on affiche
// l'adresse plutôt que de laisser l'utilisateur devant un programme muet.
func ouvrirNavigateur(url string) {
	var c *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		c = exec.Command("open", url)
	case "windows":
		c = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		c = exec.Command("xdg-open", url)
	}
	if err := c.Start(); err != nil {
		fmt.Printf("Ouvre cette adresse à la main : %s\n", url)
	}
}

// installerCommandeTerminal pose « poste » là où le terminal le trouvera.
//
// POURQUOI c'est un bouton et pas une consigne : un exécutable téléchargé
// atterrit dans Téléchargements, sans droit d'exécution et hors du PATH. « ça
// trouve pas » en terminal n'est pas une erreur de l'utilisateur, c'est le
// comportement normal — et lui demander de taper chmod puis de déplacer un
// fichier au bon endroit, c'est reproduire exactement ce qu'on cherchait à
// éviter en faisant une fenêtre.
//
// On choisit un dossier PERSONNEL en premier : écrire dans /usr/local/bin
// demande les droits d'administration, qu'une fenêtre n'a pas à réclamer.
func installerCommandeTerminal() (string, error) {
	maison, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	moi, err := os.Executable()
	if err != nil {
		return "", err
	}
	// C'est le binaire de la FENÊTRE qui tourne ; la commande en ligne est un
	// autre programme. On pose donc un petit lanceur qui appelle la fenêtre en
	// mode terminal plutôt que de prétendre copier un fichier qu'on n'a pas.
	dossier := filepath.Join(maison, ".local", "bin")
	if err := os.MkdirAll(dossier, 0o755); err != nil {
		return "", err
	}
	chemin := filepath.Join(dossier, "poste")
	script := "#!/bin/sh\n# Lanceur posé par l'application Poste.\nexec " + strconv.Quote(moi) + " \"$@\"\n"
	if err := os.WriteFile(chemin, []byte(script), 0o755); err != nil {
		return "", err
	}
	return chemin, nil
}

// enLigneDeCommande : le même travail, sans fenêtre.
func enLigneDeCommande(args []string) {
	echec := func(err error) {
		if err != nil {
			fmt.Fprintf(os.Stderr, "✗ %v\n", err)
			os.Exit(1)
		}
	}
	switch args[0] {
	case "connexion":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage : poste connexion <invitation>")
			os.Exit(2)
		}
		serveur, code, err := posteclient.LireInvitation(strings.Join(args[1:], " "))
		echec(err)
		c, err := posteclient.Connecter(serveur, code)
		echec(err)
		fmt.Printf("✓ %s connectée au profil « %s ».\n", c.Machine, c.Profil)
	case "etat":
		e := lireEtat()
		if !e.Connectee {
			fmt.Println("Cette machine n'est pas encore connectée.")
			fmt.Println("  poste connexion <invitation>")
			return
		}
		fmt.Printf("Machine : %s (%s, %s)\nProfil  : %s\n", e.Machine, e.Systeme, e.Gestion, e.Profil)
		fmt.Printf("Logiciels demandés : %d   manquants : %d\n", e.Demandes, len(e.Manquants))
		for _, m := range e.Manquants {
			fmt.Printf("  + %s\n", m)
		}
		for _, d := range e.Dossiers {
			fmt.Printf("  dossier %-18s %-40s %s (%s)\n", d.Nom, d.Chemin, d.Sens, d.Etat)
		}
	case "fenetre":
		return // traité par main()
	default:
		fmt.Println("usage : poste {connexion <invitation> | etat}")
		fmt.Println("        (sans argument : ouvre la fenêtre)")
		os.Exit(2)
	}
}
