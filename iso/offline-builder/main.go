// Command offline-builder assemble un bundle Caleope « offline » : binaires,
// store, et les images Docker des apps choisies — le tout sans dépendre d'un
// démon Docker (pull via go-containerregistry). Le bundle produit se donne à
// `install.sh --offline <bundle>` ou à `iso/build.sh` (OFFLINE_IMAGES_DIR) pour
// fabriquer une ISO air-gap taillée juste pour la sélection d'apps.
//
// Exemples :
//
//	offline-builder -apps jellyfin,nextcloud,immich -version v0.6.6 -out ./bundle
//	OFFLINE_IMAGES_DIR=./bundle/images CALEOPE_VERSION=v0.6.6 ../build.sh   # ISO offline
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/crane"
	v1 "github.com/google/go-containerregistry/pkg/v1"
)

const storeGit = "https://github.com/gaiver-it/caleope-store.git"

// image: valeurs concrètes uniquement (on ignore les placeholders Go-template et
// les images construites localement par Caleope).
var imageLine = regexp.MustCompile(`(?m)^\s*image:\s*["']?([^"'\s#]+)["']?`)

type packInfo struct {
	CaleopeVersion string    `json:"caleope_version"`
	Channel        string    `json:"channel"`
	PackedAt       time.Time `json:"packed_at"`
	Mode           string    `json:"mode"`
	Apps           []string  `json:"apps"`
	Images         []string  `json:"images"`
}

func main() {
	var (
		appsCSV = flag.String("apps", "", "apps à inclure (CSV). Vide = toutes les apps du store")
		version = flag.String("version", "v0.6.6", "version Caleope (tag de release GitHub)")
		channel = flag.String("channel", "stable", "canal du store : stable|alpha")
		storeIn = flag.String("store", "", "chemin d'un store déjà cloné (sinon clone auto)")
		outDir  = flag.String("out", "./caleope-bundle", "dossier de sortie du bundle")
		arch    = flag.String("arch", "amd64", "architecture des images/binaires")
		dry     = flag.Bool("dry", false, "afficher les apps/images sélectionnées puis quitter (aucun pull)")
	)
	flag.Parse()

	if err := run(*appsCSV, *version, *channel, *storeIn, *outDir, *arch, *dry); err != nil {
		fmt.Fprintf(os.Stderr, "\n❌ %v\n", err)
		os.Exit(1)
	}
}

