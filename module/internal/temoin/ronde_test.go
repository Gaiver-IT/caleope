package temoin

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func bloc(octet byte) []byte {
	b := make([]byte, TailleBloc)
	for i := range b {
		b[i] = octet
	}
	return b
}

func ecrireFichier(t *testing.T, chemin string, contenu []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(chemin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(chemin, contenu, 0o644); err != nil {
		t.Fatal(err)
	}
}

// Le cas fondateur : un fichier troué au milieu.
func TestAnalyserContenuVoitLeTrou(t *testing.T) {
	var buf bytes.Buffer
	buf.Write(bloc(0x41))
	buf.Write(make([]byte, TailleBloc)) // le trou
	buf.Write(bloc(0x42))

	blocs, nuls, err := AnalyserContenu(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if blocs != 3 || nuls != 1 {
		t.Fatalf("attendu 3 blocs dont 1 nul, obtenu %d et %d", blocs, nuls)
	}
}

// Un fichier sain ne doit rien déclencher, sinon l'alerte devient du bruit.
func TestAnalyserContenuSainNeSignaleRien(t *testing.T) {
	var buf bytes.Buffer
	buf.Write(bloc(0x41))
	buf.Write(bloc(0x42))
	_, nuls, err := AnalyserContenu(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if nuls != 0 {
		t.Fatalf("%d bloc(s) nul(s) signalé(s) sur un fichier sain", nuls)
	}
}

// Une queue de fichier incomplète et nulle n'est PAS la signature cherchée :
// seuls les blocs complets comptent.
func TestAnalyserContenuIgnoreLaQueueIncomplete(t *testing.T) {
	var buf bytes.Buffer
	buf.Write(bloc(0x41))
	buf.Write(make([]byte, 4096)) // queue nulle, mais pas un bloc entier
	blocs, nuls, err := AnalyserContenu(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if blocs != 1 || nuls != 0 {
		t.Fatalf("attendu 1 bloc et 0 nul, obtenu %d et %d", blocs, nuls)
	}
}

// Un fichier plus petit qu'un bloc ne peut pas porter la signature.
func TestAnalyserFichierIgnoreLesPetits(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "petit.bin")
	ecrireFichier(t, p, make([]byte, 4096))
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	tr, err := AnalyserFichier(p, info)
	if err != nil {
		t.Fatal(err)
	}
	if tr != nil {
		t.Fatal("un fichier de 4 Kio entièrement nul a été dénoncé à tort")
	}
}

// La ronde doit nommer le fichier abîmé et laisser les autres tranquilles.
func TestRondeDesigneLeFichierAbime(t *testing.T) {
	dir := t.TempDir()
	sain := filepath.Join(dir, "a-sain.bin")
	abime := filepath.Join(dir, "b-abime.bin")
	ecrireFichier(t, sain, append(bloc(0x41), bloc(0x42)...))
	ecrireFichier(t, abime, append(bloc(0x41), make([]byte, TailleBloc)...))

	res := Ronde(dir, "", 1<<30, nil)
	if res.Erreur != nil {
		t.Fatal(res.Erreur)
	}
	if len(res.Trouvailles) != 1 {
		t.Fatalf("attendu 1 trouvaille, obtenu %d", len(res.Trouvailles))
	}
	if res.Trouvailles[0].Chemin != abime {
		t.Fatalf("mauvais fichier désigné : %s", res.Trouvailles[0].Chemin)
	}
	if !res.Termine {
		t.Fatal("la ronde devrait avoir bouclé son tour")
	}
}

// Le budget est ce qui empêche la ronde d'assommer un lien lent : il doit
// réellement arrêter le passage, et le curseur doit permettre de reprendre.
func TestRondeRespecteLeBudgetPuisReprend(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"a", "b", "c"} {
		ecrireFichier(t, filepath.Join(dir, n+".bin"), bloc(0x41))
	}

	premier := Ronde(dir, "", TailleBloc, nil) // de quoi lire un seul fichier
	if premier.Fichiers != 1 {
		t.Fatalf("le budget n'a pas arrêté la ronde : %d fichiers lus", premier.Fichiers)
	}
	if premier.Termine {
		t.Fatal("une ronde arrêtée par le budget ne doit pas se dire terminée")
	}
	if premier.Curseur == "" {
		t.Fatal("curseur vide : le passage suivant repartirait du début")
	}

	second := Ronde(dir, premier.Curseur, TailleBloc, nil)
	if second.Curseur == premier.Curseur {
		t.Fatal("la ronde n'a pas avancé : elle relirait le même fichier à vie")
	}
}

// Arrivée au bout, la ronde remet le curseur à zéro pour refaire un tour.
func TestRondeBoucleLeTour(t *testing.T) {
	dir := t.TempDir()
	ecrireFichier(t, filepath.Join(dir, "seul.bin"), bloc(0x41))

	res := Ronde(dir, "", 1<<30, nil)
	if !res.Termine || res.Curseur != "" {
		t.Fatalf("fin de tour mal signalée : termine=%v curseur=%q", res.Termine, res.Curseur)
	}
}

// Un répertoire illisible ne doit pas interrompre la ronde : ce qui suit dans
// l'ordre alphabétique doit quand même être examiné.
func TestRondeContinueMalgreUnDossierIllisible(t *testing.T) {
	dir := t.TempDir()
	interdit := filepath.Join(dir, "a-interdit")
	if err := os.MkdirAll(interdit, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(interdit, 0o755)
	ecrireFichier(t, filepath.Join(dir, "z-abime.bin"), append(bloc(0x41), make([]byte, TailleBloc)...))

	res := Ronde(dir, "", 1<<30, nil)
	if len(res.Trouvailles) != 1 {
		t.Fatalf("le fichier situé après le dossier illisible n'a pas été examiné (%d trouvaille(s))", len(res.Trouvailles))
	}
}

// La ronde ne doit pas se surveiller elle-même : le dossier de travail du
// module contient par construction des blocs écrits puis relus.
func TestRondeIgnoreSonPropreDossier(t *testing.T) {
	dir := t.TempDir()
	sien := filepath.Join(dir, DossierTemoin)
	ecrireFichier(t, filepath.Join(sien, "sonde.bin"), append(bloc(0x41), make([]byte, TailleBloc)...))

	res := Ronde(dir, "", 1<<30, func() time.Time { return time.Unix(0, 0) })
	if len(res.Trouvailles) != 0 {
		t.Fatalf("la ronde a inspecté son propre dossier : %v", res.Trouvailles)
	}
}

// Reprise dans un arbre à plusieurs niveaux : l'élagage ne doit pas faire
// SAUTER des fichiers. C'est le risque de l'optimisation — aller vite en
// oubliant d'inspecter, ce qui donnerait une ronde silencieuse et inutile.
func TestRondeRepriseNOubliePersonneDansUnArbreProfond(t *testing.T) {
	dir := t.TempDir()
	var attendus []string
	for _, a := range []string{"aa", "bb", "cc"} {
		for _, b := range []string{"01", "02"} {
			p := filepath.Join(dir, a, b, "f.bin")
			ecrireFichier(t, p, bloc(0x41))
			attendus = append(attendus, p)
		}
	}

	vus := map[string]bool{}
	curseur := ""
	for tour := 0; tour < 10; tour++ {
		res := Ronde(dir, curseur, TailleBloc, nil) // un fichier par passage
		if res.Erreur != nil {
			t.Fatal(res.Erreur)
		}
		if res.Curseur != "" {
			vus[res.Curseur] = true
		}
		if res.Termine {
			break
		}
		curseur = res.Curseur
	}
	for _, p := range attendus {
		if !vus[p] {
			t.Fatalf("fichier jamais examiné après reprise : %s", p)
		}
	}
}

// Un GROS fichier ne doit pas monopoliser le lien : la ronde doit s'arrêter en
// plein milieu et le reprendre au passage suivant. C'est le défaut qui a fait
// tourner la ronde 23 minutes d'affilée sur la production.
func TestRondeCoupeUnGrosFichierEtLeReprend(t *testing.T) {
	dir := t.TempDir()
	gros := filepath.Join(dir, "film.bin")
	contenu := append(append(bloc(0x41), make([]byte, TailleBloc)...), bloc(0x42)...) // 3 Mio, trou au milieu
	ecrireFichier(t, gros, contenu)

	premier := Ronde(dir, "", TailleBloc, nil) // un seul bloc autorisé
	if premier.Octets != TailleBloc {
		t.Fatalf("la ronde a lu %d octets alors que le budget était de %d", premier.Octets, TailleBloc)
	}
	if premier.Termine {
		t.Fatal("un fichier coupé en deux ne termine pas le tour")
	}
	if len(premier.Trouvailles) != 0 {
		t.Fatal("le premier bloc est sain, rien ne doit être signalé")
	}

	// Le trou est dans le DEUXIÈME bloc : il doit être vu à la reprise.
	second := Ronde(dir, premier.Curseur, TailleBloc, nil)
	if len(second.Trouvailles) != 1 {
		t.Fatalf("le trou n'a pas été vu à la reprise : %+v", second.Trouvailles)
	}
	if second.Trouvailles[0].BlocsNuls != 1 {
		t.Fatalf("blocs nuls mal comptés : %d", second.Trouvailles[0].BlocsNuls)
	}
}

// La borne en NOMBRE de fichiers protège des arborescences de vignettes, que le
// budget en octets ne voit pas passer.
func TestRondeBorneAussiLeNombreDeFichiers(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < MaxFichiersParPasse+10; i++ {
		ecrireFichier(t, filepath.Join(dir, "v"+strconv.Itoa(100000+i)+".bin"), []byte("minuscule"))
	}
	res := Ronde(dir, "", 1<<30, nil)
	if res.Fichiers > MaxFichiersParPasse {
		t.Fatalf("%d fichiers examinés, la borne est à %d", res.Fichiers, MaxFichiersParPasse)
	}
	if res.Termine {
		t.Fatal("la ronde ne peut pas avoir bouclé son tour en s'arrêtant sur la borne")
	}
}

// Un curseur abîmé ne doit pas faire sauter le fichier : on le relit depuis le
// début. Perdre du temps est acceptable, laisser passer un trou ne l'est pas.
func TestDecoderCurseurAbimeRepartDuDebutDuFichier(t *testing.T) {
	p := decoderCurseur("/chemin/film.bin\x00pasunnombre")
	if p.chemin != "/chemin/film.bin" || p.offset != 0 {
		t.Fatalf("curseur abîmé mal interprété : %+v", p)
	}
}

// Un signalement doit pouvoir être REJOUÉ : sans la position, on ne sait pas où
// regarder dans un fichier de 45 Go, et le constat ne vaut rien.
func TestUneTrouvailleDitOuSontLesTrous(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "gros.bin")
	// sain | TROU | sain | TROU
	contenu := append(bloc(0x41), make([]byte, TailleBloc)...)
	contenu = append(contenu, bloc(0x42)...)
	contenu = append(contenu, make([]byte, TailleBloc)...)
	ecrireFichier(t, p, contenu)

	info, _ := os.Stat(p)
	tr, _, _, err := AnalyserTranche(p, info, 0, 1<<30)
	if err != nil {
		t.Fatal(err)
	}
	if tr == nil || len(tr.Positions) != 2 {
		t.Fatalf("positions manquantes : %+v", tr)
	}
	if tr.Positions[0] != TailleBloc || tr.Positions[1] != 3*TailleBloc {
		t.Fatalf("positions fausses : %v (attendu %d et %d)", tr.Positions, TailleBloc, 3*TailleBloc)
	}
}

// Reprise en plein fichier : la position doit rester ABSOLUE, sinon elle
// désigne un endroit qui n'existe pas.
func TestLesPositionsRestentAbsoluesApresReprise(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "gros.bin")
	contenu := append(bloc(0x41), make([]byte, TailleBloc)...)
	ecrireFichier(t, p, contenu)

	info, _ := os.Stat(p)
	// On reprend APRÈS le premier bloc : le trou est alors le premier lu.
	tr, _, _, err := AnalyserTranche(p, info, TailleBloc, 1<<30)
	if err != nil {
		t.Fatal(err)
	}
	if tr == nil || len(tr.Positions) != 1 || tr.Positions[0] != TailleBloc {
		t.Fatalf("position relative au lieu d'absolue : %+v", tr)
	}
}
