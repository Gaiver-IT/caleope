---
title: Komga
description: Serveur de bibliothèque pour comics, mangas et livres numériques
published: true
date: 2026-06-28
---

# Komga

Serveur de bibliothèque pour comics, mangas et livres numériques. Interface web avec lecteur intégré, REST API complète et compatibilité OPDS pour les liseuses.

## Installation

```bash
caleope install komga --domain komga.monserveur.fr
```

Les identifiants admin sont générés automatiquement et sauvegardés dans `app-config/komga/secrets.env`.

## Configuration

| Paramètre | Description | Défaut |
|-----------|-------------|--------|
| `KOMGA_PORT` | Port web | `8085` |
| `KOMGA_ADMIN_EMAIL` | Email admin | `admin@<domaine>` |
| `KOMGA_ADMIN_PASSWORD` | Mot de passe admin | généré automatiquement |

```bash
caleope install komga --domain komga.monserveur.fr \
  --param KOMGA_ADMIN_EMAIL=admin@exemple.fr
```

## Accès

```
https://komga.monserveur.fr/
```

## Récupérer les identifiants

```bash
cat /opt/gaiver-it/caleope/app-config/komga/secrets.env
```

## Commandes utiles

```bash
caleope logs komga       # Logs
caleope restart komga    # Redémarrer
caleope backup komga     # Sauvegarder
```

## Structure des données

```
app-data/komga/
├── config/     ← configuration et base de données
└── data/       ← bibliothèques de contenu
```
