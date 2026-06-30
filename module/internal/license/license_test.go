// internal/license/license_test.go
//
// Tests du module de licence (Ed25519, vérification hors-ligne).

package license

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// signToken fabrique un token "payload.signature" exactement comme le serveur
// de licence (caleope-pay) : la signature porte sur la CHAÎNE base64url du
// payload, PAS sur le JSON décodé.
func signToken(t *testing.T, priv ed25519.PrivateKey, p Payload) string {
	t.Helper()
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(raw)
	sig := ed25519.Sign(priv, []byte(encoded))
	return encoded + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// ─────────────────────────────────────────────
// verifyTokenWithKey : chemin valide + altérations
// ─────────────────────────────────────────────

func TestVerifyValidToken(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	want := Payload{Edition: "pro", MachineHash: "abc", LicenseKey: "KEY-123", IssuedAt: 42}
	tok := signToken(t, priv, want)

	got, err := verifyTokenWithKey(tok, pub)
	if err != nil {
		t.Fatalf("verifyTokenWithKey: %v", err)
	}
	if got.Edition != "pro" || got.LicenseKey != "KEY-123" || got.IssuedAt != 42 {
		t.Fatalf("payload décodé inattendu: %+v", got)
	}
}

// TestVerifiesRealServerToken verrouille le fix du bug de message signé :
// un VRAI token émis par caleope-pay (clé pro, machine de la VM .15) doit se
// vérifier avec la clé publique de production embarquée (PublicKeyB64).
// Si quelqu'un re-décode le payload avant Verify, ce test casse.
func TestVerifiesRealServerToken(t *testing.T) {
	const token = "eyJlZGl0aW9uIjoicHJvIiwibWFjaGluZV9oYXNoIjoiamxMV21BRHBsSHloSWRIM2IxTzVGalhlMk9yb1kyS1ZTUXNBTWQweXdmOCIsImxpY2Vuc2Vfa2V5IjoiQ0FMUC1aVEFNLUdXRTQtQkhaOCIsImlzc3VlZF9hdCI6MTc4Mjg0Nzk3MywidmFsaWRfdW50aWwiOiJsaWZldGltZSJ9.6budkWEJxOkcpoCZNk1OozBLO3786g7VwKal1BcwlgVXl7khs7fVW29bnq6mvcIeDGprQRkCa9e92bj-S0EuAg"
	p, err := verifyToken(token)
	if err != nil {
		t.Fatalf("le token réel du serveur doit se vérifier avec PublicKeyB64: %v", err)
	}
	if p.Edition != "pro" {
		t.Fatalf("edition = %q, attendu \"pro\"", p.Edition)
	}
	if p.LicenseKey != "CALP-ZTAM-GWE4-BHZ8" {
		t.Fatalf("license_key = %q", p.LicenseKey)
	}
}

func TestVerifyWrongKeyFails(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	otherPub, _, _ := ed25519.GenerateKey(rand.Reader)
	tok := signToken(t, priv, Payload{Edition: "pro"})

	if _, err := verifyTokenWithKey(tok, otherPub); err == nil {
		t.Fatal("vérification avec une autre clé publique aurait dû échouer")
	}
}

func TestVerifyTamperedPayloadFails(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	tok := signToken(t, priv, Payload{Edition: "community"})

	dot := strings.IndexByte(tok, '.')
	if dot < 0 {
		t.Fatal("token de test sans point")
	}

	// Remplacer le payload par un "pro" forgé, en gardant la signature d'origine.
	forged, _ := json.Marshal(Payload{Edition: "pro"})
	tampered := base64.RawURLEncoding.EncodeToString(forged) + tok[dot:]

	if _, err := verifyTokenWithKey(tampered, pub); err == nil {
		t.Fatal("payload falsifié aurait dû invalider la signature")
	}
}

func TestVerifyMalformedTokens(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	cases := map[string]string{
		"sans point":        "pasdepoint",
		"payload non-b64":   "!!!.YWJj",
		"signature non-b64": "YWJj.!!!",
		"vide":              "",
	}
	for name, tok := range cases {
		if _, err := verifyTokenWithKey(tok, pub); err == nil {
			t.Errorf("%s: aurait dû échouer", name)
		}
	}
}

// ─────────────────────────────────────────────
// Manager : état non activé
// ─────────────────────────────────────────────

func TestManagerNotActivated(t *testing.T) {
	m := NewManager(t.TempDir())

	if m.IsActivated() {
		t.Error("IsActivated=true sans token")
	}
	if e := m.Edition(); e != "" {
		t.Errorf("Edition=%q sans token, attendu vide", e)
	}
	st := m.Status()
	if st.Activated {
		t.Error("Status.Activated=true sans token")
	}
	if _, err := m.Verify(); err == nil {
		t.Error("Verify sans token aurait dû échouer")
	}
}

func TestManagerRejectsInvalidStoredToken(t *testing.T) {
	base := t.TempDir()
	m := NewManager(base)
	// Écrire un token bidon (signé par personne) → Verify doit échouer.
	tp := m.tokenPath()
	if err := os.MkdirAll(filepath.Dir(tp), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tp, []byte("forged.token\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if m.IsActivated() {
		t.Error("IsActivated=true avec un token non signé par la clé de prod")
	}
}

// ─────────────────────────────────────────────
// MachineHash
// ─────────────────────────────────────────────

func TestMachineHashStableAndNonEmpty(t *testing.T) {
	a := MachineHash()
	if a == "" {
		t.Fatal("MachineHash vide")
	}
	if b := MachineHash(); a != b {
		t.Fatalf("MachineHash non déterministe: %q vs %q", a, b)
	}
}
