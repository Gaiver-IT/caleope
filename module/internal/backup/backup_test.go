// internal/backup/backup_test.go
//
// Tests de la partie déterministe du module backup : archivage tar.gz
// (intégrité des données), listing/tri des sauvegardes, suppression sûre.
//
// Les opérations live (Backup/Restore/ResticBackup) dépendent de Docker et
// du runtime ; elles ne sont pas couvertes ici (testées en bout-en-bout).

package backup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gaiver-it/caleope/pkg/types"
)

// writeManifest crée backups/<app>/<dir>/manifest.json avec le timestamp donné.
func writeManifest(t *testing.T, base, app, dir string, ts time.Time) {
	t.Helper()
	d := filepath.Join(base, "backups", app, dir)
	if err := os.MkdirAll(d, 0755); err != nil {
		t.Fatal(err)
	}
	m := types.BackupManifest{App: app, AppName: app, Timestamp: ts}
	data, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(filepath.Join(d, "manifest.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
}

// ─────────────────────────────────────────────
// tarGz / extractTarGz : intégrité des données
// ─────────────────────────────────────────────

func TestTarGzRoundtripPreservesData(t *testing.T) {
	tmp := t.TempDir()

	// Arborescence source : myapp/{hello.txt, sub/nested.txt}
	srcParent := filepath.Join(tmp, "src")
	src := filepath.Join(srcParent, "myapp")
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "hello.txt"), []byte("bonjour"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "sub", "nested.txt"), []byte("imbriqué"), 0644); err != nil {
		t.Fatal(err)
	}

	// Archiver
	archive := filepath.Join(tmp, "myapp.tar.gz")
	if err := tarGz(src, archive); err != nil {
		t.Fatalf("tarGz: %v", err)
	}
	if fi, err := os.Stat(archive); err != nil || fi.Size() == 0 {
		t.Fatalf("archive absente ou vide: %v", err)
	}

	// Extraire ailleurs
	destParent := filepath.Join(tmp, "dest")
	if err := extractTarGz(archive, destParent); err != nil {
		t.Fatalf("extractTarGz: %v", err)
	}

	// Le dossier "myapp" doit être recréé avec son contenu intact
	got, err := os.ReadFile(filepath.Join(destParent, "myapp", "hello.txt"))
	if err != nil || string(got) != "bonjour" {
		t.Fatalf("hello.txt restauré = %q (err=%v), attendu \"bonjour\"", got, err)
	}
	got, err = os.ReadFile(filepath.Join(destParent, "myapp", "sub", "nested.txt"))
	if err != nil || string(got) != "imbriqué" {
		t.Fatalf("nested.txt restauré = %q (err=%v), attendu \"imbriqué\"", got, err)
	}
}

func TestTarGzMissingSourceFails(t *testing.T) {
	tmp := t.TempDir()
	err := tarGz(filepath.Join(tmp, "nexistepas"), filepath.Join(tmp, "out.tar.gz"))
	if err == nil {
		t.Fatal("tarGz sur source inexistante aurait dû échouer")
	}
}

// ─────────────────────────────────────────────
// ListBackups : parsing + tri (récent → ancien)
// ─────────────────────────────────────────────

