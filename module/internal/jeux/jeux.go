// Package jeux : piloter un serveur de jeu depuis Caleope.
//
// # CE QU'ON REMPLACE
//
// Pterodactyl, c'est un panneau, un démon « wings », deux bases de données et
// un modèle d'« œufs » à maintenir — pour, au fond, quatre gestes : voir la
// console, changer un réglage, ajouter un mod, sauvegarder. Ce paquet fait ces
// quatre gestes en s'appuyant sur ce que Caleope sait déjà faire (conteneurs,
// volumes, sauvegardes) plutôt qu'en réinventant un orchestrateur.
//
// # LES SOURCES RESTENT CHEZ LEURS AUTEURS
//
// Contrainte posée par l'utilisateur : Caleope ne stocke NI les binaires de
// jeu NI les mods. Ajouter un mod ici, c'est donc écrire son identifiant dans
// une liste ; c'est l'image du serveur qui le télécharge depuis Modrinth au
// démarrage suivant. Le disque de Caleope ne porte que le monde.
package jeux

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gaiver-it/caleope/internal/instances"
)

// FichierMods : la liste des mods voulus, dans la CONFIGURATION de l'app.
//
// Pas dans les données du jeu, et pas dans secrets.env : setup.sh réécrit
// secrets.env à chaque montée de version, ce qui effacerait les mods ajoutés
// depuis l'interface. Ici, le fichier survit — et setup.sh le relit.
const FichierMods = "mods.txt"

// Gestionnaire : accès aux serveurs de jeu installés.
type Gestionnaire struct {
	baseDir string
	// clientHTTP est isolé pour que les appels sortants (Modrinth) aient un
	// délai borné : une recherche qui pend bloquerait l'interface.
	clientHTTP *http.Client
}

func Nouveau(baseDir string) *Gestionnaire {
	return &Gestionnaire{
		baseDir:    baseDir,
		clientHTTP: &http.Client{Timeout: 20 * time.Second},
	}
}

// NomConteneur : « minecraft@homestead » → « minecraft-homestead ».
// Docker refuse le « @ » ; c'est la même transformation que celle du gabarit de
// compose, et elle doit le rester — sinon on parle au mauvais conteneur.
func NomConteneur(appID string) string {
	return strings.ReplaceAll(appID, instances.Separateur, "-")
}

func (g *Gestionnaire) dossierDonnees(appID string) string {
	return filepath.Join(g.baseDir, "app-data", appID, "data")
}

func (g *Gestionnaire) dossierConfig(appID string) string {
	return filepath.Join(g.baseDir, "app-config", appID)
}

// ─────────────────────────────── CONSOLE ───────────────────────────────

// Console envoie une commande au serveur et rend sa réponse.
//
// On passe par RCON, le canal d'administration prévu par le jeu — pas par
// l'entrée standard du conteneur. Écrire sur stdin marche aussi, mais ne rend
// AUCUNE réponse : l'utilisateur taperait ses commandes dans le vide.
func (g *Gestionnaire) Console(appID, commande string) (string, error) {
	commande = strings.TrimSpace(commande)
	if commande == "" {
		return "", fmt.Errorf("commande vide")
	}
	// Une commande arrive d'une interface web : on ne la passe JAMAIS par un
	// shell. exec.Command sans shell rend « ; rm -rf / » inoffensif — ce n'est
	// qu'un argument de plus pour rcon-cli.
	cmd := exec.Command("docker", "exec", NomConteneur(appID), "rcon-cli", commande)
	sortie, err := cmd.CombinedOutput()
	texte := strings.TrimSpace(string(sortie))
	if err != nil {
		if texte == "" {
			return "", fmt.Errorf("le serveur ne répond pas : %w", err)
		}
		return texte, fmt.Errorf("%s", texte)
	}
	return texte, nil
}

// ───────────────────────────── PROPRIÉTÉS ─────────────────────────────

// Proprietes lit server.properties.
func (g *Gestionnaire) Proprietes(appID string) (map[string]string, error) {
	f, err := os.Open(filepath.Join(g.dossierDonnees(appID), "server.properties"))
	if err != nil {
		return nil, fmt.Errorf("server.properties illisible — le serveur a-t-il déjà démarré une fois ?")
	}
	defer f.Close()

	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		if i := strings.Index(l, "="); i > 0 {
			out[l[:i]] = l[i+1:]
		}
	}
	return out, sc.Err()
}