func run(appsCSV, version, channel, storeIn, outDir, arch string, dry bool) error {
	// ── Store : clone si nécessaire ──────────────────────────────────────────
	storeDir := storeIn
	cleanup := func() {}
	if storeDir == "" {
		branch := "main"
		if channel == "alpha" {
			branch = "alpha"
		}
		tmp, err := os.MkdirTemp("", "caleope-store-")
		if err != nil {
			return err
		}
		cleanup = func() { os.RemoveAll(tmp) }
		defer cleanup()
		storeDir = filepath.Join(tmp, "caleope-store")
		fmt.Printf("⬇️  Clone du store (%s)…\n", branch)
		cmd := exec.Command("git", "clone", "--depth", "1", "--branch", branch, storeGit, storeDir)
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("clone du store : %w", err)
		}
	}

	// ── Sélection d'apps ─────────────────────────────────────────────────────
	selected := map[string]bool{}
	for _, a := range strings.Split(appsCSV, ",") {
		if a = strings.TrimSpace(a); a != "" {
			selected[a] = true
		}
	}
	apps, err := listApps(storeDir)
	if err != nil {
		return err
	}
	if len(selected) > 0 {
		var filtered []string
		for _, a := range apps {
			if selected[a] {
				filtered = append(filtered, a)
			} else {
				delete(selected, a)
			}
		}
		for a := range selected {
			fmt.Printf("⚠️  app inconnue, ignorée : %s\n", a)
		}
		apps = filtered
	}
	if len(apps) == 0 {
		return fmt.Errorf("aucune app à empaqueter")
	}

	// ── Découverte des images ────────────────────────────────────────────────
	imgSet := map[string]bool{}
	for _, app := range apps {
		for _, ref := range imagesForApp(storeDir, app) {
			imgSet[ref] = true
		}
	}
	images := keys(imgSet)
	fmt.Printf("🧭 %d app(s), %d image(s) à récupérer\n", len(apps), len(images))

	if dry {
		fmt.Println("\n── Apps ──")
		for _, a := range apps {
			fmt.Printf("  • %s\n", a)
		}
		fmt.Println("── Images ──")
		for _, i := range images {
			fmt.Printf("  • %s\n", i)
		}
		fmt.Println("\n(dry-run — aucun téléchargement)")
		return nil
	}

	// ── Préparer le bundle ───────────────────────────────────────────────────
	if err := os.MkdirAll(filepath.Join(outDir, "binaries"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(outDir, "images"), 0o755); err != nil {
		return err
	}

	// ── Binaires Caleope ─────────────────────────────────────────────────────
	fmt.Printf("⬇️  Binaires Caleope %s…\n", version)
	for _, b := range []string{"caleoped", "caleope", "caleope-ui"} {
		url := fmt.Sprintf("https://github.com/Gaiver-IT/caleope/releases/download/%s/%s-linux-%s", version, b, arch)
		dst := filepath.Join(outDir, "binaries", b)
		if err := download(url, dst); err != nil {
			return fmt.Errorf("binaire %s : %w", b, err)
		}
		_ = os.Chmod(dst, 0o755)
	}

	// ── Pull des images (sans Docker, via go-containerregistry) ──────────────
	platform := &v1.Platform{OS: "linux", Architecture: arch}
	var pulled []string
	for i, ref := range images {
		fmt.Printf("🐳 [%d/%d] pull %s\n", i+1, len(images), ref)
		img, err := crane.Pull(ref, crane.WithPlatform(platform))
		if err != nil {
			fmt.Printf("   ⚠️  échec (%v) — image ignorée\n", err)
			continue
		}
		tarName := sanitize(ref) + ".tar"
		if err := crane.Save(img, ref, filepath.Join(outDir, "images", tarName)); err != nil {
			fmt.Printf("   ⚠️  écriture échouée (%v)\n", err)
			continue
		}
		pulled = append(pulled, ref)
	}

	// ── Store → store.tar.gz (wrappé dans caleope-store/) ────────────────────
	fmt.Println("📦 Empaquetage du store…")
	if err := tarStore(storeDir, filepath.Join(outDir, "store.tar.gz")); err != nil {
		return err
	}

	// ── pack-info.json ───────────────────────────────────────────────────────
	info := packInfo{
		CaleopeVersion: version,
		Channel:        channel,
		PackedAt:       time.Now().UTC(),
		Mode:           "offline",
		Apps:           apps,
		Images:         pulled,
	}
	buf, _ := json.MarshalIndent(info, "", "  ")
	if err := os.WriteFile(filepath.Join(outDir, "pack-info.json"), buf, 0o644); err != nil {
		return err
	}

	fmt.Printf("\n✅ Bundle prêt : %s (%d/%d images)\n", outDir, len(pulled), len(images))
	fmt.Printf("   → install.sh --offline %s\n", outDir)
	fmt.Printf("   → OFFLINE_IMAGES_DIR=%s/images ../build.sh   (ISO offline)\n", outDir)
	return nil
}

// listApps retourne les apps (dossiers) du store, triées.
func listApps(storeDir string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(storeDir, "apps"))
	if err != nil {
		return nil, fmt.Errorf("lecture du store : %w", err)
	}
	var apps []string
	for _, e := range entries {
		if e.IsDir() {
			apps = append(apps, e.Name())
		}
	}
	sort.Strings(apps)
	return apps, nil
}

// imagesForApp extrait les images concrètes du docker-compose d'une app.
func imagesForApp(storeDir, app string) []string {
	path := filepath.Join(storeDir, "apps", app, "docker-compose.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []string
	for _, m := range imageLine.FindAllSubmatch(data, -1) {
		ref := resolveEnv(string(m[1]))
		// Ignorer les placeholders Go-template, les images buildées localement,
		// et toute variable shell non résolue restante.
		if strings.Contains(ref, "{{") || strings.HasPrefix(ref, "caleope-") || strings.Contains(ref, "$") {
			continue
		}
		out = append(out, ref)
	}
	return out
}

// resolveEnv remplace les substitutions shell ${VAR:-default} par leur valeur
// par défaut (les compose du store utilisent ce motif pour épingler une version).
var envDefault = regexp.MustCompile(`\$\{[A-Za-z_][A-Za-z0-9_]*:-([^}]*)\}`)

func resolveEnv(ref string) string {
	return envDefault.ReplaceAllString(ref, "$1")
}

func download(url, dst string) error {
	resp, err := http.Get(url) //nolint:gosec // URL construite depuis des flags de confiance
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d sur %s", resp.StatusCode, url)
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, resp.Body)
	return err
}

// tarStore crée store.tar.gz contenant un dossier wrapper caleope-store/
// (install.sh l'extrait avec --strip-components=1).
func tarStore(storeDir, out string) error {
	parent := filepath.Dir(storeDir)
	base := filepath.Base(storeDir)
	cmd := exec.Command("tar", "czf", out, "-C", parent, base)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// sanitize transforme une référence d'image en nom de fichier sûr.
func sanitize(ref string) string {
	r := strings.NewReplacer("/", "_", ":", "_", "@", "_")
	return r.Replace(ref)
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
