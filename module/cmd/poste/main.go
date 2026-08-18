// poste — l'exécutable qu'on installe sur SA machine.
//
// Le parcours voulu, et rien de plus :
//
//	poste connexion https://caleope.exemple.fr CODE   une seule fois, par machine
//	poste etat                                        ce qui manque ici
//	poste appliquer                                   essai à blanc
//	poste appliquer --pour-de-vrai                    on y va
//
// # POURQUOI UN BINAIRE ET PAS UN SCRIPT
//
// Windows. Un script shell couvre macOS et Linux ; il ne couvre pas la troisième
// machine, et c'est justement celle où l'on veut le moins bricoler. Un binaire
// unique, compilé pour les trois systèmes, retire aussi la question des
// dépendances : rien à installer avant d'installer.
//
// # DEUX RÈGLES QUI NE SE DISCUTENT PAS
//
//  1. On n'AJOUTE que. Jamais de désinstallation. Une machine qui perd un
//     logiciel parce qu'une autre ne l'avait pas est une machine cassée à
//     distance.
//  2. Rien ne s'installe sans que la commande le dise. L'essai à blanc est le
//     comportement PAR DÉFAUT ; il faut écrire --pour-de-vrai pour agir.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

// Config : ce que la machine retient entre deux commandes.
type Config struct {
	Serveur string `json:"serveur"`
	Cle     string `json:"cle"`
	Machine string `json:"machine"`
	Profil  string `json:"profil"`
}

type Dossier struct {
	Nom    string `json:"nom"`
	Chemin string `json:"chemin"`
	Sens   string `json:"sens"`
}

type Profil struct {
	Nom      string    `json:"nom"`
	Paquets  []string  `json:"paquets"`
	Dossiers []Dossier `json:"dossiers"`
}

func cheminConfig() string {
	base, err := os.UserConfigDir()
	if err != nil {
		base, _ = os.UserHomeDir()
	}
	return filepath.Join(base, "caleope-poste", "config.json")
}

func lireConfig() (Config, error) {
	var c Config
	b, err := os.ReadFile(cheminConfig())
	if err != nil {
		return c, fmt.Errorf("cette machine n'est pas encore connectée — lance « poste connexion <url> <code> »")
	}
	return c, json.Unmarshal(b, &c)
}

func ecrireConfig(c Config) error {
	p := cheminConfig()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	// 0600 : ce fichier contient la clé de la machine. Personne d'autre que son
	// propriétaire n'a de raison de le lire.
	return os.WriteFile(p, b, 0o600)
}

// ── Dialogue avec le serveur ────────────────────────────────────────────────

func appel(methode, url, cle string, corps interface{}) (map[string]interface{}, error) {
	var lecteur io.Reader
	if corps != nil {
		b, _ := json.Marshal(corps)
		lecteur = bytes.NewReader(b)
	}
	req, err := http.NewRequest(methode, url, lecteur)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if cle != "" {
		req.Header.Set("Authorization", "Machine "+cle)
	}
	cl := &http.Client{Timeout: 30 * time.Second}
	rep, err := cl.Do(req)
	if err != nil {
		return nil, fmt.Errorf("serveur injoignable : %w", err)
	}
	defer rep.Body.Close()
	var res struct {
		Success bool                   `json:"success"`
		Error   string                 `json:"error"`
		Data    map[string]interface{} `json:"data"`
	}
	if err := json.NewDecoder(rep.Body).Decode(&res); err != nil {
		return nil, fmt.Errorf("réponse illisible du serveur (code %d)", rep.StatusCode)
	}
	if !res.Success {
		return nil, fmt.Errorf("%s", res.Error)
	}
	return res.Data, nil
}

// ── Ce que la machine sait d'elle-même ──────────────────────────────────────

func systeme() (string, string) {
	switch runtime.GOOS {
	case "darwin":
		return "macos", "brew"
	case "windows":
		return "windows", "winget"
	case "linux":
		for _, g := range []struct{ bin, nom string }{{"apt-get", "apt"}, {"dnf", "dnf"}, {"pacman", "pacman"}} {
			if _, err := exec.LookPath(g.bin); err == nil {
				return "linux", g.nom
			}
		}
		return "linux", ""
	}
	return runtime.GOOS, ""
}

func nomMachine() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "machine-sans-nom"
	}
	if i := strings.Index(h, "."); i > 0 {
		h = h[:i]
	}
	return h
}

