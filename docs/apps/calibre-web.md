---
title: Calibre-Web
description: Interface web pour votre bibliothèque Calibre
published: true
date: 2026-06-28
---

# Calibre-Web

Interface web pour parcourir, lire et télécharger des ebooks depuis une bibliothèque Calibre. Compatible OPDS pour les liseuses.

## Installation

```bash
caleope install calibre-web --domain calibre.monserveur.fr
```

## Configuration

| Paramètre | Description | Défaut |
|-----------|-------------|--------|
| `CALIBREWEB_PORT_WEB` | Port web | `8083` |

## Accès

```
https://calibre.monserveur.fr/
```

- **Login** : `admin`
- **Mot de passe** : `admin123`

> Changer le mot de passe immédiatement après la première connexion dans **Admin → Edit User**.

## Bibliothèque Calibre

Placer les fichiers de la bibliothèque Calibre dans :

```
app-data/calibre-web/books/
```

Puis configurer le chemin `/books` dans Calibre-Web au premier accès (**Admin → Edit Basic Configuration → Location of Calibre Database**).

## Commandes utiles

```bash
caleope logs calibre-web       # Logs
caleope restart calibre-web    # Redémarrer
caleope backup calibre-web     # Sauvegarder
```

## Structure des données

```
app-data/calibre-web/
├── config/   ← configuration et base de données Calibre-Web
└── books/    ← bibliothèque Calibre (metadata.db + ebooks)
```
