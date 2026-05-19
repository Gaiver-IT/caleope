# Caleope — MVP v0.1.0

Plateforme de self-hosting souveraine basée sur Docker et Debian.

---

## Architecture

```
CLI (caleope-store)
       │  JSON sur UNIX socket
       ▼
Daemon (caleoped)
       │
       ├── API Server    → reçoit et route les requêtes
       ├── Runtime       → lit/écrit l'état JSON sur disque
       ├── Store         → résout les apps, gère le cache Git
       ├── Installer     → flow 12 étapes avec rollback
       ├── Docker Client → pilote docker compose
       └── Events        → JSONL append-only
```

## Structure du code

```
caleope/
├── cmd/
│   ├── caleoped/           ← Daemon (binaire principal)
│   └── caleope-store/      ← CLI (client du daemon)
│
├── internal/               ← Code interne (non importable de l'extérieur)
│   ├── api/                ← Serveur UNIX socket
│   ├── runtime/            ← Persistance JSON de l'état
│   ├── store/              ← Résolution et cache des apps
│   ├── install/            ← Flow d'installation 12 étapes
│   ├── docker/             ← Pilote docker compose
│   └── events/             ← Système d'événements JSONL
│
├── pkg/types/              ← Types partagés (AppManifest, RuntimeApp...)
│
├── example-store/          ← Exemple de dépôt store
│   └── apps/jellyfin/      ← Exemple d'application complète
│
├── Makefile                ← Compilation et installation
├── caleoped.service        ← Service systemd
└── go.mod                  ← Module Go
```

## Prérequis

- Debian 12+ ou Ubuntu 22+
- Go 1.22+ (pour compiler)
- Docker Engine + Docker Compose v2
- Git

## Installation

```bash
# 1. Compiler
make build

# 2. Installer (nécessite sudo)
sudo make install

# 3. Démarrer le daemon
sudo systemctl enable --now caleoped

# 4. Vérifier
caleope-store ping
```

## Développement

```bash
# Lancer le daemon en mode dev (pas de sudo, dossier /tmp)
make dev

# Dans un autre terminal, utiliser le CLI
# (pointe vers le socket de dev /tmp/caleoped-dev.sock)
make dev-cli ARGS="ping"
make dev-cli ARGS="list"
```

## Utilisation

```bash
# Installer une application
caleope-store install jellyfin --domain media.home.local

# Lister les apps installées
caleope-store list

# Détails d'une app
caleope-store info jellyfin

# Supprimer
caleope-store remove jellyfin

# Rechercher dans le store
caleope-store search media

# Synchroniser le store
caleope-store update
```

## Fichiers runtime générés

```
/opt/gaiver-it/caleope/
├── runtime/
│   ├── apps/
│   │   └── jellyfin.json      ← état de l'app
│   ├── ports.json             ← ports alloués
│   ├── repos.json             ← dépôts configurés
│   └── events/
│       └── events.jsonl       ← log des événements
│
└── apps-installed/
    └── jellyfin/
        ├── compose.yml        ← compose généré
        ├── app.env            ← variables d'env
        ├── override/          ← overrides utilisateur
        ├── logs/
        └── backups/
```

## Concepts Go expliqués dans le code

| Concept | Où le trouver |
|---------|--------------|
| Structs et JSON tags | `pkg/types/types.go` |
| Méthodes sur struct (receivers) | `internal/runtime/runtime.go` |
| sync.RWMutex (concurrence) | `internal/runtime/runtime.go` |
| Goroutines et channels | `cmd/caleoped/main.go` |
| context.Context (timeout) | `internal/install/install.go` |
| os/exec (commandes système) | `internal/docker/docker.go` |
| UNIX socket (net.Listen) | `internal/api/api.go` |
| text/template | `internal/install/install.go` |
| defer | partout |

## Prochaines étapes

- [ ] Interface web (future CaleopeOS UI)
- [ ] Système de backup Restic + Rclone
- [ ] Support GPU (NVIDIA, Intel)
- [ ] Authentik forward auth
- [ ] Prometheus metrics
- [ ] Tests unitaires
