package main

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/gaiver-it/caleope/pkg/version"
)

// cmdOfflinePack crée un bundle d'installation offline.
//
// Usage : caleope offline-pack <dest>
//
// Structure du bundle créé :
//
//	caleope-bundle-YYYY-MM-DD/
//	  binaries/        caleoped, caleope, caleope-ui
//	  store.tar.gz     archive du store officiel local
//	  images/          images Docker sauvegardées (.tar)
//	  caleope-completion.bash
//	  pack-info.json   métadonnées (version, date, arch)
func cmdOfflinePack(args []string) {
	if len(args) < 1 {
		die("Usage : caleope offline-pack <destination>\n\nCrée un bundle d'installation offline dans le répertoire spécifié.")
	}

	dest := args[0]
	bundleName := "caleope-bundle-" + time.Now().Format("2006-01-02")
	bundleDir := filepath.Join(dest, bundleName)

	fmt.Printf("📦 Création du bundle submarine dans %s\n\n", bundleDir)

	if err := os.MkdirAll(bundleDir, 0755); err != nil {
		die("Impossible de créer le répertoire du bundle : " + err.Error())
	}

	packStep("Copie des binaires")
	if err := packBinaries(bundleDir); err != nil {
		die("Erreur binaires : " + err.Error())
	}

	packStep("Archivage du store")
	if err := packStore(bundleDir); err != nil {
		fmt.Fprintf(os.Stderr, "⚠  Store : %v — ignoré\n", err)
	}

	packStep("Sauvegarde des images Docker")
	if err := packDockerImages(bundleDir); err != nil {
		fmt.Fprintf(os.Stderr, "⚠  Images Docker : %v — ignoré\n", err)
	}

	packStep("Copie des scripts")
	copyCompletionScript(bundleDir)

	packStep("Écriture de pack-info.json")
	if err := writePackInfo(bundleDir); err != nil {
		fmt.Fprintf(os.Stderr, "⚠  pack-info.json : %v\n", err)
	}

	fmt.Printf("\n✅ Bundle prêt : %s\n", bundleDir)
	fmt.Printf("   Pour installer depuis ce bundle :\n")
	fmt.Printf("   sudo bash install.sh --offline %s\n\n", bundleDir)
	fmt.Printf("   Pour mettre à jour :\n")
	fmt.Printf("   caleope offline-update %s\n", bundleDir)
}

// cmdOfflineUpdate applique un bundle pour mettre à jour une installation existante.
//
// Usage : caleope offline-update <bundle-path>
func cmdOfflineUpdate(args []string) {
	if len(args) < 1 {
		die("Usage : caleope offline-update <bundle-path>")
	}

	bundleDir := args[0]
	if _, err := os.Stat(bundleDir); err != nil {
		die("Bundle introuvable : " + bundleDir)
	}

	fmt.Printf("🔄 Mise à jour offline depuis %s\n\n", bundleDir)

	packStep("Mise à jour des binaires")
	if err := packBinaries(bundleDir); err != nil {
		die("Erreur binaires : " + err.Error())
	} else {
		applyBinariesFromBundle(bundleDir)
	}

	packStep("Mise à jour du store")
	applyStoreFromBundle(bundleDir)

	packStep("Chargement des nouvelles images Docker")
	loadDockerImagesFromBundle(bundleDir)

	fmt.Printf("\n✅ Mise à jour terminée. Redémarre le daemon :\n")
	fmt.Printf("   sudo systemctl restart caleoped caleope-ui\n")
}

// ── helpers ──────────────────────────────────────────────────────────────────

func packStep(msg string) {
	fmt.Printf("  ▶ %s...\n", msg)
}

func packBinaries(bundleDir string) error {
	binDir := filepath.Join(bundleDir, "binaries")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		return err
	}

	bins := []string{
		"/usr/local/bin/caleoped",
		"/usr/local/bin/caleope",
		"/usr/local/bin/caleope-ui",
	}

	copied := 0
	for _, src := range bins {
		dst := filepath.Join(binDir, filepath.Base(src))
		if err := copyFile(src, dst); err != nil {
			if os.IsNotExist(err) {
				fmt.Printf("     ⚠  %s absent — ignoré\n", filepath.Base(src))
				continue
			}
			return fmt.Errorf("%s : %w", filepath.Base(src), err)
		}
		if err := os.Chmod(dst, 0755); err != nil {
			return err
		}
		fmt.Printf("     ✔  %s\n", filepath.Base(src))
		copied++
	}

	if copied == 0 {
		return fmt.Errorf("aucun binaire Caleope trouvé dans /usr/local/bin")
	}
	return nil
}

