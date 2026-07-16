// internal/secrets/secrets_test.go
//
// Tests du module de chiffrement des secrets.
// White-box (package secrets) pour pouvoir tester encrypt/decrypt/deriveKey internes.

package secrets

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeLegacyV1Master fabrique un master.enc au format LEGACY v1
// ("hex(salt):hex(encDEK)", KDF SHA-256 itéré) pour tester la rétro-compat.
func writeLegacyV1Master(t *testing.T, baseDir, password string, dek []byte) {
	t.Helper()
	salt := bytes.Repeat([]byte{0x11}, 16)
	kek := deriveKey(password, salt) // ancien KDF
	encDEK, err := encrypt(kek, dek)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(baseDir, masterFile)
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	content := hex.EncodeToString(salt) + ":" + hex.EncodeToString(encDEK)
	if err := os.WriteFile(p, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

// ─────────────────────────────────────────────
// AES-256-GCM : encrypt/decrypt
// ─────────────────────────────────────────────

func TestEncryptDecryptRoundtrip(t *testing.T) {
	key := bytes.Repeat([]byte{0x42}, 32) // clé AES-256 factice
	plaintext := []byte("JELLYFIN_PASSWORD=s3cr3t\nQBT_USER=admin\n")

	ct, err := encrypt(key, plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if bytes.Equal(ct, plaintext) {
		t.Fatal("le ciphertext est identique au plaintext")
	}

	got, err := decrypt(key, ct)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("roundtrip cassé: got %q, want %q", got, plaintext)
	}
}

func TestEncryptUsesRandomNonce(t *testing.T) {
	key := bytes.Repeat([]byte{0x42}, 32)
	pt := []byte("même message")

	a, err := encrypt(key, pt)
	if err != nil {
		t.Fatalf("encrypt a: %v", err)
	}
	b, err := encrypt(key, pt)
	if err != nil {
		t.Fatalf("encrypt b: %v", err)
	}
	if bytes.Equal(a, b) {
		t.Fatal("deux chiffrements du même plaintext sont identiques (nonce non aléatoire)")
	}
}

func TestDecryptWrongKeyFails(t *testing.T) {
	key := bytes.Repeat([]byte{0x42}, 32)
	wrong := bytes.Repeat([]byte{0x43}, 32)

	ct, err := encrypt(key, []byte("secret"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if _, err := decrypt(wrong, ct); err == nil {
		t.Fatal("decrypt avec mauvaise clé aurait dû échouer (tag GCM)")
	}
}

func TestDecryptTooShortFails(t *testing.T) {
	key := bytes.Repeat([]byte{0x42}, 32)
	if _, err := decrypt(key, []byte{0x01, 0x02}); err == nil {
		t.Fatal("decrypt sur données < nonce aurait dû échouer")
	}
}

// ─────────────────────────────────────────────
// KDF : deriveKey
// ─────────────────────────────────────────────

func TestDeriveKeyDeterministic(t *testing.T) {
	salt := bytes.Repeat([]byte{0x01}, 16)
	a := deriveKey("hunter2", salt)
	b := deriveKey("hunter2", salt)
	if !bytes.Equal(a, b) {
		t.Fatal("deriveKey non déterministe pour mêmes entrées")
	}
	if len(a) != 32 {
		t.Fatalf("clé dérivée de %d octets, attendu 32", len(a))
	}
}

func TestDeriveKeySensitiveToPasswordAndSalt(t *testing.T) {
	salt1 := bytes.Repeat([]byte{0x01}, 16)
	salt2 := bytes.Repeat([]byte{0x02}, 16)

	base := deriveKey("hunter2", salt1)
	if bytes.Equal(base, deriveKey("hunter3", salt1)) {
		t.Fatal("clé identique pour mots de passe différents")
	}
	if bytes.Equal(base, deriveKey("hunter2", salt2)) {
		t.Fatal("clé identique pour salts différents")
	}
}

// ─────────────────────────────────────────────
// KDF v2 (PBKDF2) + migration v1 → v2
// ─────────────────────────────────────────────

func TestDeriveKeyV2(t *testing.T) {
	salt := bytes.Repeat([]byte{0x01}, 16)
	a, err := deriveKeyV2("hunter2", salt)
	if err != nil {
		t.Fatalf("deriveKeyV2: %v", err)
	}
	if len(a) != 32 {
		t.Fatalf("clé de %d octets, attendu 32", len(a))
	}
	// Déterministe
	b, _ := deriveKeyV2("hunter2", salt)
	if !bytes.Equal(a, b) {
		t.Fatal("deriveKeyV2 non déterministe")
	}
	// Différente de l'ancien KDF v1 (sinon la migration ne servirait à rien)
	if bytes.Equal(a, deriveKey("hunter2", salt)) {
		t.Fatal("deriveKeyV2 == deriveKey v1 (KDF non changé)")
	}
	// Sensible au mot de passe
	if c, _ := deriveKeyV2("hunter3", salt); bytes.Equal(a, c) {
		t.Fatal("clé identique pour mots de passe différents")
	}
}

func TestSetupWritesV2Format(t *testing.T) {
	base := t.TempDir()
	if _, err := Setup(base, "pw"); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(base, masterFile))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), kdfV2Prefix+":") {
		t.Fatalf("master.enc ne commence pas par %q: %q", kdfV2Prefix+":", string(data))
	}
}

func TestLegacyV1MasterStillUnlocks(t *testing.T) {
	base := t.TempDir()
	const pw = "ancien-mot-de-passe"
	dek := bytes.Repeat([]byte{0xAB}, 32)
	writeLegacyV1Master(t, base, pw, dek)

	got, err := UnlockDEK(base, pw)
	if err != nil {
		t.Fatalf("UnlockDEK sur fichier v1 legacy: %v", err)
	}
	if !bytes.Equal(got, dek) {
		t.Fatal("DEK déverrouillé depuis v1 ≠ DEK original")
	}
}

func TestV1MasterUpgradedToV2OnUnlock(t *testing.T) {
	base := t.TempDir()
	const pw = "ancien"
	dek := bytes.Repeat([]byte{0xCD}, 32)
	writeLegacyV1Master(t, base, pw, dek)

	p := filepath.Join(base, masterFile)
	before, _ := os.ReadFile(p)
	if strings.HasPrefix(string(before), kdfV2Prefix+":") {
		t.Fatal("le fichier de test n'est pas en v1")
	}

	// Le unlock doit migrer le fichier vers v2…
	if _, err := UnlockDEK(base, pw); err != nil {
		t.Fatalf("UnlockDEK: %v", err)
	}
	after, _ := os.ReadFile(p)
	if !strings.HasPrefix(string(after), kdfV2Prefix+":") {
		t.Fatalf("master.enc non migré en v2 après unlock: %q", string(after))
	}

	// …et le fichier migré doit toujours rendre le même DEK.
	got, err := UnlockDEK(base, pw)
	if err != nil {
		t.Fatalf("UnlockDEK après migration: %v", err)
	}
	if !bytes.Equal(got, dek) {
		t.Fatal("DEK après migration ≠ DEK original")
	}
}

func TestUnlockUnknownVersionFails(t *testing.T) {
	base := t.TempDir()
	p := filepath.Join(base, masterFile)
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	// 3 parts mais version inconnue
	if err := os.WriteFile(p, []byte("v9:"+hex.EncodeToString([]byte("salt"))+":"+hex.EncodeToString([]byte("enc"))), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := UnlockDEK(base, "x"); err == nil {
		t.Fatal("version KDF inconnue aurait dû échouer")
	}
}

// ─────────────────────────────────────────────
// Setup / Unlock (master.enc)
// ─────────────────────────────────────────────

func TestSetupUnlockRoundtrip(t *testing.T) {
	base := t.TempDir()
	const pw = "mot-de-passe-maître"

	if IsSetup(base) {
		t.Fatal("IsSetup=true avant Setup")
	}

	dek, err := Setup(base, pw)
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if len(dek) != 32 {
		t.Fatalf("DEK de %d octets, attendu 32", len(dek))
	}
	if !IsSetup(base) {
		t.Fatal("IsSetup=false après Setup")
	}

	got, err := UnlockDEK(base, pw)
	if err != nil {
		t.Fatalf("UnlockDEK: %v", err)
	}
	if !bytes.Equal(got, dek) {
		t.Fatal("DEK déverrouillé ≠ DEK initial")
	}
}

func TestUnlockWrongPasswordFails(t *testing.T) {
	base := t.TempDir()
	if _, err := Setup(base, "bon"); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if _, err := UnlockDEK(base, "mauvais"); err == nil {
		t.Fatal("UnlockDEK avec mauvais mot de passe aurait dû échouer")
	}
}

func TestUnlockMissingMasterFails(t *testing.T) {
	if _, err := UnlockDEK(t.TempDir(), "x"); err == nil {
		t.Fatal("UnlockDEK sans master.enc aurait dû échouer")
	}
}

func TestUnlockCorruptMasterFails(t *testing.T) {
	base := t.TempDir()
	p := filepath.Join(base, masterFile)
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("pas-de-deux-points"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := UnlockDEK(base, "x"); err == nil {
		t.Fatal("UnlockDEK sur master.enc corrompu aurait dû échouer")
	}
}

// ─────────────────────────────────────────────
// EncryptSecrets / ShowSecrets (secrets.env ↔ secrets.enc)
// ─────────────────────────────────────────────

func TestEncryptShowSecretsRoundtrip(t *testing.T) {
	cfg := t.TempDir()
	dek := bytes.Repeat([]byte{0x07}, 32)
	content := "USER=admin\nPASSWORD=p@ss\n"

	if err := os.WriteFile(filepath.Join(cfg, "secrets.env"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	if err := EncryptSecrets(cfg, dek); err != nil {
		t.Fatalf("EncryptSecrets: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cfg, "secrets.enc")); err != nil {
		t.Fatalf("secrets.enc non créé: %v", err)
	}

	got, err := ShowSecrets(cfg, dek)
	if err != nil {
		t.Fatalf("ShowSecrets: %v", err)
	}
	if got != content {
		t.Fatalf("ShowSecrets = %q, attendu %q", got, content)
	}
}

func TestEncryptSecretsNoFileIsNoop(t *testing.T) {
	cfg := t.TempDir()
	if err := EncryptSecrets(cfg, bytes.Repeat([]byte{0x07}, 32)); err != nil {
		t.Fatalf("EncryptSecrets sans secrets.env devrait être un no-op, got: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cfg, "secrets.enc")); !os.IsNotExist(err) {
		t.Fatal("secrets.enc créé alors qu'il n'y avait pas de secrets.env")
	}
}

func TestShowSecretsFallsBackToPlaintext(t *testing.T) {
	cfg := t.TempDir()
	content := "ONLY=plaintext\n"
	if err := os.WriteFile(filepath.Join(cfg, "secrets.env"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	// Pas de secrets.enc → ShowSecrets doit lire secrets.env directement
	got, err := ShowSecrets(cfg, bytes.Repeat([]byte{0x07}, 32))
	if err != nil {
		t.Fatalf("ShowSecrets fallback: %v", err)
	}
	if got != content {
		t.Fatalf("fallback = %q, attendu %q", got, content)
	}
}

func TestShowSecretsWrongDEKFails(t *testing.T) {
	cfg := t.TempDir()
	if err := os.WriteFile(filepath.Join(cfg, "secrets.env"), []byte("X=1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := EncryptSecrets(cfg, bytes.Repeat([]byte{0x07}, 32)); err != nil {
		t.Fatal(err)
	}
	if _, err := ShowSecrets(cfg, bytes.Repeat([]byte{0x08}, 32)); err == nil {
		t.Fatal("ShowSecrets avec mauvais DEK aurait dû échouer")
	}
}
