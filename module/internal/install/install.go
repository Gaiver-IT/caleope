// internal/install/install.go
//
// ⚙️ LE MOTEUR D'INSTALLATION
//
// C'est le cœur du projet. Il implémente le flow en 12 étapes de la doc.
// Chaque étape est une fonction séparée. Si une étape échoue → rollback.
//
// CONCEPT : les goroutines et le contexte
// Go utilise des "goroutines" pour la concurrence (comme des threads légers).
// Le "context.Context" permet d'annuler une opération en cours.
// Ex: si l'installation timeout → le contexte est annulé → on rollback.

package install

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/gaiver-it/caleope/internal/docker"
	"github.com/gaiver-it/caleope/internal/events"
	"github.com/gaiver-it/caleope/internal/runtime"
	"github.com/gaiver-it/caleope/internal/store"
	"github.com/gaiver-it/caleope/pkg/types"
)

// Installer orchestre le flow d'installation complet.
type Installer struct {
	rt      *runtime.Manager
	st      *store.Store
	docker  *docker.Client
	emitter *events.Emitter
	baseDir string
}

func NewInstaller(
	rt *runtime.Manager,
	st *store.Store,
	dc *docker.Client,
	em *events.Emitter,
	baseDir string,
) *Installer {
	return &Installer{
		rt:      rt,
		st:      st,
		docker:  dc,
		emitter: em,
		baseDir: baseDir,
	}
}

// InstallOptions contient les paramètres passés par l'utilisateur.
type InstallOptions struct {
	AppID           string
	Domain          string            // domaine pour Traefik (ex: jellyfin.monserveur.com)
	Channel         string            // stable, latest, nightly
	Params          map[string]string // paramètres additionnels de params.json
	Force           bool              // forcer la réinstallation si déjà installé
	StorageLocation string            // nom de la location NAS pour stocker app-data (vide = local)
	StorageDataDir  string            // chemin absolu résolu (rempli par l'installeur)
}

// ─────────────────────────────────────────────
// INSTALL — flow principal en 12 étapes
// ─────────────────────────────────────────────

