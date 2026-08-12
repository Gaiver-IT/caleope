package temoin

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestSentinelleDistingueLePartageDuDossierNu — c'est le test du piège le plus
// courant : quand un montage disparaît, le dossier local qui se trouve dessous
// reste inscriptible. Les données atterrissent alors sur le disque système, et
// tout a l'air normal. La sentinelle est ce qui fait la différence.
func TestSentinelleDistingueLePartageDuDossierNu(t *testing.T) {
	partage := t.TempDir()
	dossierNu := t.TempDir() // simule le point de montage démonté

	if SentinellePresente(partage) {
		t.Fatal("sentinelle annoncée présente avant d'avoir été posée")
	}
	if err := PoserSentinelle(partage); err != nil {
		t.Fatalf("pose de la sentinelle : %v", err)
	}
	if !SentinellePresente(partage) {
		t.Error("sentinelle posée mais non détectée")
	}
	if SentinellePresente(dossierNu) {
		t.Error("un dossier vide est passé pour le partage — c'est exactement la panne à éviter")
	}
}

func TestPoserSentinelleEstIdempotent(t *testing.T) {
	d := t.TempDir()
	if err := PoserSentinelle(d); err != nil {
		t.Fatal(err)
	}
	avant, _ := os.ReadFile(filepath.Join(d, FichierSentinelle))
	if err := PoserSentinelle(d); err != nil {
		t.Fatal(err)
	}
	apres, _ := os.ReadFile(filepath.Join(d, FichierSentinelle))
	if string(avant) != string(apres) {
		t.Error("la sentinelle a été réécrite : son jeton doit rester stable")
	}
}

// TestSondeSansLectureDirecteNeConcluePasSain est le test le plus important du
// paquet. Sur une plateforme sans O_DIRECT, la sonde écrit et relit très bien —
// mais depuis le cache. Le module doit alors dire « je n'ai rien prouvé », et
// surtout pas « tout va bien ».
//
// C'est la traduction en code de l'erreur que j'ai commise pendant le
// diagnostic : croire un fichier réparé parce qu'il se relisait correctement.
func TestSondeSansLectureDirecteNeConcluePasSain(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("O_DIRECT disponible : ce cas se teste sur une plateforme sans lecture directe")
	}
	d := t.TempDir()
	if err := PoserSentinelle(d); err != nil {
		t.Fatal(err)
	}
	r := Sonder(d, 1)

	if r.Directe {
		t.Fatal("la lecture directe est annoncée possible hors Linux")
	}
	d2 := Decide(Constat{
		Monte:            true,
		Sentinelle:       SentinellePresente(d),
		SondeFaite:       r.Faite,
		RelectureDirecte: r.Directe,
		BlocsNuls:        r.BlocsNuls,
		BlocsDivergents:  r.BlocsDivergents,
	}, VerdictSain, 0)

	if d2.Verdict == VerdictSain {
		t.Error("verdict « sain » rendu sans lecture directe — le module ment")
	}
	if d2.Verdict != VerdictInconnu {
		t.Errorf("verdict = %q, attendu « inconnu »", d2.Verdict)
	}
}

// TestEmpreinteBlocNul relie le code à la preuve citée dans les rapports.
func TestEmpreinteBlocNul(t *testing.T) {
	if got := EmpreinteBloc(make([]byte, TailleBloc)); got != SHA1BlocNul {
		t.Errorf("EmpreinteBloc(1 Mio de zéros) = %s, attendu %s", got, SHA1BlocNul)
	}
}

func TestTailleBlocEstUnMio(t *testing.T) {
	// Les trous observés lors de l'incident étaient alignés au Mio ; changer
	// cette taille ferait rater la signature.
	if TailleBloc != 1048576 {
		t.Errorf("TailleBloc = %d, attendu 1048576", TailleBloc)
	}
}