// paquetsInstalles interroge le gestionnaire UNE fois.
func paquetsInstalles(gest string) (map[string]bool, error) {
	var sortie []byte
	var err error
	switch gest {
	case "brew":
		f, _ := exec.Command("brew", "list", "--formula").Output()
		c, _ := exec.Command("brew", "list", "--cask").Output()
		sortie = append(f, c...)
	case "apt":
		sortie, err = exec.Command("dpkg-query", "-W", "-f=${Package}\n").Output()
	case "dnf":
		sortie, err = exec.Command("rpm", "-qa", "--qf", "%{NAME}\n").Output()
	case "pacman":
		sortie, err = exec.Command("pacman", "-Qq").Output()
	case "winget":
		sortie, err = exec.Command("winget", "list", "--disable-interactivity").Output()
	default:
		return nil, fmt.Errorf("gestionnaire de paquets non reconnu sur ce système")
	}
	if err != nil && len(sortie) == 0 {
		return nil, err
	}
	m := map[string]bool{}
	for _, l := range strings.Split(string(sortie), "\n") {
		champ := strings.TrimSpace(l)
		if champ == "" {
			continue
		}
		// winget rend un tableau : on garde le premier mot, faute de mieux.
		if gest == "winget" {
			champ = strings.Fields(champ)[0]
		}
		m[champ] = true
	}
	return m, nil
}

// decoupe lit une ligne de paquet : « nom », « nom=commande », « !nom ».
func decoupe(ligne string) (nom, commande string, forceGestionnaire bool) {
	l := strings.TrimSpace(ligne)
	if strings.HasPrefix(l, "!") {
		forceGestionnaire = true
		l = l[1:]
	}
	if i := strings.Index(l, "="); i > 0 {
		return l[:i], l[i+1:], forceGestionnaire
	}
	return l, l, forceGestionnaire
}

// manquants applique la règle de présence qui évite les faux positifs.
//
// Mesuré sur un vrai Mac : git, curl et jq sont fournis par le système et
// absents de la liste Homebrew. Comparer les seuls inventaires aurait fait
// installer trois doublons Homebrew par-dessus des outils déjà là, en changeant
// la version présente dans le PATH. Un paquet est donc présent si le
// gestionnaire le connaît OU si sa commande répond.
func manquants(p Profil, installes map[string]bool) []string {
	var out []string
	for _, ligne := range p.Paquets {
		if strings.TrimSpace(ligne) == "" || strings.HasPrefix(strings.TrimSpace(ligne), "#") {
			continue
		}
		nom, cmd, force := decoupe(ligne)
		if installes[nom] {
			continue
		}
		if !force {
			if _, err := exec.LookPath(cmd); err == nil {
				continue
			}
		}
		out = append(out, nom)
	}
	sort.Strings(out)
	return out
}

func installer(gest, paquet string) error {
	var c *exec.Cmd
	elevation := os.Geteuid() != 0 && runtime.GOOS != "windows"
	avecSudo := func(args ...string) *exec.Cmd {
		if elevation {
			if _, err := exec.LookPath("sudo"); err == nil {
				return exec.Command("sudo", args...)
			}
		}
		return exec.Command(args[0], args[1:]...)
	}
	switch gest {
	case "brew":
		c = exec.Command("brew", "install", paquet) // jamais en root
	case "apt":
		c = avecSudo("apt-get", "install", "-y", paquet)
	case "dnf":
		c = avecSudo("dnf", "install", "-y", paquet)
	case "pacman":
		c = avecSudo("pacman", "-S", "--noconfirm", paquet)
	case "winget":
		c = exec.Command("winget", "install", "--accept-package-agreements",
			"--accept-source-agreements", "--disable-interactivity", "-e", "--id", paquet)
	default:
		return fmt.Errorf("gestionnaire inconnu")
	}
	c.Stdout, c.Stderr = os.Stdout, os.Stderr
	return c.Run()
}

// preparerDossiers crée les dossiers du profil. Le CONTENU est l'affaire de
// Syncthing ; ici on garantit seulement que le point de montage existe et on
// dit clairement dans quel sens chacun est censé circuler.
func preparerDossiers(p Profil) {
	if len(p.Dossiers) == 0 {
		return
	}
	maison, _ := os.UserHomeDir()
	fmt.Println("\nDossiers du profil :")
	for _, d := range p.Dossiers {
		chemin := d.Chemin
		if strings.HasPrefix(chemin, "~") {
			chemin = filepath.Join(maison, strings.TrimPrefix(chemin, "~"))
		}
		etat := "existe"
		if _, err := os.Stat(chemin); os.IsNotExist(err) {
			if err := os.MkdirAll(chemin, 0o755); err != nil {
				etat = "création impossible : " + err.Error()
			} else {
				etat = "créé"
			}
		}
		fmt.Printf("  %-22s %-40s %s (%s)\n", d.Nom, chemin, d.Sens, etat)
	}
}

