package api

import (
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeELF fabrique un fichier ressemblant à un exécutable Linux x86-64 valide.
func fakeELF(t *testing.T, path string, size int, machine uint16, class byte) {
	t.Helper()
	buf := make([]byte, size)
	buf[0], buf[1], buf[2], buf[3] = 0x7f, 'E', 'L', 'F'
	buf[4] = class
	buf[5] = 1 // little-endian
	binary.LittleEndian.PutUint16(buf[18:20], machine)
	if err := os.WriteFile(path, buf, 0755); err != nil {
		t.Fatal(err)
	}
}

// withBinDir isole binDir dans un répertoire temporaire : ces tests ne doivent
// JAMAIS toucher aux binaires réels de la machine.
func withBinDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	old := binDir
	binDir = dir
	t.Cleanup(func() { binDir = old })
	return dir
}

func TestPreflightAccepteUnBinaireValide(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "ok")
	fakeELF(t, p, minBinarySize+1024, 0x3E, 2)
	if err := preflightBinary(p); err != nil {
		t.Fatalf("un binaire valide a été refusé : %v", err)
	}
}

// Le cas réel le plus fréquent : le téléchargement renvoie une page d'erreur ou
// s'interrompt. Le fichier existe, il est petit, et il n'est pas un exécutable.
func TestPreflightRefuseUnePageHTML(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "html")
	if err := os.WriteFile(p, []byte("<html><body>404 Not Found</body></html>"), 0755); err != nil {
		t.Fatal(err)
	}
	err := preflightBinary(p)
	if err == nil {
		t.Fatal("une page HTML a été acceptée comme binaire")
	}
	if !strings.Contains(err.Error(), "tronqué") && !strings.Contains(err.Error(), "suspect") {
		t.Logf("message : %v", err)
	}
}

func TestPreflightRefuseUnFichierTronque(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "court")
	// ELF valide en en-tête mais bien trop petit pour être un vrai binaire Go
	fakeELF(t, p, 4096, 0x3E, 2)
	if err := preflightBinary(p); err == nil {
		t.Fatal("un fichier tronqué a été accepté")
	}
}

func TestPreflightRefuseUneMauvaiseArchitecture(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "arm")
	const emAARCH64 = 0xB7
	fakeELF(t, p, minBinarySize+512, emAARCH64, 2)
	if err := preflightBinary(p); err == nil {
		t.Fatal("un binaire ARM a été accepté sur x86-64")
	}
}

func TestPreflightRefuseDu32Bits(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x86")
	fakeELF(t, p, minBinarySize+512, 0x3E, 1) // class 1 = 32 bits
	if err := preflightBinary(p); err == nil {
		t.Fatal("un binaire 32 bits a été accepté")
	}
}

func TestPreflightRefuseUnFichierAbsent(t *testing.T) {
	if err := preflightBinary(filepath.Join(t.TempDir(), "nexistepas")); err == nil {
		t.Fatal("un fichier absent a été accepté")
	}
}