func packStore(bundleDir string) error {
	storeDir := "/opt/gaiver-it/caleope/core/cache/official"
	if _, err := os.Stat(storeDir); err != nil {
		return fmt.Errorf("store introuvable : %s", storeDir)
	}

	archivePath := filepath.Join(bundleDir, "store.tar.gz")
	f, err := os.Create(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()

	gw := gzip.NewWriter(f)
	defer gw.Close()
	tw := tar.NewWriter(gw)
	defer tw.Close()

	root := filepath.Clean(storeDir)
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		// Exclure le répertoire .git (inutile dans le bundle, trop volumineux)
		if info.IsDir() && info.Name() == ".git" {
			return filepath.SkipDir
		}

		rel, _ := filepath.Rel(filepath.Dir(root), path)
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = rel
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if !info.IsDir() {
			fh, err := os.Open(path)
			if err != nil {
				return err
			}
			defer fh.Close()
			_, err = io.Copy(tw, fh)
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}

	stat, _ := f.Stat()
	fmt.Printf("     ✔  store.tar.gz (%s)\n", formatSize(stat.Size()))
	return nil
}

func packDockerImages(bundleDir string) error {
	imagesDir := filepath.Join(bundleDir, "images")
	if err := os.MkdirAll(imagesDir, 0755); err != nil {
		return err
	}

	// Collecter toutes les images Docker locales (filtre sur les images Caleope + core)
	out, err := exec.Command("docker", "images", "--format", "{{.Repository}}:{{.Tag}}").Output()
	if err != nil {
		return fmt.Errorf("docker images inaccessible : %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 0 || (len(lines) == 1 && lines[0] == "") {
		fmt.Printf("     ⚠  aucune image Docker locale\n")
		return nil
	}

	saved := 0
	for _, img := range lines {
		img = strings.TrimSpace(img)
		if img == "" || strings.HasSuffix(img, ":<none>") || strings.HasPrefix(img, "<none>") {
			continue
		}

		// Nom de fichier sûr : remplace "/" et ":" par "_"
		safe := strings.NewReplacer("/", "_", ":", "_").Replace(img)
		tarPath := filepath.Join(imagesDir, safe+".tar")

		fmt.Printf("     ▸ docker save %s...\n", img)
		cmd := exec.Command("docker", "save", "-o", tarPath, img)
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "     ⚠  échec pour %s : %v\n", img, err)
			continue
		}
		saved++
	}

	fmt.Printf("     ✔  %d image(s) sauvegardée(s)\n", saved)
	return nil
}

func copyCompletionScript(bundleDir string) {
	src := "/etc/bash_completion.d/caleope"
	dst := filepath.Join(bundleDir, "caleope-completion.bash")
	if err := copyFile(src, dst); err != nil {
		fmt.Printf("     ⚠  script autocomplétion absent — ignoré\n")
		return
	}
	fmt.Printf("     ✔  caleope-completion.bash\n")
}

func writePackInfo(bundleDir string) error {
	arch, _ := exec.Command("uname", "-m").Output()
	hostname, _ := os.Hostname()

	info := map[string]string{
		"caleope_version": version.Version,
		"packed_at":       time.Now().Format("2006-01-02 15:04:05"),
		"arch":            strings.TrimSpace(string(arch)),
		"hostname":        hostname,
		"bundle_dir":      bundleDir,
	}

	f, err := os.Create(filepath.Join(bundleDir, "pack-info.json"))
	if err != nil {
		return err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(info)
}

func applyBinariesFromBundle(bundleDir string) {
	binDir := filepath.Join(bundleDir, "binaries")
	bins := []string{"caleoped", "caleope", "caleope-ui"}
	for _, b := range bins {
		src := filepath.Join(binDir, b)
		dst := filepath.Join("/usr/local/bin", b)
		if err := copyFile(src, dst); err == nil {
			_ = os.Chmod(dst, 0755)
			fmt.Printf("     ✔  %s mis à jour\n", b)
		}
	}
}

func applyStoreFromBundle(bundleDir string) {
	archive := filepath.Join(bundleDir, "store.tar.gz")
	if _, err := os.Stat(archive); err != nil {
		fmt.Printf("     ⚠  store.tar.gz absent du bundle\n")
		return
	}
	storeDir := "/opt/gaiver-it/caleope/core/cache/official"
	cmd := exec.Command("tar", "-xzf", archive, "-C", storeDir, "--strip-components=1")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "     ⚠  extraction store : %v\n", err)
		return
	}
	fmt.Printf("     ✔  store mis à jour\n")
}

func loadDockerImagesFromBundle(bundleDir string) {
	imagesDir := filepath.Join(bundleDir, "images")
	entries, err := os.ReadDir(imagesDir)
	if err != nil {
		fmt.Printf("     ⚠  répertoire images/ absent du bundle\n")
		return
	}
	loaded := 0
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".tar") {
			continue
		}
		tarPath := filepath.Join(imagesDir, e.Name())
		cmd := exec.Command("docker", "load", "-i", tarPath)
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "     ⚠  %s : %v\n", e.Name(), err)
			continue
		}
		fmt.Printf("     ✔  %s chargée\n", strings.TrimSuffix(e.Name(), ".tar"))
		loaded++
	}
	fmt.Printf("     ✔  %d image(s) chargée(s)\n", loaded)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

func formatSize(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}
