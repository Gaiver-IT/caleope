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
// KDF : PBKDF2-HMAC-SHA256, 600 000 itérations + salt 16 octets (RFC 8018,
// recommandation OWASP). Format master.enc versionné : "v2:hex(salt):hex(encDEK)".
// Les anciens fichiers "hex(salt):hex(encDEK)" (KDF SHA-256 itéré, v1) restent
// déchiffrables et sont migrés vers v2 automatiquement au premier déverrouillage.
// Chiffrement : AES-256-GCM — nonce(12B) || ciphertext+tag — stdlib uniquement.

package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
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

// Paramètres KDF v2 (PBKDF2-HMAC-SHA256).
const (
	kdfV2Prefix = "v2"
	pbkdf2Iters = 600000 // OWASP 2023 pour PBKDF2-HMAC-SHA256
)

// ─────────────────────────────────────────────
// KDF — dériver une clé AES-256 depuis un mot de passe
// ─────────────────────────────────────────────

// deriveKeyV2 dérive une clé AES-256 (32 octets) via PBKDF2-HMAC-SHA256.
// KDF standard (RFC 8018) — remplace l'ancien deriveKey maison.
func deriveKeyV2(password string, salt []byte) ([]byte, error) {
	return pbkdf2.Key(sha256.New, password, salt, pbkdf2Iters, 32)
}

// deriveKey (LEGACY v1) — SHA-256 itéré maison. Conservé uniquement pour
// déchiffrer les master.enc créés avant la migration vers PBKDF2 (deriveKeyV2).
// Ne plus utiliser pour de nouveaux fichiers.
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

	// Dériver le KEK depuis le mot de passe (PBKDF2)
	kek, err := deriveKeyV2(password, salt)
	if err != nil {
		return nil, fmt.Errorf("dérivation clé: %w", err)
	}

	// Chiffrer le DEK avec le KEK
	encDEK, err := encrypt(kek, dek)
	if err != nil {
		return nil, fmt.Errorf("chiffrement DEK: %w", err)
	}

	// Écrire master.enc au format v2
	if err := writeMaster(baseDir, salt, encDEK); err != nil {
		return nil, err
	}

	return dek, nil
}

// writeMaster écrit master.enc au format v2 : "v2:hex(salt):hex(encDEK)".
func writeMaster(baseDir string, salt, encDEK []byte) error {
	masterPath := filepath.Join(baseDir, masterFile)
	if err := os.MkdirAll(filepath.Dir(masterPath), 0700); err != nil {
		return err
	}
	content := kdfV2Prefix + ":" + hex.EncodeToString(salt) + ":" + hex.EncodeToString(encDEK)
	if err := os.WriteFile(masterPath, []byte(content), 0600); err != nil {
		return fmt.Errorf("écriture master.enc: %w", err)
	}
	return nil
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

	// Format versionné : "v2:hex(salt):hex(encDEK)" (3 parts) ou legacy
	// "hex(salt):hex(encDEK)" (2 parts, KDF v1).
	parts := strings.Split(strings.TrimSpace(string(data)), ":")
	var version, saltHex, encHex string
	switch len(parts) {
	case 3:
		version, saltHex, encHex = parts[0], parts[1], parts[2]
	case 2:
		version, saltHex, encHex = "v1", parts[0], parts[1]
	default:
		return nil, fmt.Errorf("master.enc corrompu")
	}

	salt, err := hex.DecodeString(saltHex)
	if err != nil {
		return nil, fmt.Errorf("master.enc: salt invalide: %w", err)
	}
	encDEK, err := hex.DecodeString(encHex)
	if err != nil {
		return nil, fmt.Errorf("master.enc: DEK invalide: %w", err)
	}

	var kek []byte
	switch version {
	case kdfV2Prefix:
		kek, err = deriveKeyV2(password, salt)
		if err != nil {
			return nil, fmt.Errorf("dérivation clé: %w", err)
		}
	case "v1":
		kek = deriveKey(password, salt)
	default:
		return nil, fmt.Errorf("master.enc: version KDF inconnue %q", version)
	}

	dek, err := decrypt(kek, encDEK)
	if err != nil {
		return nil, fmt.Errorf("mot de passe incorrect ou master.enc corrompu")
	}

	// Migration opportuniste v1 → v2 : re-chiffrer le DEK avec PBKDF2 et un
	// nouveau salt. Best-effort — un échec d'écriture ne bloque pas le unlock.
	if version == "v1" {
		newSalt := make([]byte, 16)
		if _, e := io.ReadFull(rand.Reader, newSalt); e == nil {
			if newKek, e := deriveKeyV2(password, newSalt); e == nil {
				if enc, e := encrypt(newKek, dek); e == nil {
					_ = writeMaster(baseDir, newSalt, enc)
				}
			}
		}
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
