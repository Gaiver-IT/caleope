---
title: Linkding
description: Gestionnaire de favoris self-hosted
published: true
date: 2026-06-28
---

# Linkding

Gestionnaire de favoris self-hosted, minimaliste et rapide. Sauvegarde, tague et recherche des liens depuis un navigateur ou via l'API REST. Extension navigateur disponible.

## Installation

```bash
caleope install linkding --domain links.monserveur.fr
```

Les identifiants sont générés automatiquement et sauvegardés dans `app-config/linkding/secrets.env`.

## Configuration

| Paramètre | Description | Défaut |
|-----------|-------------|--------|
| `LINKDING_ADMIN_USER` | Nom d'utilisateur admin | `admin` |
| `LINKDING_ADMIN_PASS` | Mot de passe admin | généré automatiquement |

```bash
caleope install linkding --domain links.monserveur.fr \
  --param LINKDING_ADMIN_USER=admin \
  --param LINKDING_ADMIN_PASS=monmotdepasse
```

## Accès

```
https://links.monserveur.fr/
```

## Récupérer les identifiants

```bash
cat /opt/gaiver-it/caleope/app-config/linkding/secrets.env
```

## Commandes utiles

```bash
caleope logs linkding       # Logs
caleope restart linkding    # Redémarrer
caleope backup linkding     # Sauvegarder
```

## Structure des données

```
app-data/linkding/
└── data/    ← base de données SQLite et assets
```
