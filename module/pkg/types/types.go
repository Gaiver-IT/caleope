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
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	Category       string          `json:"category"`
	Channel        string          `json:"channel"`    // stable, latest, nightly
	Repository     string          `json:"repository"` // official, community, untrusted
	Capabilities   AppCapabilities `json:"capabilities"`
	Network        AppNetwork      `json:"network"`
	Ports          []AppPort       `json:"ports"`
	Volumes        []AppVolume     `json:"volumes"`
	Backup         AppBackup       `json:"backup"`
	UseBaseDomain  bool            `json:"use_base_domain"`           // true = domaine racine (pas appID.domain)
	SecureHeaders  bool            `json:"secure_headers,omitempty"`  // Traefik secure headers opt-in
	AuthMiddleware bool            `json:"auth_middleware,omitempty"` // Authentik forward auth opt-in
	NoContainer    bool            `json:"no_container,omitempty"`    // true = outil système, pas de container Docker
	Priority       string          `json:"priority,omitempty"`        // critical | normal | background — priorité ressources (cpu_shares + oom_score_adj) des containers de l'app. Override par service via label compose "caleope.priority".
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
	Container int    `json:"container"`          // port dans le container
	Host      int    `json:"host"`               // port alloué dynamiquement sur l'hôte
	Dynamic   bool   `json:"dynamic"`            // true = Caleope choisit le port hôte
	Protocol  string `json:"protocol,omitempty"` // "tcp", "udp", "any" — pour UFW
	Firewall  bool   `json:"firewall,omitempty"` // true = ouvrir dans UFW
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
type ParamDef struct {
	ID          string `json:"id"`          // nom de la variable (→ CALEOPE_PARAM_<ID>)
	Label       string `json:"label"`       // texte affiché à l'utilisateur
	Description string `json:"description"` // aide contextuelle (affiché en gris sous le label)
	Type        string `json:"type"`        // "string", "secret" (masqué), "path"
	Required    bool   `json:"required"`    // true = obligatoire, boucle jusqu'à saisie valide
	Default     string `json:"default"`     // valeur par défaut si l'utilisateur laisse vide
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
	Scope          string    `json:"scope,omitempty"` // "all", "config", "data"
	Dir            string    `json:"dir,omitempty"`   // nom du répertoire sur disque (non stocké dans manifest.json)
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
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Status          AppStatus `json:"status"`
	InstalledAt     time.Time `json:"installed_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	Version         string    `json:"version"`
	Channel         string    `json:"channel"`
	Repository      string    `json:"repository"`
	Domain          string    `json:"domain,omitempty"`           // domaine public de l'app (depuis app.env)
	Ports           []AppPort `json:"ports"`                      // avec les ports hôtes alloués
	ComposeDir      string    `json:"compose_dir"`                // chemin vers apps-installed/<id>/
	StorageLocation string    `json:"storage_location,omitempty"` // nom de la location NAS (vide = stockage local)
	Priority        string    `json:"priority,omitempty"`         // tier de priorité ressources (critical|normal|background) — ré-appliqué à chaque start/restart
	Error           string    `json:"error,omitempty"`
}

// ─────────────────────────────────────────────
// EVENTS — système JSONL
// ─────────────────────────────────────────────

// Event est une ligne dans le fichier events/events.jsonl.
// JSONL = JSON Lines : 1 objet JSON par ligne, format append-only.
// C'est idéal pour les logs car on n'a jamais besoin de réécrire le fichier.
type Event struct {
	Timestamp time.Time         `json:"timestamp"`
	Type      string            `json:"event"` // install, update, remove, error...
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
	LocationSMB   NetworkLocationType = "smb"
	LocationCIFS  NetworkLocationType = "cifs" // alias smb
	LocationNFS   NetworkLocationType = "nfs"
	LocationSFTP  NetworkLocationType = "sftp"
	LocationLocal NetworkLocationType = "local" // disque local (interne ou externe)
)

// NetworkLocation représente un emplacement réseau monté ou montable.
type NetworkLocation struct {
	Name       string              `json:"name"`
	Type       NetworkLocationType `json:"type"`
	Host       string              `json:"host"`  // hostname ou IP
	Share      string              `json:"share"` // chemin du partage (SMB: //host/share, SFTP: /path)
	Username   string              `json:"username"`
	MountPoint string              `json:"mount_point"` // /opt/gaiver-it/caleope/mounts/<name>
	Mounted    bool                `json:"mounted"`
	AddedAt    time.Time           `json:"added_at"`
	Options    string              `json:"options,omitempty"` // options de montage supplémentaires
}

// ─────────────────────────────────────────────
// PARTAGES — User Shares (à la Unraid)
// ─────────────────────────────────────────────
//
// Un Partage = un dossier + une politique d'accès (groupes Authentik) +
// les protocoles exposés dessus. SMB (lecteur réseau) et gestionnaire de
// fichiers web sont deux façades du même Partage.
//
// Identité : Authentik = source de vérité. SMB ne parle pas OIDC/LDAP-NTLM,
// donc Samba garde une base locale que le daemon PROVISIONNE depuis Authentik
// (même username, groupes → ACL). Le mot de passe réseau est un identifiant
// dédié, distinct du mot de passe web (posé par l'utilisateur dans l'UI).

// ShareAccess définit le droit d'un groupe sur un partage.
type ShareAccess string

const (
	AccessRead  ShareAccess = "ro" // lecture seule
	AccessWrite ShareAccess = "rw" // lecture-écriture
)

// ShareGroupACL lie un groupe Authentik à un droit sur le partage.
type ShareGroupACL struct {
	Group  string      `json:"group"`  // nom du groupe Authentik
	Access ShareAccess `json:"access"` // ro | rw
}

// Share représente un « User Share » : un dossier partagé, sa politique
// d'accès par groupe Authentik, et les protocoles activés.
type Share struct {
	Name       string          `json:"name"`              // nom du partage (= nom du lecteur réseau)
	Path       string          `json:"path"`              // dossier sur disque (sous app-data/_shares/<name> par défaut)
	Comment    string          `json:"comment,omitempty"` // description affichée dans l'explorateur réseau
	ACL        []ShareGroupACL `json:"acl"`               // groupes Authentik → droits
	SMBEnabled bool            `json:"smb_enabled"`       // exposer en lecteur réseau (SMB)
	CreatedAt  time.Time       `json:"created_at"`
}

// ShareUser = un utilisateur réseau provisionné dans Samba depuis Authentik.
// Le mot de passe n'est jamais stocké ici (posé via smbpasswd dans le conteneur).
type ShareUser struct {
	Username       string   `json:"username"`         // = username Authentik
	Groups         []string `json:"groups"`           // groupes Authentik mirrorés
	HasSMBPassword bool     `json:"has_smb_password"` // true une fois le mdp réseau défini
}

// ─────────────────────────────────────────────
// VMs — machines virtuelles KVM (à la Unraid, Pro-only)
// ─────────────────────────────────────────────

// VMState = état d'une machine virtuelle.
type VMState string

const (
	VMRunning VMState = "running"
	VMStopped VMState = "stopped"
	VMPaused  VMState = "paused"
)

// VM représente une machine virtuelle gérée par libvirt.
type VM struct {
	Name    string  `json:"name"`
	State   VMState `json:"state"`
	VCPUs   int     `json:"vcpus"`
	MemMB   int     `json:"mem_mb"`
	VNCPort int     `json:"vnc_port,omitempty"`
}

// VMSpec = paramètres de création d'une VM.
type VMSpec struct {
	Name    string `json:"name"`
	VCPUs   int    `json:"vcpus"`
	MemMB   int    `json:"mem_mb"`
	DiskGB  int    `json:"disk_gb"`
	ISO     string `json:"iso"`     // chemin de l'image ISO (ex: dans un Partage)
	Network string `json:"network"` // "bridge" (IP du LAN) | "nat"
}

// VMCapability décrit si l'hôte peut faire tourner des VMs.
type VMCapability struct {
	Available  bool   `json:"available"`   // prêt à l'emploi
	NeedsSetup bool   `json:"needs_setup"` // KVM ok mais libvirt à installer
	Reason     string `json:"reason"`      // explication si indisponible / à configurer
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
	Branch   string     `json:"branch,omitempty"` // branche git (défaut : "main")
}

// ─────────────────────────────────────────────
// TÂCHES PLANIFIÉES
// ─────────────────────────────────────────────

type TaskType string

const (
	TaskTypeBackup  TaskType = "backup"  // sauvegarder une ou toutes les apps
	TaskTypeUpgrade TaskType = "upgrade" // mettre à jour Caleope
	TaskTypeUpdate  TaskType = "update"  // synchroniser le catalogue d'apps
)

type BackupScope string

const (
	BackupScopeAll    BackupScope = "all"    // données + config (défaut)
	BackupScopeConfig BackupScope = "config" // config uniquement (léger)
	BackupScopeData   BackupScope = "data"   // données uniquement
)

// Task représente une tâche planifiée persistée dans core/tasks.json
type Task struct {
	ID         string      `json:"id"` // slug unique (ex: "backup-jellyfin-nuit")
	Type       TaskType    `json:"type"`
	App        string      `json:"app,omitempty"`   // pour type=backup ; vide = toutes les apps
	Scope      BackupScope `json:"scope,omitempty"` // pour type=backup
	Schedule   Schedule    `json:"schedule"`
	Enabled    bool        `json:"enabled"`
	LastRun    time.Time   `json:"last_run,omitempty"`
	LastStatus string      `json:"last_status,omitempty"` // "ok", "error: ..."
	CreatedAt  time.Time   `json:"created_at"`
}

// Schedule définit quand une tâche s'exécute.
// Hour et Minute en heure locale. Days = jours de la semaine (0=dim, 1=lun, …, 6=sam).
// Si Days est vide, la tâche tourne tous les jours.
type Schedule struct {
	Hour   int   `json:"hour"`           // 0–23
	Minute int   `json:"minute"`         // 0–59
	Days   []int `json:"days,omitempty"` // 0=dim … 6=sam ; vide = tous les jours
}
