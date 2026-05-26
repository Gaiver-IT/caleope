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

L'installeur pose une seule question : tu veux un VPN ? Réponds, et c'est tout.  
Les connexions entre services se configurent automatiquement au démarrage.

## VPN (optionnel — recommandé pour les torrents)

Un wizard interactif s'affiche pendant l'installation :

```
┌─────────────────────────────────────────────────────────────────┐
│  🔒 VPN pour qBittorrent                                        │
│                                                                 │
│  Recommandé pour isoler le trafic torrent derrière un VPN.     │
│  Utilise Gluetun — compatible ProtonVPN, Mullvad, NordVPN…     │
└─────────────────────────────────────────────────────────────────┘
  Activer un VPN ? [o/N] :
```

Si tu réponds **o**, l'installeur demande :

1. **Fournisseur** : ProtonVPN, Mullvad, NordVPN, PIA, Surfshark, ExpressVPN ou autre
2. **Protocole** : WireGuard *(recommandé)* ou OpenVPN
3. **Identifiants** selon le protocole :

| Protocole | Ce qui est demandé |
|-----------|-------------------|
| WireGuard | Clé privée (`PrivateKey` du fichier config) |
| WireGuard + Mullvad | Clé privée + adresse IP (`10.68.x.x/32`) |
| OpenVPN | Nom d'utilisateur + mot de passe |

4. **Pays du serveur** (optionnel, ex: `France`)

### ProtonVPN — où trouver la clé WireGuard

`account.proton.me` → **VPN** → **Télécharger** → **WireGuard**  
→ Génère une config → copie le champ `PrivateKey` dans `[Interface]`

### Ce qui se passe avec le VPN

qBittorrent tourne dans le namespace réseau de **Gluetun** : tout son trafic torrent passe par le tunnel VPN. Les services *arr communiquent avec lui normalement via le réseau Docker interne (bypass VPN).

```
qBittorrent ──→ Gluetun ──→ VPN ──→ Internet (torrents)
    ↑
Radarr / Sonarr / … (réseau Docker interne, bypass VPN)
```

> Le VPN est configuré à l'installation. Pour le modifier, réinstalle la stack (`caleope install arr-stack --force`).

## Services inclus

| Service | URL | Rôle |
|---------|-----|------|
| **Jellyseerr** | `/` | Interface de demande de contenu |
| **Jellyfin Vue** | `/vue` | Lecteur Jellyfin avec interface épurée |
| **Prowlarr** | `/prowlarr` | Gestionnaire d'indexeurs |
| **Radarr** | `/radarr` | Téléchargement automatique de films |
| **Sonarr** | `/sonarr` | Téléchargement automatique de séries |
| **Lidarr** | `/lidarr` | Téléchargement automatique de musique |
| **Readarr** | `/readarr` | Téléchargement automatique de livres |
| **Bazarr** | `/bazarr` | Sous-titres automatiques |
| **qBittorrent** | `/qbt` | Client torrent |
| **SABnzbd** | `/sabnzbd` | Client Usenet |

## Ce qui est configuré automatiquement

Un container **bootstrap** s'exécute une fois au démarrage et configure tout via les APIs :

| Connexion | Résultat |
|-----------|----------|
| Prowlarr → Radarr, Sonarr, Lidarr, Readarr | Synchronisation complète des indexeurs |
| Radarr, Sonarr, Lidarr, Readarr → qBittorrent | Client torrent configuré |
| Radarr, Sonarr, Lidarr, Readarr → SABnzbd | Client Usenet configuré |
| Dossiers racine | `/data/media/{movies,tv,music,books}` |
| Auth | Désactivée (réseau local, derrière reverse proxy) |

## Ce qu'il reste à faire (2 étapes)

### 1. Prowlarr — ajouter tes sources
`/prowlarr` → **Indexers** → **Add Indexer**

Ajoute tes indexeurs torrent (ex: 1337x, YGGTorrent…) et/ou Usenet. Prowlarr les synchronise automatiquement vers Radarr, Sonarr, Lidarr et Readarr.

### 2. Jellyseerr — connecter Jellyfin
`/` → wizard de premier accès :
- **Jellyfin URL** : l'adresse de ton serveur Jellyfin (ex: `https://media.monserveur.fr`)
- Jellyseerr connecte ensuite Radarr et Sonarr automatiquement via les API keys déjà configurées

## Jellyfin Vue — premier accès

`/vue` → entrer l'URL de ton serveur Jellyfin → se connecter avec ton compte Jellyfin

Jellyfin Vue est une interface de lecture épurée et moderne, alternative au client web officiel.

## Jellyfin — ajouter la bibliothèque arr-stack

Dans Jellyfin, ajoute une bibliothèque pointant vers le dossier `media` de l'arr-stack :

```
Films   → /opt/gaiver-it/caleope/app-data/arr-stack/data/media/movies
Séries  → /opt/gaiver-it/caleope/app-data/arr-stack/data/media/tv
Musique → /opt/gaiver-it/caleope/app-data/arr-stack/data/media/music
```

Si stockage NAS : le chemin est celui passé avec `--param storage_path=...`.

## Jellyseerr vs Jellyfin Vue

| | Jellyseerr | Jellyfin Vue |
|---|---|---|
| **Usage** | Demander du nouveau contenu | Regarder le contenu existant |
| **Auth** | Compte Jellyseerr propre | Compte Jellyfin |
| **Interface** | Moderne, style streaming | Épurée, minimaliste |

## Structure des données

Tous les services partagent `/data` pour que les hardlinks fonctionnent (déplacement de fichiers instantané) :

```
data/
├── downloads/
│   ├── complete/{movies,tv,music,books}   ← *arr récupère ici après DL
│   └── incomplete/                        ← en cours de téléchargement
└── media/
    ├── movies/    ← Jellyfin lit ici
    ├── tv/
    ├── music/
    └── books/
```

## Commandes utiles

```bash
caleope logs arr-stack       # logs de tous les services
caleope restart arr-stack    # redémarrer la stack
caleope backup arr-stack     # sauvegarder les configs
caleope stop arr-stack       # arrêter proprement

# Voir les logs du bootstrap (connexions automatiques)
docker logs arr-bootstrap
```