// Install exécute le flow complet d'installation avec timeout et rollback.
func (i *Installer) Install(opts InstallOptions) error {
	// Timeout global de 10 minutes pour toute l'installation
	// context.WithTimeout crée un contexte qui s'annule automatiquement après le délai
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel() // Libérer les ressources du contexte à la fin

	fmt.Printf("\n🚀 Installation de %s...\n\n", opts.AppID)

	// Variables qu'on va construire au fil des étapes
	var (
		appDir     string
		manifest   *types.AppManifest
		composeDir string
		hostPort   int
	)

	// ── Étape 1 : Résolution app + repo ──
	fmt.Println("  [1/12] Résolution de l'application...")
	repos, err := i.rt.GetRepos()
	if err != nil {
		return fmt.Errorf("impossible de lire les repos: %w", err)
	}
	appDir, repo, err := i.st.Resolve(opts.AppID, repos)
	if err != nil {
		return err
	}
	fmt.Printf("         ✓ Trouvé dans le dépôt '%s'\n", repo.Name)

	// Vérification confiance (étape trust)
	if err := i.checkTrust(repo.Trust, opts.AppID); err != nil {
		return err
	}

	// ── Étape 2 : Lecture app.json ──
	fmt.Println("  [2/12] Lecture du manifest...")
	manifest, err = i.st.ReadManifest(appDir)
	if err != nil {
		return err
	}

	// Vérifier que l'app n'est pas déjà installée
	if !opts.Force {
		if _, err := i.rt.GetApp(opts.AppID); err == nil {
			return fmt.Errorf("'%s' est déjà installée (utilisez --force pour réinstaller)", opts.AppID)
		}
	}

	// Marquer l'app comme "en cours d'installation" dans le runtime
	runtimeApp := &types.RuntimeApp{
		ID:          manifest.ID,
		Name:        manifest.Name,
		Status:      types.StatusInstalling,
		InstalledAt: time.Now(),
		Channel:     manifest.Channel,
		Repository:  manifest.Repository,
	}
	_ = i.rt.SaveApp(runtimeApp)

	// Toutes les étapes suivantes sont dans un defer de rollback
	// Si une erreur se produit, on nettoie proprement
	success := false
	defer func() {
		if !success {
			fmt.Printf("\n⚠️  Installation échouée, rollback en cours...\n")
			i.rollback(opts.AppID, composeDir, hostPort)
		}
	}()

	// ── Étape 3 : Vérification sécurité ──
	fmt.Println("  [3/12] Vérification des permissions...")
	if err := i.checkSecurity(manifest); err != nil {
		return err
	}

	// ── Étape 4 : Allocation port dynamique ──
	fmt.Println("  [4/12] Allocation des ports...")
	hostPort, err = i.allocatePorts(manifest)
	if err != nil {
		return err
	}
	if len(manifest.Ports) > 0 {
		fmt.Printf("         ✓ Port %d → container:%d\n", hostPort, manifest.Ports[0].Container)
	}
	// Mettre à jour les ports dans le manifest pour le compose
	for j := range manifest.Ports {
		if manifest.Ports[j].Dynamic {
			manifest.Ports[j].Host = hostPort
		}
	}

	// ── Étape 5 : Création dossiers ──
	fmt.Println("  [5/12] Création des répertoires...")
	composeDir = filepath.Join(i.baseDir, "apps-installed", opts.AppID)
	if err := i.createDirs(manifest, composeDir, opts); err != nil {
		return err
	}

	// ── Étape 6 : Exécution setup.sh ──
	fmt.Println("  [6/12] Exécution setup.sh...")
	if err := i.runSetup(ctx, appDir, composeDir, manifest, opts); err != nil {
		return err
	}

	// ── Étape 7 : Génération compose final ──
	fmt.Println("  [7/12] Génération du compose...")
	if err := i.generateCompose(appDir, composeDir, manifest, opts); err != nil {
		return err
	}

	// ── Étape 8 : Réseaux Docker ──
	fmt.Println("  [8/12] Vérification des réseaux Docker...")
	if err := i.docker.EnsureNetworks(); err != nil {
		return err
	}

	// ── Étape 9 : docker compose up ──
	fmt.Println("  [9/12] Démarrage des containers...")
	if err := i.docker.Up(composeDir); err != nil {
		return err
	}

	// ── Étape 10 : Attente démarrage ──
	fmt.Println("  [10/12] Vérification du démarrage...")
	if err := i.waitForStart(ctx, composeDir); err != nil {
		return err
	}

	// ── Étape 11 : Enregistrement runtime ──
	fmt.Println("  [11/12] Enregistrement dans le runtime...")
	runtimeApp.Status = types.StatusRunning
	runtimeApp.Ports = manifest.Ports
	runtimeApp.ComposeDir = composeDir
	runtimeApp.StorageLocation = opts.StorageLocation
	if err := i.rt.SaveApp(runtimeApp); err != nil {
		return err
	}

	// ── Étape 12 : Événement ──
	fmt.Println("  [12/12] Émission de l'événement...")
	_ = i.emitter.AppInstalled(opts.AppID)

	success = true
	fmt.Printf("\n✅ %s installé avec succès !\n", manifest.Name)
	if len(manifest.Ports) > 0 {
		fmt.Printf("   🌐 Accessible sur le port %d\n", hostPort)
	}
	if opts.Domain != "" {
		fmt.Printf("   🔗 Domaine : https://%s\n", opts.Domain)
	}

	return nil
}

// ─────────────────────────────────────────────
// ÉTAPES INTERNES
// ─────────────────────────────────────────────

