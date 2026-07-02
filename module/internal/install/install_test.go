// internal/install/install_test.go
//
// Tests de la réécriture des images vers le registre miroir.

package install

import "testing"

func TestRewriteImages(t *testing.T) {
	reg := "caleope-registry.gaiver-it.fr"
	in := `services:
  db:
    image: postgres:16-alpine
  authentik:
    image: ghcr.io/goauthentik/server:2024.12
  radarr:
    image: "lscr.io/linuxserver/radarr:latest"
  bot:
    image: caleope-azuracast-discord-bot:latest
    build: ./src
  pending:
    image: {{.SomeVar}}`

	out := rewriteImages(in, reg)

	// Images upstream → préfixées
	for _, want := range []string{
		"image: caleope-registry.gaiver-it.fr/postgres:16-alpine",
		"image: caleope-registry.gaiver-it.fr/ghcr.io/goauthentik/server:2024.12",
		"image: caleope-registry.gaiver-it.fr/lscr.io/linuxserver/radarr:latest",
	} {
		if !contains(out, want) {
			t.Errorf("attendu réécrit: %q\n--- got ---\n%s", want, out)
		}
	}

	// Image locale (build caleope-*) → PAS réécrite
	if contains(out, "caleope-registry.gaiver-it.fr/caleope-azuracast") {
		t.Error("l'image locale caleope-* ne doit PAS être réécrite")
	}
	// Template non résolu → PAS réécrit
	if !contains(out, "image: {{.SomeVar}}") {
		t.Error("un template non résolu ne doit pas être réécrit")
	}
}

func TestRewriteImagesIdempotent(t *testing.T) {
	reg := "reg.local:5000"
	in := "    image: reg.local:5000/postgres:16"
	if got := rewriteImages(in, reg); got != in {
		t.Errorf("réécriture non idempotente: %q → %q", in, got)
	}
}

func TestRewriteImagesEmptyRegistryNoop(t *testing.T) {
	in := "    image: postgres:16"
	if got := rewriteImages(in, ""); got != in {
		t.Errorf("registre vide devrait être un no-op: %q → %q", in, got)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
