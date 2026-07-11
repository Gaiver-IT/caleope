// internal/store/store.go
//
// 🏪 LE STORE — résolution et cache des applications
//
// Le store est responsable de :
//  1. Maintenir un cache local des dépôts Git
//  2. Résoudre "jellyfin" → chemin vers les fichiers de l'app
//  3. Lire le app.json d'une application
//
// CONCEPT : Pourquoi un cache Git local ?
// Cloner un repo à chaque installation serait lent.
// On clone une fois, et on fait des "git pull" pour les mises à jour.
// Le cache est dans core/cache/<repo_name>/

package store

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gaiver-it/caleope/pkg/types"
)

// Store gère le cache local des applications disponibles.
type Store struct {
	baseDir  string
	cacheDir string
}

func NewStore(baseDir string) *Store {
	return &Store{
		baseDir:  baseDir,
		cacheDir: filepath.Join(baseDir, "core", "cache"),
	}
}

// ─────────────────────────────────────────────
// RÉSOLUTION D'APP
// ─────────────────────────────────────────────

// Resolve trouve le dossier d'une application par son nom.
// Il cherche dans les repos dans l'ordre de priorité :
//  1. Official (Gaiver IT)
//  2. Community
//  3. Untrusted (avec avertissement)
func (s *Store) Resolve(appID string, repos []types.Repo) (string, *types.Repo, error) {
	// Ordre de priorité : official → community → untrusted
	order := []types.TrustLevel{
		types.TrustOfficial,
		types.TrustCommunity,
		types.TrustUntrusted,
	}

	for _, trust := range order {
		for i := range repos {
			repo := &repos[i]
			if repo.Trust != trust {
				continue
			}

			// Chercher <cache>/<repo_name>/apps/<appID>/
			appDir := filepath.Join(repo.LocalDir, "apps", appID)
			if _, err := os.Stat(appDir); err == nil {
				return appDir, repo, nil
			}
		}
	}

	return "", nil, fmt.Errorf("application '%s' introuvable dans les dépôts configurés", appID)
}

// ─────────────────────────────────────────────
// LECTURE DU MANIFEST
// ─────────────────────────────────────────────

// ReadManifest lit et parse le app.json d'une application.
func (s *Store) ReadManifest(appDir string) (*types.AppManifest, error) {
	manifestPath := filepath.Join(appDir, "app.json")

	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("app.json introuvable dans %s: %w", appDir, err)
	}

	var manifest types.AppManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("app.json invalide: %w", err)
	}

	return &manifest, nil
}

// ReadParams lit et parse le params.json d'une application.
// Retourne nil sans erreur si le fichier n'existe pas (l'app n'a pas de params).
func (s *Store) ReadParams(appDir string) ([]types.ParamDef, error) {
	paramsPath := filepath.Join(appDir, "params.json")
	data, err := os.ReadFile(paramsPath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("params.json illisible: %w", err)
	}
	var params []types.ParamDef
	if err := json.Unmarshal(data, &params); err != nil {
		return nil, fmt.Errorf("params.json invalide: %w", err)
	}
	return params, nil
}

// ─────────────────────────────────────────────
// SYNCHRONISATION GIT
// ─────────────────────────────────────────────

// SyncRepo synchronise un dépôt Git local.
// Si le cache n'existe pas → git clone
// Si le cache existe → git fetch + reset --hard
func (s *Store) SyncRepo(repo *types.Repo) error {
	if err := os.MkdirAll(filepath.Dir(repo.LocalDir), 0755); err != nil {
		return err
	}

	branch := repo.Branch
	if branch == "" {
		branch = "main"
	}

	if _, err := os.Stat(filepath.Join(repo.LocalDir, ".git")); os.IsNotExist(err) {
		fmt.Printf("→ Clonage du dépôt %s (branche %s)...\n", repo.Name, branch)
		return s.gitClone(repo.URL, repo.LocalDir, branch)
	}

	fmt.Printf("→ Synchronisation du dépôt %s (branche %s)...\n", repo.Name, branch)
	return s.gitPull(repo.LocalDir, branch)
}

func (s *Store) gitClone(url, destDir, branch string) error {
	cmd := exec.Command("git", "clone", "--depth=1", "--branch", branch, url, destDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git clone %s (branch %s): %w", url, branch, err)
	}
	return nil
}

func (s *Store) gitPull(repoDir, branch string) error {
	// fetch la branche explicitement : sur un clone --depth=1, "git fetch origin"
	// sans argument ne crée pas les remote-tracking refs des autres branches.
	// On utilise FETCH_HEAD après le fetch pour éviter ce problème.
	//
	// -c safe.directory=<dir> : le cache store appartient à user-caleope mais le
	// daemon tourne en root → git refuse (« dubious ownership », exit 128) et le
	// store ne se synchronise JAMAIS sur les installs ISO (leur sync_store
	// n'ajoute pas de safe.directory global). L'option par-commande est robuste
	// quel que soit l'utilisateur/HOME du process.
	fetch := exec.Command("git", "-c", "safe.directory="+repoDir, "-C", repoDir, "fetch", "origin", branch)
	fetch.Stdout = os.Stdout
	fetch.Stderr = os.Stderr
	if err := fetch.Run(); err != nil {
		return fmt.Errorf("git fetch dans %s: %w", repoDir, err)
	}

	reset := exec.Command("git", "-c", "safe.directory="+repoDir, "-C", repoDir, "reset", "--hard", "FETCH_HEAD")
	reset.Stdout = os.Stdout
	reset.Stderr = os.Stderr
	if err := reset.Run(); err != nil {
		return fmt.Errorf("git reset dans %s: %w", repoDir, err)
	}

	return nil
}

// ─────────────────────────────────────────────
// LISTE DES APPS DISPONIBLES
// ─────────────────────────────────────────────

// ListAvailable retourne toutes les apps disponibles dans tous les repos.
func (s *Store) ListAvailable(repos []types.Repo) ([]types.AppManifest, error) {
	var apps []types.AppManifest
	seen := make(map[string]bool) // Pour éviter les doublons entre repos

	// Même ordre de priorité que Resolve
	order := []types.TrustLevel{
		types.TrustOfficial,
		types.TrustCommunity,
		types.TrustUntrusted,
	}

	for _, trust := range order {
		for _, repo := range repos {
			if repo.Trust != trust {
				continue
			}

			appsDir := filepath.Join(repo.LocalDir, "apps")
			entries, err := os.ReadDir(appsDir)
			if err != nil {
				continue // Repo pas encore synchronisé
			}

			for _, entry := range entries {
				if !entry.IsDir() {
					continue
				}

				appID := entry.Name()
				if seen[appID] {
					continue // Déjà trouvé dans un repo prioritaire
				}

				appDir := filepath.Join(appsDir, appID)
				manifest, err := s.ReadManifest(appDir)
				if err != nil {
					continue
				}

				apps = append(apps, *manifest)
				seen[appID] = true
			}
		}
	}

	return apps, nil
}

// Search cherche des apps dont le nom ou la catégorie contient le terme.
func (s *Store) Search(term string, repos []types.Repo) ([]types.AppManifest, error) {
	all, err := s.ListAvailable(repos)
	if err != nil {
		return nil, err
	}

	term = strings.ToLower(term)
	var results []types.AppManifest

	for _, app := range all {
		// strings.Contains = recherche une sous-chaîne
		if strings.Contains(strings.ToLower(app.Name), term) ||
			strings.Contains(strings.ToLower(app.Category), term) ||
			strings.Contains(strings.ToLower(app.ID), term) {
			results = append(results, app)
		}
	}

	return results, nil
}
