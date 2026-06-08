---
title: Arr Stack
description: Suite complète de gestion et téléchargement de médias
published: true
date: 2026-06-07
---

# Arr Stack

Suite complète de services pour automatiser le téléchargement et la gestion de médias, avec Jellyfin intégrable en un seul `caleope install`.

## Installation

```bash
# Minimal (tout par défaut, Jellyfin inclus, langue française)
caleope install arr-stack

# Avec domaine personnalisé
caleope install arr-stack --domain media.monserveur.fr

# Stockage des médias sur NAS (nom d'emplacement Caleope ou chemin absolu)
caleope install arr-stack --storage mon-nas
caleope install arr-stack --storage /mnt/nas/media

# Langue différente (en, de, es, it, pt, nl, pl, ja)
caleope install arr-stack --language en

# Avec VPN activé dès l'installation
caleope install arr-stack \
  --param vpn_enabled=true \
  --param vpn_provider=protonvpn \
  --param vpn_type=wireguard \
  --param vpn_wg_private_key=<clé> \
  --param vpn_wg_addresses=10.2.0.2/32
```

L'installation est **entièrement automatique** — aucune interaction requise. Tout se configure au démarrage.

## Paramètres disponibles

| Paramètre | Valeurs | Défaut | Description |
|-----------|---------|--------|-------------|
| `language` | `fr` `en` `de` `es` `it` `pt` `nl` `pl` `ja` | `fr` | Langue de l'UI et des médias téléchargés |
| `storage_path` | chemin absolu | `app-data/arr-stack/data` | Dossier pour les médias et téléchargements |
| `jellyfin_mode` | `embedded` `external` `none` | auto-détecté | Mode Jellyfin (voir ci-dessous) |
| `vpn_enabled` | `true` `false` | `false` | VPN pour qBittorrent |
| `vpn_provider` | `protonvpn` `mullvad` `nordvpn` … | `protonvpn` | Fournisseur VPN |
| `vpn_type` | `wireguard` `openvpn` | `wireguard` | Protocole VPN |
| `vpn_wg_private_key` | chaîne | — | Clé privée WireGuard |
| `vpn_wg_addresses` | ex: `10.2.0.2/32` | — | Adresse WireGuard |
| `vpn_openvpn_user` | chaîne | — | Identifiant OpenVPN |
| `vpn_openvpn_password` | chaîne | — | Mot de passe OpenVPN |
| `vpn_server_countries` | ex: `France` | — | Pays du serveur VPN (optionnel) |

## Détection automatique de Jellyfin

L'installeur détecte automatiquement si Jellyfin est présent, dans cet ordre de priorité :

| Situation | Comportement |
|-----------|-------------|
| Jellyfin installé via `caleope install jellyfin` | Réutilisé comme instance externe — credentials lus automatiquement |
| Jellyfin container en cours (non-Caleope) | Détecté et proposé en réutilisation |
| Aucun Jellyfin | Inclus dans la stack (profil `jellyfin`) |
| `--param jellyfin_mode=none` | Aucun Jellyfin dans la stack |

> **Recommandé** : installer Jellyfin séparément d'abord (`caleope install jellyfin`), puis installer arr-stack. Les credentials sont partagés automatiquement et Jellyseerr est configuré sans aucune action manuelle.

## VPN pour qBittorrent

Configurable à l'installation ou après coup :

```bash
# Reconfigurer le VPN à tout moment
caleope configure arr-stack
```

Avec VPN actif, qBittorrent tourne dans le namespace réseau de **Gluetun** — tout son trafic passe par le tunnel. Les apps *arr communiquent avec lui via le réseau Docker interne (bypass VPN).

| Fournisseur | Protocole | Ce qui est demandé |
|-------------|-----------|-------------------|
| ProtonVPN | WireGuard | `PrivateKey` + `Address` du fichier .conf |
| Mullvad | WireGuard | `PrivateKey` + `Address` |
| Tous | OpenVPN | Nom d'utilisateur + mot de passe |

**ProtonVPN** : `account.proton.me` → VPN → Télécharger → WireGuard → copier `PrivateKey` et `Address`

## Services inclus

| Service | URL | Rôle |
|---------|-----|------|
| **Jellyseerr** | `jellyseerr.<domaine>` | Interface de demande de contenu |
| **Jellyfin** | `jellyfin.<domaine>` | Serveur multimédia *(si inclus dans la stack)* |
| **Jellyfin Vue** | `vue.<domaine>` | Interface de lecture épurée |
| **Prowlarr** | `prowlarr.<domaine>` | Gestionnaire d'indexeurs |
| **Radarr** | `radarr.<domaine>` | Films |
| **Sonarr** | `sonarr.<domaine>` | Séries TV |
| **Lidarr** | `lidarr.<domaine>` | Musique |
| **Bazarr** | `bazarr.<domaine>` | Sous-titres automatiques |
| **qBittorrent** | `qbt.<domaine>` | Client torrent (accès direct, sans mdp) |
| **SABnzbd** | `sabnzbd.<domaine>` | Client Usenet |

