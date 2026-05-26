---
title: Arr Stack
description: Suite complète de gestion et téléchargement de médias
published: true
date: 2026-05-26
---

# Arr Stack

Suite complète de services pour automatiser le téléchargement et la gestion de médias — avec Jellyfin intégrable en un seul `caleope install`.

## Installation

```bash
# Stockage local (défaut)
caleope install arr-stack --domain media.monserveur.fr

# Stockage sur NAS
caleope install arr-stack --domain media.monserveur.fr \
  --param storage_path=/opt/gaiver-it/caleope/mounts/mon-nas/media
```

L'installeur pose deux questions (Jellyfin + VPN), puis **tout le reste se configure automatiquement** au démarrage.

## Wizard d'installation

### 🎬 Jellyfin

```
  Jellyfin n'est pas installé.
  L'inclure dans la stack ? [O/n] :
```

| Réponse | Ce qui se passe |
|---------|-----------------|
| **O** (défaut) | Jellyfin est lancé dans la stack, les bibliothèques Films/Séries/Musique sont créées automatiquement, un compte admin est généré |
| **n + URL** | Tu fournis l'URL de ton Jellyfin existant, le bootstrap y ajoute les bibliothèques automatiquement |

Si Jellyfin est déjà détecté (container en cours), l'installeur propose de le réutiliser directement.

### 🔒 VPN (pour qBittorrent)

```
  Activer un VPN ? [o/N] :
```

Si **o** :
1. **Fournisseur** : ProtonVPN, Mullvad, NordVPN, PIA, Surfshark, ExpressVPN ou autre
2. **Protocole** : WireGuard *(recommandé)* ou OpenVPN
3. **Identifiants** :

| Protocole | Ce qui est demandé |
|-----------|-------------------|
| WireGuard | Clé privée (`PrivateKey` du fichier config) |
| WireGuard + Mullvad | Clé privée + adresse IP (`10.68.x.x/32`) |
| OpenVPN | Nom d'utilisateur + mot de passe |

4. **Pays du serveur** (optionnel, ex: `France`)

**ProtonVPN** : `account.proton.me` → VPN → Télécharger → WireGuard → copie `PrivateKey`

Avec VPN actif, qBittorrent tourne dans le namespace réseau de Gluetun — tout son trafic passe par le tunnel. Les *arr communiquent avec lui via le réseau Docker interne (bypass VPN).

> VPN et Jellyfin sont configurés à l'installation. Pour les modifier : `caleope install arr-stack --force`

## Services inclus

| Service | URL | Rôle |
|---------|-----|------|
| **Jellyseerr** | `/` | Interface de demande de contenu |
| **Jellyfin** | `/jellyfin` | Serveur multimédia *(si inclus dans la stack)* |
| **Jellyfin Vue** | `/vue` | Lecteur avec interface épurée |
| **Prowlarr** | `/prowlarr` | Gestionnaire d'indexeurs |
| **Radarr** | `/radarr` | Téléchargement automatique de films |
| **Sonarr** | `/sonarr` | Téléchargement automatique de séries |
| **Lidarr** | `/lidarr` | Téléchargement automatique de musique |
| **Readarr** | `/readarr` | Téléchargement automatique de livres |
| **Bazarr** | `/bazarr` | Sous-titres automatiques |
| **qBittorrent** | `/qbt` | Client torrent |
| **SABnzbd** | `/sabnzbd` | Client Usenet |

## Ce qui est configuré automatiquement

Un container **bootstrap** s'exécute une fois au démarrage :

| Connexion | Résultat |
|-----------|----------|
| Prowlarr → Radarr, Sonarr, Lidarr, Readarr | Synchronisation des indexeurs |
| Radarr, Sonarr, Lidarr, Readarr → qBittorrent | Client torrent configuré |
| Radarr, Sonarr, Lidarr, Readarr → SABnzbd | Client Usenet configuré |
| Jellyfin → Bibliothèques | Films, Séries, Musique ajoutées automatiquement |
| Dossiers racine | `/data/media/{movies,tv,music,books}` |
| Auth | Désactivée (réseau local, derrière reverse proxy) |

## Ce qu'il reste à faire (2 étapes)

### 1. Prowlarr — ajouter tes sources
`/prowlarr` → **Indexers** → **Add Indexer**

Ajoute tes indexeurs torrent (ex: 1337x, YGGTorrent…) et/ou Usenet. Prowlarr les synchronise automatiquement vers Radarr, Sonarr, Lidarr et Readarr.

### 2. Jellyseerr — connecter Jellyfin
`/` → wizard de premier accès :
- **Jellyfin URL** :
  - Stack intégrée : `https://media.monserveur.fr/jellyfin`
  - Jellyfin externe : l'adresse de ton serveur
- Jellyseerr connecte ensuite Radarr et Sonarr automatiquement via les API keys déjà configurées

## Jellyfin Vue — premier accès

`/vue` → entrer l'URL de ton serveur Jellyfin → se connecter avec ton compte Jellyfin

Jellyfin Vue est une interface de lecture épurée et moderne, alternative au client web officiel.

## Jellyseerr vs Jellyfin Vue

| | Jellyseerr | Jellyfin Vue |
|---|---|---|
| **Usage** | Demander du nouveau contenu | Regarder le contenu existant |
| **Auth** | Compte Jellyseerr propre | Compte Jellyfin |
| **Interface** | Moderne, style streaming | Épurée, minimaliste |

## Structure des données

Tous les services partagent `/data` pour que les hardlinks fonctionnent :

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
