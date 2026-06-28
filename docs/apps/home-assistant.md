---
title: Home Assistant
description: Plateforme domotique open-source
published: true
date: 2026-06-28
---

# Home Assistant

Plateforme domotique open-source pour centraliser et automatiser les objets connectés. Compatible avec des milliers d'intégrations (Zigbee, Z-Wave, Matter, Philips Hue, IKEA, Google, Apple, etc.).

## Installation

```bash
caleope install home-assistant --domain ha.monserveur.fr
```

## Configuration

| Paramètre | Description | Défaut |
|-----------|-------------|--------|
| `HA_PORT_WEB` | Port web | `8123` |

Un fichier `configuration.yaml` est pré-généré avec les trusted proxies Traefik configurés.

## Accès

```
https://ha.monserveur.fr/
```

Suivre l'assistant de configuration au premier accès pour créer le compte administrateur.

## Commandes utiles

```bash
caleope logs home-assistant       # Logs
caleope restart home-assistant    # Redémarrer
caleope backup home-assistant     # Sauvegarder
```

## Structure des données

```
app-data/home-assistant/
└── config/    ← configuration YAML, automations, scripts, intégrations
```

## Notes

- Les trusted proxies sont pré-configurés pour Traefik (`172.16.0.0/12`, `10.0.0.0/8`, `192.168.0.0/16`).
- Pour la découverte mDNS et les intégrations réseau locales (Zigbee USB, etc.), un accès réseau étendu peut être nécessaire — modifier le `docker-compose` pour passer en `network_mode: host`.