func TestListBackupsSortedRecentFirst(t *testing.T) {
	base := t.TempDir()
	m := NewManager(nil, nil, base)

	old := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	mid := time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
	recent := time.Date(2026, 6, 30, 8, 0, 0, 0, time.UTC)

	// Écrits dans le désordre exprès
	writeManifest(t, base, "nextcloud", "b-mid", mid)
	writeManifest(t, base, "nextcloud", "c-old", old)
	writeManifest(t, base, "nextcloud", "a-recent", recent)

	list, err := m.ListBackups("nextcloud")
	if err != nil {
		t.Fatalf("ListBackups: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("%d backups, attendu 3", len(list))
	}
	if !list[0].Timestamp.Equal(recent) || !list[2].Timestamp.Equal(old) {
		t.Fatalf("tri incorrect: [0]=%v [2]=%v", list[0].Timestamp, list[2].Timestamp)
	}
	// Le champ Dir doit refléter le nom réel du répertoire
	if list[0].Dir != "a-recent" {
		t.Fatalf("Dir du plus récent = %q, attendu \"a-recent\"", list[0].Dir)
	}
}

func TestListBackupsEmptyForUnknownApp(t *testing.T) {
	m := NewManager(nil, nil, t.TempDir())
	list, err := m.ListBackups("jamais-installé")
	if err != nil {
		t.Fatalf("ListBackups devrait retourner liste vide sans erreur, got: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("attendu 0 backup, got %d", len(list))
	}
}

func TestListBackupsIgnoresStrayFiles(t *testing.T) {
	base := t.TempDir()
	m := NewManager(nil, nil, base)
	writeManifest(t, base, "ghost", "ok", time.Now())
	// Un fichier parasite à côté des dossiers de backup
	stray := filepath.Join(base, "backups", "ghost", "notes.txt")
	if err := os.WriteFile(stray, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	list, err := m.ListBackups("ghost")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("le fichier parasite aurait dû être ignoré, got %d entrées", len(list))
	}
}

// ─────────────────────────────────────────────
// latestBackup
// ─────────────────────────────────────────────

func TestLatestBackup(t *testing.T) {
	base := t.TempDir()
	m := NewManager(nil, nil, base)

	recent := time.Date(2026, 6, 30, 8, 0, 0, 0, time.UTC)
	writeManifest(t, base, "ghost", "vieux", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	writeManifest(t, base, "ghost", "neuf", recent)

	ts, err := m.latestBackup("ghost")
	if err != nil {
		t.Fatalf("latestBackup: %v", err)
	}
	if want := recent.Format("2006-01-02T15-04-05"); ts != want {
		t.Fatalf("latestBackup = %q, attendu %q", ts, want)
	}
}

func TestLatestBackupNoneFails(t *testing.T) {
	m := NewManager(nil, nil, t.TempDir())
	if _, err := m.latestBackup("rien"); err == nil {
		t.Fatal("latestBackup sans backup aurait dû échouer")
	}
}

// ─────────────────────────────────────────────
// DeleteBackup : suppression + garde-fou anti-chemin
// ─────────────────────────────────────────────

func TestDeleteBackupRemovesDir(t *testing.T) {
	base := t.TempDir()
	m := NewManager(nil, nil, base)
	writeManifest(t, base, "ghost", "2026-06-30T08-00-00", time.Now())

	if err := m.DeleteBackup("ghost", "2026-06-30T08-00-00"); err != nil {
		t.Fatalf("DeleteBackup: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "backups", "ghost", "2026-06-30T08-00-00")); !os.IsNotExist(err) {
		t.Fatal("le dossier de backup existe encore après suppression")
	}
}

func TestDeleteBackupRejectsPathTraversal(t *testing.T) {
	m := NewManager(nil, nil, t.TempDir())
	for _, bad := range []string{"../../etc", "a/b", `a\b`} {
		if err := m.DeleteBackup("ghost", bad); err == nil {
			t.Errorf("DeleteBackup(%q) aurait dû être rejeté (anti-traversal)", bad)
		}
	}
}

func TestDeleteBackupRejectsEmptyArgs(t *testing.T) {
	m := NewManager(nil, nil, t.TempDir())
	if err := m.DeleteBackup("", "x"); err == nil {
		t.Error("DeleteBackup avec app vide aurait dû échouer")
	}
	if err := m.DeleteBackup("ghost", ""); err == nil {
		t.Error("DeleteBackup avec dir vide aurait dû échouer")
	}
}

func TestDeleteBackupMissingFails(t *testing.T) {
	m := NewManager(nil, nil, t.TempDir())
	if err := m.DeleteBackup("ghost", "inexistant"); err == nil {
		t.Fatal("DeleteBackup sur backup inexistant aurait dû échouer")
	}
}
