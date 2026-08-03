// Package supervisor surveille les applications installées et relance celles
// qui sont tombées.
//
// ── POURQUOI CE PAQUET EXISTE ────────────────────────────────────────────────
// La politique Docker `unless-stopped` ne relance PAS un conteneur dont le
// `start` a échoué : Docker le considère arrêté volontairement, et personne ne
// revient jamais le chercher — ni au tick suivant, ni au redémarrage de la
// machine. Sur une installation de production, cela a laissé une application
// éteinte pendant trois jours sans que rien ne le signale, pendant que les
// conteneurs voisins de la même app tournaient.
//
// Second problème, plus insidieux : le statut enregistré dans runtime/apps/
// n'est écrit QUE lorsqu'on agit sur l'application (installation, démarrage,
// arrêt). Rien ne le confronte jamais à la réalité. Une app dont les conteneurs
// meurent reste donc affichée « en marche » dans l'interface. Le tableau de
// bord ment, et c'est pire que pas de tableau de bord.
//
// Ce paquet répond aux deux : il RÉCONCILIE (le statut affiché redevient vrai)
// et il RÉPARE (ce qui devait tourner est relancé).
//
// ── LES TROIS RÈGLES DE SÛRETÉ ───────────────────────────────────────────────
//
//	A. On ne relance QUE ce que l'utilisateur avait laissé en marche. Une app
//	   arrêtée volontairement le reste : un garde-fou qui se bat contre son
//	   propriétaire est un défaut, pas une fonctionnalité.
//
//	B. On ne démarre JAMAIS une app dont le stockage réseau est absent. Un
//	   démarrage dans un montage mort échoue et laisse l'app éteinte : la
//	   « réparation » aggrave alors la panne au lieu de la corriger. C'est
//	   exactement l'enchaînement qui a produit les trois jours d'arrêt.
//
//	C. Toute action est VÉRIFIÉE. On relit l'état après avoir agi et on
//	   journalise ce qu'on a constaté, jamais ce qu'on a tenté.
package supervisor

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gaiver-it/caleope/pkg/types"
)

// DefaultInterval : une panne est rattrapée en moins d'une minute, pour un coût
// négligeable (quelques `docker compose ps` par minute).
const DefaultInterval = 60 * time.Second

// DefaultCooldown : délai minimum entre deux tentatives sur la MÊME app, pour
// qu'une app réellement cassée ne soit pas relancée en boucle.
const DefaultCooldown = 5 * time.Minute

// ── Dépendances, sous forme d'interfaces pour que la décision soit testable
// sans Docker, sans NFS et sans disque. ──────────────────────────────────────

type AppStore interface {
	ListApps() ([]*types.RuntimeApp, error)
	SaveApp(app *types.RuntimeApp) error
}

type Compose interface {
	IsRunning(composeDir string) (bool, error)
	Start(composeDir string) error
}

// Mounts répond à une seule question : le stockage nommé est-il utilisable ?
type Mounts interface {
	Healthy(location string) bool
}

// Action est ce que le superviseur a décidé pour une application.
type Action int

const (
	ActionNone       Action = iota // rien à faire
	ActionReconcile                // l'état enregistré était faux, on le corrige
	ActionStart                    // on relance
	ActionBlocked                  // il faudrait relancer, mais le stockage est absent
	ActionCooldown                 // il faudrait relancer, mais c'est trop tôt
)

func (a Action) String() string {
	switch a {
	case ActionReconcile:
		return "reconcile"
	case ActionStart:
		return "start"
	case ActionBlocked:
		return "blocked"
	case ActionCooldown:
		return "cooldown"
	}
	return "none"
}

// Report décrit ce qui a été fait sur une application pendant un passage.
type Report struct {
	App     string
	Action  Action
	Repaired bool   // vrai seulement si on a CONSTATÉ le retour en marche
	Detail  string
}

