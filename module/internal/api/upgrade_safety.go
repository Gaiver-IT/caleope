// internal/api/upgrade_safety.go
//
// 🪂 FILET DE SÉCURITÉ DE LA MISE À JOUR
//
// La mise à jour remplace les trois binaires en service puis redémarre les
// services. Sans garde-fou, ce chemin est le plus dangereux du produit :
//   • il écrasait l'ancien binaire sans le conserver — une release cassée
//     laissait la machine sans daemon ET sans CLI pour la réparer ;
//   • rien ne vérifiait que le fichier téléchargé était même un exécutable
//     valide (un téléchargement tronqué ou une page d'erreur HTML enregistrée
//     à la place du binaire passaient sans bruit) ;
//   • rien ne vérifiait que le service redémarrait effectivement ;
//   • et la tâche planifiée « upgrade » rend tout cela automatique, la nuit,
//     chez des utilisateurs — pendant que l'éditeur peut être absent.
//
// Trois protections, du moins cher au plus utile :
//   1. CONTRÔLE AVANT BASCULE : on refuse d'installer un fichier qui n'est pas
//      un exécutable Linux x86-64 plausible. Coût quasi nul, attrape la
//      majorité des échecs réels.
//   2. CONSERVATION DE L'ANCIEN : les binaires en service sont copiés en .prev
//      avant d'être remplacés. C'est ce qui rend un retour arrière possible.
//   3. VÉRIFICATION APRÈS REDÉMARRAGE + RETOUR ARRIÈRE AUTOMATIQUE : un script
//      détaché redémarre, attend que le daemon réponde, et restaure les .prev
//      s'il ne répond pas. La logique de secours doit vivre HORS du processus
//      qu'on remplace — sinon elle meurt avec lui.

package api

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// minBinarySize : plancher volontairement bas. Les binaires réels pèsent une
// dizaine de mégaoctets ; on cherche seulement à écarter les pages d'erreur HTML
// et les téléchargements tronqués, pas à deviner la taille attendue.
const minBinarySize = 1 << 20 // 1 Mio

// managedBinaries liste les exécutables remplacés par une mise à jour.
var managedBinaries = []string{"caleoped", "caleope", "caleope-ui"}

// binDir est extrait en variable pour que les tests puissent l'isoler.
var binDir = "/usr/local/bin"

// preflightBinary refuse un fichier qui n'est pas un exécutable Linux x86-64
// plausible. On valide l'en-tête ELF plutôt que d'exécuter le fichier : c'est
// suffisant pour attraper les échecs réels (troncature, page HTML enregistrée à
// la place du binaire, mauvaise architecture) et ça n'exécute rien d'inconnu.
func preflightBinary(path string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("fichier téléchargé introuvable: %w", err)
	}
	if fi.Size() < minBinarySize {
		return fmt.Errorf("fichier suspect (%d octets, minimum attendu %d) — téléchargement tronqué ou page d'erreur",
			fi.Size(), minBinarySize)
	}

	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	hdr := make([]byte, 20)
	if _, err := io.ReadFull(f, hdr); err != nil {
		return fmt.Errorf("lecture de l'en-tête impossible: %w", err)
	}
	if hdr[0] != 0x7f || hdr[1] != 'E' || hdr[2] != 'L' || hdr[3] != 'F' {
		return fmt.Errorf("ce n'est pas un exécutable ELF — le téléchargement a probablement renvoyé autre chose")
	}
	if hdr[4] != 2 {
		return fmt.Errorf("exécutable non 64 bits")
	}
	// e_machine, 16 bits à l'offset 18 (little-endian pour hdr[5]==1)
	machine := binary.LittleEndian.Uint16(hdr[18:20])
	const emX8664 = 0x3E
	if machine != emX8664 {
		return fmt.Errorf("architecture inattendue (e_machine=0x%X, attendu x86-64)", machine)
	}
	return nil
}

// backupBinary copie un binaire en service vers son .prev. Une copie, pas un
// déplacement : le fichier en service doit rester intact si la suite échoue.
func backupBinary(name string) error {
	src := filepath.Join(binDir, name)
	if _, err := os.Stat(src); err != nil {
		// Binaire absent (installation partielle) : rien à sauvegarder, ce n'est
		// pas une erreur — on ne veut pas bloquer une mise à jour pour ça.
		return nil
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("lecture de %s: %w", src, err)
	}
	dst := filepath.Join(binDir, name+".prev")
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, data, 0755); err != nil {
		return fmt.Errorf("écriture de %s: %w", tmp, err)
	}
	// Renommage final atomique : on ne laisse jamais un .prev à moitié écrit,
	// qui serait pire qu'aucun .prev puisqu'on croirait pouvoir revenir.
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("installation de %s: %w", dst, err)
	}
	return nil
}

// backupBinaries sauvegarde les trois binaires gérés.
func backupBinaries() error {
	for _, b := range managedBinaries {
		if err := backupBinary(b); err != nil {
			return err
		}
	}
	return nil
}

// hasPreviousBinaries indique si un retour arrière est possible.
func hasPreviousBinaries() bool {
	for _, b := range managedBinaries {
		if _, err := os.Stat(filepath.Join(binDir, b+".prev")); err == nil {
			return true
		}
	}
	return false
}

