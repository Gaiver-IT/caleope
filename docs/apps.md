---
title: Applications disponibles
description: Catalogue des applications du store Caleope
published: true
date: 2026-05-25
---

# Applications disponibles

Toutes les applications s'installent avec `caleope install <id>`.  
Synchronise le catalogue avant la première installation : `caleope update`

---

## Média

### [Jellyfin](/apps/jellyfin)
Serveur multimédia libre — films, séries, musique, photos.
```bash
caleope install jellyfin --domain media.monserveur.fr
```

### [Arr Stack](/apps/arr-stack)
Suite complète : Prowlarr, Radarr, Sonarr, Lidarr, Readarr, Bazarr, qBittorrent, SABnzbd, Jellyseerr, Jellyfin Vue — avec Jellyfin et VPN optionnels configurés par wizard à l'installation.
```bash
caleope install arr-stack --domain media.monserveur.fr
# Sur NAS :
caleope install arr-stack --domain media.monserveur.fr \
  --param storage_path=/opt/gaiver-it/caleope/mounts/mon-nas/media
```

---

## Cloud & Productivité

### [Nextcloud + OnlyOffice](/apps/nextcloud)
Suite collaborative — fichiers, agenda, contacts, édition de documents.
```bash
caleope install nextcloud --domain cloud.monserveur.fr
```

---

## Supervision

### [Prometheus + Grafana](/apps/prometheus-grafana)
Métriques système et par application, dashboards historiques.
```bash
caleope install prometheus-grafana --domain metrics.monserveur.fr
```

---

## Communication

### [Fluxer-Discord Bridge](/apps/fluxer-discord-bridge)
Bot passerelle de messages bidirectionnelle entre Discord et Fluxer.
```bash
caleope install fluxer-discord-bridge
# → demande interactivement les tokens Discord et Fluxer
```

---

## Documentation

### [Wiki.js](/apps/wikijs)
Wiki moderne avec éditeur web et synchronisation GitHub.
```bash
caleope install wikijs --domain docs.monserveur.fr
```

---

## Ajouter une application au store

Le store est open-source : [github.com/Gaiver-IT/caleope-store](https://github.com/Gaiver-IT/caleope-store)

Chaque application est un dossier `apps/<id>/` contenant :
- `app.json` — métadonnées, ports, volumes
- `docker-compose.yml` — template Docker Compose (variables Go templates)
- `setup.sh` — préparation : génération de secrets, création de dossiers, config initiale
