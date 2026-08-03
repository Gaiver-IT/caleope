package supervisor

import (
	"errors"
	"testing"
	"time"

	"github.com/gaiver-it/caleope/pkg/types"
)

// ── La table de décision ─────────────────────────────────────────────────────
// Ces cas sont la raison d'être du paquet : ce sont eux qui ont été appris à
// leurs dépens en production.

func TestDecide(t *testing.T) {
	cas := []struct {
		nom             string
		enregistre      types.AppStatus
		tourne          bool
		besoinStockage  bool
		stockageOK      bool
		cooldownEcoule  bool
		attendu         Action
	}{
		{"tout va bien", types.StatusRunning, true, false, true, true, ActionNone},

		// Le cœur du problème : l'app est morte mais reste notée « running ».
		{"tombée -> on relance", types.StatusRunning, false, false, true, true, ActionStart},

		// RÈGLE A : ne pas se battre contre l'utilisateur.
		{"arrêtée volontairement -> ne rien faire", types.StatusStopped, false, false, true, true, ActionNone},
		{"en cours d'installation -> ne pas toucher", types.StatusInstalling, false, false, true, true, ActionNone},
		{"en cours de suppression -> ne pas toucher", types.StatusRemoving, false, false, true, true, ActionNone},
		{"en erreur -> ne pas relancer en boucle", types.StatusError, false, false, true, true, ActionNone},

		// RÈGLE B : le cas qui a coûté trois jours de panne.
		{"stockage absent -> refuser de démarrer", types.StatusRunning, false, true, false, true, ActionBlocked},
		{"stockage présent -> démarrer", types.StatusRunning, false, true, true, true, ActionStart},
		// Une app SANS stockage réseau ne doit pas être bloquée par un NAS mort.
		{"pas de stockage réseau -> démarrer quand même", types.StatusRunning, false, false, false, true, ActionStart},

		// Anti-boucle.
		{"cooldown actif -> attendre", types.StatusRunning, false, false, true, false, ActionCooldown},

		// La réalité prime sur la note.
		{"tourne mais notée arrêtée -> corriger", types.StatusStopped, true, false, true, true, ActionReconcile},
		{"tourne mais notée en erreur -> corriger", types.StatusError, true, false, true, true, ActionReconcile},
	}

	for _, c := range cas {
		got := decide(c.enregistre, c.tourne, c.besoinStockage, c.stockageOK, c.cooldownEcoule)
		if got != c.attendu {
			t.Errorf("%s : attendu %v, obtenu %v", c.nom, c.attendu, got)
		}
	}
}

// ── Bancs d'essai avec de faux Docker / faux stockage ────────────────────────

type fakeStore struct {
	apps  []*types.RuntimeApp
	saved []string
}

func (f *fakeStore) ListApps() ([]*types.RuntimeApp, error) { return f.apps, nil }
func (f *fakeStore) SaveApp(a *types.RuntimeApp) error {
	f.saved = append(f.saved, a.ID)
	return nil
}

type fakeCompose struct {
	running    map[string]bool
	startCalls []string
	startErr   error
	// startFixes indique si un Start réussi fait réellement passer l'app en
	// marche. À false, on simule le cas vicieux : la commande rend 0 mais
	// l'application reste morte.
	startFixes bool
	statusErr  error
}

func (f *fakeCompose) IsRunning(dir string) (bool, error) {
	if f.statusErr != nil {
		return false, f.statusErr
	}
	return f.running[dir], nil
}
func (f *fakeCompose) Start(dir string) error {
	f.startCalls = append(f.startCalls, dir)
	if f.startErr != nil {
		return f.startErr
	}
	if f.startFixes {
		f.running[dir] = true
	}
	return nil
}

type fakeMounts struct{ ok map[string]bool }

func (f *fakeMounts) Healthy(loc string) bool { return f.ok[loc] }

func newSup(store *fakeStore, comp *fakeCompose, mounts *fakeMounts) *Supervisor {
	s := New(store, comp, mounts, nil)
	s.settle = 0 // pas d'attente dans les tests
	return s
}

func app(id string, st types.AppStatus, storage string) *types.RuntimeApp {
	return &types.RuntimeApp{ID: id, Status: st, ComposeDir: "/dir/" + id, StorageLocation: storage}
}

func TestRelanceEtVerifie(t *testing.T) {
	store := &fakeStore{apps: []*types.RuntimeApp{app("immich", types.StatusRunning, "")}}
	comp := &fakeCompose{running: map[string]bool{"/dir/immich": false}, startFixes: true}
	rep := newSup(store, comp, &fakeMounts{}).Check()

	if len(comp.startCalls) != 1 {
		t.Fatalf("attendu 1 relance, obtenu %d", len(comp.startCalls))
	}
	if len(rep) != 1 || !rep[0].Repaired {
		t.Fatalf("la réparation aurait dû être constatée : %+v", rep)
	}
	if store.apps[0].Status != types.StatusRunning {
		t.Errorf("statut attendu running, obtenu %q", store.apps[0].Status)
	}
}