// La sauvegarde doit être une COPIE : le binaire en service reste intact, sinon
// un échec après la sauvegarde laisserait la machine sans exécutable.
func TestBackupConserveLOriginal(t *testing.T) {
	dir := withBinDir(t)
	src := filepath.Join(dir, "caleoped")
	if err := os.WriteFile(src, []byte("version-A"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := backupBinary("caleoped"); err != nil {
		t.Fatalf("backupBinary: %v", err)
	}
	if b, _ := os.ReadFile(src); string(b) != "version-A" {
		t.Fatal("le binaire en service a été déplacé au lieu d'être copié")
	}
	prev, err := os.ReadFile(filepath.Join(dir, "caleoped.prev"))
	if err != nil {
		t.Fatalf("le .prev n'a pas été créé : %v", err)
	}
	if string(prev) != "version-A" {
		t.Fatalf(".prev contient %q", string(prev))
	}
	// Pas de fichier temporaire laissé derrière.
	if _, err := os.Stat(filepath.Join(dir, "caleoped.prev.tmp")); !os.IsNotExist(err) {
		t.Error("un fichier .tmp a été laissé derrière")
	}
}

// Un binaire absent (installation partielle) ne doit pas faire échouer la
// sauvegarde — sinon on bloque une mise à jour pour une raison sans rapport.
func TestBackupBinaireAbsentNEstPasUneErreur(t *testing.T) {
	withBinDir(t)
	if err := backupBinary("caleope-ui"); err != nil {
		t.Fatalf("un binaire absent devrait être ignoré, erreur : %v", err)
	}
}

func TestRestaurationCompleteEtIdempotente(t *testing.T) {
	dir := withBinDir(t)
	for _, b := range managedBinaries {
		if err := os.WriteFile(filepath.Join(dir, b), []byte("cassé"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, b+".prev"), []byte("bon-"+b), 0755); err != nil {
			t.Fatal(err)
		}
	}
	restored, err := restorePreviousBinaries()
	if err != nil {
		t.Fatalf("restorePreviousBinaries: %v", err)
	}
	if len(restored) != len(managedBinaries) {
		t.Fatalf("%d restaurés sur %d", len(restored), len(managedBinaries))
	}
	for _, b := range managedBinaries {
		got, _ := os.ReadFile(filepath.Join(dir, b))
		if string(got) != "bon-"+b {
			t.Errorf("%s = %q, attendu %q", b, string(got), "bon-"+b)
		}
		// le .prev doit survivre : on peut vouloir rejouer la restauration
		if _, err := os.Stat(filepath.Join(dir, b+".prev")); err != nil {
			t.Errorf("le .prev de %s a disparu après restauration", b)
		}
		fi, _ := os.Stat(filepath.Join(dir, b))
		if fi.Mode().Perm() != 0755 {
			t.Errorf("%s : permissions %o, attendu 755", b, fi.Mode().Perm())
		}
	}
}

func TestRestaurationSansPrecedentEchoueProprement(t *testing.T) {
	withBinDir(t)
	if _, err := restorePreviousBinaries(); err == nil {
		t.Fatal("restaurer sans .prev devrait échouer explicitement")
	}
	if hasPreviousBinaries() {
		t.Fatal("hasPreviousBinaries devrait être faux")
	}
}

// Le script de surveillance doit être du shell valide et contenir la logique de
// secours : c'est le seul recours si le daemon ne redémarre pas.
func TestScriptDeSurveillance(t *testing.T) {
	dir := withBinDir(t)
	sc := upgradeWatchdogScript(filepath.Join(dir, "upgrade.log"))
	for _, must := range []string{
		"systemctl restart caleoped",
		"systemctl restart caleope-ui",
		"is-active",
		"ping",
		"restore",
		".prev",
	} {
		if !strings.Contains(sc, must) {
			t.Errorf("le script ne contient pas %q", must)
		}
	}
	// Le chemin des binaires doit être injecté, pas codé en dur.
	if !strings.Contains(sc, dir) {
		t.Error("le script n'utilise pas le binDir courant")
	}
}

// Le script de surveillance tourne DÉTACHÉ sur la machine d'un utilisateur, sans
// personne pour lire une erreur de syntaxe. Une coquille shell le rendrait
// inopérant exactement au moment où il est censé rattraper une mise à jour
// ratée. On valide donc sa syntaxe à la compilation des tests.
func TestScriptDeSurveillanceSyntaxeShell(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash absent de cet environnement")
	}
	f := filepath.Join(t.TempDir(), "watchdog.sh")
	if err := os.WriteFile(f, []byte(upgradeWatchdogScript("/tmp/upgrade-test.log")), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(bash, "-n", f).CombinedOutput()
	if err != nil {
		t.Fatalf("syntaxe shell invalide : %v\n%s", err, out)
	}
}
