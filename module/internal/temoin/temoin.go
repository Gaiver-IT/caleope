package temoin

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gaiver-it/caleope/pkg/types"
)

// IntervalleDefaut : un passage par heure. La sonde écrit puis relit quelques
// mégaoctets ; toutes les minutes ce serait du bruit sur le lien et sur le
// disque, une fois par jour laisserait passer une nuit entière de dégâts.
const IntervalleDefaut = time.Hour

// TailleSondeMio : ce qu'on écrit puis relit à chaque passage. Assez pour
// traverser plusieurs blocs, assez peu pour ne rien coûter (quelques secondes
// même sur un lien lent).
const TailleSondeMio = 4

// Emplacements est la partie du gestionnaire réseau dont le module a besoin.
// Interface étroite : elle rend le module testable avec une fausse liste.
type Emplacements interface {
	List() ([]types.NetworkLocation, error)
}

// Journal reçoit les changements de verdict. Nil est accepté.
type Journal interface {
	Emit(eventType string, app string, meta map[string]string) error
}

// Temoin fait un passage périodique sur chaque Emplacement réseau.
//
// Il n'agit sur RIEN : il constate, il enregistre, il prévient. Le champ Gel de
// la décision est calculé et publié, mais son application (empêcher Caleope
// d'écrire) n'est PAS branchée dans cette version — on observe d'abord une
// semaine de verdicts réels avant de laisser un automatisme retirer des droits.
type Temoin struct {
	baseDir     string
	emp         Emplacements
	journal     Journal
	logf        func(string, ...any)
	intervalle  time.Duration
	tailleMio   int
	budgetRonde int64
	now         func() time.Time
	stop        chan struct{}
	stopOnce    sync.Once
}

func New(baseDir string, emp Emplacements, journal Journal, logf func(string, ...any)) *Temoin {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Temoin{
		baseDir:     baseDir,
		emp:         emp,
		journal:     journal,
		logf:        logf,
		intervalle:  IntervalleDefaut,
		tailleMio:   TailleSondeMio,
		budgetRonde: BudgetRondeDefaut,
		now:         time.Now,
		stop:        make(chan struct{}),
	}
}

func (t *Temoin) Start() {
	go func() {
		// Premier passage tout de suite : sinon, après un redémarrage, on
		// attendrait une heure avant de savoir quoi que ce soit.
		t.Passage()
		tk := time.NewTicker(t.intervalle)
		defer tk.Stop()
		for {
			select {
			case <-tk.C:
				t.Passage()
			case <-t.stop:
				return
			}
		}
	}()
}

func (t *Temoin) Stop() { t.stopOnce.Do(func() { close(t.stop) }) }

// Passage vérifie tous les Emplacements et rend leurs états à jour.
func (t *Temoin) Passage() []Etat {
	locs, err := t.emp.List()
	if err != nil {
		t.logf("témoin : impossible de lister les Emplacements : %v", err)
		return nil
	}
	var out []Etat
	for _, loc := range locs {
		if loc.Type == types.LocationLocal {
			continue // un disque local n'a pas le problème qu'on surveille
		}
		out = append(out, t.Verifier(loc))
	}
	return out
}

// Verifier fait un passage complet sur UN Emplacement.
func (t *Temoin) Verifier(loc types.NetworkLocation) Etat {
	etat := ChargerEtat(t.baseDir, loc.Name)
	precedent := etat.Verdict
	maintenant := t.now()

	c := Constat{
		Monte:        estPointDeMontage(loc.MountPoint),
		SondeEnCours: etat.SondeEnCours,
	}

	var compteurs CompteursNFS
	if c.Monte {
		c.Sentinelle = SentinellePresente(loc.MountPoint)
		if !c.Sentinelle {
			// Première visite : on la pose, et on la relit pour de vrai.
			if err := PoserSentinelle(loc.MountPoint); err == nil {
				c.Sentinelle = SentinellePresente(loc.MountPoint)
			}
		}
		opts := OptionsMontage(loc.MountPoint)
		c.OptionSoft = ContientOption(opts, "soft")

		compteurs = LireMountstats()[loc.MountPoint]
		// ⚠️ Au TOUT PREMIER passage, il n'y a pas de relevé précédent : le delta
		// vaudrait le cumul depuis le montage. Sur une machine qui a vécu, ça
		// remonte des milliers d'erreurs vieilles de plusieurs semaines et le
		// module annonce « 106 erreurs depuis le dernier passage » — ce qui est
		// faux et détruit la confiance qu'on lui accorde dès sa première minute.
		// Le premier passage établit une RÉFÉRENCE, il ne juge pas les compteurs.
		if etat.Passages > 0 {
			d := compteurs.Delta(etat.Compteurs)
			c.DeltaErrWrite = d.WriteErrs
			c.DeltaErrRead = d.ReadErrs
		}

		if c.Sentinelle && !c.SondeEnCours {
			// Drapeau posé AVANT de sonder : si la sonde ne revient jamais
			// (montage figé), le passage suivant le verra et ne relancera pas
			// une deuxième sonde par-dessus.
			etat.SondeEnCours = true
			_ = EnregistrerEtat(t.baseDir, etat)

			r := Sonder(loc.MountPoint, t.tailleMio)
			c.SondeFaite = r.Faite
			c.RelectureDirecte = r.Directe
			c.BlocsNuls = r.BlocsNuls
			c.BlocsDivergents = r.BlocsDivergents
			if r.Erreur != nil {
				t.logf("témoin[%s] : %v", loc.Name, r.Erreur)
			}
			etat.SondeEnCours = false
		}
	}

	// ── Ronde sur les données réelles ────────────────────────────────────────
	// Elle vient APRÈS la sonde et n'entre PAS dans le verdict : la sonde juge
	// le lien, la ronde regarde ce qui est déjà écrit. Mélanger les deux
	// laisserait un faux positif de ronde condamner un Emplacement sain.
	// Elle ne tourne que si la sentinelle est là : sans elle, on lirait le
	// dossier local nu qui se trouve sous un montage absent, et on déclarerait
	// « 0 fichier abîmé » sur une bibliothèque qu'on n'a même pas ouverte.
	if c.Monte && c.Sentinelle {
		t.faireRonde(&etat, loc.Name, loc.MountPoint, maintenant)
	}

	d := Decide(c, precedent, etat.BonsConsecutifs)
	etat.Appliquer(d, c, compteurs, maintenant)
	if err := EnregistrerEtat(t.baseDir, etat); err != nil {
		t.logf("témoin[%s] : état non enregistré : %v", loc.Name, err)
	}

	if Change(precedent, d.Verdict) {
		t.logf("témoin[%s] : %s → %s — %s", loc.Name, precedent, d.Verdict, d.Raison)
		if t.journal != nil {
			evt := "emplacement.retabli"
			switch d.Verdict {
			case VerdictRompu:
				evt = "emplacement.rompu"
			case VerdictSuspect:
				evt = "emplacement.suspect"
			case VerdictInconnu:
				evt = "emplacement.indetermine"
			}
			_ = t.journal.Emit(evt, loc.Name, map[string]string{
				"verdict":   string(d.Verdict),
				"precedent": string(precedent),
				"raison":    d.Raison,
			})
		}
	}
	return etat
}

