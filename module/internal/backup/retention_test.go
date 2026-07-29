package backup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gaiver-it/caleope/pkg/types"
)

// mk fabrique une liste de sauvegardes triées de la plus récente à la plus
// ancienne, espacées d'un jour, comme le ferait une sauvegarde quotidienne.
func mk(n int, now time.Time) []types.BackupManifest {
	out := make([]types.BackupManifest, n)
	for i := 0; i < n; i++ {
		out[i] = types.BackupManifest{
			App:       "test",
			Timestamp: now.Add(-time.Duration(i) * 24 * time.Hour),
			Dir:       time.Now().Add(-time.Duration(i) * 24 * time.Hour).Format("2006-01-02T15-04-05"),
		}
	}
	return out
}

func dirsOf(ms []types.BackupManifest) map[string]bool {
	s := map[string]bool{}
	for _, m := range ms {
		s[m.Dir] = true
	}
	return s
}

// LA règle de sûreté n°1 : ne jamais laisser l'utilisateur sans point de
// restauration, même avec une politique absurde.
func TestNeSupprimeJamaisLaPlusRecente(t *testing.T) {
	now := time.Now()
	for _, p := range []types.RetentionPolicy{
		{KeepLast: 1},
		{KeepDays: 1},
		{KeepLast: 1, KeepDays: 1},
	} {
		all := mk(10, now)
		expired := selectExpired(all, p, now)
		if dirsOf(expired)[all[0].Dir] {
			t.Fatalf("politique %+v : la sauvegarde la plus récente a été sélectionnée pour suppression", p)
		}
		if len(expired) >= len(all) {
			t.Fatalf("politique %+v : tout a été supprimé (%d/%d)", p, len(expired), len(all))
		}
	}
}

func TestKeepLast(t *testing.T) {
	now := time.Now()
	all := mk(10, now)
	expired := selectExpired(all, types.RetentionPolicy{KeepLast: 3}, now)
	if len(expired) != 7 {
		t.Fatalf("attendu 7 expirées sur 10 avec keep_last=3, obtenu %d", len(expired))
	}
	kept := dirsOf(expired)
	for i := 0; i < 3; i++ {
		if kept[all[i].Dir] {
			t.Errorf("la sauvegarde #%d aurait dû être conservée", i)
		}
	}
}

func TestKeepDays(t *testing.T) {
	now := time.Now()
	all := mk(10, now) // 0 à 9 jours d'âge
	expired := selectExpired(all, types.RetentionPolicy{KeepDays: 5}, now)
	// on garde ce qui a moins de 5 jours : indices 0..4 → 5 expirées
	if len(expired) != 5 {
		t.Fatalf("attendu 5 expirées avec keep_days=5, obtenu %d", len(expired))
	}
}

// Les deux critères se CUMULENT : en cas d'ambiguïté, on garde.
func TestCriteresCumulatifs(t *testing.T) {
	now := time.Now()
	all := mk(10, now)
	// keep_last=2 garderait 0,1 ; keep_days=5 garde 0..4 → union = 0..4
	expired := selectExpired(all, types.RetentionPolicy{KeepLast: 2, KeepDays: 5}, now)
	if len(expired) != 5 {
		t.Fatalf("l'union des deux critères devrait garder 5 sauvegardes, %d expirées", len(expired))
	}
	e := dirsOf(expired)
	for i := 0; i < 5; i++ {
		if e[all[i].Dir] {
			t.Errorf("#%d est couverte par keep_days et ne doit pas expirer", i)
		}
	}
}

func TestPolitiqueDesactivee(t *testing.T) {
	now := time.Now()
	if got := selectExpired(mk(50, now), types.RetentionPolicy{}, now); got != nil {
		t.Fatalf("politique vide : aucune purge attendue, %d sélectionnées", len(got))
	}
}

func TestUneSeuleSauvegarde(t *testing.T) {
	now := time.Now()
	if got := selectExpired(mk(1, now), types.RetentionPolicy{KeepLast: 1}, now); got != nil {
		t.Fatalf("une seule sauvegarde ne doit jamais être purgée, %d sélectionnées", len(got))
	}
}

