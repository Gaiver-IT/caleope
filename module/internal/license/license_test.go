// internal/license/license_test.go
//
// Tests du module de licence (Ed25519, vérification hors-ligne).

package license

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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
// Activate : réseau (mock httptest) + erreur actionnable
// ─────────────────────────────────────────────

// realServerToken = token pro réel émis par caleope-pay (même fixture que
// TestVerifiesRealServerToken), réutilisé pour simuler la réponse du serveur.
const realServerToken = "eyJlZGl0aW9uIjoicHJvIiwibWFjaGluZV9oYXNoIjoiamxMV21BRHBsSHloSWRIM2IxTzVGalhlMk9yb1kyS1ZTUXNBTWQweXdmOCIsImxpY2Vuc2Vfa2V5IjoiQ0FMUC1aVEFNLUdXRTQtQkhaOCIsImlzc3VlZF9hdCI6MTc4Mjg0Nzk3MywidmFsaWRfdW50aWwiOiJsaWZldGltZSJ9.6budkWEJxOkcpoCZNk1OozBLO3786g7VwKal1BcwlgVXl7khs7fVW29bnq6mvcIeDGprQRkCa9e92bj-S0EuAg"

// Activation réussie : le serveur renvoie un token valide → il est vérifié puis
// stocké, et le Manager passe activé.
func TestActivateStoresValidServerToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/activate" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"token":%q,"edition":"pro"}`, realServerToken)
	}))
	defer srv.Close()
	t.Setenv("CALEOPE_LICENSE_SERVER", srv.URL)

	m := NewManager(t.TempDir())
	if err := m.Activate("CALP-ZTAM-GWE4-BHZ8"); err != nil {
		t.Fatalf("Activate a échoué sur un serveur joignable: %v", err)
	}
	if !m.IsActivated() {
		t.Fatal("IsActivated=false après une activation réussie")
	}
	if e := m.Edition(); e != "pro" {
		t.Fatalf("Edition=%q après activation, attendu \"pro\"", e)
	}
}

// Serveur injoignable : l'erreur doit être ACTIONNABLE (mentionne « injoignable »
// et l'override CALEOPE_LICENSE_SERVER), pas un « no route to host » brut. C'est
// le cœur du fix v0.7.7.
func TestActivateUnreachableGivesActionableError(t *testing.T) {
	t.Setenv("CALEOPE_LICENSE_SERVER", "http://127.0.0.1:1") // port fermé → refus immédiat
	m := NewManager(t.TempDir())

	err := m.Activate("CALP-ZTAM-GWE4-BHZ8")
	if err == nil {
		t.Fatal("Activate aurait dû échouer avec un serveur injoignable")
	}
	msg := err.Error()
	if !strings.Contains(msg, "injoignable") {
		t.Errorf("message d'erreur sans « injoignable »: %q", msg)
	}
	if !strings.Contains(msg, "CALEOPE_LICENSE_SERVER") {
		t.Errorf("message d'erreur ne guide pas vers CALEOPE_LICENSE_SERVER: %q", msg)
	}
	if m.IsActivated() {
		t.Error("IsActivated=true après un échec d'activation")
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

// TestRecuperationHorsLigne verrouille le chemin de récupération d'un client qui
// a perdu sa machine. C'est LE scénario qui laissait les gens bloqués : jeton
// perdu (réinstallation, nouveau serveur, restauration d'une sauvegarde Caleope
// qui n'incluait pas core/license), réactivation refusée par un 409 renvoyant
// vers /api/transfer — un endpoint qu'aucun client n'appelle.
//
// La propriété testée ici est délibérée : un jeton authentique est accepté sur
// N'IMPORTE QUELLE machine, parce que Verify() ne contrôle que la signature. Le
// jeton utilisé porte l'empreinte d'une autre machine (VM .15) et doit tout de
// même s'importer. Ne « corrigez » pas ce comportement en ajoutant un contrôle
// d'empreinte : ce serait rendre les clients captifs d'un serveur de licences
// qui peut être éteint pendant des mois.
func TestRecuperationHorsLigne(t *testing.T) {
	// Machine d'origine
	m1 := NewManager(t.TempDir())
	if _, err := m1.ImportToken(realServerToken); err != nil {
		t.Fatalf("import initial: %v", err)
	}
	if !m1.IsActivated() {
		t.Fatal("licence inactive après import")
	}

	// L'utilisateur met sa licence à l'abri
	exported, err := m1.ExportToken()
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if exported != strings.TrimSpace(realServerToken) {
		t.Fatal("le jeton exporté diffère du jeton installé")
	}

	// Nouvelle machine, AUCUN réseau disponible : la restauration doit suffire.
	m2 := NewManager(t.TempDir())
	p, err := m2.ImportToken(exported)
	if err != nil {
		t.Fatalf("récupération sur une nouvelle machine: %v", err)
	}
	if p.Edition != "pro" || !m2.IsActivated() {
		t.Fatalf("édition=%q activée=%v", p.Edition, m2.IsActivated())
	}

	// Le jeton doit rester protégé sur le disque.
	fi, err := os.Stat(m2.TokenPath())
	if err != nil {
		t.Fatalf("jeton absent: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0600 {
		t.Errorf("permissions du jeton = %o, attendu 600", perm)
	}

	// Un jeton falsifié doit être refusé, et ne doit PAS écraser le jeton valide.
	if _, err := m2.ImportToken(exported[:len(exported)-6] + "AAAAAA"); err == nil {
		t.Fatal("un jeton falsifié a été accepté")
	}
	if !m2.IsActivated() {
		t.Fatal("un import refusé a détruit la licence valide en place")
	}
}
