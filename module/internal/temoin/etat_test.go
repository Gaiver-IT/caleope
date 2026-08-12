package temoin

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gaiver-it/caleope/pkg/types"
)

func TestEtatAbsentEstInconnuPasSain(t *testing.T) {
	// Un Emplacement jamais vérifié ne doit pas démarrer « sain » : ce serait
	// afficher un voyant vert sur quelque chose qu'on n'a jamais regardé.
	e := ChargerEtat(t.TempDir(), "mon-nas")
	if e.Verdict != VerdictInconnu {
		t.Errorf("verdict initial = %q, attendu « inconnu »", e.Verdict)
	}
}

func TestEtatAbimeRetombeSurInconnu(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "runtime", "temoin")
	os.MkdirAll(dir, 0o750)
	os.WriteFile(filepath.Join(dir, "mon-nas.json"), []byte("{ceci n'est pas du json"), 0o640)

	e := ChargerEtat(base, "mon-nas")
	if e.Verdict != VerdictInconnu {
		t.Errorf("un état illisible doit rendre « inconnu », pas %q", e.Verdict)
	}
}

func TestEnregistrerPuisRelire(t *testing.T) {
	base := t.TempDir()
	e := Etat{Nom: "mon-nas", Verdict: VerdictRompu, Raison: "sentinelle absente",
		Gel: true, TotalBlocsNuls: 33, Passages: 7}
	if err := EnregistrerEtat(base, e); err != nil {
		t.Fatal(err)
	}
	lu := ChargerEtat(base, "mon-nas")
	if lu.Verdict != VerdictRompu || !lu.Gel || lu.TotalBlocsNuls != 33 || lu.Passages != 7 {
		t.Errorf("état relu différent : %+v", lu)
	}
	// Aucun fichier temporaire ne doit traîner après une écriture atomique.
	entries, _ := os.ReadDir(filepath.Join(base, "runtime", "temoin"))
	for _, f := range entries {
		if filepath.Ext(f.Name()) == ".tmp" {
			t.Errorf("fichier temporaire laissé derrière : %s", f.Name())
		}
	}
}

// TestNomFichierNeSortPasDuDossier : un nom d'Emplacement est saisi par
// l'utilisateur. « ../../etc/cron.d/truc » ne doit pas écrire hors du dossier.
func TestNomFichierNeSortPasDuDossier(t *testing.T) {
	for _, mauvais := range []string{"../../etc/passwd", "a/b", `c\d`, "..", ""} {
		n := nomFichier(mauvais)
		if filepath.Base(n) != n {
			t.Errorf("nomFichier(%q) = %q : sort du dossier", mauvais, n)
		}
	}
}

func TestDegelDemandeTroisBonsPassages(t *testing.T) {
	base := t.TempDir()
	e := Etat{Nom: "nas", Verdict: VerdictRompu}
	maintenant := time.Now()

	sainC := Constat{Monte: true, Sentinelle: true, SondeFaite: true, RelectureDirecte: true}
	for i := 1; i <= 3; i++ {
		d := Decide(sainC, e.Verdict, e.BonsConsecutifs)
		e.Appliquer(d, sainC, CompteursNFS{}, maintenant)
		if i < 3 && e.Verdict == VerdictSain {
			t.Errorf("dégel au passage %d : trop tôt", i)
		}
	}
	if e.Verdict != VerdictSain {
		t.Errorf("après trois passages irréprochables, verdict = %q, attendu « sain »", e.Verdict)
	}
	if err := EnregistrerEtat(base, e); err != nil {
		t.Fatal(err)
	}
}

// ── passage complet, avec de fausses dépendances ────────────────────────────

type fausseListe struct{ locs []types.NetworkLocation }

func (f fausseListe) List() ([]types.NetworkLocation, error) { return f.locs, nil }

type faussesTraces struct{ evts []string }

func (j *faussesTraces) Emit(t, app string, meta map[string]string) error {
	j.evts = append(j.evts, t+":"+app)
	return nil
}

// TestPassageSurEmplacementNonMonte : le cas le plus fréquent et le plus
// dangereux — le dossier existe, il est inscriptible, mais rien n'est monté.
func TestPassageSurEmplacementNonMonte(t *testing.T) {
	base := t.TempDir()
	j := &faussesTraces{}
	tm := New(base, fausseListe{[]types.NetworkLocation{{
		Name: "mon-nas", Type: types.LocationNFS,
		MountPoint: filepath.Join(base, "mounts", "mon-nas"),
	}}}, j, nil)

	etats := tm.Passage()
	if len(etats) != 1 {
		t.Fatalf("%d état(s) rendu(s), attendu 1", len(etats))
	}
	if etats[0].Verdict != VerdictRompu {
		t.Errorf("verdict = %q, attendu « rompu » (rien n'est monté)", etats[0].Verdict)
	}
	if !etats[0].Gel {
		t.Error("un Emplacement non monté doit être gelé : écrire y remplirait le disque système")
	}
	if len(j.evts) != 1 || j.evts[0] != "emplacement.rompu:mon-nas" {
		t.Errorf("événement attendu « emplacement.rompu:mon-nas », obtenu %v", j.evts)
	}

	// Deuxième passage : même verdict → aucun nouvel événement, sinon le
	// journal se remplirait d'une ligne par heure.
	tm.Passage()
	if len(j.evts) != 1 {
		t.Errorf("un événement doit être émis au CHANGEMENT de verdict seulement, obtenu %v", j.evts)
	}
}

// TestEmplacementLocalIgnore : un disque local n'a pas la maladie qu'on
// surveille, inutile d'y écrire une sonde toutes les heures.
func TestEmplacementLocalIgnore(t *testing.T) {
	base := t.TempDir()
	tm := New(base, fausseListe{[]types.NetworkLocation{
		{Name: "disque-local", Type: types.LocationLocal, MountPoint: base},
	}}, nil, nil)
	if etats := tm.Passage(); len(etats) != 0 {
		t.Errorf("%d état(s) rendu(s) pour un emplacement local, attendu 0", len(etats))
	}
}

// TestPremierPassageNeJugePasLesCompteurs : sur une machine qui a vécu,
// /proc/self/mountstats porte le cumul depuis le montage. Au premier passage il
// n'y a pas de relevé précédent — prendre ce cumul pour un delta ferait
// annoncer « 106 erreurs depuis le dernier passage » sur des erreurs vieilles
// de plusieurs semaines. C'est arrivé en production le jour du déploiement.
func TestPremierPassageNeJugePasLesCompteurs(t *testing.T) {
	e := Etat{Nom: "nas", Passages: 0}
	// Le code de Verifier ne calcule le delta que si Passages > 0 ; on vérifie
	// ici la règle qui le gouverne, pour qu'elle ne se perde pas à une refonte.
	if e.Passages > 0 {
		t.Fatal("état de départ incohérent")
	}

	// Deuxième passage : là, le delta a un sens.
	e.Passages = 1
	e.Compteurs = CompteursNFS{WriteErrs: 106}
	maintenant := CompteursNFS{WriteErrs: 110}
	d := maintenant.Delta(e.Compteurs)
	if d.WriteErrs != 4 {
		t.Errorf("delta au deuxième passage = %d, attendu 4", d.WriteErrs)
	}
}