// decide est le cœur du paquet, et il est volontairement PUR : pas d'appel
// système, pas d'horloge cachée, pas d'effet de bord. Toute la logique de
// sûreté se teste donc à la table, y compris les cas d'échec — qui sont
// justement ceux qui ne se produisent jamais pendant qu'on regarde.
func decide(recorded types.AppStatus, actuallyRunning, needsStorage, storageOK, cooldownElapsed bool) Action {
	if actuallyRunning {
		// La réalité fait foi : si l'app tourne mais qu'on l'avait notée
		// autrement, c'est la note qui est fausse.
		if recorded != types.StatusRunning {
			return ActionReconcile
		}
		return ActionNone
	}

	// L'app ne tourne pas. N'agir que si elle était censée tourner (règle A).
	// « installing » et « removing » sont des états transitoires : une
	// installation en cours ne doit surtout pas être « réparée » au milieu.
	if recorded != types.StatusRunning {
		return ActionNone
	}

	// Règle B : sans son stockage, la relancer ne peut qu'échouer.
	if needsStorage && !storageOK {
		return ActionBlocked
	}

	if !cooldownElapsed {
		return ActionCooldown
	}
	return ActionStart
}

// ── Superviseur ──────────────────────────────────────────────────────────────

type Supervisor struct {
	apps     AppStore
	compose  Compose
	mounts   Mounts
	interval time.Duration
	cooldown time.Duration
	logf     func(format string, args ...any)

	mu       sync.Mutex
	lastTry  map[string]time.Time
	now      func() time.Time // injectable pour les tests
	settle   time.Duration    // temps laissé à l'app pour démarrer avant vérification
	stop     chan struct{}
	stopOnce sync.Once
}

func New(apps AppStore, compose Compose, mounts Mounts, logf func(string, ...any)) *Supervisor {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Supervisor{
		apps:     apps,
		compose:  compose,
		mounts:   mounts,
		interval: DefaultInterval,
		cooldown: DefaultCooldown,
		logf:     logf,
		lastTry:  map[string]time.Time{},
		now:      time.Now,
		settle:   5 * time.Second,
		stop:     make(chan struct{}),
	}
}

func (s *Supervisor) Start() {
	go func() {
		t := time.NewTicker(s.interval)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				s.Check()
			case <-s.stop:
				return
			}
		}
	}()
}

func (s *Supervisor) Stop() { s.stopOnce.Do(func() { close(s.stop) }) }

// Check effectue un passage complet et rend le compte rendu de ce qui a été
// réellement constaté.
func (s *Supervisor) Check() []Report {
	apps, err := s.apps.ListApps()
	if err != nil {
		s.logf("supervision : impossible de lister les applications : %v", err)
		return nil
	}

	var reports []Report
	for _, app := range apps {
		if app == nil || app.ComposeDir == "" {
			continue
		}

		running, err := s.compose.IsRunning(app.ComposeDir)
		if err != nil {
			// On ne SAIT pas : on ne touche à rien. Agir sur une mesure
			// ratée, c'est exactement la faute qu'on cherche à éviter.
			s.logf("supervision : état de %s indéterminé (%v) — aucune action", app.ID, err)
			continue
		}

		needsStorage := app.StorageLocation != ""
		storageOK := !needsStorage || s.mounts.Healthy(app.StorageLocation)

		s.mu.Lock()
		last := s.lastTry[app.ID]
		s.mu.Unlock()
		cooldownElapsed := s.now().Sub(last) >= s.cooldown

		action := decide(app.Status, running, needsStorage, storageOK, cooldownElapsed)
		rep := Report{App: app.ID, Action: action}

		switch action {
		case ActionReconcile:
			rep.Detail = fmt.Sprintf("état enregistré %q alors que l'app tourne — corrigé", app.Status)
			app.Status = types.StatusRunning
			app.Error = ""
			if err := s.apps.SaveApp(app); err != nil {
				rep.Detail = fmt.Sprintf("correction de l'état impossible : %v", err)
			}
			s.logf("supervision : %s — %s", app.ID, rep.Detail)

		case ActionBlocked:
			rep.Detail = fmt.Sprintf("stockage réseau %q absent — relance NON tentée (un démarrage échouerait et laisserait l'app éteinte)", app.StorageLocation)
			s.logf("supervision : %s — %s", app.ID, rep.Detail)

		case ActionCooldown:
			rep.Detail = "relance déjà tentée récemment — nouvelle tentative au prochain passage"

		case ActionStart:
			s.mu.Lock()
			s.lastTry[app.ID] = s.now()
			s.mu.Unlock()
			s.logf("supervision : %s est arrêtée alors qu'elle devrait tourner — relance", app.ID)

			startErr := s.compose.Start(app.ComposeDir)
			time.Sleep(s.settle)

			// Règle C : on relit l'état réel. Un `start` qui rend 0 ne prouve
			// rien, et un `start` en erreur peut malgré tout avoir abouti.
			nowRunning, checkErr := s.compose.IsRunning(app.ComposeDir)
			switch {
			case checkErr == nil && nowRunning:
				rep.Repaired = true
				rep.Detail = "relancée et vérifiée en marche"
				app.Status = types.StatusRunning
				app.Error = ""
				_ = s.apps.SaveApp(app)
			default:
				detail := "la relance n'a pas abouti"
				if startErr != nil {
					detail = fmt.Sprintf("la relance a échoué : %v", startErr)
				}
				rep.Detail = detail + " — intervention humaine requise"
				app.Status = types.StatusError
				app.Error = detail
				_ = s.apps.SaveApp(app)
			}
			s.logf("supervision : %s — %s", app.ID, rep.Detail)
		}

		if action != ActionNone {
			reports = append(reports, rep)
		}
	}
	return reports
}