// Un horodatage illisible ne doit pas conduire à supprimer sur un critère d'âge
// qu'on ne sait pas évaluer.
func TestHorodatageAbsentAvecCritereAge(t *testing.T) {
	now := time.Now()
	all := mk(4, now)
	all[2].Timestamp = time.Time{} // manifest corrompu
	expired := selectExpired(all, types.RetentionPolicy{KeepDays: 1}, now)
	if dirsOf(expired)[all[2].Dir] {
		t.Fatal("une sauvegarde sans horodatage a été purgée sur un critère d'âge")
	}
}

// Aller-retour de la politique sur disque, et défaut en l'absence de fichier.
func TestPersistancePolitique(t *testing.T) {
	m := &Manager{baseDir: t.TempDir()}
	if got := m.Retention(); got != types.DefaultRetention() {
		t.Fatalf("sans fichier, on attend le défaut %+v, obtenu %+v", types.DefaultRetention(), got)
	}
	want := types.RetentionPolicy{KeepLast: 3, KeepDays: 14}
	if err := m.SetRetention(want); err != nil {
		t.Fatalf("SetRetention: %v", err)
	}
	if got := m.Retention(); got != want {
		t.Fatalf("relecture = %+v, attendu %+v", got, want)
	}
	if err := m.SetRetention(types.RetentionPolicy{KeepLast: -1}); err == nil {
		t.Fatal("une valeur négative devrait être refusée")
	}
}

// ApplyRetention supprime pour de vrai : ce test manipule des fichiers sur
// disque, là où selectExpired ne raisonne que sur des structures.
func TestApplyRetentionSurDisque(t *testing.T) {
	base := t.TempDir()
	m := &Manager{baseDir: base}
	if err := m.SetRetention(types.RetentionPolicy{KeepLast: 3}); err != nil {
		t.Fatalf("SetRetention: %v", err)
	}

	now := time.Now()
	var dirs []string
	for i := 0; i < 6; i++ {
		ts := now.Add(-time.Duration(i) * 24 * time.Hour)
		name := ts.Format("2006-01-02T15-04-05")
		dir := filepath.Join(base, "backups", "demo", name)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		bm := types.BackupManifest{App: "demo", Timestamp: ts, HasData: true}
		data, _ := json.Marshal(bm)
		if err := os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0644); err != nil {
			t.Fatal(err)
		}
		// contenu factice, pour vérifier que la suppression est récursive
		_ = os.WriteFile(filepath.Join(dir, "data.tar.gz"), []byte("x"), 0644)
		dirs = append(dirs, dir)
	}

	deleted, err := m.ApplyRetention("demo")
	if err != nil {
		t.Fatalf("ApplyRetention: %v", err)
	}
	if len(deleted) != 3 {
		t.Fatalf("attendu 3 suppressions avec keep_last=3 sur 6, obtenu %d", len(deleted))
	}
	// les 3 plus récentes survivent, les 3 plus anciennes ont disparu
	for i := 0; i < 3; i++ {
		if _, err := os.Stat(dirs[i]); err != nil {
			t.Errorf("la sauvegarde récente #%d a été supprimée à tort", i)
		}
	}
	for i := 3; i < 6; i++ {
		if _, err := os.Stat(dirs[i]); !os.IsNotExist(err) {
			t.Errorf("la sauvegarde ancienne #%d aurait dû être supprimée", i)
		}
	}

	// Idempotence : rejouer ne doit plus rien supprimer.
	again, err := m.ApplyRetention("demo")
	if err != nil {
		t.Fatalf("second passage: %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("second passage : %d suppressions, attendu 0", len(again))
	}

	// Une app sans aucune sauvegarde ne doit pas produire d'erreur.
	if _, err := m.ApplyRetention("inconnue"); err != nil {
		t.Fatalf("app sans sauvegarde: %v", err)
	}
}
