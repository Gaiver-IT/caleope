package install

import (
	"testing"

	"github.com/gaiver-it/caleope/pkg/types"
)

// TestStaticHostPort : le port vérifié sur l'hôte doit être Host quand il est
// défini (gitea 2222:22), sinon Container (pihole 53:53). Régression du bug où
// gitea/forgejo étaient refusés parce qu'on testait le port container 22 (sshd).
func TestStaticHostPort(t *testing.T) {
	cas := []struct {
		nom  string
		port types.AppPort
		want int
	}{
		{"host défini (gitea ssh)", types.AppPort{Container: 22, Host: 2222}, 2222},
		{"host défini (forgejo ssh)", types.AppPort{Container: 22, Host: 2223}, 2223},
		{"host absent (pihole dns)", types.AppPort{Container: 53, Host: 0}, 53},
		{"web dynamique (host alloué)", types.AppPort{Container: 3000, Host: 8001}, 8001},
	}
	for _, c := range cas {
		if got := staticHostPort(c.port); got != c.want {
			t.Errorf("%s : staticHostPort=%d, attendu %d", c.nom, got, c.want)
		}
	}
}