// EcrireProprietes applique des changements SANS réécrire le fichier entier.
//
// Réécrire à partir de la carte perdrait les commentaires, l'ordre, et surtout
// les clés qu'on n'a pas lues — un fichier de propriétés évolue avec les
// versions du jeu, et une réécriture naïve efface ce qu'on ne connaît pas.
func (g *Gestionnaire) EcrireProprietes(appID string, changements map[string]string) error {
	chemin := filepath.Join(g.dossierDonnees(appID), "server.properties")
	brut, err := os.ReadFile(chemin)
	if err != nil {
		return fmt.Errorf("server.properties illisible")
	}
	restants := map[string]string{}
	for k, v := range changements {
		if err := ValiderProprieteAppliquee(k, v); err != nil {
			return err
		}
		restants[k] = v
	}

	lignes := strings.Split(string(brut), "\n")
	for i, l := range lignes {
		t := strings.TrimSpace(l)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		j := strings.Index(t, "=")
		if j <= 0 {
			continue
		}
		cle := t[:j]
		if v, ok := restants[cle]; ok {
			lignes[i] = cle + "=" + v
			delete(restants, cle)
		}
	}
	// Les clés inconnues du fichier sont ajoutées à la fin plutôt qu'ignorées :
	// une propriété absente est souvent une propriété par défaut, et l'utilisateur
	// qui la fixe attend qu'elle prenne effet.
	cles := make([]string, 0, len(restants))
	for k := range restants {
		cles = append(cles, k)
	}
	sort.Strings(cles)
	for _, k := range cles {
		lignes = append(lignes, k+"="+restants[k])
	}
	return os.WriteFile(chemin, []byte(strings.Join(lignes, "\n")), 0o644)
}

// ValiderProprieteAppliquee refuse ce qui casserait le fichier.
//
// Une valeur contenant un saut de ligne écrirait une propriété supplémentaire —
// c'est le « injection » du monde des fichiers de configuration.
func ValiderProprieteAppliquee(cle, valeur string) error {
	if strings.TrimSpace(cle) == "" {
		return fmt.Errorf("nom de propriété vide")
	}
	if strings.ContainsAny(cle, "=\n\r") {
		return fmt.Errorf("nom de propriété invalide : %q", cle)
	}
	if strings.ContainsAny(valeur, "\n\r") {
		return fmt.Errorf("la valeur de « %s » ne peut pas contenir de saut de ligne", cle)
	}
	return nil
}

// ─────────────────────────────── MODS ───────────────────────────────

// Mods rend la liste des mods demandés pour cette instance.
func (g *Gestionnaire) Mods(appID string) ([]string, error) {
	brut, err := os.ReadFile(filepath.Join(g.dossierConfig(appID), FichierMods))
	if os.IsNotExist(err) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	return LireListeMods(string(brut)), nil
}

// LireListeMods : une entrée par ligne, commentaires et vides ignorés.
func LireListeMods(contenu string) []string {
	var out []string
	for _, l := range strings.Split(contenu, "\n") {
		t := strings.TrimSpace(l)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		out = append(out, t)
	}
	return out
}

// AjouterMod ajoute un projet à la liste. Sans doublon : demander deux fois le
// même mod ferait échouer le démarrage du serveur.
func (g *Gestionnaire) AjouterMod(appID, projet string) error {
	if err := ValiderNomProjet(projet); err != nil {
		return err
	}
	liste, err := g.Mods(appID)
	if err != nil {
		return err
	}
	for _, m := range liste {
		if strings.EqualFold(m, projet) {
			return fmt.Errorf("« %s » est déjà dans la liste", projet)
		}
	}
	return g.ecrireMods(appID, append(liste, projet))
}

// RetirerMod enlève un projet de la liste.
func (g *Gestionnaire) RetirerMod(appID, projet string) error {
	liste, err := g.Mods(appID)
	if err != nil {
		return err
	}
	var reste []string
	trouve := false
	for _, m := range liste {
		if strings.EqualFold(m, projet) {
			trouve = true
			continue
		}
		reste = append(reste, m)
	}
	if !trouve {
		return fmt.Errorf("« %s » n'est pas dans la liste", projet)
	}
	return g.ecrireMods(appID, reste)
}

