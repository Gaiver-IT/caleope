---
title: Nextcloud + OnlyOffice
description: Suite collaborative — fichiers, agenda, contacts, édition de documents
published: true
date: 2026-05-25
---

# Nextcloud + OnlyOffice

Suite collaborative complète auto-hébergée. Partage de fichiers, agenda, contacts, édition de documents en ligne (OnlyOffice), visioconférence.

## Installation

```bash
caleope install nextcloud --domain cloud.monserveur.fr
```

Les identifiants admin sont affichés à la fin de l'installation et sauvegardés dans `app-config/nextcloud/secrets.env`.

> ⏳ Nextcloud initialise sa base de données au premier démarrage (3-5 minutes). OnlyOffice démarre ensuite (2-3 minutes supplémentaires).

## Services inclus

| Service | Rôle |
|---------|------|
| **Nextcloud** | Application principale |
| **MariaDB** | Base de données |
| **Redis** | Cache et sessions |
| **OnlyOffice** | Édition de documents Word/Excel/PowerPoint |
| **Bootstrap** | Configure automatiquement le connecteur OnlyOffice |

## Accès

```
https://cloud.monserveur.fr       ← Nextcloud
https://onlyoffice.monserveur.fr  ← OnlyOffice (domaine dérivé automatiquement)
```

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
