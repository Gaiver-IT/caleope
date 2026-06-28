---
title: AdGuard Home
description: Bloqueur de publicités et trackers DNS — serveur DNS réseau complet
published: true
date: 2026-06-28
---

# AdGuard Home

Bloqueur de publicités et trackers DNS auto-hébergé. Fonctionne comme serveur DNS sur tout le réseau — bloque les pubs et trackers sur tous les appareils sans installer de client.

## Installation

```bash
caleope install adguard --domain adguard.monserveur.fr
```

Les identifiants admin sont générés automatiquement et sauvegardés dans `app-config/adguard/secrets.env`.

## Configuration

| Paramètre | Description | Défaut |
|-----------|-------------|--------|
| `ADGUARD_USERNAME` | Nom d'utilisateur admin | `admin` |
| `ADGUARD_PASSWORD` | Mot de passe admin | généré automatiquement |
| `ADGUARD_DNS1` | DNS upstream primaire | `1.1.1.1` |
| `ADGUARD_DNS2` | DNS upstream secondaire | `1.0.0.1` |

```bash
caleope install adguard --domain adguard.monserveur.fr \
  --param ADGUARD_USERNAME=admin \
  --param ADGUARD_DNS1=9.9.9.9
```

La configuration AdGuardHome.yaml est pré-écrite avant le démarrage du container. Les listes de blocage AdGuard DNS filter et AdAway sont activées par défaut.

## Accès

```
https://adguard.monserveur.fr/     ← Interface admin
```

## Utiliser AdGuard comme DNS réseau

Configurer l'IP de ce serveur comme DNS primaire sur ton routeur ou tes clients. Le port DNS est **53** (TCP + UDP).

## Récupérer les identifiants

```bash
cat /opt/gaiver-it/caleope/app-config/adguard/secrets.env
```

## Commandes utiles

```bash
caleope logs adguard         # Logs AdGuard Home
caleope restart adguard      # Redémarrer
caleope backup adguard       # Sauvegarder la config
```

## Notes

- Le wizard de configuration initiale s'affiche si `bcrypt` n'était pas disponible à l'installation — utiliser les credentials de `secrets.env` pour se connecter.
- Port DNS 53 exposé en TCP et UDP — nécessite que le port soit libre sur l'hôte.
