package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// parseMiddlewares lit la configuration produite par securityHeadersConfig et
// rend, pour chaque middleware, l'ensemble de ses clés d'en-tête.
//
// On n'utilise pas de bibliothèque YAML : le module n'en dépend pas, et une
// comparaison de sous-chaîne ne prouverait rien ici. Ce qu'on veut savoir, c'est
// « frameDeny appartient-il à secure-headers ou à frame-deny ? » — donc à quel
// bloc appartient la ligne, pas seulement si le mot existe quelque part.
func parseMiddlewares(t *testing.T, conf string) map[string]map[string]string {
	t.Helper()
	const (
		mwIndent  = 4 // "    secure-headers:"
		keyIndent = 8 // "        frameDeny: true"
	)
	out := map[string]map[string]string{}
	current := ""
	sawMiddlewaresRoot := false

	for _, line := range strings.Split(conf, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		trimmed := strings.TrimSpace(line)

		if indent == 2 && trimmed == "middlewares:" {
			sawMiddlewaresRoot = true
			continue
		}
		if indent == mwIndent && strings.HasSuffix(trimmed, ":") {
			current = strings.TrimSuffix(trimmed, ":")
			out[current] = map[string]string{}
			continue
		}
		if indent == 6 && trimmed == "headers:" {
			continue // le bloc "headers:" du middleware courant
		}
		if indent >= keyIndent && current != "" {
			k, v, found := strings.Cut(trimmed, ":")
			if !found {
				continue
			}
			out[current][strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	if !sawMiddlewaresRoot {
		t.Fatalf("configuration inattendue : aucun bloc 'middlewares:' trouvé\n%s", conf)
	}
	return out
}

// TestSecureHeadersNeFaitPasDeFrameDeny est LE test de non-régression de la
// panne du 2026-08-09 : le middleware "secure-headers" est appliqué globalement
// sur les entryPoints, donc un frameDeny dedans écrase la politique de cadrage
// de toutes les apps et casse en silence tout ce qui s'affiche dans un cadre
// (OnlyOffice, Collabora, l'éditeur interne de Nextcloud).
func TestSecureHeadersNeFaitPasDeFrameDeny(t *testing.T) {
	mws := parseMiddlewares(t, securityHeadersConfig())

	secure, ok := mws["secure-headers"]
	if !ok {
		t.Fatalf("le middleware 'secure-headers' a disparu — il est référencé par les entryPoints (install.sh), son absence casserait tout le proxy")
	}
	if v, present := secure["frameDeny"]; present {
		t.Errorf("secure-headers contient frameDeny=%q : appliqué globalement, il écrase la politique de cadrage des apps et casse OnlyOffice/Collabora", v)
	}
	if v, present := secure["customFrameOptionsValue"]; present {
		t.Errorf("secure-headers impose customFrameOptionsValue=%q : même problème, et SAMEORIGIN ne suffirait pas pour un serveur de documents sur un autre sous-domaine", v)
	}
}

// TestSecureHeadersGardeLesProtectionsNonLieesAuCadrage vérifie qu'on n'a pas
// jeté le bébé avec l'eau du bain en retirant frameDeny.
func TestSecureHeadersGardeLesProtectionsNonLieesAuCadrage(t *testing.T) {
	secure := parseMiddlewares(t, securityHeadersConfig())["secure-headers"]

	attendus := map[string]string{
		"stsSeconds":           "31536000",
		"stsIncludeSubdomains": "true",
		"stsPreload":           "true",
		"forceSTSHeader":       "true",
		"contentTypeNosniff":   "true",
		"browserXssFilter":     "true",
	}
	for k, want := range attendus {
		got, present := secure[k]
		if !present {
			t.Errorf("protection perdue : %s absent de secure-headers", k)
			continue
		}
		if got != want {
			t.Errorf("%s = %q, attendu %q", k, got, want)
		}
	}
	for _, k := range []string{"referrerPolicy", "permissionsPolicy"} {
		if _, present := secure[k]; !present {
			t.Errorf("protection perdue : %s absent de secure-headers", k)
		}
	}
}

// TestFrameDenyResteDisponibleEnOptIn : la protection anti-clickjacking n'est
// pas supprimée, elle devient choisie. Une app qui ne publie aucune politique
// de cadrage peut l'attacher par label Traefik.
func TestFrameDenyResteDisponibleEnOptIn(t *testing.T) {
	mws := parseMiddlewares(t, securityHeadersConfig())

	frameDeny, ok := mws["frame-deny"]
	if !ok {
		t.Fatalf("le middleware opt-in 'frame-deny' est absent : les apps sans politique de cadrage propre n'auraient plus aucun recours")
	}
	if frameDeny["frameDeny"] != "true" {
		t.Errorf("frame-deny.frameDeny = %q, attendu \"true\"", frameDeny["frameDeny"])
	}
}

// TestEnsureSecurityHeadersSupprimeLeFichierFantome : le daemon a renommé son
// fichier (secure-headers.yml → security-headers.yml) sans supprimer l'ancien.
// Les deux définissent le MÊME middleware ; tant que le fantôme survit, lequel
// gagne est indéterminé et le correctif peut n'avoir aucun effet — en silence.
func TestEnsureSecurityHeadersSupprimeLeFichierFantome(t *testing.T) {
	base := t.TempDir()
	dyn := filepath.Join(base, "data", "traefik", "dynamic")
	if err := os.MkdirAll(dyn, 0755); err != nil {
		t.Fatal(err)
	}
	fantome := filepath.Join(dyn, "secure-headers.yml")
	if err := os.WriteFile(fantome, []byte("http:\n  middlewares:\n    secure-headers:\n      headers:\n        frameDeny: true\n"), 0644); err != nil {
		t.Fatal(err)
	}

	s := &Server{baseDir: base}
	s.EnsureSecurityHeaders()

	if _, err := os.Stat(fantome); !os.IsNotExist(err) {
		t.Errorf("le fichier fantôme secure-headers.yml survit (err=%v) : il continuerait d'imposer frameDeny", err)
	}
	courant := filepath.Join(dyn, securityHeadersFile)
	data, err := os.ReadFile(courant)
	if err != nil {
		t.Fatalf("le fichier courant %s n'a pas été écrit : %v", securityHeadersFile, err)
	}
	if strings.Contains(string(data), "frameDeny") {
		if mws := parseMiddlewares(t, string(data)); mws["secure-headers"]["frameDeny"] != "" {
			t.Errorf("le fichier écrit impose encore frameDeny globalement")
		}
	}
}

// TestEnsureSecurityHeadersEstIdempotent : appelé à chaque démarrage du daemon.
func TestEnsureSecurityHeadersEstIdempotent(t *testing.T) {
	base := t.TempDir()
	s := &Server{baseDir: base}
	courant := filepath.Join(base, "data", "traefik", "dynamic", securityHeadersFile)

	s.EnsureSecurityHeaders()
	premier, err := os.ReadFile(courant)
	if err != nil {
		t.Fatalf("premier appel : %v", err)
	}
	s.EnsureSecurityHeaders()
	second, err := os.ReadFile(courant)
	if err != nil {
		t.Fatalf("second appel : %v", err)
	}
	if string(premier) != string(second) {
		t.Errorf("le contenu change entre deux appels — non idempotent")
	}
}

// TestPasDAutodestruction : garde-fou contre une future faute de frappe qui
// mettrait le fichier courant dans la liste des fichiers à supprimer — le
// daemon effacerait alors sa propre configuration à chaque démarrage.
func TestPasDAutodestruction(t *testing.T) {
	for _, stale := range staleSecurityHeaderFiles {
		if stale == securityHeadersFile {
			t.Fatalf("%q figure à la fois comme fichier courant et comme fichier à supprimer", stale)
		}
	}
}
