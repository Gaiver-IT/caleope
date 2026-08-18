package main

import "testing"

// La règle de présence est le cœur du client : c'est elle qui a failli faire
// installer des doublons Homebrew par-dessus les outils système d'un Mac.
func TestManquantsIgnoreCeQueLeSystemeFournitDeja(t *testing.T) {
	p := Profil{Paquets: []string{"git", "outil-qui-nexiste-pas-du-tout"}}
	// « git » n'est PAS dans l'inventaire du gestionnaire, mais la commande
	// existe sur toute machine de développement : il ne doit pas être réclamé.
	m := manquants(p, map[string]bool{})
	for _, x := range m {
		if x == "git" {
			t.Fatal("git réclamé alors que la commande répond déjà")
		}
	}
	if len(m) != 1 || m[0] != "outil-qui-nexiste-pas-du-tout" {
		t.Fatalf("manquants inattendus : %v", m)
	}
}

// Le préfixe « ! » sert quand on veut la version du gestionnaire malgré tout.
func TestManquantsForceLeGestionnaireAvecPointExclamation(t *testing.T) {
	p := Profil{Paquets: []string{"!git"}}
	m := manquants(p, map[string]bool{})
	if len(m) != 1 || m[0] != "git" {
		t.Fatalf("« !git » aurait dû être réclamé : %v", m)
	}
	// …mais s'il est déjà connu du gestionnaire, il ne l'est plus.
	if len(manquants(p, map[string]bool{"git": true})) != 0 {
		t.Fatal("« !git » réclamé alors que le gestionnaire le connaît")
	}
}

func TestDecoupeDesFormatsDeLigne(t *testing.T) {
	cas := []struct {
		ligne, nom, cmd string
		force           bool
	}{
		{"git", "git", "git", false},
		{"ripgrep=rg", "ripgrep", "rg", false},
		{"!coreutils=gls", "coreutils", "gls", true},
		{"  jq  ", "jq", "jq", false},
	}
	for _, c := range cas {
		n, cm, f := decoupe(c.ligne)
		if n != c.nom || cm != c.cmd || f != c.force {
			t.Fatalf("%q → (%q,%q,%v), attendu (%q,%q,%v)", c.ligne, n, cm, f, c.nom, c.cmd, c.force)
		}
	}
}

// Les lignes vides et les commentaires ne doivent pas devenir des paquets.
func TestManquantsIgnoreVidesEtCommentaires(t *testing.T) {
	p := Profil{Paquets: []string{"", "   ", "# un commentaire", "outil-inexistant-xyz"}}
	if m := manquants(p, map[string]bool{}); len(m) != 1 {
		t.Fatalf("lignes non pertinentes prises pour des paquets : %v", m)
	}
}
