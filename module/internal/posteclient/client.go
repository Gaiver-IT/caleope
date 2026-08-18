// Package posteclient : tout ce qu'un poste sait faire, sans interface.
//
// La ligne de commande (cmd/poste) et la fenêtre graphique (cmd/poste-bureau)
// s'appuient toutes deux dessus. C'est délibéré : deux copies du même
// raisonnement divergent au premier correctif, et l'utilisateur se retrouve avec
// une interface qui dit « à jour » quand le terminal dit « 3 manquants ».
package posteclient

import (
	"bytes"
	"encoding/base64"
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

func CheminConfig() string {
	base, err := os.UserConfigDir()
	if err != nil {
		base, _ = os.UserHomeDir()
	}
	return filepath.Join(base, "caleope-poste", "config.json")
}

func LireConfig() (Config, error) {
	var c Config
	b, err := os.ReadFile(CheminConfig())
	if err != nil {
		return c, fmt.Errorf("cette machine n'est pas encore connectée — lance « poste connexion <url> <code> »")
	}
	return c, json.Unmarshal(b, &c)
}

func EcrireConfig(c Config) error {
	p := CheminConfig()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	// 0600 : ce fichier contient la clé de la machine. Personne d'autre que son
	// propriétaire n'a de raison de le lire.
	return os.WriteFile(p, b, 0o600)
}

// ── Dialogue avec le serveur ────────────────────────────────────────────────

func Appel(methode, url, cle string, corps interface{}) (map[string]interface{}, error) {
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

func Systeme() (string, string) {
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

func NomMachine() string {
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
func PaquetsInstalles(gest string) (map[string]bool, error) {
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
func Decoupe(ligne string) (nom, commande string, forceGestionnaire bool) {
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
func Manquants(p Profil, installes map[string]bool) []string {
	var out []string
	for _, ligne := range p.Paquets {
		if strings.TrimSpace(ligne) == "" || strings.HasPrefix(strings.TrimSpace(ligne), "#") {
			continue
		}
		nom, cmd, force := Decoupe(ligne)
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

func Installer(gest, paquet string) error {
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

// EtatDossier : ce qu'on a constaté pour un dossier du profil.
type EtatDossier struct {
	Nom    string `json:"nom"`
	Chemin string `json:"chemin"` // chemin RÉSOLU, « ~ » remplacé
	Sens   string `json:"sens"`
	Etat   string `json:"etat"` // existe | créé | erreur
	Detail string `json:"detail,omitempty"`
}

// PreparerDossiers crée les dossiers du profil et REND ce qu'elle a constaté.
//
// Elle n'écrit pas à l'écran : c'est l'appelant qui décide de l'affichage — des
// lignes dans un terminal, des cartes dans une fenêtre. Une fonction qui imprime
// elle-même ne sert jamais qu'une seule interface.
//
// Le CONTENU des dossiers n'est pas son affaire : elle garantit que le point de
// départ existe ; la synchronisation est le travail de Syncthing.
func PreparerDossiers(p Profil) []EtatDossier {
	out := make([]EtatDossier, 0, len(p.Dossiers))
	maison, _ := os.UserHomeDir()
	for _, d := range p.Dossiers {
		chemin := d.Chemin
		if strings.HasPrefix(chemin, "~") {
			chemin = filepath.Join(maison, strings.TrimPrefix(chemin, "~"))
		}
		e := EtatDossier{Nom: d.Nom, Chemin: chemin, Sens: d.Sens, Etat: "existe"}
		if _, err := os.Stat(chemin); os.IsNotExist(err) {
			if err := os.MkdirAll(chemin, 0o755); err != nil {
				e.Etat, e.Detail = "erreur", err.Error()
			} else {
				e.Etat = "créé"
			}
		}
		out = append(out, e)
	}
	return out
}

func TirerProfil(c Config) (Profil, error) {
	data, err := Appel("GET", strings.TrimRight(c.Serveur, "/")+"/api/v1/postes/ma-conf", c.Cle, nil)
	if err != nil {
		return Profil{}, err
	}
	brut, _ := json.Marshal(data["profil"])
	var p Profil
	return p, json.Unmarshal(brut, &p)
}

func Rapporter(c Config, n int) {
	// Le rapport est un confort d'affichage, pas une étape critique : s'il
	// échoue, la machine a quand même fait son travail. On ne bloque pas dessus.
	_, _ = Appel("POST", strings.TrimRight(c.Serveur, "/")+"/api/v1/postes/rapport", c.Cle,
		map[string]int{"manquants": n})
}

// Connecter appaire cette machine et retient sa clé.
//
// Factorisé ici parce que la ligne de commande ET la fenêtre le font : c'est le
// seul geste où l'utilisateur tape quelque chose, il n'a pas à se comporter
// différemment selon l'interface par laquelle il passe.
func Connecter(serveur, code string) (Config, error) {
	url := strings.TrimRight(strings.TrimSpace(serveur), "/")
	code = strings.TrimSpace(code)
	if url == "" || code == "" {
		return Config{}, fmt.Errorf("l'adresse du serveur et le code sont tous les deux nécessaires")
	}
	sys, _ := Systeme()
	data, err := Appel("POST", url+"/api/v1/postes/appairage", "", map[string]string{
		"jeton": code, "machine": NomMachine(), "systeme": sys,
	})
	if err != nil {
		return Config{}, err
	}
	c := Config{
		Serveur: url,
		Cle:     fmt.Sprint(data["cle"]),
		Machine: fmt.Sprint(data["machine"]),
		Profil:  fmt.Sprint(data["profil"]),
	}
	return c, EcrireConfig(c)
}

// ── Invitation : une seule chose à copier ───────────────────────────────────
//
// POURQUOI : la première version demandait de recopier l'adresse du serveur ET
// le code, dans deux champs. Deux saisies à la main, dont une URL — c'est pénible
// et c'est là qu'on se trompe. L'invitation encode les deux en une chaîne que
// l'interface fabrique (elle SAIT par quelle adresse on l'atteint) et que le
// poste décode. L'utilisateur copie une fois, colle une fois.
//
// Aucun secret n'est protégé par l'encodage : ce n'est pas du chiffrement, juste
// un emballage. Le code reste à usage unique et périmé en deux heures — c'est
// LUI qui tient la porte, pas l'obscurité de la chaîne.

const prefixeInvitation = "CALEOPE1:"

// FabriquerInvitation assemble adresse + code.
func FabriquerInvitation(serveur, code string) string {
	brut := strings.TrimRight(strings.TrimSpace(serveur), "/") + "|" + strings.TrimSpace(code)
	return prefixeInvitation + base64.RawURLEncoding.EncodeToString([]byte(brut))
}

// LireInvitation accepte l'invitation, mais AUSSI les formes qu'un humain
// produit naturellement : l'adresse et le code séparés par un espace, ou collés
// depuis deux champs. Refuser ces variantes pour des raisons de pureté ferait
// échouer l'appairage sur un détail de presse-papiers.
func LireInvitation(texte string) (serveur, code string, err error) {
	t := strings.TrimSpace(texte)
	if t == "" {
		return "", "", fmt.Errorf("invitation vide")
	}
	if strings.HasPrefix(t, prefixeInvitation) {
		b, e := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(t, prefixeInvitation))
		if e != nil {
			return "", "", fmt.Errorf("invitation illisible — recopie-la en entier")
		}
		parts := strings.SplitN(string(b), "|", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return "", "", fmt.Errorf("invitation incomplète")
		}
		return parts[0], parts[1], nil
	}
	// Repli : « https://serveur CODE », séparés par un espace ou un saut de ligne.
	champs := strings.Fields(t)
	if len(champs) == 2 {
		return strings.TrimRight(champs[0], "/"), champs[1], nil
	}
	return "", "", fmt.Errorf("format non reconnu : colle l'invitation affichée dans Caleope")
}