// ── Vérification des montages réseau ─────────────────────────────────────────

// MountPointer est ce que le paquet network fournit : le chemin d'un stockage.
type MountPointer interface {
	MountPoint(name string) string
}

type realMounts struct{ mp MountPointer }

// NewMounts construit le vérificateur de montages réel.
func NewMounts(mp MountPointer) Mounts { return &realMounts{mp: mp} }

// Healthy exige DEUX choses, et la première est facile à oublier :
//
//	1. que le chemin soit un VRAI point de montage. Quand un partage réseau
//	   disparaît, le dossier local nu subsiste et reste parfaitement lisible :
//	   un simple test de lecture répond « tout va bien » pendant que Docker,
//	   lui, n'arrive plus à créer son bind mount.
//	2. qu'il soit lisible DANS UN DÉLAI BORNÉ. Un montage NFS mort ne renvoie
//	   pas d'erreur : il bloque. Sans délai maximum, c'est le superviseur
//	   lui-même qui se fige.
func (r *realMounts) Healthy(location string) bool {
	path := r.mp.MountPoint(location)
	if path == "" {
		return false
	}
	if !isMountPoint(path) {
		return false
	}
	return readableWithin(path, 8*time.Second)
}

func isMountPoint(path string) bool {
	f, err := os.Open("/proc/mounts")
	if err != nil {
		// Pas de /proc (hors Linux) : on ne peut pas trancher, on ne bloque
		// donc pas les démarrages sur une information qu'on n'a pas.
		return true
	}
	defer f.Close()

	clean := strings.TrimSuffix(path, "/")
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		// Les espaces sont encodés \040 dans /proc/mounts.
		mp := strings.ReplaceAll(fields[1], `\040`, " ")
		if strings.TrimSuffix(mp, "/") == clean {
			return true
		}
	}
	return false
}

func readableWithin(path string, d time.Duration) bool {
	done := make(chan bool, 1)
	go func() {
		f, err := os.Open(path)
		if err != nil {
			done <- false
			return
		}
		defer f.Close()
		_, err = f.Readdirnames(1)
		// io.EOF = dossier vide mais lisible : c'est un succès.
		done <- err == nil || err.Error() == "EOF"
	}()
	select {
	case ok := <-done:
		return ok
	case <-time.After(d):
		// La goroutine reste bloquée sur le NFS mort ; elle se terminera avec
		// le processus. On ne peut pas l'interrompre, mais on refuse de
		// bloquer avec elle.
		return false
	}
}
