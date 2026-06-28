---
title: Pi-hole
description: Bloqueur de publicités DNS réseau
published: true
date: 2026-06-28
---

# Pi-hole

Bloqueur de publicités et trackers au niveau DNS pour tout le réseau. Fonctionne comme serveur DNS — bloque les requêtes vers les domaines publicitaires sur tous les appareils sans configuration client.

## Installation

```bash
caleope install pihole --domain pihole.monserveur.fr
```

Le mot de passe admin est généré automatiquement et sauvegardé dans `app-config/pihole/secrets.env`.

## Configuration

| Paramètre | Description | Défaut |
|-----------|-------------|--------|
| `PIHOLE_WEBPASSWORD` | Mot de passe interface admin | généré automatiquement |
| `PIHOLE_DNS1` | DNS upstream primaire | `1.1.1.1` |
| `PIHOLE_DNS2` | DNS upstream secondaire | `1.0.0.1` |

```bash
caleope install pihole --domain pihole.monserveur.fr \
  --param PIHOLE_DNS1=9.9.9.9 \
  --param PIHOLE_DNS2=149.112.112.112
```

## Accès

```
https://pihole.monserveur.fr/admin     ← Interface d'administration
```

## Récupérer les identifiants

```bash
cat /opt/gaiver-it/caleope/app-config/pihole/secrets.env
```

## Utiliser Pi-hole comme DNS réseau

Configurer l'IP de ce serveur comme DNS primaire sur le routeur. Port DNS : **53** (TCP + UDP).

## Commandes utiles

```bash
caleope logs pihole       # Logs
caleope restart pihole    # Redémarrer
caleope backup pihole     # Sauvegarder
```

## Notes

- Port DNS 53 exposé en TCP et UDP — s'assurer que le port est libre sur l'hôte.
- Pi-hole et AdGuard Home font le même travail — ne pas les installer simultanément.
