// internal/vms/appliance.go
//
// 📦 APPLIANCES — VM créée depuis une ISO officielle + preseed d'auto-install.
//
// Une app de type "appliance" (cf types.ApplianceSpec) n'est pas un docker-compose
// mais une VM : on télécharge une ISO d'installation officielle (ex: netinst
// Debian), on vérifie son SHA256, puis on lance virt-install en mode installation
// automatique (preseed injecté dans l'initrd). Le système s'installe seul puis,
// au premier boot, exécute son script d'appliance (ex: xivo_install.sh officiel).
//
// Aucune ISO n'est hébergée par Caleope : tout vient de sources officielles
// (Debian de cdimage.debian.org, l'appliance de son éditeur). L'intégrité est
// garantie par le SHA256 du manifeste.

package vms

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gaiver-it/caleope/pkg/types"
)

func (m *Manager) isoDir() string { return filepath.Join(m.vmsDir(), "iso") }

// InstallAppliance télécharge l'ISO (vérifiée SHA256), puis crée+démarre la VM
// en installation automatique (preseed). `name` = nom de la VM (= app ID).
// `preseedFile` = chemin local du preseed à injecter ("" = pas de preseed,
// installation manuelle via VNC).
func (m *Manager) InstallAppliance(name string, spec types.ApplianceSpec, preseedFile string) (string, error) {
	if err := m.requireReady(); err != nil {
		return "", err
	}
	if !nameRe.MatchString(name) {
		return "", fmt.Errorf("nom d'appliance invalide (a-z, 0-9, - et _)")
	}
	if strings.TrimSpace(spec.ISOURL) == "" {
		return "", fmt.Errorf("appliance: iso_url manquant dans le manifeste")
	}
	if strings.TrimSpace(spec.ISOSha256) == "" {
		return "", fmt.Errorf("appliance: iso_sha256 manquant (intégrité obligatoire)")
	}
	if spec.VCPUs <= 0 {
		spec.VCPUs = 2
	}
	if spec.MemMB <= 0 {
		spec.MemMB = 2048
	}
	if spec.DiskGB <= 0 {
		spec.DiskGB = 20
	}

	// 1) Récupérer l'ISO (cache par SHA256, re-téléchargée si absente/corrompue)
	iso, err := m.fetchISO(spec.ISOURL, spec.ISOSha256)
	if err != nil {
		return "", err
	}

	// 2) Préparer le disque
	if err := os.MkdirAll(m.vmsDir(), 0o755); err != nil {
		return "", err
	}
	disk := filepath.Join(m.vmsDir(), name+".qcow2")
	if _, err := os.Stat(disk); err == nil {
		return "", fmt.Errorf("un disque pour '%s' existe déjà", name)
	}

	netArg := "network=default"
	if spec.Network == "bridge" {
		netArg = "bridge=br0"
	}

	args := []string{
		"--name", name,
		"--vcpus", strconv.Itoa(spec.VCPUs),
		"--memory", strconv.Itoa(spec.MemMB),
		"--disk", fmt.Sprintf("path=%s,size=%d,format=qcow2", disk, spec.DiskGB),
		"--osinfo", "detect=on,require=off",
		"--graphics", "vnc,listen=127.0.0.1",
		"--network", netArg,
		"--noautoconsole",
	}

	if preseedFile != "" {
		// Installation AUTOMATIQUE : --location extrait le noyau/initrd de l'ISO,
		// --initrd-inject glisse le preseed dans l'initrd, --extra-args dit à
		// l'installeur Debian de le charger sans interaction.
		extra := "auto=true priority=critical preseed/file=/preseed.cfg"
		if strings.TrimSpace(spec.ExtraArgs) != "" {
			extra += " " + strings.TrimSpace(spec.ExtraArgs)
		}
		args = append(args,
			"--location", iso,
			"--initrd-inject", preseedFile,
			"--extra-args", extra,
		)
	} else {
		// Pas de preseed : boot classique sur l'ISO (install manuelle via VNC).
		args = append(args, "--cdrom", iso)
	}

	if out, err := exec.Command("virt-install", args...).CombinedOutput(); err != nil {
		return "", fmt.Errorf("virt-install: %s", strings.TrimSpace(string(out)))
	}
	return name, nil
}

// fetchISO renvoie le chemin local de l'ISO, en la téléchargeant si nécessaire.
// Cache par SHA256 : si un fichier au bon hash existe déjà, on le réutilise.
func (m *Manager) fetchISO(url, wantSum string) (string, error) {
	if err := os.MkdirAll(m.isoDir(), 0o755); err != nil {
		return "", err
	}
	wantSum = strings.ToLower(strings.TrimSpace(wantSum))
	dst := filepath.Join(m.isoDir(), wantSum+".iso")

	// Déjà en cache et intègre ?
	if sum, err := fileSHA256(dst); err == nil && sum == wantSum {
		return dst, nil
	}

	// Télécharger vers un fichier temporaire, vérifier, puis renommer (atomique).
	tmp := dst + ".part"
	_ = os.Remove(tmp)
	out, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: 30 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		out.Close()
		_ = os.Remove(tmp)
		return "", fmt.Errorf("téléchargement ISO: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		out.Close()
		_ = os.Remove(tmp)
		return "", fmt.Errorf("téléchargement ISO: HTTP %d", resp.StatusCode)
	}
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(out, h), resp.Body); err != nil {
		out.Close()
		_ = os.Remove(tmp)
		return "", fmt.Errorf("téléchargement ISO: %w", err)
	}
	out.Close()
	got := hex.EncodeToString(h.Sum(nil))
	if got != wantSum {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("ISO corrompue: SHA256 attendu %s, obtenu %s", wantSum, got)
	}
	if err := os.Rename(tmp, dst); err != nil {
		return "", err
	}
	return dst, nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
