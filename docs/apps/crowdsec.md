---
title: CrowdSec
description: Protection réseau collaborative et firewall applicatif
published: true
date: 2026-06-19
---

# CrowdSec

Système de protection réseau collaboratif. Analyse les logs Traefik en temps réel, détecte les attaques (brute force, scan de ports, bots…) et bloque automatiquement les IPs malveillantes via un bouncer Traefik.

## Installation

```bash
caleope install crowdsec
```

> CrowdSec n'a pas de domaine public — il tourne en interne et protège toutes les apps via Traefik.

## Architecture

```
Requête entrante
    → Traefik
        → Bouncer CrowdSec (forwardAuth)
            ✓ IP autorisée → app
            ✗ IP bannie → 403
```

**CrowdSec analyse** les logs Traefik (`/var/log/traefik/`) et les logs système (`/var/log/auth.log`).  
**Le bouncer Traefik** intercepte chaque requête et vérifie si l'IP est bannie.

## Ajouter la protection CrowdSec à une app

Le middleware `crowdsec@file` est disponible dans Traefik. Pour protéger une app spécifique, ajouter dans `app-config/<app>/secrets.env` :

```bash
CALEOPE_AUTH_MIDDLEWARE=crowdsec@file
```

Puis redémarrer : `caleope restart <app>`

> Combiner avec Authentik : `CALEOPE_AUTH_MIDDLEWARE=authentik@docker,crowdsec@file`

## Commandes de supervision

```bash
# Décisions actives (IPs bannies)
docker exec crowdsec cscli decisions list

# Alertes récentes
docker exec crowdsec cscli alerts list

# Statut des bouncers
docker exec crowdsec cscli bouncers list

# Métriques
docker exec crowdsec cscli metrics
```

## Débloquer une IP

```bash
docker exec crowdsec cscli decisions delete --ip 1.2.3.4
```

## Commandes utiles

```bash
caleope logs crowdsec
caleope restart crowdsec
caleope backup crowdsec
```
