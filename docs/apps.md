---
title: Applications disponibles
description: Catalogue des applications du store Caleope
published: true
date: 2026-06-19
---

# Applications disponibles

Toutes les applications s'installent avec `caleope install <id>`.  
Synchronise le catalogue avant la première installation : `caleope update`

---

## Média

### [Jellyfin](/apps/jellyfin)
Serveur multimédia libre — films, séries, musique, photos. Support GPU pour le transcodage matériel.
```bash
caleope install jellyfin --domain media.monserveur.fr
caleope install jellyfin --domain media.monserveur.fr --gpu   # avec accélération GPU
```

### [Arr Stack](/apps/arr-stack)
Suite complète : Prowlarr, Radarr, Sonarr, Lidarr, Readarr, Bazarr, qBittorrent, SABnzbd, Jellyseerr, Jellyfin Vue.
```bash
caleope install arr-stack --domain media.monserveur.fr
```

### [Immich](/apps/immich)
Alternative self-hosted à Google Photos — sauvegarde mobile, reconnaissance faciale, galerie.
```bash
caleope install immich --domain photos.monserveur.fr
```

### [AzuraCast](/apps/azuracast)
Station de radio web — playlists, streaming live, AutoDJ, statistiques d'audience.
```bash
caleope install azuracast --domain radio.monserveur.fr
```

---

## Cloud & Productivité

### [Nextcloud + OnlyOffice](/apps/nextcloud)
Suite collaborative — fichiers, agenda, contacts, édition de documents.
```bash
caleope install nextcloud --domain cloud.monserveur.fr
```

### [Wiki.js](/apps/wikijs)
Wiki moderne avec éditeur web et synchronisation GitHub.
```bash
caleope install wikijs --domain docs.monserveur.fr
```

---

## Sécurité & Identité

### [Authentik](/apps/authentik)
Gestionnaire d'identités SSO/OIDC. Centralise l'authentification de toutes les apps.
```bash
caleope install authentik --domain auth.monserveur.fr
```

### [Vaultwarden](/apps/vaultwarden)
Gestionnaire de mots de passe compatible Bitwarden.
```bash
caleope install vaultwarden --domain vault.monserveur.fr
```

### [CrowdSec](/apps/crowdsec)
Protection réseau collaborative — détection d'intrusion, blocage d'IPs malveillantes.
```bash
caleope install crowdsec
```

---

## Réseau & VPN

### [WG-Easy](/apps/wg-easy)
Interface web WireGuard pour VPN personnel — gestion des clients en quelques clics.
```bash
caleope install wg-easy --domain vpn.monserveur.fr
```

### [Tailscale](/apps/tailscale)
VPN mesh basé sur WireGuard — connecte tes appareils sans configuration de ports.
```bash
caleope install tailscale --param TS_AUTHKEY=tskey-auth-xxxxx
```

---

## Supervision

### [Prometheus + Grafana](/apps/prometheus-grafana)
Métriques système et par application, dashboards historiques.
```bash
caleope install prometheus-grafana --domain metrics.monserveur.fr
```

---

## CMS & Publication

### [Ghost](/apps/ghost)
Plateforme de publication et newsletter moderne.
```bash
caleope install ghost --domain blog.monserveur.fr
```

### [WordPress](/apps/wordpress)
CMS le plus utilisé — blog, site vitrine, e-commerce.
```bash
caleope install wordpress --domain site.monserveur.fr
```

---

## Développement & DevOps

### [Gitea](/apps/gitea)
Forge Git légère — dépôts, issues, pull requests, CI/CD.
```bash
caleope install gitea --domain git.monserveur.fr
```

---

## ITSM & Management

### [GLPI](/apps/glpi)
Gestion de parc informatique et helpdesk.
```bash
caleope install glpi --domain itsm.monserveur.fr
```

---

## Gaming

### [Pterodactyl Panel](/apps/pterodactyl-panel)
Panel de gestion de serveurs de jeux (Minecraft, CS2, Valheim…).
```bash
caleope install pterodactyl-panel --domain panel.monserveur.fr
```

### [Pterodactyl Wings](/apps/pterodactyl-wings)
Daemon d'exécution des serveurs de jeux Pterodactyl.
```bash
caleope install pterodactyl-wings --param NODE_FQDN=<ip-serveur>
```

---

## Outils système

### [Restic](/apps/restic)
Outil de backup incrémental avec déduplication — backend alternatif pour `caleope backup --restic`.
```bash
caleope install restic
```

---

## Ajouter une application au store

Le store est open-source : [github.com/Gaiver-IT/caleope-store](https://github.com/Gaiver-IT/caleope-store)

Chaque application est un dossier `apps/<id>/` contenant :
- `app.json` — métadonnées, ports, volumes, capabilities
- `docker-compose.yml` — template Docker Compose (variables Go templates)
- `setup.sh` — préparation : génération de secrets, configuration initiale, intégrations
