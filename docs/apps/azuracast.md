---
title: AzuraCast
description: Station de radio web auto-hébergée
published: true
date: 2026-06-19
---

# AzuraCast

Plateforme de radio web complète : gestion de playlists, streaming live, statistiques d'audience, AutoDJ. Supporte Icecast et SHOUTcast.

## Installation

```bash
caleope install azuracast --domain radio.monserveur.fr
```

## Accès

| Interface | URL |
|-----------|-----|
| Admin | `https://radio.monserveur.fr` |
| Stream Icecast | `https://radio.monserveur.fr:8500` |
| SFTP médias | `sftp://<server>:<sftp-port>` |

- **Login** : `admin`
- **Mot de passe** : généré à l'installation, affiché dans le terminal et dans `app-config/azuracast/secrets.env`

## Créer une station

1. **Stations → Créer une nouvelle station**
2. Choisir le nom, le fuseau horaire, le codec (MP3, AAC, Opus…)
3. Uploader de la musique ou configurer l'AutoDJ
4. Partager l'URL de stream : `https://radio.monserveur.fr/<port>/radio.mp3`

## Streaming live (Mixxx, BUTT…)

Configurer ton logiciel de diffusion :
- **Serveur** : `radio.monserveur.fr`
- **Port** : le port Icecast de ta station (visible dans Station → Montage)
- **Mot de passe** : visible dans Station → Montage → Paramètres

## Commandes utiles

```bash
caleope logs azuracast
caleope backup azuracast
caleope restart azuracast
```