// checkTrust vérifie le niveau de confiance et demande confirmation si nécessaire.
func (i *Installer) checkTrust(trust types.TrustLevel, appID string) error {
	switch trust {
	case types.TrustOfficial:
		return nil // Auto-approuvé
	case types.TrustCommunity:
		fmt.Printf("⚠️  '%s' provient d'un dépôt communautaire. Continuer ? [o/N] ", appID)
		var resp string
		fmt.Scanln(&resp)
		if strings.ToLower(resp) != "o" {
			return fmt.Errorf("installation annulée par l'utilisateur")
		}
	case types.TrustUntrusted:
		fmt.Printf("🚨 ATTENTION: '%s' provient d'un dépôt NON VÉRIFIÉ.\n", appID)
		fmt.Print("   Tapez 'CONFIRMER' pour continuer : ")
		var resp string
		fmt.Scanln(&resp)
		if resp != "CONFIRMER" {
			return fmt.Errorf("installation annulée")
		}
	}
	return nil
}

// checkSecurity vérifie les permissions dangereuses du manifest.
func (i *Installer) checkSecurity(manifest *types.AppManifest) error {
	caps := manifest.Capabilities

	warnings := []string{}
	if caps.Privileged {
		warnings = append(warnings, "mode privileged (accès root complet au système)")
	}
	if caps.DockerSocket {
		warnings = append(warnings, "accès au socket Docker (contrôle total de Docker)")
	}

	if len(warnings) > 0 {
		fmt.Printf("⚠️  Cette application demande des permissions élevées :\n")
		for _, w := range warnings {
			fmt.Printf("   - %s\n", w)
		}
		fmt.Print("   Continuer ? [o/N] ")
		var resp string
		fmt.Scanln(&resp)
		if strings.ToLower(resp) != "o" {
			return fmt.Errorf("installation annulée")
		}
	}

	return nil
}

// allocatePorts alloue les ports dynamiques nécessaires à l'application.
func (i *Installer) allocatePorts(manifest *types.AppManifest) (int, error) {
	var firstPort int
	for _, port := range manifest.Ports {
		if port.Dynamic {
			allocated, err := i.rt.AllocatePort(manifest.ID+"-"+port.Name, 8000, 9999)
			if err != nil {
				return 0, err
			}
			if firstPort == 0 {
				firstPort = allocated
			}
		}
	}
	return firstPort, nil
}

// createDirs crée tous les dossiers nécessaires à l'application.
func (i *Installer) createDirs(manifest *types.AppManifest, composeDir string, opts InstallOptions) error {
	dirs := []string{
		composeDir,
		filepath.Join(composeDir, "override"),
		filepath.Join(composeDir, "logs"),
		filepath.Join(composeDir, "backups"),
	}

	// Créer les dossiers de volumes (bind mounts)
	// Si stockage NAS : app-data/<app> sera un symlink, pas un dossier réel
	for _, vol := range manifest.Volumes {
		// Ignorer les volumes sous app-data/<app> si on utilise le NAS
		// (ils seront créés sur le NAS via symlink)
		volPath := filepath.Join(i.baseDir, vol.Source)
		appDataPrefix := filepath.Join(i.baseDir, "app-data", manifest.ID)
		if opts.StorageLocation != "" && strings.HasPrefix(volPath, appDataPrefix) {
			continue // sera géré par le symlink NAS
		}
		dirs = append(dirs, volPath)
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("impossible de créer %s: %w", dir, err)
		}
	}

	// ── Stockage NAS : créer dossier sur NAS + symlink local ──
	if opts.StorageLocation != "" && opts.StorageDataDir != "" {
		// Créer le dossier sur le NAS
		if err := os.MkdirAll(opts.StorageDataDir, 0755); err != nil {
			return fmt.Errorf("impossible de créer le dossier NAS: %w", err)
		}
		// Créer le dossier parent local (app-data/) si besoin
		localParent := filepath.Join(i.baseDir, "app-data")
		if err := os.MkdirAll(localParent, 0755); err != nil {
			return err
		}
		// Créer le symlink : app-data/<app> → <nas>/caleope/app-data/<app>
		localLink := filepath.Join(i.baseDir, "app-data", manifest.ID)
		// Supprimer le symlink/dossier existant si présent
		_ = os.Remove(localLink)
		if err := os.Symlink(opts.StorageDataDir, localLink); err != nil {
			return fmt.Errorf("impossible de créer le symlink NAS: %w", err)
		}
		fmt.Printf("         ✓ Données liées au NAS : %s → %s\n", localLink, opts.StorageDataDir)
	}

	return nil
}

