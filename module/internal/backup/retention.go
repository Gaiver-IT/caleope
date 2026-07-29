// internal/backup/retention.go
//
// 🧹 RÉTENTION DES SAUVEGARDES
//
// Une sauvegarde planifiée sans rétention remplit mécaniquement le disque. Sur
// un serveur auto-hébergé que personne ne surveille, c'est la panne la plus
// bête et la plus coûteuse : elle arrive sans témoin, et elle emporte les
// applications avec elle.
//
// La purge suit trois règles de sûreté, dans cet ordre :
//   1. elle ne s'exécute QU'APRÈS une sauvegarde réussie — jamais en balayage
//      de fond. Une application qu'on ne sauvegarde plus voit donc son archive
//      intacte, ce qui évite de détruire l'historique de quelqu'un qui a changé
//      d'avis ;
//   2. elle ne supprime JAMAIS la sauvegarde la plus récente, quelle que soit
//      la politique — même mal configurée, on ne laisse pas l'utilisateur sans
//      aucun point de restauration ;
//   3. un échec de purge n'échoue jamais la sauvegarde. La sauvegarde a réussi,
//      c'est ce qui compte ; le ménage est secondaire.

package backup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gaiver-it/caleope/pkg/types"
)

func (m *Manager) retentionPath() string {
	return filepath.Join(m.baseDir, "core", "backup-retention.json")
}

// Retention lit la politique en vigueur. En l'absence de fichier — cas de toute
// installation existante — on renvoie le défaut plutôt que « aucune purge » :
// laisser grossir sans limite est précisément le comportement qu'on corrige.
func (m *Manager) Retention() types.RetentionPolicy {
	data, err := os.ReadFile(m.retentionPath())
	if err != nil {
		return types.DefaultRetention()
	}
	var p types.RetentionPolicy
	if err := json.Unmarshal(data, &p); err != nil {
		return types.DefaultRetention()
	}
	if p.KeepLast < 0 {
		p.KeepLast = 0
	}
	if p.KeepDays < 0 {
		p.KeepDays = 0
	}
	return p
}

// SetRetention enregistre la politique.
func (m *Manager) SetRetention(p types.RetentionPolicy) error {
	if p.KeepLast < 0 || p.KeepDays < 0 {
		return fmt.Errorf("les valeurs de rétention ne peuvent pas être négatives")
	}
	dir := filepath.Dir(m.retentionPath())
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("création du répertoire de configuration: %w", err)
	}
	data, _ := json.MarshalIndent(p, "", "  ")
	if err := os.WriteFile(m.retentionPath(), data, 0644); err != nil {
		return fmt.Errorf("écriture de la politique de rétention: %w", err)
	}
	return nil
}

// selectExpired décide, parmi des sauvegardes triées de la plus récente à la
// plus ancienne, lesquelles peuvent être supprimées.
//
// Extrait comme fonction pure : c'est la partie où une erreur détruit des
// données, elle doit donc être testable sans toucher au disque.
func selectExpired(manifests []types.BackupManifest, p types.RetentionPolicy, now time.Time) []types.BackupManifest {
	if !p.Enabled() || len(manifests) <= 1 {
		return nil
	}
	var expired []types.BackupManifest
	for i, bm := range manifests {
		// Règle 2 : la plus récente est intouchable.
		if i == 0 {
			continue
		}
		keep := false
		if p.KeepLast > 0 && i < p.KeepLast {
			keep = true
		}
		if p.KeepDays > 0 && !bm.Timestamp.IsZero() &&
			now.Sub(bm.Timestamp) < time.Duration(p.KeepDays)*24*time.Hour {
			keep = true
		}
		// Une sauvegarde sans horodatage exploitable ne doit pas être supprimée
		// sur un critère d'âge qu'on ne sait pas évaluer.
		if bm.Timestamp.IsZero() && p.KeepLast == 0 {
			keep = true
		}
		if !keep {
			expired = append(expired, bm)
		}
	}
	return expired
}

// ApplyRetention supprime les sauvegardes hors politique pour une application
// et renvoie les répertoires effectivement supprimés.
func (m *Manager) ApplyRetention(appID string) ([]string, error) {
	p := m.Retention()
	if !p.Enabled() {
		return nil, nil
	}
	manifests, err := m.ListBackups(appID)
	if err != nil {
		return nil, err
	}
	expired := selectExpired(manifests, p, time.Now())
	var deleted []string
	for _, bm := range expired {
		if bm.Dir == "" {
			continue
		}
		if err := m.DeleteBackup(appID, bm.Dir); err != nil {
			// On continue : une suppression qui échoue ne doit pas empêcher les
			// suivantes de libérer de la place.
			fmt.Printf("  ⚠ purge de %s/%s : %v\n", appID, bm.Dir, err)
			continue
		}
		deleted = append(deleted, bm.Dir)
	}
	return deleted, nil
}
