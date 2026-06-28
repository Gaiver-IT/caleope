---
title: Code Server
description: VS Code dans le navigateur
published: true
date: 2026-06-28
---

# Code Server

VS Code dans le navigateur — éditeur de code complet accessible depuis n'importe où, avec terminal intégré, extensions et accès aux fichiers du serveur.

## Installation

```bash
caleope install code-server --domain code.monserveur.fr
```

Le mot de passe est généré automatiquement et sauvegardé dans `app-config/code-server/secrets.env`.

## Configuration

| Paramètre | Description | Défaut |
|-----------|-------------|--------|
| `CODE_SERVER_PORT` | Port web | `8443` |
| `CODE_SERVER_PASSWORD` | Mot de passe d'accès | généré automatiquement |
| `CODE_SERVER_SUDO_PASSWORD` | Mot de passe sudo dans le terminal | identique au mot de passe |

```bash
caleope install code-server --domain code.monserveur.fr \
  --param CODE_SERVER_PASSWORD=monmotdepasse
```

## Accès

```
https://code.monserveur.fr/
```

- **Mot de passe** : voir `app-config/code-server/secrets.env`

## Récupérer le mot de passe

```bash
cat /opt/gaiver-it/caleope/app-config/code-server/secrets.env
```

## Commandes utiles

```bash
caleope logs code-server       # Logs
caleope restart code-server    # Redémarrer
caleope backup code-server     # Sauvegarder config et workspace
```

## Structure des données

```
app-data/code-server/
├── config/      ← configuration VS Code (extensions, settings)
└── workspace/   ← répertoire de travail par défaut
```
