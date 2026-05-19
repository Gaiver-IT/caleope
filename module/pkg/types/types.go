// pkg/types/types.go
//
// 📦 LES TYPES PARTAGÉS
//
// En Go, on définit des "structs" pour représenter des données structurées.
// C'est l'équivalent des classes en Python/Java, mais sans méthodes attachées.
// Ces types sont utilisés partout dans le projet, donc on les met dans "pkg/"
// (= code réutilisable, pas spécifique à un composant).

package types

import "time"

// ─────────────────────────────────────────────
// APP MANIFEST — correspond à app.json dans le store
// ─────────────────────────────────────────────

// AppManifest est la structure qui représente un app.json.
// Les tags `json:"..."` indiquent comment le champ s'appelle dans le JSON.
// Ex: "id" dans le JSON → champ Id dans Go.
type AppManifest struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Category     string            `json:"category"`
	Channel      string            `json:"channel"`       // stable, latest, nightly
	Repository   string            `json:"repository"`    // official, community, untrusted
	Capabilities AppCapabilities   `json:"capabilities"`
	Network      AppNetwork        `json:"network"`
	Ports        []AppPort         `json:"ports"`
	Volumes      []AppVolume       `json:"volumes"`
	Backup       AppBackup         `json:"backup"`
}

type AppCapabilities struct {
	ReverseProxy bool `json:"reverse_proxy"`
	GPU          bool `json:"gpu"`
	DockerSocket bool `json:"docker_socket"`
	Privileged   bool `json:"privileged"`
}

type AppNetwork struct {
	Public   bool `json:"public"`
	Internal bool `json:"internal"`
}

type AppPort struct {
	Name      string `json:"name"`
	Container int    `json:"container"` // port dans le container
	Host      int    `json:"host"`      // port alloué dynamiquement sur l'hôte
	Dynamic   bool   `json:"dynamic"`   // true = Caleope choisit le port hôte
}

type AppVolume struct {
	Source string `json:"source"` // chemin relatif sur l'hôte (ex: app-data/jellyfin)
	Target string `json:"target"` // chemin dans le container (ex: /config)
}

type AppBackup struct {
	Enabled bool `json:"enabled"`
}

// ─────────────────────────────────────────────
// RUNTIME APP — état d'une app installée
// Stocké dans runtime/apps/<id>.json
// ─────────────────────────────────────────────

// AppStatus représente l'état courant d'une application.
// Go utilise des "constantes typées" pour les valeurs fixes comme les statuts.
type AppStatus string

const (
	StatusInstalling AppStatus = "installing"
	StatusRunning    AppStatus = "running"
	StatusStopped    AppStatus = "stopped"
	StatusError      AppStatus = "error"
	StatusRemoving   AppStatus = "removing"
)

// RuntimeApp est ce qu'on stocke dans runtime/apps/jellyfin.json
// C'est l'état vivant de l'application sur le système.
type RuntimeApp struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Status      AppStatus  `json:"status"`
	InstalledAt time.Time  `json:"installed_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	Version     string     `json:"version"`
	Channel     string     `json:"channel"`
	Repository  string     `json:"repository"`
	Ports       []AppPort  `json:"ports"`   // avec les ports hôtes alloués
	ComposeDir  string     `json:"compose_dir"`  // chemin vers apps-installed/<id>/
	Error       string     `json:"error,omitempty"` // omitempty = absent du JSON si vide
}

// ─────────────────────────────────────────────
// EVENTS — système JSONL
// ─────────────────────────────────────────────

// Event est une ligne dans le fichier events/events.jsonl.
// JSONL = JSON Lines : 1 objet JSON par ligne, format append-only.
// C'est idéal pour les logs car on n'a jamais besoin de réécrire le fichier.
type Event struct {
	Timestamp time.Time         `json:"timestamp"`
	Type      string            `json:"event"`   // install, update, remove, error...
	App       string            `json:"app,omitempty"`
	Meta      map[string]string `json:"meta,omitempty"` // données additionnelles libres
}

// ─────────────────────────────────────────────
// API — messages échangés sur le socket UNIX
// ─────────────────────────────────────────────

// APIRequest est ce que le CLI envoie au daemon.
// Command = "install", "remove", "list", "info", "logs"
// Args = paramètres de la commande (ex: {"app": "jellyfin"})
type APIRequest struct {
	Command string            `json:"command"`
	Args    map[string]string `json:"args,omitempty"`
}

// APIResponse est ce que le daemon renvoie au CLI.
// Success = true/false
// Data = n'importe quelle structure JSON (on utilise interface{} = type quelconque)
// Error = message d'erreur si Success=false
type APIResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// ─────────────────────────────────────────────
// REPO — dépôts Git du store
// ─────────────────────────────────────────────

// TrustLevel définit le niveau de confiance d'un dépôt.
type TrustLevel string

const (
	TrustOfficial  TrustLevel = "official"
	TrustCommunity TrustLevel = "community"
	TrustUntrusted TrustLevel = "untrusted"
)

// Repo représente un dépôt enregistré dans runtime/repos.json
type Repo struct {
	Name     string     `json:"name"`
	URL      string     `json:"url"`
	Trust    TrustLevel `json:"trust"`
	LocalDir string     `json:"local_dir"` // chemin du cache local
	LastSync time.Time  `json:"last_sync"`
}