// restorePreviousBinaries remet les .prev en service. Utilisé par la commande
// de retour arrière manuelle ; la version automatique vit dans le script
// détaché, qui doit fonctionner même sans daemon.
func restorePreviousBinaries() ([]string, error) {
	if !hasPreviousBinaries() {
		return nil, fmt.Errorf("aucune version précédente conservée — rien à restaurer")
	}
	var restored []string
	for _, b := range managedBinaries {
		prev := filepath.Join(binDir, b+".prev")
		data, err := os.ReadFile(prev)
		if err != nil {
			continue // ce binaire n'a pas de précédent, on passe
		}
		dst := filepath.Join(binDir, b)
		tmp := dst + ".restore"
		if err := os.WriteFile(tmp, data, 0755); err != nil {
			return restored, fmt.Errorf("écriture de %s: %w", tmp, err)
		}
		if err := os.Rename(tmp, dst); err != nil {
			_ = os.Remove(tmp)
			return restored, fmt.Errorf("restauration de %s: %w", dst, err)
		}
		restored = append(restored, b)
	}
	return restored, nil
}

// launchUpgradeWatchdog écrit le script de surveillance sur disque et le lance
// dans un cgroup INDÉPENDANT via systemd-run.
//
// ⚠️ POURQUOI systemd-run ET PAS un simple exec.Command("bash", …) : caleoped.service
// n'a pas de KillMode explicite, donc systemd utilise « control-group » par défaut.
// Un processus lancé par le daemon appartient à son cgroup et se fait TUER par le
// « systemctl restart caleoped » que le script déclenche lui-même — le filet ne
// s'exécuterait jamais au-delà de sa première ligne utile, tout en donnant
// l'illusion d'exister. Le dépôt connaît déjà ce piège et le résout de la même
// façon pour la finalisation d'installation (cmd/caleope-ui/main.go).
//
// Repli sur bash si systemd-run est absent : mieux vaut un filet fragile que pas
// de filet, et l'appelant journalise la dégradation.
func (s *Server) launchUpgradeWatchdog(logPath string) error {
	scriptPath := filepath.Join(s.baseDir, "core", "upgrade-watchdog.sh")
	if err := os.MkdirAll(filepath.Dir(scriptPath), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(scriptPath, []byte(upgradeWatchdogScript(logPath)), 0700); err != nil {
		return err
	}
	if _, err := exec.LookPath("systemd-run"); err == nil {
		cmd := exec.Command("systemd-run",
			"--unit=caleope-upgrade-watchdog", "--collect",
			"/bin/bash", scriptPath)
		if err := cmd.Start(); err == nil {
			return nil
		}
	}
	// Repli dégradé : le script risque d'être tué avec le daemon.
	return exec.Command("bash", scriptPath).Start()
}

// upgradeWatchdogScript construit le script qui redémarre les services, vérifie
// que le daemon répond, et restaure les binaires précédents sinon.
//
// Il doit être autonome : au moment où il tourne, le processus qui l'a lancé est
// mort (systemctl restart caleoped le tue). Il n'utilise donc que des outils du
// système et des chemins absolus, et il journalise tout — c'est la seule trace
// disponible si la machine se répare seule pendant une absence.
func upgradeWatchdogScript(logPath string) string {
	return fmt.Sprintf(`set -u
LOG=%[1]q
BIN=%[2]q
log() { printf '[%%s] %%s\n' "$(date -Is)" "$*" >> "$LOG" 2>/dev/null || true; }

restore() {
  log "RETOUR ARRIÈRE : le daemon n'a pas répondu, restauration de la version précédente"
  n=0
  attendus=0
  for b in caleoped caleope caleope-ui; do
    if [ -f "$BIN/$b.prev" ]; then
      attendus=$((attendus+1))
      # Copie vers un temporaire puis renommage : on ne laisse jamais un binaire
      # à moitié écrit à la place d'un binaire qui, lui, démarrait au moins.
      if cp -f "$BIN/$b.prev" "$BIN/$b.restore" && chmod 755 "$BIN/$b.restore" \
         && mv -f "$BIN/$b.restore" "$BIN/$b"; then
        n=$((n+1)); log "  restauré : $b"
      else
        rm -f "$BIN/$b.restore" 2>/dev/null
        log "  ⚠ ÉCHEC de la restauration de $b"
      fi
    fi
  done
  systemctl restart caleoped >/dev/null 2>&1 || log "  ⚠ restart caleoped a échoué"
  sleep 3
  systemctl restart caleope-ui >/dev/null 2>&1 || log "  ⚠ restart caleope-ui a échoué"

  # Ne pas annoncer un succès qu'on n'a pas constaté : on revérifie réellement.
  ok2=0
  for j in $(seq 1 10); do
    if systemctl is-active --quiet caleoped && "$BIN/caleope" ping >/dev/null 2>&1; then
      ok2=1; break
    fi
    sleep 3
  done
  if [ "$n" -eq "$attendus" ] && [ "$ok2" = "1" ]; then
    log "RETOUR ARRIÈRE terminé — la machine tourne sur la version précédente"
  else
    log "ÉTAT CRITIQUE : $n/$attendus binaires restaurés, daemon répond=$ok2"
    log "  → intervention manuelle nécessaire : caleope rollback, ou réinstallation"
  fi
}

sleep 2
log "=== redémarrage des services après mise à jour ==="
systemctl restart caleoped >/dev/null 2>&1 || log "⚠ restart caleoped a retourné une erreur"
sleep 2
systemctl restart caleope-ui >/dev/null 2>&1 || log "⚠ restart caleope-ui a retourné une erreur"

# Contrôle de santé : le service doit être actif ET le daemon doit répondre.
# 20 tentatives espacées de 3 s = 60 s, largement de quoi couvrir un démarrage lent.
ok=0
for i in $(seq 1 20); do
  if systemctl is-active --quiet caleoped && "$BIN/caleope" ping >/dev/null 2>&1; then
    ok=1
    break
  fi
  sleep 3
done

if [ "$ok" = "1" ]; then
  log "SANTÉ OK — mise à jour confirmée"
else
  log "SANTÉ ÉCHOUÉE après 60 s"
  restore
fi
`, logPath, binDir)
}
