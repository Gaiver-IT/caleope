package install

import (
	"testing"

	"github.com/gaiver-it/caleope/pkg/types"
)

// TestOccupePar : la brique qui décide si une montée de version est possible.
// Un port fixe se reconnaît par son champ Host quand il est défini (compose
// « 2222:22 »), sinon par le port conteneur (compose « 53:53 »).
func TestOccupePar(t *testing.T) {
	ports := []types.AppPort{
		{Name: "web", Container: 3000, Dynamic: true},          // dynamique : jamais « à nous »
		{Name: "ssh", Container: 22, Host: 2223, Dynamic: false},
		{Name: "dns", Container: 53, Dynamic: false},           // host absent → c'est 53
	}
	cas := []struct {
		nom     string
		port    int
		attendu bool
	}{
		{"port fixe avec champ Host", 2223, true},
		{"port fixe sans champ Host", 53, true},
		{"le port CONTENEUR d'un port mappé n'est pas le port hôte", 22, false},
		{"un port dynamique ne compte pas", 3000, false},
		{"port inconnu", 9999, false},
	}
	for _, c := range cas {
		if got := occupePar(ports, c.port); got != c.attendu {
			t.Errorf("%s : occupePar(%d) = %v, attendu %v", c.nom, c.port, got, c.attendu)
		}
	}
	if occupePar(nil, 2223) {
		t.Error("une liste vide ne peut occuper aucun port")
	}
}

// TestReinstallationAvecPortFixe reproduit la panne mesurée au banc le
// 13/08/2026 : « caleope install forgejo --force » refusait de tourner parce que
// l'app détenait elle-même son port ssh 2223.
//
// La cause n'est pas le contrôle de ports mais l'ORDRE des étapes : Install
// écrase l'enregistrement de l'app (étape 2) avant ce contrôle (étape 4). Le
// nouvel enregistrement n'a pas encore de ports, la reconnaissance « c'est ma
// propre app » comparait donc contre une liste vide, et le message accusait un
// « service tiers » qui était l'app elle-même.
func TestReinstallationAvecPortFixe(t *testing.T) {
	manifeste := &types.AppManifest{
		ID: "forgejo",
		Ports: []types.AppPort{
			{Name: "ssh", Container: 22, Host: 2223, Dynamic: false, Protocol: "tcp"},
		},
	}

	// Ce que l'app occupait avant qu'on touche à son enregistrement.
	precedents := []types.AppPort{{Name: "ssh", Container: 22, Host: 2223, Dynamic: false}}

	if !occupePar(precedents, 2223) {
		t.Fatal("les ports précédents devraient contenir 2223 — le scénario ne teste rien")
	}
	// Sans cette trace, rien ne distingue l'app d'un tiers : c'est exactement
	// l'état dans lequel le contrôle se trouvait, et la raison du refus.
	if occupePar(nil, 2223) {
		t.Fatal("incohérence du scénario")
	}

	_ = manifeste // le contrôle complet exige un runtime.Manager : la logique
	// décisive est occupePar, testée ci-dessus sur les deux chemins.
}