## Ce qui est configuré automatiquement

Un container **bootstrap** s'exécute une fois au premier démarrage de la stack :

| Configuration | Résultat |
|--------------|----------|
| Prowlarr → Radarr, Sonarr, Lidarr | Indexeurs synchronisés automatiquement |
| Prowlarr → FlareSolverr | Proxy anti-CloudFlare activé |
| Prowlarr → Indexeurs publics | YTS (films) ajouté actif · 1337x + EZTV ajoutés désactivés* |
| Radarr, Sonarr, Lidarr → qBittorrent | Client torrent configuré avec catégories |
| Radarr, Sonarr, Lidarr → SABnzbd | Client Usenet configuré avec catégories |
| Dossiers racine | `/data/media/{movies,tv,music}` |
| Langue UI | Radarr, Sonarr, Lidarr, Prowlarr dans la langue choisie |
| Langue médias | Profils qualité + Custom Format audio avec score +500 |
| Sous-titres Bazarr | Profil `<Langue> + English` créé et appliqué |
| Jellyfin | Bibliothèques Films, Séries, Musique ajoutées |
| Jellyseerr → Jellyfin | Connexion automatique (si credentials disponibles) |
| Jellyseerr → Radarr, Sonarr | Configurés avec API keys et dossiers racine |

*1337x et EZTV sont désactivés par défaut car protégés par CloudFlare — à activer depuis Prowlarr après avoir assigné le tag FlareSolverr.

## Ce qu'il reste à faire

### Prowlarr — ajouter tes indexeurs perso
`prowlarr.<domaine>` → **Indexers** → **Add Indexer**

Ajoute tes indexeurs avec compte (YGGTorrent, BetaSeries, indexeurs privés…). Prowlarr les synchronise automatiquement vers Radarr, Sonarr et Lidarr.

### Bazarr — activer les fournisseurs de sous-titres
`bazarr.<domaine>` → **Settings** → **Providers**

Tous les fournisseurs de sous-titres nécessitent un compte externe (OpenSubtitles, Subscene, Addic7ed…).

### SABnzbd — configurer ton fournisseur Usenet
`sabnzbd.<domaine>` → wizard de premier accès → entrer les credentials de ton abonnement Usenet.

## Flux de téléchargement

```
Jellyseerr (demande film ou série)
    → Radarr / Sonarr (gère le suivi, cherche dans les indexeurs)
        → qBittorrent ou SABnzbd (télécharge)
            → Radarr / Sonarr (renomme, déplace)
                → /data/media/{movies,tv}/
                    → Jellyfin (scan automatique → visible dans la bibliothèque)
```

> Les fichiers téléchargés apparaissent dans Jellyfin **quelques minutes** après la fin du téléchargement et de l'extraction (SABnzbd/qBittorrent → Sonarr/Radarr → scan Jellyfin).

## Structure des données

Tous les services partagent `/data` pour que les hardlinks fonctionnent (renommage sans copie) :

```
data/
├── downloads/
│   ├── complete/{movies,tv,music,books}   ← *arr récupère ici après DL
│   └── incomplete/                        ← en cours de téléchargement
└── media/
    ├── movies/    ← Jellyfin + Radarr
    ├── tv/        ← Jellyfin + Sonarr
    ├── music/     ← Jellyfin + Lidarr
    └── books/
```

Les **configs** restent toujours sur le disque local (`app-data/arr-stack/config/`).
Avec `--storage`, seuls les médias et téléchargements vont sur le NAS —
`app-data/arr-stack/data/` devient un symlink vers le NAS.

## Reconfiguration VPN

```bash
caleope configure arr-stack
```

Lance un wizard interactif pour activer, désactiver ou changer le VPN. La stack redémarre automatiquement.

## Commandes utiles

```bash
caleope logs arr-stack          # logs de tous les services
caleope restart arr-stack       # redémarrer la stack
caleope backup arr-stack        # sauvegarder les configs
caleope configure arr-stack     # reconfigurer le VPN
caleope remove arr-stack        # supprimer (app-data conservé)

# Bootstrap : voir les connexions automatiques au démarrage
docker logs arr-bootstrap
```
