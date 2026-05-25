---
title: Arr Stack
description: Suite complète de gestion et téléchargement de médias
published: true
date: 2026-05-25
---

# Arr Stack

Suite complète de 10 services pour automatiser le téléchargement et la gestion de médias, avec une interface de demande de contenu et un lecteur épuré.

## Installation

```bash
# Stockage local (défaut)
caleope install arr-stack --domain media.monserveur.fr

# Stockage sur NAS
caleope install arr-stack --domain media.monserveur.fr \
  --param storage_path=/opt/gaiver-it/caleope/mounts/mon-nas/media
```

Les identifiants et API keys s'affichent à la fin de l'installation.

## Services inclus

| Service | URL | Rôle |
|---------|-----|------|
| **Jellyseerr** | `/` | Interface de demande de contenu |
| **Jellyfin Vue** | `/vue` | Lecteur Jellyfin avec interface épurée |
| **Prowlarr** | `/prowlarr` | Gestionnaire d'indexeurs (remplace Jackett) |
| **Radarr** | `/radarr` | Téléchargement automatique de films |
| **Sonarr** | `/sonarr` | Téléchargement automatique de séries |
| **Lidarr** | `/lidarr` | Téléchargement automatique de musique |
| **Readarr** | `/readarr` | Téléchargement automatique de livres |
| **Bazarr** | `/bazarr` | Sous-titres automatiques |
| **qBittorrent** | `/qbt` | Client torrent |
| **SABnzbd** | `/sabnzbd` | Client Usenet |

## Structure des données

Tous les services partagent le même répertoire `/data` pour que les hardlinks fonctionnent (déplacement de fichiers instantané sans copie) :

```
data/
├── downloads/
│   ├── complete/
│   │   ├── movies/      ← Radarr récupère ici après DL
│   │   ├── tv/          ← Sonarr récupère ici
│   │   ├── music/       ← Lidarr récupère ici
│   │   └── books/       ← Readarr récupère ici
│   └── incomplete/      ← fichiers en cours de téléchargement
└── media/               ← bibliothèques finales (Jellyfin lit ici)
    ├── movies/
    ├── tv/
    ├── music/
    └── books/
```

## Ordre de configuration

### 1. Prowlarr — ajouter les indexeurs
`/prowlarr` → Indexers → Add Indexer → choisir tes sources

### 2. Prowlarr → connecter les *arr
`/prowlarr` → Settings → Apps → Add Application  
Renseigne les API keys affichées à l'installation pour chaque app.

### 3. Radarr/Sonarr — configurer le client de téléchargement
`/radarr` → Settings → Download Clients → qBittorrent :
```
Host     : qbittorrent
Port     : 8080
Category : movies   (ou tv pour Sonarr)
```

Pour Usenet via SABnzbd :
```
Host     : sabnzbd
Port     : 8080
```

### 4. Radarr/Sonarr — configurer les chemins
Settings → Media Management → Root Folders :
- Radarr : `/data/media/movies`
- Sonarr : `/data/media/tv`
- Lidarr : `/data/media/music`

### 5. Jellyseerr — connecter Jellyfin + Radarr/Sonarr
Premier accès sur `/` → wizard :
- Jellyfin URL : `http://jellyfin:8096` (si sur le même serveur)
- Radarr/Sonarr : URL interne + API key

### 6. Jellyfin — ajouter la bibliothèque arr-stack
Dans Jellyfin → Bibliothèques → Ajouter :
- Films : chemin vers `data/media/movies`
- Séries : chemin vers `data/media/tv`

### 7. Jellyfin Vue — premier accès
`/vue` → entrer l'URL de ton serveur Jellyfin → se connecter

## Jellyfin Vue vs Jellyseerr

| | Jellyseerr | Jellyfin Vue |
|---|---|---|
| **Usage** | Demander du nouveau contenu | Regarder le contenu existant |
| **Auth** | Compte Jellyseerr propre | Compte Jellyfin |
| **Interface** | Moderne, style streaming | Épurée, minimaliste |
| **Sur mobile** | Site web | Site web (PWA) |

## Commandes utiles

```bash
caleope logs arr-stack       # Logs de tous les services
caleope restart arr-stack    # Redémarrer la stack
caleope backup arr-stack     # Sauvegarder les configs
caleope stop arr-stack       # Arrêter proprement
```
