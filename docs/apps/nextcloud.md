---
title: Nextcloud + OnlyOffice
description: Suite collaborative — fichiers, agenda, contacts, édition de documents
published: true
date: 2026-06-28
---

# Nextcloud + OnlyOffice

Suite collaborative complète auto-hébergée. Partage de fichiers, agenda, contacts, édition de documents en ligne (OnlyOffice), visioconférence.

## Installation

```bash
caleope install nextcloud --domain cloud.monserveur.fr
```

Les identifiants admin sont affichés à la fin de l'installation et sauvegardés dans `app-config/nextcloud/secrets.env`.

> ⏳ Nextcloud initialise sa base de données au premier démarrage (3-5 minutes). OnlyOffice se connecte ensuite automatiquement.

## Services inclus

| Service | Rôle |
|---------|------|
| **Nextcloud** | Application principale |
| **MariaDB** | Base de données |
| **Redis** | Cache et sessions |
| **OnlyOffice** | Édition de documents Word/Excel/PowerPoint |
| **Bootstrap** | Configure automatiquement le connecteur OnlyOffice + OIDC |

## Accès

```
https://cloud.monserveur.fr       ← Nextcloud
https://onlyoffice.cloud.monserveur.fr  ← OnlyOffice (sous-domaine dérivé)
```

## Configuration automatique au démarrage

Le container bootstrap configure automatiquement :

- Connexion OnlyOffice ↔ Nextcloud (JWT, URLs internes)
- Trusted proxies pour Traefik
- OIDC Authentik via l'app `user_oidc` (si Authentik est installé)

## SSO Authentik (OIDC)

Si Authentik est installé, l'app `user_oidc` est activée et configurée automatiquement.

- Callback URI : `https://cloud.monserveur.fr/apps/user_oidc/code`
- Connexion : bouton **"Se connecter avec Authentik"** sur la page de login Nextcloud

## Applications Nextcloud recommandées

Dans Nextcloud → Applications :

| App | Utilité |
|-----|---------|
| **Calendar** | Agenda (CalDAV) |
| **Contacts** | Carnet d'adresses (CardDAV) |
| **Talk** | Visioconférence et messagerie |
| **Notes** | Prise de notes Markdown |
| **Tasks** | Gestionnaire de tâches |
| **Photos** | Galerie photos |

## Synchronisation locale

- **PC** : client Nextcloud Desktop (Windows/Mac/Linux)
- **Mobile** : Nextcloud iOS / Android
- **Calendrier** : compatible Apple Calendar, Thunderbird, etc. via CalDAV
- **Contacts** : compatible macOS Contacts, etc. via CardDAV

## Sauvegardes

```bash
caleope backup nextcloud     # sauvegarde données + config
caleope backups nextcloud    # liste les sauvegardes
caleope restore nextcloud    # restaure la plus récente
```

## Récupérer les identifiants

```bash
cat /opt/gaiver-it/caleope/app-config/nextcloud/secrets.env
```

## Structure des données

```
app-data/nextcloud/
├── html/    ← fichiers de l'application Nextcloud
├── db/      ← base de données MariaDB
├── config/  ← configuration Nextcloud
└── data/    ← fichiers des utilisateurs
```
