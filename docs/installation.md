---
title: Installation
description: Installer Caleope sur Debian/Ubuntu
published: true
date: 2026-06-18
---

# Installation

## Prérequis

| Élément | Version minimale |
|---------|-----------------|
| OS | Debian 12 ou Ubuntu 22.04+ |
| Accès | Root (ou sudo) |
| Réseau | Connexion internet active |

> Docker est installé automatiquement par l'installateur si nécessaire.

---

## Installer Caleope

```bash
# 1. S'assurer que curl est disponible
apt install -y curl

# 2. Lancer l'installateur
curl -fsSL https://raw.githubusercontent.com/Gaiver-IT/caleope/main/install.sh | bash
```

**Installation non-interactive** (scripts, CI) — passer les variables d'environnement pour sauter les prompts :

```bash
CALEOPE_DOMAIN=caleope.home.local \
CALEOPE_PROXY_MODE=standalone \
CALEOPE_CHANNEL=stable \
bash install.sh
```

L'installateur te demande quelques informations :

- **Domaine de base** — ex : `home.local` ou `monserveur.fr`  
  Les applications seront accessibles sur `<app>.<domaine-base>` (ex: `jellyfin.home.local`)

- **Mode reverse proxy** — trois options :

  | Mode | Quand l'utiliser |
  |------|-----------------|
  | `npm` | Derrière NPM, Caddy ou un autre proxy existant — Traefik reçoit du HTTP, pas de gestion des certs |
  | `traefik` | Traefik gère tout en direct — HTTPS et Let's Encrypt automatiques |
  | `standalone` | Réseau local, hors-ligne, air-gap — HTTP uniquement, aucun certificat requis |

- **Canal** — `stable` (recommandé) ou `alpha` (fonctionnalités en avant-première)

Après installation, le daemon `caleoped` démarre automatiquement via systemd.

---

## Vérifier l'installation

```bash
caleope ping
# → ✓ Daemon actif — version v0.4.7

caleope version
# → caleope v0.4.7
```

---

## Première application

```bash
# Synchroniser le catalogue d'apps
caleope update

# Rechercher une application
caleope search media

# Installer Jellyfin
caleope install jellyfin --domain media.monserveur.fr
```

→ L'application est accessible immédiatement à l'adresse affichée.

---

## Structure des fichiers

Caleope s'installe dans `/opt/gaiver-it/caleope/` :

```
/opt/gaiver-it/caleope/
├── app-config/       # Configuration et secrets de chaque app
├── app-data/         # Données des applications (volumes Docker)
├── apps-installed/   # Fichiers docker-compose générés
├── backups/          # Sauvegardes
├── mounts/           # Points de montage NAS
└── caleope.conf      # Configuration globale
```

---

## Installation hors-ligne (mode submarine)

Pour installer Caleope **sans accès internet** (réseau isolé, clé USB, air-gap) :

```bash
# 1. Sur une machine connectée, créer un bundle
caleope offline-pack /media/usb/

# 2. Sur le serveur hors-ligne, lancer l'installation
sudo bash install.sh --offline /media/usb/caleope-bundle-2026-06-22
```

> Docker doit être installé avant de lancer l'installation offline.

→ [Documentation complète du mode submarine](/submarine)

---

## Mises à jour

```bash
caleope upgrade          # Mettre à jour Caleope
caleope upgrade --check  # Vérifier sans installer
caleope update           # Synchroniser le catalogue d'apps
```

---

## Désinstaller Caleope

```bash
# Arrêter le daemon
systemctl stop caleoped

# Supprimer les binaires
rm /usr/local/bin/caleope /usr/local/bin/caleoped

# Supprimer les données (optionnel)
rm -rf /opt/gaiver-it/caleope
```
