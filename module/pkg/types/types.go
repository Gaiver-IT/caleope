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
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Category      string          `json:"category"`
	Channel       string          `json:"channel"`        // stable, latest, nightly
	Repository    string          `json:"repository"`     // official, community, untrusted
	Capabilities  AppCapabilities `json:"capabilities"`
	Network       AppNetwork      `json:"network"`
	Ports         []AppPort       `json:"ports"`
	Volumes       []AppVolume     `json:"volumes"`
	Backup        AppBackup       `json:"backup"`
	UseBaseDomain bool            `json:"use_base_domain"` // true = domaine racine (pas appID.domain)
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
// PARAMS — paramètres demandés à l'utilisateur lors de l'install
// Définis dans params.json à la racine de l'app dans le store
// ─────────────────────────────────────────────

// ParamDef décrit un paramètre interactif demandé lors de l'installation.
// Le champ ID (en majuscules) devient la variable CALEOPE_PARAM_<ID> dans setup.sh.
// Types supportés : "string", "secret" (masqué), "path", "bool", "select".
type ParamDef struct {
	ID          string   `json:"id"`                    // nom de la variable (→ CALEOPE_PARAM_<ID>)
	Label       string   `json:"label"`                 // texte affiché à l'utilisateur
	Description string   `json:"description"`           // aide contextuelle (affiché en gris sous le label)
	Type        string   `json:"type"`                  // "string", "secret", "path", "bool", "select"
	Options     []string `json:"options,omitempty"`     // pour type "select" : liste des choix possibles
	Required    bool     `json:"required"`              // true = obligatoire, boucle jusqu'à saisie valide
	Default     string   `json:"default"`               // valeur par défaut si l'utilisateur laisse vide
	When        string   `json:"when,omitempty"`        // condition d'affichage (ex: "VPN_ENABLED=true")
}

// InstallSessionStatus est retourné par la commande "install-status".
type InstallSessionStatus struct {
	SessionID string    `json:"session_id"`
	AppID     string    `json:"app_id"`
	Status    string    `json:"status"`          // "running", "done", "error"
	Lines     []string  `json:"lines"`           // logs accumulés
	Error     string    `json:"error,omitempty"`
	Notes     string    `json:"notes,omitempty"` // post-install.txt si done
	StartAt   time.Time `json:"started_at"`
}

// ─────────────────────────────────────────────
// BACKUP — manifest d'une sauvegarde
// Stocké dans backups/<app>/<timestamp>/manifest.json
// ─────────────────────────────────────────────

// ─────────────────────────────────────────────
// SUPERVISION — snapshot de métriques
// ─────────────────────────────────────────────

// AppStats contient les métriques live d'une application.
type AppStats struct {
	AppID      string  `json:"app_id"`
	Name       string  `json:"name"`
	Status     string  `json:"status"`
	CPUPercent float64 `json:"cpu_percent"`
	MemoryMB   float64 `json:"memory_mb"`
	DiskMB     int64   `json:"disk_mb"` // -1 si non calculé (mode simple)
	Port       int     `json:"port"`
}

// StatsSnapshot est le résultat d'une collecte complète.
type StatsSnapshot struct {
	Timestamp   time.Time  `json:"timestamp"`
	Apps        []AppStats `json:"apps"`
	MemUsedMB   float64    `json:"mem_used_mb"`
	MemTotalMB  float64    `json:"mem_total_mb"`
	DiskUsedGB  float64    `json:"disk_used_gb"`
	DiskTotalGB float64    `json:"disk_total_gb"`
}

// ─────────────────────────────────────────────
// BACKUP — manifest d'une sauvegarde
// ─────────────────────────────────────────────

type BackupManifest struct {
	App            string    `json:"app"`
	AppName        string    `json:"app_name"`
	Timestamp      time.Time `json:"timestamp"`
	CaleopeVersion string    `json:"caleope_version"`
	HasData        bool      `json:"has_data"`
	HasConfig      bool      `json:"has_config"`
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
	Ports           []AppPort  `json:"ports"`                      // avec les ports hôtes alloués
	ComposeDir      string     `json:"compose_dir"`                 // chemin vers apps-installed/<id>/
	StorageLocation string     `json:"storage_location,omitempty"` // nom de la location NAS (vide = stockage local)
	Error           string     `json:"error,omitempty"`
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
// NETWORK LOCATIONS — emplacements réseau montables
// Stockés dans runtime/locations/<name>.json
// ─────────────────────────────────────────────

// NetworkLocationType définit le protocole de l'emplacement.
type NetworkLocationType string

const (
	LocationSMB  NetworkLocationType = "smb"
	LocationCIFS NetworkLocationType = "cifs" // alias smb
	LocationSFTP NetworkLocationType = "sftp"
)

// NetworkLocation représente un emplacement réseau monté ou montable.
type NetworkLocation struct {
	Name       string              `json:"name"`
	Type       NetworkLocationType `json:"type"`
	Host       string              `json:"host"`        // hostname ou IP
	Share      string              `json:"share"`       // chemin du partage (SMB: //host/share, SFTP: /path)
	Username   string              `json:"username"`
	MountPoint string              `json:"mount_point"` // /opt/gaiver-it/caleope/mounts/<name>
	Mounted    bool                `json:"mounted"`
	AddedAt    time.Time           `json:"added_at"`
	Options    string              `json:"options,omitempty"` // options de montage supplémentaires
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