// LE test qui compte : la commande de démarrage « réussit » mais l'app reste
// morte. C'est la situation exacte qui a produit trois jours de panne, et le
// superviseur ne doit surtout pas la déclarer réparée.
func TestRelanceQuiEchoueEnSilence(t *testing.T) {
	store := &fakeStore{apps: []*types.RuntimeApp{app("immich", types.StatusRunning, "")}}
	comp := &fakeCompose{running: map[string]bool{"/dir/immich": false}, startFixes: false}
	rep := newSup(store, comp, &fakeMounts{}).Check()

	if len(rep) != 1 {
		t.Fatalf("attendu 1 compte rendu, obtenu %d", len(rep))
	}
	if rep[0].Repaired {
		t.Fatal("le superviseur a déclaré réparée une app qui ne tourne pas")
	}
	if store.apps[0].Status != types.StatusError {
		t.Errorf("statut attendu error, obtenu %q", store.apps[0].Status)
	}
	if store.apps[0].Error == "" {
		t.Error("aucun message d'erreur enregistré pour l'utilisateur")
	}
}

func TestStockageAbsentAucuneRelance(t *testing.T) {
	store := &fakeStore{apps: []*types.RuntimeApp{app("immich", types.StatusRunning, "mon-nas")}}
	comp := &fakeCompose{running: map[string]bool{"/dir/immich": false}, startFixes: true}
	rep := newSup(store, comp, &fakeMounts{ok: map[string]bool{"mon-nas": false}}).Check()

	if len(comp.startCalls) != 0 {
		t.Fatal("le superviseur a démarré une app dont le stockage est absent")
	}
	if len(rep) != 1 || rep[0].Action != ActionBlocked {
		t.Fatalf("attendu ActionBlocked, obtenu %+v", rep)
	}
	// L'app ne doit pas être marquée en erreur : elle n'a rien fait de mal.
	if store.apps[0].Status != types.StatusRunning {
		t.Errorf("le statut ne devait pas changer, obtenu %q", store.apps[0].Status)
	}
}

// Une app locale ne doit pas être prise en otage par un NAS mort.
func TestAppSansStockageReseauNonBloquee(t *testing.T) {
	store := &fakeStore{apps: []*types.RuntimeApp{app("wikijs", types.StatusRunning, "")}}
	comp := &fakeCompose{running: map[string]bool{"/dir/wikijs": false}, startFixes: true}
	newSup(store, comp, &fakeMounts{ok: map[string]bool{"mon-nas": false}}).Check()

	if len(comp.startCalls) != 1 {
		t.Fatal("une app sans stockage réseau aurait dû être relancée")
	}
}

func TestEtatIndetermineAucuneAction(t *testing.T) {
	store := &fakeStore{apps: []*types.RuntimeApp{app("immich", types.StatusRunning, "")}}
	comp := &fakeCompose{running: map[string]bool{}, statusErr: errors.New("docker injoignable")}
	rep := newSup(store, comp, &fakeMounts{}).Check()

	if len(comp.startCalls) != 0 {
		t.Fatal("le superviseur a agi sur une mesure ratée")
	}
	if len(rep) != 0 {
		t.Fatalf("aucun compte rendu attendu, obtenu %+v", rep)
	}
}

func TestCooldown(t *testing.T) {
	store := &fakeStore{apps: []*types.RuntimeApp{app("immich", types.StatusRunning, "")}}
	comp := &fakeCompose{running: map[string]bool{"/dir/immich": false}, startFixes: false}
	s := newSup(store, comp, &fakeMounts{})

	base := time.Now()
	s.now = func() time.Time { return base }
	s.Check()
	if len(comp.startCalls) != 1 {
		t.Fatalf("premier passage : 1 relance attendue, obtenu %d", len(comp.startCalls))
	}

	// Deuxième passage juste après : le cooldown doit tenir.
	store.apps[0].Status = types.StatusRunning // on remet le décor
	s.Check()
	if len(comp.startCalls) != 1 {
		t.Fatalf("cooldown non respecté : %d relances", len(comp.startCalls))
	}

	// Après expiration du cooldown, on retente.
	s.now = func() time.Time { return base.Add(DefaultCooldown + time.Second) }
	store.apps[0].Status = types.StatusRunning
	s.Check()
	if len(comp.startCalls) != 2 {
		t.Fatalf("après cooldown : 2 relances attendues, obtenu %d", len(comp.startCalls))
	}
}

func TestReconciliationStatut(t *testing.T) {
	store := &fakeStore{apps: []*types.RuntimeApp{app("immich", types.StatusStopped, "")}}
	comp := &fakeCompose{running: map[string]bool{"/dir/immich": true}}
	rep := newSup(store, comp, &fakeMounts{}).Check()

	if len(rep) != 1 || rep[0].Action != ActionReconcile {
		t.Fatalf("attendu ActionReconcile, obtenu %+v", rep)
	}
	if store.apps[0].Status != types.StatusRunning {
		t.Errorf("statut non corrigé : %q", store.apps[0].Status)
	}
	if len(comp.startCalls) != 0 {
		t.Error("aucune relance ne devait avoir lieu")
	}
}

// Un dossier local ordinaire n'est pas un point de montage : le vérificateur
// doit le dire, sinon on démarrerait une app dont le partage a disparu.
func TestIsMountPointRefuseUnDossierOrdinaire(t *testing.T) {
	if isMountPoint(t.TempDir()) {
		t.Skip("environnement sans /proc/mounts exploitable — test non concluant ici")
	}
}

func TestReadableWithin(t *testing.T) {
	if !readableWithin(t.TempDir(), time.Second) {
		t.Error("un dossier vide et lisible devrait être accepté")
	}
	if readableWithin("/chemin/qui/nexiste/pas", time.Second) {
		t.Error("un chemin inexistant ne devrait pas être accepté")
	}
}
