// Package postes gère les machines de l'utilisateur : ses profils de poste, et
// l'appairage d'un nouvel ordinateur.
//
// # CE QU'ON CHERCHE À OBTENIR
//
// « J'installe un exécutable, je me connecte, je choisis la conf, et tout
// redescend. » Tout le reste de ce fichier découle de cette phrase.
//
//   - Un PROFIL décrit ce qu'une machine doit avoir : des paquets, des dossiers.
//     Il est écrit une fois, dans l'interface, et sert à plusieurs machines.
//   - Un JETON D'APPAIRAGE est ce que l'utilisateur recopie dans l'exécutable.
//     Court, à usage unique, périmé vite : c'est la seule chose qu'il tape.
//   - Une CLÉ DE MACHINE, rendue à l'appairage, sert ensuite à tirer le profil.
//     La machine ne redemande jamais le jeton.
//
// # CE QUE CE PAQUET NE FAIT PAS
//
// Il ne pousse RIEN. C'est le poste qui vient chercher sa configuration quand il
// le décide. Un serveur qui pousserait des installations sur des portables
// éteints la moitié du temps passerait sa vie à réessayer, et surtout il faudrait
// lui ouvrir un chemin vers chaque machine — l'inverse de ce qu'on veut.
package postes

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// DureeJeton : un jeton d'appairage vit deux heures. Assez pour aller chercher
// l'autre machine et taper la commande, trop peu pour traîner dans un
// presse-papiers pendant des semaines.
const DureeJeton = 2 * time.Hour

// SensDossier : ce qu'on fait d'un dossier déclaré dans un profil.
const (
	// SensDescendre : le serveur fait autorité, le poste reçoit.
	SensDescendre = "descendre"
	// SensSauvegarder : le poste fait autorité, le serveur conserve une copie.
	SensSauvegarder = "sauvegarder"
	// SensDeuxSens : les deux côtés écrivent (fonds d'écran, notes…).
	SensDeuxSens = "deux-sens"
)

// Dossier : un dossier suivi sur le poste.
type Dossier struct {
	Nom    string `json:"nom"`
	Chemin string `json:"chemin"` // côté poste ; « ~ » est résolu par le client
	Sens   string `json:"sens"`
}

// Profil : ce qu'une machine doit avoir.
type Profil struct {
	Nom         string `json:"nom"`
	Description string `json:"description,omitempty"`
	// Paquets : une ligne par logiciel. Formats acceptés, identiques à ceux de
	// l'outil poste : « nom », « nom=commande », « !nom » pour exiger le
	// gestionnaire même si le système fournit déjà la commande.
	Paquets  []string  `json:"paquets"`
	Dossiers []Dossier `json:"dossiers"`
	Modifie  time.Time `json:"modifie"`
}

// Machine : un poste appairé.
type Machine struct {
	Nom       string    `json:"nom"`
	Systeme   string    `json:"systeme"`
	Profil    string    `json:"profil"`
	Cle       string    `json:"-"` // jamais rendue par l'API après l'appairage
	Empreinte string    `json:"empreinte"`
	Appairee  time.Time `json:"appairee"`
	DernierVu time.Time `json:"dernier_vu,omitempty"`
	Manquants int       `json:"manquants"`
}

// jeton d'appairage en attente.
type jeton struct {
	Valeur  string    `json:"valeur"`
	Profil  string    `json:"profil"`
	Expire  time.Time `json:"expire"`
	Utilise bool      `json:"utilise"`
}

type etat struct {
	Profils  map[string]Profil  `json:"profils"`
	Machines map[string]Machine `json:"machines"`
	Jetons   map[string]jeton   `json:"jetons"`
}

// Gestionnaire : la porte d'entrée du paquet.
type Gestionnaire struct {
	fichier string
	mu      sync.Mutex
	now     func() time.Time
}

func Nouveau(baseDir string) *Gestionnaire {
	return &Gestionnaire{
		fichier: filepath.Join(baseDir, "runtime", "postes.json"),
		now:     time.Now,
	}
}

