package install

import (
	"testing"

	"github.com/gaiver-it/caleope/pkg/types"
)

func manifestJeu() *types.AppManifest {
	return &types.AppManifest{
		ID: "minecraft",
		Ports: []types.AppPort{
			{Name: "jeu", Container: 25565, Dynamic: false, Param: "MC_PORT"},
			{Name: "rcon", Container: 25575, Dynamic: true},
		},
	}
}

// Le cas qui débloque le multi-instance : deux serveurs sur des ports différents.
func TestUnParametreFixeLePortHote(t *testing.T) {
	m := manifestJeu()
	if err := appliquerPortsParametres(m, map[string]string{"MC_PORT": "25566"}); err != nil {
		t.Fatal(err)
	}
	if m.Ports[0].Host != 25566 {
		t.Fatalf("port hôte non appliqué : %d", m.Ports[0].Host)
	}
	if m.Ports[1].Host != 0 {
		t.Fatal("le port dynamique ne doit pas être touché")
	}
}

// Sans paramètre, l'application garde son port bien connu : c'est le cas
// ordinaire, et le changer silencieusement casserait toutes les installations.
func TestSansParametreLeManifesteDecide(t *testing.T) {
	for _, params := range []map[string]string{nil, {}, {"MC_PORT": ""}, {"MC_PORT": "   "}} {
		m := manifestJeu()
		if err := appliquerPortsParametres(m, params); err != nil {
			t.Fatal(err)
		}
		if m.Ports[0].Host != 0 {
			t.Fatalf("port modifié à tort : %d", m.Ports[0].Host)
		}
	}
}

// Une valeur absurde doit être refusée ICI, avec un message lisible — pas plus
// loin par Docker avec une erreur obscure.
func TestUnPortAbsurdeEstRefuseTot(t *testing.T) {
	for _, mauvais := range []string{"0", "-1", "70000", "vingt-cinq", "25565a", "25 565"} {
		m := manifestJeu()
		if err := appliquerPortsParametres(m, map[string]string{"MC_PORT": mauvais}); err == nil {
			t.Fatalf("valeur acceptée à tort : %q", mauvais)
		}
	}
}

// Le contrôle de conflit doit voir la valeur du paramètre, sinon la deuxième
// instance est refusée alors que l'utilisateur a bien changé son port.
func TestLeControleDeConflitVoitLePortDuParametre(t *testing.T) {
	m := manifestJeu()
	_ = appliquerPortsParametres(m, map[string]string{"MC_PORT": "25566"})
	if got := staticHostPort(m.Ports[0]); got != 25566 {
		t.Fatalf("le contrôle verrait le port %d au lieu de 25566", got)
	}
}

// La ligne de commande met les noms de paramètres en MINUSCULES, le manifeste
// les écrit en MAJUSCULES. Une comparaison stricte ne trouve rien, le port
// retombe à 0, et Docker en attribue un au hasard — mesuré : 32807 au lieu de
// 25565, sur une installation qui demandait pourtant explicitement 25565.
func TestLeParametreEstTrouveQuelleQueSoitLaCasse(t *testing.T) {
	for _, cle := range []string{"MC_PORT", "mc_port", "Mc_Port"} {
		m := manifestJeu()
		if err := appliquerPortsParametres(m, map[string]string{cle: "25566"}); err != nil {
			t.Fatal(err)
		}
		if m.Ports[0].Host != 25566 {
			t.Fatalf("clé %q non reconnue : port = %d", cle, m.Ports[0].Host)
		}
	}
}
