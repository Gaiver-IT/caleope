// internal/secrets/secrets.go
//
// 🔐 CHIFFREMENT DES SECRETS
//
// Schéma :
//   1. À l'install : utilisateur fournit un mot de passe
//   2. Dérivation de clé : SHA-256 × 100 000 itérations + salt 16 octets
//   3. Un DEK (Data Encryption Key) aléatoire 32 octets est généré
//   4. DEK chiffré avec la clé dérivée → stocké dans core/daemon/master.enc
//   5. Chaque secrets.env est aussi chiffré → secrets.enc (lecture seule via `caleope secrets show`)
//
// Format d'un fichier .enc : salt(16) || nonce(12) || ciphertext(variable) || tag(16)
// Le tag est inclus dans la sortie de AES-GCM automatiquement en Go.
//
// IMPORTANT : secrets.env reste en clair pour que Docker Compose puisse l'utiliser.
// secrets.enc est la copie chiffrée pour affichage sécurisé.

package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const (
	kdfIterations = 100_000
	saltSize      = 16
	nonceSize     = 12
	keySize       = 32 // AES-256
)

// masterEncPath retourne le chemin du fichier master.enc.
func masterEncPath(baseDir string) string {
	return filepath.Join(baseDir, "core", "daemon", "master.enc")
}

// IsSetup retourne true si le chiffrement a été initialisé (master.enc présent).
func IsSetup(baseDir string) bool {
	_, err := os.Stat(masterEncPath(baseDir))
	return err == nil
}

// Setup initialise le chiffrement avec le mot de passe fourni.
// Génère un DEK aléatoire, le chiffre avec la clé dérivée du mot de passe,
// et écrit le résultat dans core/daemon/master.enc.
// Retourne le DEK en clair (à utiliser immédiatement pour chiffrer les secrets existants).
func Setup(baseDir, password string) ([]byte, error) {
	// Générer un salt aléatoire pour la dérivation de clé
	salt := make([]byte, saltSize)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("génération salt: %w", err)
	}

	// Dériver la clé depuis le mot de passe
	kek := deriveKey([]byte(password), salt)

	// Générer un DEK aléatoire
	dek := make([]byte, keySize)
	if _, err := rand.Read(dek); err != nil {
		return nil, fmt.Errorf("génération DEK: %w", err)
	}

	// Chiffrer le DEK avec la KEK
	encDEK, err := encryptBytes(kek, dek)
	if err != nil {
		return nil, fmt.Errorf("chiffrement DEK: %w", err)
	}

	// Format master.enc : hex(salt) + ":" + hex(encDEK)
	content := hex.EncodeToString(salt) + ":" + hex.EncodeToString(encDEK)

	encPath := masterEncPath(baseDir)
	if err := os.MkdirAll(filepath.Dir(encPath), 0700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(encPath, []byte(content), 0600); err != nil {
		return nil, fmt.Errorf("écriture master.enc: %w", err)
	}

	return dek, nil
}

// UnlockDEK déchiffre et retourne le DEK depuis master.enc avec le mot de passe fourni.
func UnlockDEK(baseDir, password string) ([]byte, error) {
	content, err := os.ReadFile(masterEncPath(baseDir))
	if err != nil {
		return nil, fmt.Errorf("master.enc introuvable (chiffrement non configuré ?): %w", err)
	}

	parts := splitOnce(string(content), ":")
	if len(parts) != 2 {
		return nil, errors.New("master.enc corrompu")
	}

	salt, err := hex.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("master.enc: salt invalide: %w", err)
	}
	encDEK, err := hex.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("master.enc: DEK invalide: %w", err)
	}

	kek := deriveKey([]byte(password), salt)

	dek, err := decryptBytes(kek, encDEK)
	if err != nil {
		return nil, errors.New("mot de passe incorrect ou master.enc corrompu")
	}
	return dek, nil
}

// EncryptSecrets chiffre le contenu de secrets.env et écrit secrets.enc.
// Appelé après chaque écriture de secrets.env si le chiffrement est activé.
func EncryptSecrets(configDir string, dek []byte) error {
	plain, err := os.ReadFile(filepath.Join(configDir, "secrets.env"))
	if err != nil {
		return nil // pas de secrets.env → rien à chiffrer
	}

	encrypted, err := encryptBytes(dek, plain)
	if err != nil {
		return fmt.Errorf("chiffrement secrets.env: %w", err)
	}

	return os.WriteFile(filepath.Join(configDir, "secrets.enc"), encrypted, 0600)
}

// ShowSecrets déchiffre et retourne le contenu de secrets.enc.
// Retourne les secrets en clair pour affichage uniquement.
func ShowSecrets(configDir string, dek []byte) (string, error) {
	encPath := filepath.Join(configDir, "secrets.enc")
	encrypted, err := os.ReadFile(encPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Fallback : lire secrets.env directement (chiffrement non activé)
			plain, err2 := os.ReadFile(filepath.Join(configDir, "secrets.env"))
			if err2 != nil {
				return "", fmt.Errorf("aucun fichier de secrets trouvé pour cette app")
			}
			return string(plain), nil
		}
		return "", err
	}

	plain, err := decryptBytes(dek, encrypted)
	if err != nil {
		return "", fmt.Errorf("déchiffrement échoué: %w", err)
	}
	return string(plain), nil
}

// ─────────────────────────────────────────────
// CRYPTO INTERNE
// ─────────────────────────────────────────────

// deriveKey dérive une clé AES-256 depuis un mot de passe et un salt.
// Utilise SHA-256 en chaîne (100 000 itérations) — approche simple sans dépendance externe.
func deriveKey(password, salt []byte) []byte {
	key := make([]byte, keySize)
	copy(key, password)
	for i := 0; i < kdfIterations; i++ {
		h := sha256.New()
		h.Write(key)
		h.Write(salt)
		// Mixer l'itération pour éviter que des itérations identiques donnent le même hash
		h.Write([]byte{byte(i >> 24), byte(i >> 16), byte(i >> 8), byte(i)})
		copy(key, h.Sum(nil))
	}
	return key
}

// encryptBytes chiffre data avec AES-256-GCM et une clé de 32 octets.
// Format de sortie : nonce(12) || ciphertext+tag
func encryptBytes(key, data []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, nonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	// Seal : nonce en préfixe, ciphertext+tag en suffixe
	ciphertext := gcm.Seal(nonce, nonce, data, nil)
	return ciphertext, nil
}

// decryptBytes déchiffre data avec AES-256-GCM.
// Format attendu : nonce(12) || ciphertext+tag
func decryptBytes(key, data []byte) ([]byte, error) {
	if len(data) < nonceSize {
		return nil, errors.New("données chiffrées trop courtes")
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := data[:nonceSize]
	ciphertext := data[nonceSize:]

	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, errors.New("déchiffrement AES-GCM échoué (mauvaise clé ?)")
	}
	return plain, nil
}

func splitOnce(s, sep string) []string {
	idx := -1
	for i := 0; i < len(s)-len(sep)+1; i++ {
		if s[i:i+len(sep)] == sep {
			idx = i
			break
		}
	}
	if idx < 0 {
		return []string{s}
	}
	return []string{s[:idx], s[idx+len(sep):]}
}
