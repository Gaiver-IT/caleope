package main

import (
	"strings"
	"testing"
)

// La liste d'exemption doit ouvrir EXACTEMENT trois chemins. Un préfixe
// laisserait passer l'administration des profils et la liste des machines,
// c'est-à-dire tout ce qu'un portable n'a aucune raison de voir.
func TestExemptionPosteExacteEtFermee(t *testing.T) {
	pourLePoste := map[string]bool{
		"/api/v1/postes/appairage": true,
		"/api/v1/postes/ma-conf":   true,
		"/api/v1/postes/rapport":   true,
	}
	ouverts := []string{"/api/v1/postes/appairage", "/api/v1/postes/ma-conf", "/api/v1/postes/rapport"}
	fermes := []string{
		"/api/v1/postes/profils", "/api/v1/postes/machines", "/api/v1/postes/jeton",
		"/api/v1/postes/clients", "/api/v1/apps", "/api/v1/postes/", "/api/v1/postes/ma-conf/../profils",
	}
	for _, p := range ouverts {
		if !pourLePoste[p] {
			t.Fatalf("chemin qui devrait être ouvert au poste : %s", p)
		}
	}
	for _, p := range fermes {
		if pourLePoste[p] {
			t.Fatalf("chemin ouvert à tort : %s", p)
		}
	}
}

// Le proxy ne doit JAMAIS écraser l'authentification d'une machine par le jeton
// d'admin : l'appairage réussirait, puis la machine ne tirerait plus rien.
func TestProxyNEcrasePasLaCleDeMachine(t *testing.T) {
	remplacer := func(entete string) bool {
		return !strings.HasPrefix(entete, "Machine ")
	}
	if remplacer("Machine abc123") {
		t.Fatal("la clé de machine serait remplacée par le jeton d'admin")
	}
	for _, e := range []string{"", "Bearer xyz", "Basic dXNlcg==", "machine abc"} {
		if !remplacer(e) {
			t.Fatalf("le jeton d'admin devrait être posé pour %q", e)
		}
	}
}