func tirerProfil(c Config) (Profil, error) {
	data, err := appel("GET", strings.TrimRight(c.Serveur, "/")+"/api/v1/postes/ma-conf", c.Cle, nil)
	if err != nil {
		return Profil{}, err
	}
	brut, _ := json.Marshal(data["profil"])
	var p Profil
	return p, json.Unmarshal(brut, &p)
}

func rapporter(c Config, n int) {
	// Le rapport est un confort d'affichage, pas une étape critique : s'il
	// échoue, la machine a quand même fait son travail. On ne bloque pas dessus.
	_, _ = appel("POST", strings.TrimRight(c.Serveur, "/")+"/api/v1/postes/rapport", c.Cle,
		map[string]int{"manquants": n})
}

// ── Commandes ───────────────────────────────────────────────────────────────

func cmdConnexion(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage : poste connexion <url-du-serveur> <code>")
	}
	url := strings.TrimRight(args[0], "/")
	sys, _ := systeme()
	data, err := appel("POST", url+"/api/v1/postes/appairage", "", map[string]string{
		"jeton":   args[1],
		"machine": nomMachine(),
		"systeme": sys,
	})
	if err != nil {
		return err
	}
	c := Config{
		Serveur: url,
		Cle:     fmt.Sprint(data["cle"]),
		Machine: fmt.Sprint(data["machine"]),
		Profil:  fmt.Sprint(data["profil"]),
	}
	if err := ecrireConfig(c); err != nil {
		return err
	}
	fmt.Printf("✓ %s connectée au profil « %s ».\n", c.Machine, c.Profil)
	fmt.Println("  Le code d'appairage vient d'être consommé : il ne resservira pas.")
	fmt.Println("  Ensuite :  poste etat")
	return nil
}

func cmdEtat() error {
	c, err := lireConfig()
	if err != nil {
		return err
	}
	p, err := tirerProfil(c)
	if err != nil {
		return err
	}
	sys, gest := systeme()
	fmt.Printf("Machine : %s (%s, %s)\nProfil  : %s\n", c.Machine, sys, gest, p.Nom)

	inst, err := paquetsInstalles(gest)
	if err != nil {
		return err
	}
	m := manquants(p, inst)
	fmt.Printf("Logiciels demandés : %d   manquants ici : %d\n", len(p.Paquets), len(m))
	for _, x := range m {
		fmt.Printf("  + %s\n", x)
	}
	preparerDossiers(p)
	rapporter(c, len(m))
	return nil
}

func cmdAppliquer(pourDeVrai bool) error {
	c, err := lireConfig()
	if err != nil {
		return err
	}
	p, err := tirerProfil(c)
	if err != nil {
		return err
	}
	_, gest := systeme()
	inst, err := paquetsInstalles(gest)
	if err != nil {
		return err
	}
	m := manquants(p, inst)
	preparerDossiers(p)

	if len(m) == 0 {
		fmt.Println("\nRien à installer : cette machine a déjà tout ce que son profil demande.")
		rapporter(c, 0)
		return nil
	}
	fmt.Printf("\n%d logiciel(s) manquant(s) :\n", len(m))
	for _, x := range m {
		fmt.Printf("  + %s\n", x)
	}
	if !pourDeVrai {
		fmt.Println("\n→ Essai à blanc. Rien n'a été installé.")
		fmt.Println("  Pour agir :  poste appliquer --pour-de-vrai")
		rapporter(c, len(m))
		return nil
	}
	echecs := 0
	for _, x := range m {
		fmt.Printf("\n→ installation de %s…\n", x)
		if err := installer(gest, x); err != nil {
			// Un nom peut ne pas exister sur cette distribution : on continue,
			// sinon un seul paquet exotique bloque tous les autres.
			fmt.Fprintf(os.Stderr, "✗ échec sur %s : %v — on continue\n", x, err)
			echecs++
		}
	}
	// On RE-MESURE au lieu d'annoncer : un rattrapage qui ne se relit pas
	// transforme une petite panne en longue panne.
	inst2, _ := paquetsInstalles(gest)
	reste := manquants(p, inst2)
	fmt.Printf("\nTerminé : %d échec(s), %d encore manquant(s).\n", echecs, len(reste))
	rapporter(c, len(reste))
	return nil
}

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		args = []string{"etat"}
	}
	var err error
	switch args[0] {
	case "connexion":
		err = cmdConnexion(args[1:])
	case "etat":
		err = cmdEtat()
	case "appliquer":
		err = cmdAppliquer(len(args) > 1 && args[1] == "--pour-de-vrai")
	case "version":
		fmt.Println("poste (Caleope) — client de poste nomade")
	default:
		fmt.Println("usage : poste {connexion <url> <code> | etat | appliquer [--pour-de-vrai]}")
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "✗ %v\n", err)
		os.Exit(1)
	}
}