// estPointDeMontage lit /proc/self/mounts plutôt que de comparer des numéros de
// périphérique : c'est ce qui distingue un vrai partage monté du dossier local
// nu qui se trouve dessous quand rien n'est monté.
func estPointDeMontage(chemin string) bool {
	abs, err := filepath.Abs(chemin)
	if err != nil {
		abs = chemin
	}
	f, err := os.Open("/proc/self/mounts")
	if err != nil {
		// Pas de /proc (poste de développement) : on ne prétend pas savoir.
		return false
	}
	defer f.Close()
	_, ok := ParseMounts(f)[abs]
	return ok
}

// faireRonde exécute une tranche de ronde et range ce qu'elle a vu dans l'état.
//
// Isolée du reste pour être éprouvable : le contrôle « est-ce monté » lit
// /proc/self/mounts et rend toujours faux sur un poste de développement, ce qui
// rendrait ce chemin de code impossible à tester s'il restait en ligne.
func (t *Temoin) faireRonde(etat *Etat, nom, racine string, maintenant time.Time) {
	r := Ronde(racine, etat.RondeCurseur, t.budgetRonde, t.now)
	if r.Erreur != nil {
		t.logf("témoin[%s] : ronde interrompue : %v", nom, r.Erreur)
		return
	}
	etat.RondeCurseur = r.Curseur
	etat.RondeFichiers += r.Fichiers
	etat.RondeOctets += r.Octets
	etat.RondeDerniere = maintenant
	if r.Termine {
		etat.RondeTours++
	}
	if len(r.Trouvailles) == 0 {
		return
	}
	// ⚠️ On ne signale QUE les fichiers encore inconnus. Sans ça, chaque tour de
	// ronde re-crierait sur les mêmes fichiers et l'alerte finirait ignorée —
	// exactement le sort qu'a connu l'affichage « tout est vert » de l'incident.
	deja := map[string]bool{}
	for _, a := range etat.RondeAbimes {
		deja[a.Chemin] = true
	}
	var inedites []Trouvaille
	for _, tr := range r.Trouvailles {
		if !deja[tr.Chemin] {
			inedites = append(inedites, tr)
		}
	}
	etat.RondeAbimes = fusionnerTrouvailles(etat.RondeAbimes, r.Trouvailles)
	if len(inedites) == 0 {
		return
	}
	t.logf("témoin[%s] : %d fichier(s) troué(s) repéré(s), dont %s",
		nom, len(inedites), inedites[0].Chemin)
	if t.journal != nil {
		_ = t.journal.Emit("emplacement.donnees_abimees", nom, map[string]string{
			"fichiers": fmt.Sprint(len(inedites)),
			"exemple":  inedites[0].Chemin,
			"lus":      fmt.Sprint(r.Fichiers),
		})
	}
}

// fusionnerTrouvailles garde une liste bornée, sans doublon de chemin, la plus
// récente d'abord. Sans dédoublonnage, un fichier abîmé serait re-signalé à
// chaque tour de ronde et finirait par chasser tous les autres de la liste.
func fusionnerTrouvailles(anciennes, nouvelles []Trouvaille) []Trouvaille {
	vus := map[string]bool{}
	out := make([]Trouvaille, 0, MaxTrouvaillesRetenues)
	for _, t := range append(append([]Trouvaille{}, nouvelles...), anciennes...) {
		if vus[t.Chemin] {
			continue
		}
		vus[t.Chemin] = true
		out = append(out, t)
		if len(out) >= MaxTrouvaillesRetenues {
			break
		}
	}
	return out
}