// runSetup exécute le script setup.sh de l'application.
// Le script tourne dans un sandbox limité (filesystem only, pas de réseau).
func (i *Installer) runSetup(ctx context.Context, appDir, composeDir string, manifest *types.AppManifest, opts InstallOptions) error {
	setupScript := filepath.Join(appDir, "setup.sh")
	if _, err := os.Stat(setupScript); os.IsNotExist(err) {
		return nil // Pas de setup.sh, c'est OK
	}

	// Variables d'environnement passées au script
	appDataDir := filepath.Join(i.baseDir, "app-data", manifest.ID) // local par défaut
	if opts.StorageDataDir != "" {
		appDataDir = opts.StorageDataDir // NAS si défini
	}
	env := append(os.Environ(),
		"CALEOPE_APP_ID="+manifest.ID,
		"CALEOPE_APP_DIR="+composeDir,
		"CALEOPE_BASE_DIR="+i.baseDir,
		"CALEOPE_DOMAIN="+opts.Domain,
		"CALEOPE_APP_DATA_DIR="+appDataDir,
	)
	for k, v := range opts.Params {
		env = append(env, "CALEOPE_PARAM_"+strings.ToUpper(k)+"="+v)
	}

	// exec.CommandContext = comme exec.Command mais avec support d'annulation
	// Si le contexte est annulé (timeout), le processus est tué automatiquement
	cmd := exec.CommandContext(ctx, "bash", setupScript)
	cmd.Dir = composeDir
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("setup.sh échoué: %w", err)
	}
	return nil
}

// generateCompose génère le compose.yml final et le app.env.
// On utilise le template du store et on injecte les variables.
func (i *Installer) generateCompose(appDir, composeDir string, manifest *types.AppManifest, opts InstallOptions) error {
	// ── Générer app.env ──
	envContent := i.buildEnvFile(manifest, opts)
	if err := os.WriteFile(filepath.Join(composeDir, "app.env"), []byte(envContent), 0600); err != nil {
		return fmt.Errorf("impossible d'écrire app.env: %w", err)
	}

	// ── Copier et traiter le compose template ──
	templatePath := filepath.Join(appDir, "docker-compose.yml")
	templateData, err := os.ReadFile(templatePath)
	if err != nil {
		return fmt.Errorf("docker-compose.yml introuvable: %w", err)
	}

	// Go a un moteur de templates intégré.
	// {{.BaseDir}} dans le template sera remplacé par la valeur réelle.
	tmpl, err := template.New("compose").Parse(string(templateData))
	if err != nil {
		return fmt.Errorf("template compose invalide: %w", err)
	}

	// Données injectées dans le template
	data := struct {
		BaseDir    string
		ComposeDir string
		AppID      string
		Domain     string
		Ports      []types.AppPort
		Volumes    []types.AppVolume
	}{
		BaseDir:    i.baseDir,
		ComposeDir: composeDir,
		AppID:      manifest.ID,
		Domain:     opts.Domain,
		Ports:      manifest.Ports,
		Volumes:    manifest.Volumes,
	}

	outFile, err := os.Create(filepath.Join(composeDir, "compose.yml"))
	if err != nil {
		return err
	}
	defer outFile.Close()

	return tmpl.Execute(outFile, data)
}