func (g *Gestionnaire) ecrireMods(appID string, liste []string) error {
	dossier := g.dossierConfig(appID)
	if err := os.MkdirAll(dossier, 0o755); err != nil {
		return err
	}
	var sb strings.Builder
	sb.WriteString("# Mods et plugins demandés pour ce serveur, un par ligne.\n")
	sb.WriteString("# Ils sont téléchargés depuis Modrinth AU DÉMARRAGE : Caleope n'en\n")
	sb.WriteString("# stocke aucun. Retirer une ligne retire le mod au redémarrage suivant.\n")
	for _, m := range liste {
		sb.WriteString(m + "\n")
	}
	return os.WriteFile(filepath.Join(dossier, FichierMods), []byte(sb.String()), 0o644)
}

// ValiderNomProjet : un identifiant Modrinth, pas une adresse ni un chemin.
//
// Cette valeur finit dans une variable d'environnement lue par l'image du
// serveur : y laisser passer un espace ou une virgule ferait dérailler la liste
// entière, et un « / » ouvrirait la porte à autre chose qu'un projet.
func ValiderNomProjet(p string) error {
	p = strings.TrimSpace(p)
	if p == "" {
		return fmt.Errorf("identifiant de projet vide")
	}
	if len(p) > 64 {
		return fmt.Errorf("identifiant de projet trop long")
	}
	for _, r := range p {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-', r == '_':
		default:
			return fmt.Errorf("identifiant de projet invalide : « %s » — lettres, chiffres, tirets et soulignés seulement", p)
		}
	}
	return nil
}

// ───────────────────────────── MODRINTH ─────────────────────────────

// ResultatModrinth : ce que l'interface affiche pour un projet trouvé.
type ResultatModrinth struct {
	Identifiant     string `json:"identifiant"`
	Titre           string `json:"titre"`
	Description     string `json:"description"`
	Telechargements int    `json:"telechargements"`
	Icone           string `json:"icone,omitempty"`
}

// ChercherModrinth interroge Modrinth. Aucune clé n'est nécessaire — c'est
// pour ça que ce dépôt est branché en premier ; CurseForge, lui, exige une clé
// personnelle que l'utilisateur doit demander.
func (g *Gestionnaire) ChercherModrinth(requete, moteur, versionJeu string) ([]ResultatModrinth, error) {
	requete = strings.TrimSpace(requete)
	if requete == "" {
		return nil, fmt.Errorf("recherche vide")
	}
	// Les facettes restreignent aux projets réellement compatibles : proposer un
	// mod Fabric à un serveur Paper, c'est promettre une installation qui
	// échouera au démarrage.
	facettes := [][]string{}
	if moteur != "" {
		facettes = append(facettes, []string{"categories:" + strings.ToLower(moteur)})
	}
	if versionJeu != "" && !strings.EqualFold(versionJeu, "LATEST") {
		facettes = append(facettes, []string{"versions:" + versionJeu})
	}
	f, _ := json.Marshal(facettes)

	u := "https://api.modrinth.com/v2/search?limit=20&query=" + url.QueryEscape(requete)
	if len(facettes) > 0 {
		u += "&facets=" + url.QueryEscape(string(f))
	}
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	// Modrinth demande qu'on s'identifie ; un client anonyme se fait limiter.
	req.Header.Set("User-Agent", "Gaiver-IT/Caleope (serveur auto-hébergé)")

	rep, err := g.clientHTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Modrinth injoignable : %w", err)
	}
	defer rep.Body.Close()
	if rep.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Modrinth a répondu %d", rep.StatusCode)
	}
	var corps struct {
		Hits []struct {
			Slug        string `json:"slug"`
			Title       string `json:"title"`
			Description string `json:"description"`
			Downloads   int    `json:"downloads"`
			IconURL     string `json:"icon_url"`
		} `json:"hits"`
	}
	if err := json.NewDecoder(rep.Body).Decode(&corps); err != nil {
		return nil, err
	}
	out := make([]ResultatModrinth, 0, len(corps.Hits))
	for _, h := range corps.Hits {
		out = append(out, ResultatModrinth{
			Identifiant: h.Slug, Titre: h.Title, Description: h.Description,
			Telechargements: h.Downloads, Icone: h.IconURL,
		})
	}
	return out, nil
}
