package temoin

import (
	"path/filepath"
	"testing"
	"time"
)

type journalEspion struct {
	evenements []string
	metas      []map[string]string
}

func (j *journalEspion) Emit(evt, app string, meta map[string]string) error {
	j.evenements = append(j.evenements, evt)
	j.metas = append(j.metas, meta)
	return nil
}

func temoinDeTest(t *testing.T, j Journal, budget int64) *Temoin {
	t.Helper()
	tm := New(t.TempDir(), nil, j, nil)
	tm.budgetRonde = budget
	tm.now = func() time.Time { return time.Unix(1_700_000_000, 0) }
	return tm
}

// Un fichier troué doit finir NOMMÉ dans l'état et signalé au journal.
// C'est tout l'objet du module : l'incident fondateur n'a produit aucune trace.
func TestFaireRondeSignaleEtRetientLeFichierAbime(t *testing.T) {
	dir := t.TempDir()
	abime := filepath.Join(dir, "photo.bin")
	ecrireFichier(t, abime, append(bloc(0x41), make([]byte, TailleBloc)...))

	j := &journalEspion{}
	tm := temoinDeTest(t, j, 1<<30)
	var etat Etat
	tm.faireRonde(&etat, "mon-nas", dir, time.Unix(1_700_000_000, 0))

	if len(etat.RondeAbimes) != 1 || etat.RondeAbimes[0].Chemin != abime {
		t.Fatalf("fichier abîmé non retenu : %+v", etat.RondeAbimes)
	}
	if len(j.evenements) != 1 || j.evenements[0] != "emplacement.donnees_abimees" {
		t.Fatalf("journal : %v", j.evenements)
	}
	if etat.RondeTours != 1 {
		t.Fatalf("tour non compté : %d", etat.RondeTours)
	}
}

// Deuxième tour sur le MÊME fichier abîmé : plus d'événement. Une alerte qui se
// répète à chaque heure devient un bruit qu'on filtre, donc une alerte perdue.
func TestFaireRondeNeRecrieRienSurUnFichierDejaConnu(t *testing.T) {
	dir := t.TempDir()
	ecrireFichier(t, filepath.Join(dir, "photo.bin"), append(bloc(0x41), make([]byte, TailleBloc)...))

	j := &journalEspion{}
	tm := temoinDeTest(t, j, 1<<30)
	var etat Etat
	tm.faireRonde(&etat, "mon-nas", dir, time.Unix(1_700_000_000, 0))
	tm.faireRonde(&etat, "mon-nas", dir, time.Unix(1_700_003_600, 0))

	if len(j.evenements) != 1 {
		t.Fatalf("attendu 1 signalement, obtenu %d : %v", len(j.evenements), j.evenements)
	}
	if len(etat.RondeAbimes) != 1 {
		t.Fatalf("le fichier a été retenu en double : %d entrées", len(etat.RondeAbimes))
	}
}

// Une bibliothèque saine ne doit produire AUCUN événement — sinon personne ne
// croira le module le jour où il aura raison.
func TestFaireRondeSeTaitSurUneBibliothequeSaine(t *testing.T) {
	dir := t.TempDir()
	ecrireFichier(t, filepath.Join(dir, "ok.bin"), bloc(0x41))

	j := &journalEspion{}
	tm := temoinDeTest(t, j, 1<<30)
	var etat Etat
	tm.faireRonde(&etat, "mon-nas", dir, time.Unix(1_700_000_000, 0))

	if len(j.evenements) != 0 {
		t.Fatalf("bruit sur une bibliothèque saine : %v", j.evenements)
	}
	if etat.RondeFichiers != 1 {
		t.Fatalf("fichiers lus mal comptés : %d", etat.RondeFichiers)
	}
}

// L'avancement doit être RETENU d'un passage à l'autre : c'est ce qui permet
// de faire le tour d'une grosse bibliothèque en plusieurs jours au lieu de
// relire éternellement le début de l'alphabet.
func TestFaireRondeRetientSonAvancement(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"a", "b"} {
		ecrireFichier(t, filepath.Join(dir, n+".bin"), bloc(0x41))
	}

	tm := temoinDeTest(t, nil, TailleBloc) // un fichier par passage
	var etat Etat
	tm.faireRonde(&etat, "mon-nas", dir, time.Unix(1_700_000_000, 0))
	premier := etat.RondeCurseur
	if premier == "" {
		t.Fatal("curseur non retenu après le premier passage")
	}
	tm.faireRonde(&etat, "mon-nas", dir, time.Unix(1_700_003_600, 0))
	if etat.RondeCurseur == premier {
		t.Fatal("la ronde n'a pas avancé d'un passage à l'autre")
	}
	if etat.RondeFichiers != 2 {
		t.Fatalf("compteur cumulé faux : %d", etat.RondeFichiers)
	}
}
