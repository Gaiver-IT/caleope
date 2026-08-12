package temoin

import (
	"bytes"
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// La sonde : écrire quelque chose de connu, puis le relire depuis le serveur et
// comparer. C'est tout. C'est aussi la seule chose qui aurait révélé l'incident
// le jour même au lieu de six semaines plus tard.

// DossierTemoin est le sous-dossier de travail sur l'Emplacement.
const DossierTemoin = "caleope/.temoin"

// FichierSentinelle prouve qu'on écrit bien sur le PARTAGE et non sur le
// dossier local nu qui se trouve dessous quand rien n'est monté.
const FichierSentinelle = "caleope/.emplacement"

// Resultat rassemble ce qu'une sonde a observé.
type Resultat struct {
	Faite           bool
	Directe         bool
	BlocsNuls       int
	BlocsDivergents int
	Octets          int
	Duree           time.Duration
	Erreur          error
}

// Sonder écrit un motif aléatoire de tailleMio mégaoctets sur l'Emplacement,
// le relit en contournant le cache, et compare bloc à bloc.
//
// Un motif ALÉATOIRE, pas un motif fixe : avec un motif constant, une lecture
// qui rendrait le contenu d'un ancien fichier passerait pour un succès.
//
// La distinction que fait cette fonction est le cœur du diagnostic :
//   - bloc relu entièrement NUL      → écriture perdue (le mal de l'incident)
//   - bloc relu différent, non nul   → corruption en transit (autre maladie)
func Sonder(pointDeMontage string, tailleMio int) Resultat {
	début := time.Now()
	r := Resultat{}
	if tailleMio < 1 {
		tailleMio = 1
	}

	dir := filepath.Join(pointDeMontage, DossierTemoin)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		r.Erreur = fmt.Errorf("dossier de sonde impossible à créer : %w", err)
		return r
	}

	// Nom unique : deux sondes simultanées ne doivent pas se marcher dessus.
	graine := make([]byte, 8)
	_, _ = rand.Read(graine)
	chemin := filepath.Join(dir, "sonde-"+hex.EncodeToString(graine)+".bin")
	defer os.Remove(chemin)

	motif := make([]byte, tailleMio*TailleBloc)
	if _, err := rand.Read(motif); err != nil {
		r.Erreur = fmt.Errorf("motif aléatoire indisponible : %w", err)
		return r
	}

	// Écriture, puis Sync() : sans lui, on ne teste que le cache d'écriture du
	// client et le serveur n'a peut-être encore rien vu.
	f, err := os.OpenFile(chemin, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		r.Erreur = fmt.Errorf("écriture impossible sur l'Emplacement : %w", err)
		return r
	}
	if _, err := f.Write(motif); err != nil {
		f.Close()
		r.Erreur = fmt.Errorf("écriture interrompue : %w", err)
		return r
	}
	if err := f.Sync(); err != nil {
		f.Close()
		// Une erreur ICI est précieuse : c'est exactement celle que `soft`
		// rendait et que personne ne lisait.
		r.Erreur = fmt.Errorf("synchronisation refusée par le serveur : %w", err)
		return r
	}
	if err := f.Close(); err != nil {
		r.Erreur = fmt.Errorf("fermeture en échec (donnée peut-être perdue) : %w", err)
		return r
	}

	relu, directe, err := LireDirect(chemin, len(motif))
	r.Directe = directe
	if err != nil && len(relu) == 0 {
		r.Erreur = fmt.Errorf("relecture impossible : %w", err)
		return r
	}
	if !directe {
		// Pas de preuve possible : on s'arrête là plutôt que de comparer un
		// contenu venu du cache et d'en tirer un « tout va bien ».
		r.Erreur = err
		return r
	}

	nul := make([]byte, TailleBloc)
	for i := 0; i+TailleBloc <= len(relu) && i+TailleBloc <= len(motif); i += TailleBloc {
		bloc := relu[i : i+TailleBloc]
		if bytes.Equal(bloc, motif[i:i+TailleBloc]) {
			continue
		}
		if bytes.Equal(bloc, nul) {
			r.BlocsNuls++
		} else {
			r.BlocsDivergents++
		}
	}

	r.Octets = len(relu)
	r.Faite = len(relu) == len(motif)
	if !r.Faite && r.Erreur == nil {
		r.Erreur = fmt.Errorf("relecture incomplète : %d octets sur %d", len(relu), len(motif))
	}
	r.Duree = time.Since(début)
	return r
}

// EmpreinteBloc rend l'empreinte d'un bloc, pour pouvoir citer une preuve
// rejouable dans un rapport (un bloc nul rend SHA1BlocNul).
func EmpreinteBloc(b []byte) string {
	h := sha1.Sum(b)
	return hex.EncodeToString(h[:])
}

// PoserSentinelle dépose le fichier témoin sur l'Emplacement s'il n'y est pas.
// Son contenu importe peu ; ce qui compte est qu'il ne puisse exister QUE si le
// partage est réellement monté au moment où on le lit.
func PoserSentinelle(pointDeMontage string) error {
	chemin := filepath.Join(pointDeMontage, FichierSentinelle)
	if _, err := os.Stat(chemin); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(chemin), 0o750); err != nil {
		return err
	}
	jeton := make([]byte, 16)
	_, _ = rand.Read(jeton)
	contenu := fmt.Sprintf("caleope-emplacement %s\n", hex.EncodeToString(jeton))
	return os.WriteFile(chemin, []byte(contenu), 0o640)
}

// SentinellePresente dit si le fichier témoin est lisible et conforme.
// Un dossier local vide qui se fait passer pour le partage échoue ici.
func SentinellePresente(pointDeMontage string) bool {
	b, err := os.ReadFile(filepath.Join(pointDeMontage, FichierSentinelle))
	if err != nil {
		return false
	}
	return bytes.HasPrefix(b, []byte("caleope-emplacement "))
}
