package temoin

import (
	"crypto/sha1"
	"strings"
	"testing"
)

// sain est un Constat de référence : tout va bien. Chaque cas de test part de
// là et ne dégrade qu'UNE chose — sinon on ne saurait pas laquelle a décidé.
func sain() Constat {
	return Constat{
		Monte:            true,
		Sentinelle:       true,
		SondeFaite:       true,
		RelectureDirecte: true,
	}
}

func TestDecide(t *testing.T) {
	cas := []struct {
		nom     string
		constat func() Constat
		prec    Verdict
		bons    int
		verdict Verdict
		gel     bool
	}{
		{
			nom:     "tout va bien → sain, aucun gel",
			constat: sain,
			prec:    VerdictSain,
			verdict: VerdictSain, gel: false,
		},
		{
			nom: "montage disparu → rompu et gel (écrire remplirait le disque système)",
			constat: func() Constat {
				c := sain()
				c.Monte = false
				return c
			},
			prec:    VerdictSain,
			verdict: VerdictRompu, gel: true,
		},
		{
			nom: "sentinelle absente sur un dossier pourtant lisible → rompu",
			constat: func() Constat {
				c := sain()
				c.Sentinelle = false
				return c
			},
			prec:    VerdictSain,
			verdict: VerdictRompu, gel: true,
		},
		{
			nom: "sonde précédente bloquée → inconnu et gel, sans en relancer une",
			constat: func() Constat {
				c := sain()
				c.SondeEnCours = true
				return c
			},
			prec:    VerdictSain,
			verdict: VerdictInconnu, gel: true,
		},
		{
			nom: "RELECTURE DEPUIS LE CACHE NE PROUVE RIEN → inconnu, surtout pas sain",
			constat: func() Constat {
				c := sain()
				c.RelectureDirecte = false
				return c
			},
			prec:    VerdictSain,
			verdict: VerdictInconnu, gel: false,
		},
		{
			nom: "sonde jamais menée à terme → inconnu",
			constat: func() Constat {
				c := sain()
				c.SondeFaite = false
				return c
			},
			prec:    VerdictSain,
			verdict: VerdictInconnu, gel: false,
		},
		{
			nom: "un bloc nul isolé → suspect et gel immédiat",
			constat: func() Constat {
				c := sain()
				c.BlocsNuls = 1
				return c
			},
			prec:    VerdictSain,
			verdict: VerdictSuspect, gel: true,
		},
		{
			nom: "deuxième passage avec bloc nul → rompu",
			constat: func() Constat {
				c := sain()
				c.BlocsNuls = 3
				return c
			},
			prec:    VerdictSuspect,
			verdict: VerdictRompu, gel: true,
		},
		{
			nom: "bloc divergent non nul → corruption en transit, suspect et gel",
			constat: func() Constat {
				c := sain()
				c.BlocsDivergents = 2
				return c
			},
			prec:    VerdictSain,
			verdict: VerdictSuspect, gel: true,
		},
		{
			nom: "erreurs d'écriture NFS → suspect et gel",
			constat: func() Constat {
				c := sain()
				c.DeltaErrWrite = 42
				return c
			},
			prec:    VerdictSain,
			verdict: VerdictSuspect, gel: true,
		},
		{
			nom: "erreurs de LECTURE seulement → suspect mais PAS de gel (rien n'est détruit)",
			constat: func() Constat {
				c := sain()
				c.DeltaErrRead = 17
				return c
			},
			prec:    VerdictSain,
			verdict: VerdictSuspect, gel: false,
		},
		{
			nom: "montage en soft → signalé, mais on ne gèle pas tout le parc",
			constat: func() Constat {
				c := sain()
				c.OptionSoft = true
				return c
			},
			prec:    VerdictSain,
			verdict: VerdictSuspect, gel: false,
		},
		{
			nom:     "sortie d'incident : deux bons passages ne suffisent pas à dégeler",
			constat: sain,
			prec:    VerdictRompu, bons: 2,
			verdict: VerdictSuspect, gel: true,
		},
		{
			nom:     "sortie d'incident : trois bons passages dégèlent",
			constat: sain,
			prec:    VerdictRompu, bons: 3,
			verdict: VerdictSain, gel: false,
		},
	}

	for _, c := range cas {
		t.Run(c.nom, func(t *testing.T) {
			d := Decide(c.constat(), c.prec, c.bons)
			if d.Verdict != c.verdict {
				t.Errorf("verdict = %q, attendu %q (raison : %s)", d.Verdict, c.verdict, d.Raison)
			}
			if d.Gel != c.gel {
				t.Errorf("gel = %v, attendu %v (raison : %s)", d.Gel, c.gel, d.Raison)
			}
			if d.Verdict != VerdictSain && strings.TrimSpace(d.Raison) == "" {
				t.Error("un verdict autre que « sain » doit porter une raison lisible")
			}
		})
	}
}

// TestPrioriteDesRegles : quand plusieurs anomalies coexistent, c'est la plus
// grave qui doit parler. Un montage absent ET des blocs nuls doit dire « montage
// absent » — sinon on part réparer des fichiers alors que le partage n'est pas là.
func TestPrioriteDesRegles(t *testing.T) {
	c := sain()
	c.Monte = false
	c.BlocsNuls = 10
	c.DeltaErrWrite = 99
	d := Decide(c, VerdictSain, 0)
	if !strings.Contains(d.Raison, "point de montage") {
		t.Errorf("la règle la plus grave doit l'emporter, or la raison est : %s", d.Raison)
	}
}

// TestSignatureBlocNul vérifie que la constante documentaire est exacte. Si elle
// dérive, tous les rapports du module citent une preuve fausse.
func TestSignatureBlocNul(t *testing.T) {
	h := sha1.Sum(make([]byte, 1048576))
	got := ""
	for _, b := range h {
		got += string("0123456789abcdef"[b>>4]) + string("0123456789abcdef"[b&0xf])
	}
	if got != SHA1BlocNul {
		t.Errorf("SHA1BlocNul = %s, or sha1(1 Mio de zéros) = %s", SHA1BlocNul, got)
	}
}

// TestJamaisSainSansPreuve est le garde-fou de principe : aucune combinaison
// d'entrées ne doit rendre « sain » si la relecture directe n'a pas eu lieu.
// C'est la règle qui distingue ce module d'un simple voyant vert.
func TestJamaisSainSansPreuve(t *testing.T) {
	for _, sondeFaite := range []bool{true, false} {
		for _, directe := range []bool{true, false} {
			if sondeFaite && directe {
				continue // le seul cas où « sain » est permis
			}
			c := sain()
			c.SondeFaite = sondeFaite
			c.RelectureDirecte = directe
			if d := Decide(c, VerdictSain, 99); d.Verdict == VerdictSain {
				t.Errorf("sain déclaré sans preuve (SondeFaite=%v RelectureDirecte=%v)",
					sondeFaite, directe)
			}
		}
	}
}
