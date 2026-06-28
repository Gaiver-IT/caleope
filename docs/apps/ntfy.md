---
title: ntfy
description: Serveur de notifications push HTTP/WebSocket self-hosted
published: true
date: 2026-06-28
---

# ntfy

Serveur de notifications push self-hosted via HTTP/WebSocket. Envoie des notifications depuis n'importe quel script ou service vers des apps mobiles ou le web, sans compte requis.

## Installation

```bash
caleope install ntfy --domain ntfy.monserveur.fr
```

## Configuration

| Paramètre | Description | Défaut |
|-----------|-------------|--------|
| `NTFY_PORT_WEB` | Port web | `8070` |

## Accès

```
https://ntfy.monserveur.fr/
```

## Envoyer une notification

```bash
# Simple
curl -d "Sauvegarde terminée" https://ntfy.monserveur.fr/mon-topic

# Avec titre et priorité
curl -H "Title: Backup" \
     -H "Priority: high" \
     -d "La sauvegarde est terminée" \
     https://ntfy.monserveur.fr/mon-topic
```

## Application mobile

- **Android** : [ntfy sur Play Store / F-Droid](https://ntfy.sh)
- **iOS** : [ntfy sur App Store](https://ntfy.sh)

Dans l'app, configurer le serveur : `https://ntfy.monserveur.fr`

## Commandes utiles

```bash
caleope logs ntfy       # Logs
caleope restart ntfy    # Redémarrer
caleope backup ntfy     # Sauvegarder
```

## Structure des données

```
app-data/ntfy/
└── data/    ← configuration et cache
```
