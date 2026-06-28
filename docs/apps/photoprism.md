---
title: PhotoPrism
description: Gestionnaire de photos AI-powered — alternative à Google Photos
published: true
date: 2026-06-28
---

# PhotoPrism

Gestionnaire de photos auto-hébergé avec intelligence artificielle — reconnaissance faciale, classification automatique, géolocalisation et recherche par sujet. Alternative à Google Photos.

## Installation

```bash
caleope install photoprism --domain photos.monserveur.fr
```

Le mot de passe admin est généré automatiquement et sauvegardé dans `app-config/photoprism/secrets.env`.

## Configuration

| Paramètre | Description | Défaut |
|-----------|-------------|--------|
| `PHOTOPRISM_PORT_WEB` | Port web | `2342` |
| `PHOTOPRISM_ADMIN_PASSWORD` | Mot de passe admin | généré automatiquement |

```bash
caleope install photoprism --domain photos.monserveur.fr \
  --param PHOTOPRISM_ADMIN_PASSWORD=monmotdepasse
```

## Accès

```
https://photos.monserveur.fr/
```

- **Login** : `admin`
- **Mot de passe** : voir `app-config/photoprism/secrets.env`

## Récupérer les identifiants

```bash
cat /opt/gaiver-it/caleope/app-config/photoprism/secrets.env
```

## Commandes utiles

```bash
caleope logs photoprism       # Logs
caleope restart photoprism    # Redémarrer
caleope backup photoprism     # Sauvegarder
```

## Structure des données

```
app-data/photoprism/
├── storage/    ← cache, miniatures, base de données
└── originals/  ← photos originales
```

## Notes

- L'indexation initiale peut prendre plusieurs minutes à heures selon la taille de la bibliothèque.
- Placer les photos dans `app-data/photoprism/originals/` ou configurer un volume bind supplémentaire.
