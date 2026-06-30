package license

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultServerURL = "https://caleope-pay.gaiver-it.fr"
	PublicKeyB64     = "XbqQpcK0XsWVK36MAXZeQAcV6jZQhzAR6LxYHBHxfao="
)

func serverURL() string {
	if u := os.Getenv("CALEOPE_LICENSE_SERVER"); u != "" {
		return strings.TrimRight(u, "/")
	}
	return defaultServerURL
}

// Payload correspond au contenu signé d'un token de licence.
type Payload struct {
	Edition     string `json:"edition"`
	MachineHash string `json:"machine_hash"`
	LicenseKey  string `json:"license_key"`
	IssuedAt    int64  `json:"issued_at"`
	ValidUntil  string `json:"valid_until"`
}

// Status est retourné par Status() pour l'affichage CLI et l'API.
type Status struct {
	Activated   bool   `json:"activated"`
	Edition     string `json:"edition"`   // "community", "pro", ""
	LicenseKey  string `json:"license_key"`
	MachineHash string `json:"machine_hash"`
	IssuedAt    int64  `json:"issued_at"`
}

// Manager gère la licence locale.
type Manager struct {
	baseDir string
}

func NewManager(baseDir string) *Manager {
	return &Manager{baseDir: baseDir}
}

func (m *Manager) tokenPath() string {
	return filepath.Join(m.baseDir, "core", "license", "license.token")
}

// MachineHash retourne SHA256(/etc/machine-id) encodé en base64url.
func MachineHash() string {
	data, err := os.ReadFile("/etc/machine-id")
	if err != nil {
		// Fallback sur le hostname si machine-id absent
		if h, err2 := os.Hostname(); err2 == nil {
			data = []byte(h)
		} else {
			data = []byte("unknown")
		}
	}
	h := sha256.Sum256([]byte(strings.TrimSpace(string(data))))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

// IsActivated retourne true si un token valide est stocké.
func (m *Manager) IsActivated() bool {
	_, err := m.Verify()
	return err == nil
}

// Edition retourne l'édition active ("community", "pro") ou "" si non activé.
func (m *Manager) Edition() string {
	p, err := m.Verify()
	if err != nil {
		return ""
	}
	return p.Edition
}

// Status retourne l'état complet de la licence.
func (m *Manager) Status() Status {
	p, err := m.Verify()
	if err != nil {
		return Status{Activated: false}
	}
	return Status{
		Activated:   true,
		Edition:     p.Edition,
		LicenseKey:  p.LicenseKey,
		MachineHash: p.MachineHash,
		IssuedAt:    p.IssuedAt,
	}
}

// Verify lit et vérifie le token stocké hors-ligne (Ed25519).
func (m *Manager) Verify() (*Payload, error) {
	data, err := os.ReadFile(m.tokenPath())
	if err != nil {
		return nil, fmt.Errorf("licence non activée")
	}
	return verifyToken(strings.TrimSpace(string(data)))
}

// Activate appelle le serveur de licence et stocke le token localement.
func (m *Manager) Activate(licenseKey string) error {
	licenseKey = strings.TrimSpace(licenseKey)
	if licenseKey == "" {
		return fmt.Errorf("clé de licence vide")
	}

	machineHash := MachineHash()

	body := fmt.Sprintf(`{"license_key":%q,"machine_hash":%q}`, licenseKey, machineHash)
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Post(serverURL()+"/api/activate", "application/json", strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("impossible de contacter le serveur de licence: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	var result struct {
		Token   string `json:"token"`
		Edition string `json:"edition"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("réponse serveur invalide")
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("activation refusée: %s", result.Error)
	}
	if result.Token == "" {
		return fmt.Errorf("token absent dans la réponse serveur")
	}

	// Vérifier le token reçu avant de le stocker
	if _, err := verifyToken(result.Token); err != nil {
		return fmt.Errorf("token reçu invalide: %w", err)
	}

	// Stocker le token
	dir := filepath.Dir(m.tokenPath())
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("impossible de créer le répertoire licence: %w", err)
	}
	if err := os.WriteFile(m.tokenPath(), []byte(result.Token+"\n"), 0600); err != nil {
		return fmt.Errorf("impossible de sauvegarder le token: %w", err)
	}

	return nil
}

// verifyToken vérifie la signature Ed25519 avec la clé publique de production.
func verifyToken(token string) (*Payload, error) {
	pubKeyBytes, err := base64.StdEncoding.DecodeString(PublicKeyB64)
	if err != nil {
		return nil, fmt.Errorf("clé publique invalide")
	}
	return verifyTokenWithKey(token, ed25519.PublicKey(pubKeyBytes))
}

// verifyTokenWithKey vérifie la signature Ed25519 d'un token contre une clé
// publique donnée et retourne le payload. Couture extraite pour les tests.
func verifyTokenWithKey(token string, pub ed25519.PublicKey) (*Payload, error) {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("format token invalide")
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("payload token invalide")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("signature token invalide")
	}

	// Le serveur de licence signe la CHAÎNE encodée en base64url (parts[0]),
	// pas le JSON décodé. On doit vérifier exactement les mêmes octets.
	if !ed25519.Verify(pub, []byte(parts[0]), sig) {
		return nil, fmt.Errorf("signature invalide")
	}

	var p Payload
	if err := json.Unmarshal(payloadBytes, &p); err != nil {
		return nil, fmt.Errorf("payload JSON invalide")
	}

	return &p, nil
}