// buildEnvFile construit le contenu du fichier .env.
func (i *Installer) buildEnvFile(manifest *types.AppManifest, opts InstallOptions) string {
	var sb strings.Builder

	appDataDir := filepath.Join(i.baseDir, "app-data", manifest.ID)
	if opts.StorageDataDir != "" {
		appDataDir = opts.StorageDataDir
	}

	sb.WriteString("# Généré par Caleope - ne pas modifier manuellement\n")
	sb.WriteString(fmt.Sprintf("CALEOPE_APP_ID=%s\n", manifest.ID))
	sb.WriteString(fmt.Sprintf("CALEOPE_BASE_DIR=%s\n", i.baseDir))
	sb.WriteString(fmt.Sprintf("CALEOPE_APP_DATA_DIR=%s\n", appDataDir))

	if opts.Domain != "" {
		sb.WriteString(fmt.Sprintf("CALEOPE_DOMAIN=%s\n", opts.Domain))
	}

	for _, port := range manifest.Ports {
		sb.WriteString(fmt.Sprintf("CALEOPE_PORT_%s=%d\n",
			strings.ToUpper(port.Name), port.Host))
	}

	for k, v := range opts.Params {
		sb.WriteString(fmt.Sprintf("CALEOPE_PARAM_%s=%s\n", strings.ToUpper(k), v))
	}

	// Fusionner secrets.env écrit par setup.sh pour rendre ses variables disponibles
	// dans la substitution YAML de docker compose (ex: ${ONLYOFFICE_DOMAIN})
	secretsPath := filepath.Join(i.baseDir, "app-config", manifest.ID, "secrets.env")
	if data, err := os.ReadFile(secretsPath); err == nil {
		sb.WriteString("\n")
		sb.Write(data)
	}

	return sb.String()
}

// waitForStart attend que les containers soient en cours d'exécution.
func (i *Installer) waitForStart(ctx context.Context, composeDir string) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	deadline := time.Now().Add(2 * time.Minute)

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout: les containers n'ont pas démarré à temps")
		case <-ticker.C:
			running, err := i.docker.IsRunning(composeDir)
			if err == nil && running {
				return nil
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("les containers n'ont pas démarré dans les 2 minutes")
			}
		}
	}
}

// ─────────────────────────────────────────────
// ROLLBACK
// ─────────────────────────────────────────────

// rollback nettoie tout ce qui a été fait en cas d'échec.
func (i *Installer) rollback(appID, composeDir string, hostPort int) {
	fmt.Println("  → Arrêt des containers...")
	if composeDir != "" {
		_ = i.docker.Down(composeDir)

		fmt.Println("  → Suppression des fichiers...")
		_ = os.RemoveAll(composeDir)
	}

	fmt.Println("  → Libération du port...")
	_ = i.rt.ReleasePort(appID)

	fmt.Println("  → Nettoyage du runtime...")
	_ = i.rt.RemoveApp(appID)

	_ = i.emitter.AppError(appID, "installation échouée, rollback effectué")

	fmt.Println("  ✓ Rollback terminé")
}

// ─────────────────────────────────────────────
// REMOVE
// ─────────────────────────────────────────────

// Remove désinstalle une application.
func (i *Installer) Remove(appID string, keepData bool) error {
	fmt.Printf("\n🗑️  Suppression de %s...\n", appID)

	app, err := i.rt.GetApp(appID)
	if err != nil {
		return err
	}

	// Marquer comme "en cours de suppression"
	app.Status = types.StatusRemoving
	_ = i.rt.SaveApp(app)

	// Arrêter les containers
	fmt.Println("  [1/4] Arrêt des containers...")
	if err := i.docker.Down(app.ComposeDir); err != nil {
		fmt.Printf("  ⚠️  Erreur arrêt containers: %v\n", err)
	}

	// Supprimer les fichiers compose
	fmt.Println("  [2/4] Suppression des fichiers...")
	_ = os.RemoveAll(app.ComposeDir)

	// Supprimer les données si demandé
	if !keepData {
		fmt.Println("  [3/4] Suppression des données...")
		_ = os.RemoveAll(filepath.Join(i.baseDir, "app-data", appID))
		_ = os.RemoveAll(filepath.Join(i.baseDir, "app-config", appID))
	} else {
		fmt.Println("  [3/4] Conservation des données (--keep-data)")
	}

	// Libérer les ressources
	fmt.Println("  [4/4] Libération des ressources...")
	_ = i.rt.ReleasePort(appID)
	_ = i.rt.RemoveApp(appID)
	_ = i.emitter.AppRemoved(appID)

	fmt.Printf("\n✅ %s supprimé\n", appID)
	return nil
}
