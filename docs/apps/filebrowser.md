---
title: File Browser
description: Gestionnaire de fichiers web avec upload, édition et partage
published: true
date: 2026-06-28
---

# File Browser

Gestionnaire de fichiers web léger avec upload, édition, partage de liens et gestion multi-utilisateurs. Donne accès aux fichiers du serveur depuis un navigateur.

## Installation

```bash
caleope install filebrowser --domain files.monserveur.fr
```

## Configuration

| Paramètre | Description | Défaut |
|-----------|-------------|--------|
| `FILEBROWSER_PORT_WEB` | Port web | `8085` |

## Accès

```
https://files.monserveur.fr/
```

- **Login** : `admin`
- **Mot de passe** : `admin`

> Changer le mot de passe immédiatement après la première connexion dans **Settings → User Management**.

## Commandes utiles

```bash
caleope logs filebrowser       # Logs
caleope restart filebrowser    # Redémarrer
caleope backup filebrowser     # Sauvegarder
```

## Structure des données

```
app-data/filebrowser/
├── db/      ← base de données (utilisateurs, partages)
├── conf/    ← configuration filebrowser
└── (racine) ← fichiers servis (/srv dans le container)
```

## Notes

- La racine exposée dans l'interface est `/srv` dans le container, monté sur `app-data/filebrowser/`.
- Pour exposer un autre répertoire du serveur, utiliser un volume bind supplémentaire.
