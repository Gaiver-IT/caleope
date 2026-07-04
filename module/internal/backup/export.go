// internal/backup/export.go
//
// EXPORT AUTO-SUFFISANT ("caleope export") — le pendant "submarine" du restore.
//
// Une archive d'export contient TOUT ce qu'il faut pour recréer une app et ses
// données des mois plus tard, même si l'app a disparu du store ou si internet
// est coupé :
//   payload/data.tar.gz          données (app-data/<app>)
//   payload/config.tar.gz        secrets (app-config/<app>)
//   payload/definition/installed compose résolu + app.env (apps-installed/<app>)
//   payload/definition/store     définition d'origine (app.json, setup.sh, params)
//   payload/definition/runtime.json  état runtime
//   payload/images.tar           images Docker (docker save) — restore hors-ligne
//   payload/export-manifest.json  métadonnées (version Caleope, app, images…)
//
// `caleope import` recrée l'app depuis cette archive SANS le store :
//   - --legacy  : à l'identique (images figées) → marche toujours ;
//   - --migrate : (défaut si dispo) restaure dans le nouveau standard via le
//                 migrate.sh versionné du store, sinon fallback legacy.

package backup

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/gaiver-it/caleope/pkg/version"
)

const exportFormatVersion = 1

// ExportManifest décrit une archive d'export auto-suffisante.
type ExportManifest struct {
	App            string    `json:"app"`
	CaleopeVersion string    `json:"caleope_version"`
	ExportedAt     time.Time `json:"exported_at"`
	FormatVersion  int       `json:"format_version"`
	Images         []string  `json:"images"`
	HasImages      bool      `json:"has_images"`
	Domain         string    `json:"domain,omitempty"`
}

var imageLineRe = regexp.MustCompile(`(?m)^\s*image:\s*["']?([^"'\s#]+)["']?`)

// imageRefsFromCompose extrait les refs d'images concrètes d'un compose.yml résolu.
func imageRefsFromCompose(composePath string) []string {
	data, err := os.ReadFile(composePath)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, m := range imageLineRe.FindAllSubmatch(data, -1) {
		ref := string(m[1])
		if ref == "" || strings.Contains(ref, "{{") || strings.Contains(ref, "$") {
			continue // template / variable non résolue
		}
		if !seen[ref] {
			seen[ref] = true
			out = append(out, ref)
		}
	}
	return out
}

