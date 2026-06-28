---
title: AzuraCast
description: Station de radio web auto-hébergée
published: true
date: 2026-06-28
---

# AzuraCast

Plateforme de radio web complète : gestion de playlists, streaming live, statistiques d'audience, AutoDJ. Supporte Icecast et SHOUTcast.

## Installation

```bash
caleope install azuracast --domain radio.monserveur.fr
```

Les identifiants admin sont générés et affichés à la fin de l'installation. Ils sont aussi sauvegardés dans `app-config/azuracast/secrets.env`.

## Ports alloués

| Service | Accès | Description |
|---------|-------|-------------|
| Web UI | `https://radio.monserveur.fr` | Interface admin via Traefik |
| Icecast | port dynamique (ex: `:8003`) | Streaming radio (host=container) |
| SFTP | port dynamique | Upload de fichiers audio |

> Le port Icecast est le même côté hôte et côté container (symétrique) — Icecast annonce son propre port dans les URLs de flux. Il est visible dans `caleope list azuracast`.

## Accès

- **Interface admin** : `https://radio.monserveur.fr`
- **Login** : email configuré à l'installation
- **Mot de passe** : affiché à l'installation, dans `app-config/azuracast/secrets.env`

## Configuration initiale (bootstrap automatique)

À l'installation, un container bootstrap configure automatiquement :

1. Création du compte admin (via le formulaire web AzuraCast)
2. Création d'une station radio de démonstration
3. Configuration du port Icecast dynamique

> Si le bootstrap échoue (timeout), l'admin AzuraCast reste accessible pour une configuration manuelle.

## Créer une station

1. **Stations → Créer une nouvelle station**
2. Choisir le nom, le fuseau horaire, le codec (MP3, AAC, Opus…)
3. Uploader de la musique via SFTP ou l'interface web
4. Activer l'AutoDJ ou diffuser en live

## URL de stream

```
https://radio.monserveur.fr/<port-icecast>/radio.mp3
```

Le port Icecast est visible dans **Station → Montage → URL du flux**.

## Streaming live (Mixxx, BUTT…)

Configurer ton logiciel de diffusion :
- **Serveur** : `radio.monserveur.fr`
- **Port** : port Icecast de ta station (Station → Montage)
- **Mot de passe** : visible dans Station → Montage → Paramètres

## Commandes utiles

```bash
caleope logs azuracast
caleope backup azuracast
caleope restart azuracast
```

## Structure des données

```
app-data/azuracast/
├── stations/   ← fichiers audio et playlists
├── geoip/      ← base GeoIP pour statistiques
└── backups/    ← sauvegardes AzuraCast
```
