// internal/scheduler/scheduler.go
//
// Planificateur de tâches — tourne en goroutine dans le daemon.
// Lit tasks.json chaque minute, exécute les tâches dont l'heure et le jour correspondent.
// Les exécutions sont enregistrées dans tasks.json (last_run, last_status).

package scheduler

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gaiver-it/caleope/pkg/types"
)

const tasksFile = "core/tasks.json"

// Runner est l'interface que le scheduler utilise pour exécuter les tâches.
// Le daemon injecte une implémentation concrète qui appelle backup, upgrade, etc.
type Runner interface {
	RunBackup(app string, scope types.BackupScope) error
	RunUpgrade() error
	RunUpdate() error
}

type Scheduler struct {
	baseDir string
	runner  Runner
	mu      sync.Mutex
	stop    chan struct{}
}

func New(baseDir string, runner Runner) *Scheduler {
	return &Scheduler{
		baseDir: baseDir,
		runner:  runner,
		stop:    make(chan struct{}),
	}
}

// Start lance le scheduler en arrière-plan. Il se réveille chaque minute.
func (s *Scheduler) Start() {
	go func() {
		// Attendre le début de la minute suivante pour se synchroniser avec l'horloge
		now := time.Now()
		sleepUntil := now.Truncate(time.Minute).Add(time.Minute)
		select {
		case <-time.After(time.Until(sleepUntil)):
		case <-s.stop:
			return
		}

		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()

		s.tick()
		for {
			select {
			case <-ticker.C:
				s.tick()
			case <-s.stop:
				return
			}
		}
	}()
}

func (s *Scheduler) Stop() {
	close(s.stop)
}

func (s *Scheduler) tick() {
	now := time.Now()
	tasks, err := s.Load()
	if err != nil || len(tasks) == 0 {
		return
	}

	changed := false
	for i, t := range tasks {
		if !t.Enabled {
			continue
		}
		if !shouldRun(t, now) {
			continue
		}
		// Vérifier qu'on n'a pas déjà exécuté cette tâche cette minute
		if !t.LastRun.IsZero() && t.LastRun.Year() == now.Year() &&
			t.LastRun.YearDay() == now.YearDay() &&
			t.LastRun.Hour() == now.Hour() &&
			t.LastRun.Minute() == now.Minute() {
			continue
		}

		err := s.execute(t)
		tasks[i].LastRun = now
		if err != nil {
			tasks[i].LastStatus = "error: " + err.Error()
		} else {
			tasks[i].LastStatus = "ok"
		}
		changed = true
	}

	if changed {
		_ = s.save(tasks)
	}
}

func (s *Scheduler) execute(t types.Task) error {
	switch t.Type {
	case types.TaskTypeBackup:
		scope := t.Scope
		if scope == "" {
			scope = types.BackupScopeAll
		}
		return s.runner.RunBackup(t.App, scope)
	case types.TaskTypeUpgrade:
		return s.runner.RunUpgrade()
	case types.TaskTypeUpdate:
		return s.runner.RunUpdate()
	default:
		return fmt.Errorf("type de tâche inconnu : %s", t.Type)
	}
}

// shouldRun retourne true si la tâche doit s'exécuter à l'instant donné.
func shouldRun(t types.Task, now time.Time) bool {
	if t.Schedule.Hour != now.Hour() || t.Schedule.Minute != now.Minute() {
		return false
	}
	if len(t.Schedule.Days) == 0 {
		return true
	}
	wd := int(now.Weekday()) // 0=dim, 1=lun, …, 6=sam
	for _, d := range t.Schedule.Days {
		if d == wd {
			return true
		}
	}
	return false
}

// ── Persistance ───────────────────────────────────────────────────────────────

func (s *Scheduler) Load() ([]types.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return loadTasks(filepath.Join(s.baseDir, tasksFile))
}

func (s *Scheduler) save(tasks []types.Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return saveTasks(filepath.Join(s.baseDir, tasksFile), tasks)
}

func (s *Scheduler) Add(t types.Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := filepath.Join(s.baseDir, tasksFile)
	tasks, _ := loadTasks(path)
	for _, existing := range tasks {
		if existing.ID == t.ID {
			return fmt.Errorf("une tâche avec l'id '%s' existe déjà", t.ID)
		}
	}
	tasks = append(tasks, t)
	return saveTasks(path, tasks)
}

func (s *Scheduler) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := filepath.Join(s.baseDir, tasksFile)
	tasks, err := loadTasks(path)
	if err != nil {
		return err
	}
	filtered := tasks[:0]
	found := false
	for _, t := range tasks {
		if t.ID == id {
			found = true
			continue
		}
		filtered = append(filtered, t)
	}
	if !found {
		return fmt.Errorf("tâche '%s' introuvable", id)
	}
	return saveTasks(path, filtered)
}

func (s *Scheduler) Toggle(id string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := filepath.Join(s.baseDir, tasksFile)
	tasks, err := loadTasks(path)
	if err != nil {
		return err
	}
	for i, t := range tasks {
		if t.ID == id {
			tasks[i].Enabled = enabled
			return saveTasks(path, tasks)
		}
	}
	return fmt.Errorf("tâche '%s' introuvable", id)
}

// RunNow exécute une tâche immédiatement (hors scheduler).
func (s *Scheduler) RunNow(id string) error {
	tasks, err := s.Load()
	if err != nil {
		return err
	}
	for i, t := range tasks {
		if t.ID == id {
			err := s.execute(t)
			tasks[i].LastRun = time.Now()
			if err != nil {
				tasks[i].LastStatus = "error: " + err.Error()
			} else {
				tasks[i].LastStatus = "ok"
			}
			_ = s.save(tasks)
			return err
		}
	}
	return fmt.Errorf("tâche '%s' introuvable", id)
}

func loadTasks(path string) ([]types.Task, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return []types.Task{}, nil
	}
	if err != nil {
		return nil, err
	}
	var tasks []types.Task
	if err := json.Unmarshal(data, &tasks); err != nil {
		return nil, err
	}
	return tasks, nil
}

func saveTasks(path string, tasks []types.Task) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(tasks, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