// Export crée une archive auto-suffisante de <appID> à <dest> (défaut :
// <baseDir>/exports/caleope-export-<app>-<ts>.tar.gz). L'app est arrêtée le temps
// de l'export (snapshot cohérent) puis redémarrée.
func (m *Manager) Export(appID, dest string, withImages bool) (string, error) {
	app, err := m.rt.GetApp(appID)
	if err != nil {
		return "", fmt.Errorf("application '%s' non trouvée: %w", appID, err)
	}

	tmp, err := os.MkdirTemp("", "caleope-export-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	payload := filepath.Join(tmp, "payload")
	if err := os.MkdirAll(payload, 0755); err != nil {
		return "", err
	}

	fmt.Println("  [1/5] Arrêt des containers (snapshot cohérent)...")
	if err := m.docker.Stop(app.ComposeDir); err != nil {
		return "", fmt.Errorf("arrêt containers: %w", err)
	}
	defer func() {
		fmt.Println("  → Redémarrage des containers...")
		_ = m.docker.Start(app.ComposeDir)
	}()

	// 1. données + config
	fmt.Println("  [2/5] Archivage données + config...")
	if dataDir := filepath.Join(m.baseDir, "app-data", appID); dirExists(dataDir) {
		if err := tarGz(dataDir, filepath.Join(payload, "data.tar.gz")); err != nil {
			return "", fmt.Errorf("tar data: %w", err)
		}
	}
	if cfgDir := filepath.Join(m.baseDir, "app-config", appID); dirExists(cfgDir) {
		if err := tarGz(cfgDir, filepath.Join(payload, "config.tar.gz")); err != nil {
			return "", fmt.Errorf("tar config: %w", err)
		}
	}

	// 2. définition figée
	fmt.Println("  [3/5] Capture de la définition (compose + store + runtime)...")
	defDir := filepath.Join(payload, "definition")
	_ = os.MkdirAll(defDir, 0755)
	installedDir := filepath.Join(m.baseDir, "apps-installed", appID)
	if err := copyTree(installedDir, filepath.Join(defDir, "installed")); err != nil {
		return "", fmt.Errorf("copie compose installé: %w", err)
	}
	// définition d'origine du store (best-effort — peut avoir disparu, on garde ce qu'on a)
	_ = copyTree(filepath.Join(m.baseDir, "core", "cache", "official", "apps", appID),
		filepath.Join(defDir, "store"))
	_ = copyFile(filepath.Join(m.baseDir, "runtime", "apps", appID+".json"),
		filepath.Join(defDir, "runtime.json"))

	// 3. images
	images := imageRefsFromCompose(filepath.Join(installedDir, "compose.yml"))
	manifest := ExportManifest{
		App:            appID,
		CaleopeVersion: version.Version,
		ExportedAt:     time.Now().UTC(),
		FormatVersion:  exportFormatVersion,
		Images:         images,
		Domain:         app.Domain,
	}
	if withImages && len(images) > 0 {
		fmt.Printf("  [4/5] Export de %d image(s) (couches complètes, restore hors-ligne)...\n", len(images))
		if err := m.docker.ExportImages(images, filepath.Join(payload, "images")); err != nil {
			return "", fmt.Errorf("export images: %w", err)
		}
		manifest.HasImages = true
	} else {
		fmt.Println("  [4/5] Images non embarquées (re-pull au restore)")
	}

	// 4. manifest + packaging
	buf, _ := json.MarshalIndent(manifest, "", "  ")
	if err := os.WriteFile(filepath.Join(payload, "export-manifest.json"), buf, 0644); err != nil {
		return "", err
	}
	if dest == "" {
		exportsDir := filepath.Join(m.baseDir, "exports")
		_ = os.MkdirAll(exportsDir, 0755)
		dest = filepath.Join(exportsDir,
			fmt.Sprintf("caleope-export-%s-%s.tar.gz", appID, time.Now().Format("2006-01-02T15-04-05")))
	}
	fmt.Println("  [5/5] Empaquetage de l'archive...")
	if err := tarGz(payload, dest); err != nil {
		return "", fmt.Errorf("empaquetage: %w", err)
	}
	return dest, nil
}

// Import recrée une app depuis une archive d'export, sans dépendre du store.
// mode : "legacy" (à l'identique) ou "migrate" (nouveau standard si dispo, sinon legacy).
func (m *Manager) Import(archivePath, mode string) error {
	if _, err := os.Stat(archivePath); err != nil {
		return fmt.Errorf("archive introuvable: %s", archivePath)
	}
	tmp, err := os.MkdirTemp("", "caleope-import-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	fmt.Println("  [1/6] Extraction de l'archive...")
	if err := extractTarGz(archivePath, tmp); err != nil {
		return err
	}
	payload := filepath.Join(tmp, "payload")

	var manifest ExportManifest
	mBuf, err := os.ReadFile(filepath.Join(payload, "export-manifest.json"))
	if err != nil {
		return fmt.Errorf("manifest d'export manquant (archive invalide ?): %w", err)
	}
	if err := json.Unmarshal(mBuf, &manifest); err != nil {
		return fmt.Errorf("manifest corrompu: %w", err)
	}
	appID := manifest.App
	if appID == "" {
		return fmt.Errorf("manifest sans app")
	}
	fmt.Printf("  → App: %s | exportée en %s (Caleope %s) | %d image(s)\n",
		appID, manifest.ExportedAt.Format("2006-01-02"), manifest.CaleopeVersion, len(manifest.Images))

	// 2. images : docker load (hors-ligne) sinon re-pull
	fmt.Println("  [2/6] Restauration des images...")
	imagesDir := filepath.Join(payload, "images")
	if manifest.HasImages && dirExists(imagesDir) {
		if err := m.docker.LoadImagesDir(imagesDir); err != nil {
			return fmt.Errorf("docker load: %w", err)
		}
	} else {
		for _, ref := range manifest.Images {
			if !m.docker.ImageExists(ref) {
				if err := m.docker.PullImage(ref); err != nil {
					fmt.Printf("     ⚠ image %s non chargée (%v)\n", ref, err)
				}
			}
		}
	}

	// 3. définition → apps-installed/<app>/
	fmt.Println("  [3/6] Restauration de la définition (compose)...")
	installedDst := filepath.Join(m.baseDir, "apps-installed", appID)
	if err := copyTree(filepath.Join(payload, "definition", "installed"), installedDst); err != nil {
		return fmt.Errorf("restauration compose: %w", err)
	}

	// 4. données + config
	fmt.Println("  [4/6] Restauration des données + config...")
	if fileExists(filepath.Join(payload, "data.tar.gz")) {
		if err := extractTarGz(filepath.Join(payload, "data.tar.gz"), filepath.Join(m.baseDir, "app-data")); err != nil {
			return fmt.Errorf("restauration données: %w", err)
		}
	}
	if fileExists(filepath.Join(payload, "config.tar.gz")) {
		if err := extractTarGz(filepath.Join(payload, "config.tar.gz"), filepath.Join(m.baseDir, "app-config")); err != nil {
			return fmt.Errorf("restauration config: %w", err)
		}
	}

	// 5. état runtime
	fmt.Println("  [5/6] Enregistrement dans le runtime...")
	_ = os.MkdirAll(filepath.Join(m.baseDir, "runtime", "apps"), 0755)
	_ = copyFile(filepath.Join(payload, "definition", "runtime.json"),
		filepath.Join(m.baseDir, "runtime", "apps", appID+".json"))

	// 6. migration éventuelle (mode migrate) puis démarrage
	if mode == "migrate" {
		if applied := m.tryMigrate(appID, installedDst, manifest); applied {
			fmt.Println("  → Migration vers le standard courant appliquée.")
		} else {
			fmt.Println("  → Pas de migration disponible → restauration LEGACY (à l'identique).")
		}
	}
	fmt.Println("  [6/6] Démarrage de l'app...")
	if err := m.docker.Up(installedDst); err != nil {
		return fmt.Errorf("démarrage: %w", err)
	}
	fmt.Printf("✅ App '%s' restaurée depuis l'export.\n", appID)
	return nil
}

// tryMigrate lance apps/<app>/migrate.sh du store courant si présent (mode migrate).
// Retourne true si une migration a été appliquée. Best-effort ; en cas d'échec on
// reste en legacy (l'app démarre avec sa définition figée).
func (m *Manager) tryMigrate(appID, installedDst string, manifest ExportManifest) bool {
	migrate := filepath.Join(m.baseDir, "core", "cache", "official", "apps", appID, "migrate.sh")
	if !fileExists(migrate) {
		return false
	}
	cmd := exec.Command("/bin/bash", migrate)
	cmd.Dir = installedDst
	cmd.Env = append(os.Environ(),
		"CALEOPE_BASE_DIR="+m.baseDir,
		"CALEOPE_APP_ID="+appID,
		"CALEOPE_FROM_VERSION="+manifest.CaleopeVersion,
	)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Printf("     ⚠ migrate.sh a échoué (%v) → legacy\n", err)
		return false
	}
	return true
}

// ─── helpers fichiers ───────────────────────────────────────────────────────

func dirExists(p string) bool  { fi, err := os.Stat(p); return err == nil && fi.IsDir() }
func fileExists(p string) bool { fi, err := os.Stat(p); return err == nil && !fi.IsDir() }

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}

// copyTree copie le CONTENU de src dans dst (dst créé au besoin). No-op si src absent.
// `cp -a src/. dst` copie le contenu (fichiers cachés inclus) sans ambiguïté
// dossier-dans-dossier. Préserve permissions/liens/timestamps.
func copyTree(src, dst string) error {
	if !dirExists(src) {
		return nil
	}
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	if out, err := exec.Command("cp", "-a", src+"/.", dst).CombinedOutput(); err != nil {
		return fmt.Errorf("cp -a %s → %s: %w\n%s", src, dst, err, string(out))
	}
	return nil
}