func (g *Gestionnaire) lire() etat {
	e := etat{
		Profils:  map[string]Profil{},
		Machines: map[string]Machine{},
		Jetons:   map[string]jeton{},
	}
	b, err := os.ReadFile(g.fichier)
	if err != nil {
		return e
	}
	_ = json.Unmarshal(b, &e)
	if e.Profils == nil {
		e.Profils = map[string]Profil{}
	}
	if e.Machines == nil {
		e.Machines = map[string]Machine{}
	}
	if e.Jetons == nil {
		e.Jetons = map[string]jeton{}
	}
	return e
}

func (g *Gestionnaire) ecrire(e etat) error {
	if err := os.MkdirAll(filepath.Dir(g.fichier), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return err
	}
	// Écriture par fichier temporaire puis renommage : une coupure au mauvais
	// moment laisserait sinon un fichier tronqué, donc tous les profils perdus.
	tmp := g.fichier + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, g.fichier)
}

// ─────────────────────────────── PROFILS ───────────────────────────────

func (g *Gestionnaire) ListerProfils() []Profil {
	g.mu.Lock()
	defer g.mu.Unlock()
	e := g.lire()
	out := make([]Profil, 0, len(e.Profils))
	for _, p := range e.Profils {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Nom < out[j].Nom })
	return out
}

