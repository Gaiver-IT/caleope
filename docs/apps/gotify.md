---
title: Gotify
description: Serveur de notifications push auto-hébergé
published: true
date: 2026-06-28
---

# Gotify

Serveur de notifications push auto-hébergé, simple et léger. Envoie des notifications depuis scripts, apps ou services vers des clients Android ou web via une API REST.

## Installation

```bash
caleope install gotify --domain gotify.monserveur.fr
```

Le mot de passe admin est généré automatiquement et sauvegardé dans `app-config/gotify/secrets.env`.

## Configuration

| Paramètre | Description | Défaut |
|-----------|-------------|--------|
| `GOTIFY_PORT_WEB` | Port web | `8090` |
| `GOTIFY_DEFAULTUSER_PASS` | Mot de passe admin | généré automatiquement |

## Accès

```
https://gotify.monserveur.fr/
```

- **Login** : `admin`
- **Mot de passe** : voir `app-config/gotify/secrets.env`

## Récupérer les identifiants

```bash
cat /opt/gaiver-it/caleope/app-config/gotify/secrets.env
```

## Envoyer une notification

```bash
curl -X POST "https://gotify.monserveur.fr/message?token=<TOKEN_APP>" \
  -F "title=Titre" \
  -F "message=Mon message" \
  -F "priority=5"
```

## Intégration Caleope

Gotify est utilisé par Caleope pour les notifications système. Un token client est généré automatiquement à l'installation.

## Commandes utiles

```bash
caleope logs gotify       # Logs
caleope restart gotify    # Redémarrer
caleope backup gotify     # Sauvegarder
```

## Structure des données

```
app-data/gotify/
└── data/    ← applications, messages, tokens
```
