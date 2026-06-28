---
title: Memos
description: Application de notes légère style Twitter
published: true
date: 2026-06-28
---

# Memos

Application de notes légère et rapide, style microblog. Prend des notes courtes avec tags, Markdown, images et liens. API REST pour intégrations.

## Installation

```bash
caleope install memos --domain memos.monserveur.fr
```

Les identifiants sont générés automatiquement et sauvegardés dans `app-config/memos/secrets.env`.

## Configuration

| Paramètre | Description | Défaut |
|-----------|-------------|--------|
| `MEMOS_ADMIN_USER` | Nom d'utilisateur admin | `admin` |
| `MEMOS_ADMIN_PASS` | Mot de passe admin | généré automatiquement |

## Accès

```
https://memos.monserveur.fr/
```

## Récupérer les identifiants

```bash
cat /opt/gaiver-it/caleope/app-config/memos/secrets.env
```

## Commandes utiles

```bash
caleope logs memos       # Logs
caleope restart memos    # Redémarrer
caleope backup memos     # Sauvegarder
```

## Structure des données

```
app-data/memos/
└── data/    ← base de données et fichiers uploadés
```