func (g *Gestionnaire) EnregistrerProfil(p Profil) error {
	if err := ValiderProfil(&p); err != nil {
		return err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	e := g.lire()
	p.Modifie = g.now()
	e.Profils[p.Nom] = p
	return g.ecrire(e)
}

func (g *Gestionnaire) SupprimerProfil(nom string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	e := g.lire()
	if _, ok := e.Profils[nom]; !ok {
		return fmt.Errorf("profil '%s' inconnu", nom)
	}
	// Un profil encore utilisé ne disparaît pas en silence : les machines qui
	// s'en servent tireraient une configuration vide au prochain passage, et
	// « convergence automatique » deviendrait « plus rien à installer ».
	for _, m := range e.Machines {
		if m.Profil == nom {
			return fmt.Errorf("profil '%s' encore utilisé par la machine '%s'", nom, m.Nom)
		}
	}
	delete(e.Profils, nom)
	return g.ecrire(e)
}

// ValiderProfil refuse ce qui rendrait un poste incohérent.
func ValiderProfil(p *Profil) error {
	p.Nom = strings.TrimSpace(p.Nom)
	if p.Nom == "" {
		return fmt.Errorf("le profil doit avoir un nom")
	}
	// Le nom sert de clé et voyage dans une URL : on le tient propre.
	if strings.ContainsAny(p.Nom, "/\\ ?&#") {
		return fmt.Errorf("nom de profil invalide : '%s'", p.Nom)
	}
	for i, d := range p.Dossiers {
		if strings.TrimSpace(d.Chemin) == "" {
			return fmt.Errorf("dossier #%d : chemin vide", i+1)
		}
		switch d.Sens {
		case SensDescendre, SensSauvegarder, SensDeuxSens:
		case "":
			p.Dossiers[i].Sens = SensDeuxSens
		default:
			return fmt.Errorf("dossier '%s' : sens inconnu '%s'", d.Nom, d.Sens)
		}
	}
	return nil
}

// ─────────────────────────────── APPAIRAGE ───────────────────────────────

func aleatoire(octets int) string {
	b := make([]byte, octets)
	if _, err := rand.Read(b); err != nil {
		// Sans hasard sûr, on n'invente pas un jeton faible : mieux vaut échouer.
		return ""
	}
	return hex.EncodeToString(b)
}

// CreerJeton rend le code court que l'utilisateur recopie dans l'exécutable.
func (g *Gestionnaire) CreerJeton(profil string) (string, time.Time, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	e := g.lire()
	if _, ok := e.Profils[profil]; !ok {
		return "", time.Time{}, fmt.Errorf("profil '%s' inconnu", profil)
	}
	v := aleatoire(9) // 18 caractères : recopiable à la main, impossible à deviner
	if v == "" {
		return "", time.Time{}, fmt.Errorf("source d'aléa indisponible")
	}
	exp := g.now().Add(DureeJeton)
	e.Jetons[v] = jeton{Valeur: v, Profil: profil, Expire: exp}
	g.purgerJetons(&e)
	return v, exp, g.ecrire(e)
}

// purgerJetons retire les jetons périmés ou consommés. Sans ça le fichier
// grossit indéfiniment et garde des secrets dont plus personne n'a besoin.
func (g *Gestionnaire) purgerJetons(e *etat) {
	for k, j := range e.Jetons {
		if j.Utilise || g.now().After(j.Expire) {
			delete(e.Jetons, k)
		}
	}
}

// Appairer consomme un jeton et rend la clé durable de la machine.
func (g *Gestionnaire) Appairer(valeurJeton, nomMachine, systeme string) (Machine, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	e := g.lire()

	j, ok := e.Jetons[valeurJeton]
	if !ok {
		return Machine{}, fmt.Errorf("jeton inconnu")
	}
	if j.Utilise {
		return Machine{}, fmt.Errorf("jeton déjà utilisé")
	}
	if g.now().After(j.Expire) {
		return Machine{}, fmt.Errorf("jeton périmé")
	}
	if strings.TrimSpace(nomMachine) == "" {
		return Machine{}, fmt.Errorf("la machine doit donner son nom")
	}

	cle := aleatoire(32)
	if cle == "" {
		return Machine{}, fmt.Errorf("source d'aléa indisponible")
	}
	m := Machine{
		Nom:       nomMachine,
		Systeme:   systeme,
		Profil:    j.Profil,
		Cle:       cle,
		Empreinte: cle[:8], // de quoi reconnaître la machine sans exposer sa clé
		Appairee:  g.now(),
	}
	e.Machines[cle] = m
	// Le jeton est marqué utilisé PUIS purgé : un appairage rejoué avec le même
	// code créerait une deuxième machine avec les mêmes droits.
	j.Utilise = true
	e.Jetons[valeurJeton] = j
	g.purgerJetons(&e)
	return m, g.ecrire(e)
}

// ProfilDeLaMachine rend la configuration à appliquer, à partir de la clé.
func (g *Gestionnaire) ProfilDeLaMachine(cle string) (Machine, Profil, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	e := g.lire()
	m, ok := e.Machines[cle]
	if !ok {
		return Machine{}, Profil{}, fmt.Errorf("machine inconnue")
	}
	p, ok := e.Profils[m.Profil]
	if !ok {
		return m, Profil{}, fmt.Errorf("profil '%s' introuvable", m.Profil)
	}
	return m, p, nil
}

// Rapporter enregistre ce que la machine a constaté chez elle.
func (g *Gestionnaire) Rapporter(cle string, manquants int) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	e := g.lire()
	m, ok := e.Machines[cle]
	if !ok {
		return fmt.Errorf("machine inconnue")
	}
	m.DernierVu = g.now()
	m.Manquants = manquants
	e.Machines[cle] = m
	return g.ecrire(e)
}

func (g *Gestionnaire) ListerMachines() []Machine {
	g.mu.Lock()
	defer g.mu.Unlock()
	e := g.lire()
	out := make([]Machine, 0, len(e.Machines))
	for _, m := range e.Machines {
		out = append(out, m) // Cle a le tag json:"-" : elle ne sort jamais d'ici
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Nom < out[j].Nom })
	return out
}

// OublierMachine retire un poste : c'est la révocation. Sa clé ne vaut plus rien.
func (g *Gestionnaire) OublierMachine(empreinte string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	e := g.lire()
	for cle, m := range e.Machines {
		if m.Empreinte == empreinte {
			delete(e.Machines, cle)
			return g.ecrire(e)
		}
	}
	return fmt.Errorf("machine '%s' inconnue", empreinte)
}
