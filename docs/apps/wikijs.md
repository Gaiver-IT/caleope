---
title: Wiki.js
description: Wiki moderne avec éditeur web et synchronisation GitHub
published: true
date: 2026-05-25
---

# Wiki.js

Wiki moderne avec éditeur web intégré, lecture publique, et synchronisation bidirectionnelle avec GitHub.

## Installation

```bash
caleope install wikijs --domain docs.monserveur.fr
```

Wiki.js affiche un **wizard de première configuration** à l'ouverture. Les identifiants admin générés s'affichent à la fin de l'installation.

## Premier démarrage — Wizard

1. Ouvre `https://docs.monserveur.fr`
2. Renseigne un email admin (n'importe lequel) et le mot de passe affiché à l'installation
3. La base de données est **déjà configurée** automatiquement — ne pas modifier

## Activer la lecture publique

`Administration → Groups → Guests`  
Cocher :
- ✅ `read:pages`
- ✅ `read:assets`

→ **Save**

## Synchronisation GitHub (pages auto-importées)

`Administration → Storage → Git → Enable`

| Champ | Valeur |
|-------|--------|
| Repository URL | `https://github.com/ton-user/ton-repo` |
| Branch | `main` |
| Username | ton username GitHub |
| Password | Personal Access Token (scope: `repo`) |
| Local folder path | dossier contenant tes fichiers Markdown (ex: `docs`) |
| Sync direction | Pull from remote |

→ **Apply** puis **Sync Now**

> **Personal Access Token** : génère-le sur `github.com → Settings → Developer settings → Tokens (classic)` avec le scope `repo`.

## Éditeur

Wiki.js supporte plusieurs formats d'édition :
- **Markdown** (recommandé)
- **WYSIWYG** (éditeur visuel)
- **Code** (HTML brut)

## Définir la page d'accueil

`Administration → General → Home Page`  
→ entrer le chemin de ta page principale (ex: `/home`)

## Structure des données

```
app-data/wikijs/
└── db/    ← base de données PostgreSQL
```

La configuration de chaque page est en base de données. Les fichiers Markdown du repo GitHub sont synchronisés dans Wiki.js, pas stockés dans `app-data`.

## Sauvegardes

```bash
caleope backup wikijs     # sauvegarde la base PostgreSQL
```

> Les pages Markdown étant dans le repo GitHub, le backup Caleope couvre principalement la config et les médias uploadés.
