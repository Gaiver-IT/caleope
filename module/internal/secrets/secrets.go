// internal/secrets/secrets.go
//
// 🔐 Chiffrement des secrets d'application
//
// Architecture à deux couches :
//   - DEK (Data Encryption Key) : clé AES-256 aléatoire, chiffre les secrets.env
//   - KEK (Key Encryption Key)  : dérivée du mot de passe utilisateur (KDF), chiffre le DEK
//
// Fichiers :
//   - master.enc : hex(salt):hex(encryptedDEK) — stocké dans <baseDir>/core/daemon/
//   - app-config/<app>/secrets.enc : version chiffrée de secrets.env (pour `caleope secrets show`)
//   - app-config/<app>/secrets.env : version plaintext pour Docker (inchangée)
//
// KDF : SHA-256 × 100 000 iterations + salt 16 octets (stdlib uniquement)
// Chiffrement : AES-256-GCM — nonce(12B) || ciphertext+tag

package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const masterFile = "core/daemon/master.enc"

// ─────────────────────────────────────────────
// KDF — dériver une clé AES-256 depuis un mot de passe
// ─────────────────────────────────────────────

// deriveKey dérive une clé de 32 octets depuis password + salt via SHA-256 itéré.
func deriveKey(password string, salt []byte) []byte {
	h := sha256.New()
	h.Write([]byte(password))
	h.Write(salt)
	key := h.Sum(nil) // 32 bytes

	// 100 000 itérations pour ralentir les attaques par force brute
	for i := 0; i < 99999; i++ {
		h.Reset()
		h.Write(key)
		h.Write(salt)
		key = h.Sum(nil)
	}
	return key
}

// ─────────────────────────────────────────────
// AES-256-GCM
// ─────────────────────────────────────────────

func encrypt(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize()) // 12 octets
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	// nonce || ciphertext+tag
	return append(nonce, gcm.Seal(nil, nonce, plaintext, nil)...), nil
}

func decrypt(key, data []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(data) < gcm.NonceSize() {
		return nil, fmt.Errorf("données chiffrées trop courtes")
	}
	nonce, ciphertext := data[:gcm.NonceSize()], data[gcm.NonceSize():]
	return gcm.Open(nil, nonce, ciphertext, nil)
}

// ─────────────────────────────────────────────
// SETUP — initialisation à l'installation de Caleope
// ─────────────────────────────────────────────

// IsSetup retourne true si master.enc existe (chiffrement déjà initialisé).
func IsSetup(baseDir string) bool {
	_, err := os.Stat(filepath.Join(baseDir, masterFile))
	return err == nil
}

// Setup génère un DEK aléatoire, le chiffre avec le mot de passe, et écrit master.enc.
// Retourne le DEK brut (pour chiffrer les secrets existants au premier démarrage).
func Setup(baseDir, password string) ([]byte, error) {
	// Générer le DEK (32 octets aléatoires)
	dek := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, dek); err != nil {
		return nil, fmt.Errorf("génération DEK: %w", err)
	}

	// Générer le salt KDF
	salt := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, fmt.Errorf("génération salt: %w", err)
	}

	// Dériver le KEK depuis le mot de passe
	kek := deriveKey(password, salt)

	// Chiffrer le DEK avec le KEK
	encDEK, err := encrypt(kek, dek)
	if err != nil {
		return nil, fmt.Errorf("chiffrement DEK: %w", err)
	}

	// Écrire master.enc : hex(salt):hex(encDEK)
	masterPath := filepath.Join(baseDir, masterFile)
	if err := os.MkdirAll(filepath.Dir(masterPath), 0700); err != nil {
		return nil, err
	}
	content := hex.EncodeToString(salt) + ":" + hex.EncodeToString(encDEK)
	if err := os.WriteFile(masterPath, []byte(content), 0600); err != nil {
		return nil, fmt.Errorf("écriture master.enc: %w", err)
	}

	return dek, nil
}

// ─────────────────────────────────────────────
// UNLOCK — déchiffrer le DEK depuis le mot de passe
// ─────────────────────────────────────────────

// UnlockDEK lit master.enc, dérive le KEK depuis password, déchiffre et retourne le DEK.
func UnlockDEK(baseDir, password string) ([]byte, error) {
	masterPath := filepath.Join(baseDir, masterFile)
	data, err := os.ReadFile(masterPath)
	if err != nil {
		return nil, fmt.Errorf("master.enc introuvable (chiffrement non initialisé): %w", err)
	}

	parts := strings.SplitN(strings.TrimSpace(string(data)), ":", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("master.enc corrompu")
	}

	salt, err := hex.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("master.enc: salt invalide: %w", err)
	}

	encDEK, err := hex.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("master.enc: DEK invalide: %w", err)
	}

	kek := deriveKey(password, salt)
	dek, err := decrypt(kek, encDEK)
	if err != nil {
		return nil, fmt.Errorf("mot de passe incorrect ou master.enc corrompu")
	}

	return dek, nil
}

// ─────────────────────────────────────────────
// ENCRYPT / SHOW — chiffrer et déchiffrer secrets.env
// ─────────────────────────────────────────────

// EncryptSecrets chiffre secrets.env → secrets.enc dans le répertoire de config de l'app.
func EncryptSecrets(configDir string, dek []byte) error {
	secretsPath := filepath.Join(configDir, "secrets.env")
	plaintext, err := os.ReadFile(secretsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // pas de secrets à chiffrer
		}
		return fmt.Errorf("lecture secrets.env: %w", err)
	}

	ciphertext, err := encrypt(dek, plaintext)
	if err != nil {
		return fmt.Errorf("chiffrement secrets: %w", err)
	}

	encPath := filepath.Join(configDir, "secrets.enc")
	return os.WriteFile(encPath, []byte(hex.EncodeToString(ciphertext)), 0600)
}

// ShowSecrets déchiffre secrets.enc et retourne le contenu en clair.
func ShowSecrets(configDir string, dek []byte) (string, error) {
	encPath := filepath.Join(configDir, "secrets.enc")
	hexData, err := os.ReadFile(encPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Pas encore chiffré — lire secrets.env directement (fallback)
			plain, err2 := os.ReadFile(filepath.Join(configDir, "secrets.env"))
			if err2 != nil {
				return "", fmt.Errorf("secrets.enc et secrets.env introuvables")
			}
			return string(plain), nil
		}
		return "", fmt.Errorf("lecture secrets.enc: %w", err)
	}

	ciphertext, err := hex.DecodeString(strings.TrimSpace(string(hexData)))
	if err != nil {
		return "", fmt.Errorf("secrets.enc: format invalide")
	}

	plaintext, err := decrypt(dek, ciphertext)
	if err != nil {
		return "", fmt.Errorf("déchiffrement secrets: %w", err)
	}

	return string(plaintext), nil
}
