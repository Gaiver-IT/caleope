package backup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestImageRefsFromCompose(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "compose.yml")
	os.WriteFile(p, []byte(`services:
  app:
    image: ghcr.io/homarr-labs/homarr:latest
  db:
    image: "postgres:16-alpine"
  tmpl:
    image: {{.SomeVar}}
  var:
    image: ghcr.io/x/y:${TAG:-latest}
`), 0644)
	got := imageRefsFromCompose(p)
	want := map[string]bool{"ghcr.io/homarr-labs/homarr:latest": true, "postgres:16-alpine": true}
	if len(got) != 2 {
		t.Fatalf("attendu 2 images concrètes, obtenu %v", got)
	}
	for _, g := range got {
		if !want[g] {
			t.Errorf("image inattendue (template/var non filtré ?): %s", g)
		}
	}
}
