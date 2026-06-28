---
title: Applications disponibles
description: Catalogue des applications du store Caleope
published: true
date: 2026-06-28
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

### [Jellyseerr](/apps/jellyseerr)
Gestionnaire de demandes de médias pour Jellyfin — films et séries à la demande.
```bash
caleope install jellyseerr --domain media.monserveur.fr
```

### [Immich](/apps/immich)
Alternative self-hosted à Google Photos — sauvegarde mobile, reconnaissance faciale, galerie.
```bash
caleope install immich --domain photos.monserveur.fr
```

### [PhotoPrism](/apps/photoprism)
Gestionnaire de photos AI-powered — reconnaissance faciale, classification automatique, géolocalisation.
```bash
caleope install photoprism --domain photos.monserveur.fr
```

### [Plex](/apps/plex)
Serveur multimédia populaire — apps natives iOS, Android, TV, console et web.
```bash
caleope install plex --domain plex.monserveur.fr
caleope install plex --domain plex.monserveur.fr --param PLEX_CLAIM=claim-xxxxxx
```

### [AzuraCast](/apps/azuracast)
Station de radio web — playlists, streaming live, AutoDJ, statistiques d'audience.
```bash
caleope install azuracast --domain radio.monserveur.fr
```

### [Navidrome](/apps/navidrome)
Serveur de musique compatible Subsonic — streamez votre collection musicale.
```bash
caleope install navidrome --domain music.monserveur.fr
```

### [Calibre-Web](/apps/calibre-web)
Interface web pour votre bibliothèque Calibre — parcourez, lisez et téléchargez des ebooks.
```bash
caleope install calibre-web --domain calibre.monserveur.fr
```

### [Kavita](/apps/kavita)
Serveur de lecture pour mangas, comics, ebooks et BD — interface web et mobile.
```bash
caleope install kavita --domain kavita.monserveur.fr
```

### [Komga](/apps/komga)
Serveur de bibliothèque pour comics, mangas et livres numériques.
```bash
caleope install komga --domain komga.monserveur.fr
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

### [FreshRSS](/apps/freshrss)
Agrégateur RSS/Atom self-hosted compatible avec les clients mobiles.
```bash
caleope install freshrss --domain rss.monserveur.fr
```

### [Linkding](/apps/linkding)
Gestionnaire de favoris minimaliste — sauvegarde, tague et recherche de liens.
```bash
caleope install linkding --domain links.monserveur.fr
```

### [Paperless-ngx](/apps/paperless-ngx)
Gestion de documents numérisés avec OCR automatique.
```bash
caleope install paperless-ngx --domain paperless.monserveur.fr
```

### [Stirling PDF](/apps/stirling-pdf)
Suite d'outils PDF — fusion, division, compression, OCR et conversion.
```bash
caleope install stirling-pdf --domain pdf.monserveur.fr
```

### [Memos](/apps/memos)
Application de notes légère style microblog — rapide, Markdown, tags.
```bash
caleope install memos --domain memos.monserveur.fr
```

### [n8n](/apps/n8n)
Plateforme d'automatisation de workflows, alternative à Zapier.
```bash
caleope install n8n --domain n8n.monserveur.fr
```

### [Syncthing](/apps/syncthing)
Synchronisation de fichiers peer-to-peer chiffrée entre appareils.
```bash
caleope install syncthing --domain sync.monserveur.fr
```

### [Code Server](/apps/code-server)
VS Code dans le navigateur — éditeur de code complet accessible depuis partout.
```bash
caleope install code-server --domain code.monserveur.fr
```

### [File Browser](/apps/filebrowser)
Gestionnaire de fichiers web avec upload, édition et partage.
```bash
caleope install filebrowser --domain files.monserveur.fr
```

---

## Vie quotidienne

### [Grocy](/apps/grocy)
ERP pour la maison — stock alimentaire, courses, recettes, équipement.
```bash
caleope install grocy --domain grocy.monserveur.fr
```

### [Mealie](/apps/mealie)
Gestionnaire de recettes avec import automatique depuis URLs et planificateur de repas.
```bash
caleope install mealie --domain mealie.monserveur.fr
```

### [Monica](/apps/monica)
CRM personnel — gérez vos relations, contacts, anniversaires et interactions sociales.
```bash
caleope install monica --domain monica.monserveur.fr
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

### [AdGuard Home](/apps/adguard)
Bloqueur de publicités et trackers DNS réseau — serveur DNS complet.
```bash
caleope install adguard --domain adguard.monserveur.fr
```

### [Pi-hole](/apps/pihole)
Bloqueur de publicités DNS — filtre les pubs sur tout le réseau.
```bash
caleope install pihole --domain pihole.monserveur.fr
```

---

## Supervision & Monitoring

### [Prometheus + Grafana](/apps/prometheus-grafana)
Métriques système et par application, dashboards historiques.
```bash
caleope install prometheus-grafana --domain metrics.monserveur.fr
```

### [Uptime Kuma](/apps/uptime-kuma)
Monitoring de disponibilité des services — alertes multicanaux.
```bash
caleope install uptime-kuma --domain status.monserveur.fr
```

### [Changedetection.io](/apps/changedetection)
Surveillance de changements de pages web avec alertes.
```bash
caleope install changedetection --domain changedetection.monserveur.fr
```

### [Scrutiny](/apps/scrutiny)
Monitoring de santé des disques durs via S.M.A.R.T.
```bash
caleope install scrutiny --domain scrutiny.monserveur.fr
```

---

## Notifications

### [Gotify](/apps/gotify)
Serveur de notifications push auto-hébergé, simple et léger.
```bash
caleope install gotify --domain gotify.monserveur.fr
```

### [ntfy](/apps/ntfy)
Notifications push HTTP/WebSocket self-hosted — sans compte requis.
```bash
caleope install ntfy --domain ntfy.monserveur.fr
```

---

## Domotique

### [Home Assistant](/apps/home-assistant)
Plateforme domotique open-source — centralise et automatise les objets connectés.
```bash
caleope install home-assistant --domain ha.monserveur.fr
```

### [Homarr](/apps/homarr)
Dashboard de liens et widgets pour centraliser vos applications self-hosted.
```bash
caleope install homarr --domain home.monserveur.fr
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
