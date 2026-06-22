---
title: Tailscale
description: VPN mesh basé sur WireGuard avec réseau privé automatique
published: true
date: 2026-06-19
---

# Tailscale

VPN mesh qui connecte tes appareils et serveurs en réseau privé automatique, sans configuration de ports ni de routage. Basé sur WireGuard.

## Prérequis

Un **Auth Key** Tailscale est nécessaire avant l'installation :

1. Aller sur [login.tailscale.com/admin/settings/keys](https://login.tailscale.com/admin/settings/keys)
2. **Generate auth key** → `Reusable: true` (pour autoriser plusieurs machines)
3. Copier la clé (`tskey-auth-...`)

## Installation

```bash
caleope install tailscale --param TS_AUTHKEY=tskey-auth-xxxxx
```

Ou via variable d'environnement :

```bash
CALEOPE_PARAM_TS_AUTHKEY=tskey-auth-xxxxx caleope install tailscale
```

> Tailscale est un **outil système** (`no_container`) — il n'a pas de container Docker. Il s'installe directement sur l'hôte.

## Vérifier la connexion

```bash
tailscale status        # Voir les appareils connectés
tailscale ip            # IP Tailscale du serveur
```

## Cas d'usage

- **Accès distant sécurisé** : atteindre le serveur depuis n'importe où sans exposer de ports
- **Exit node** : faire passer tout le trafic de tes appareils par le serveur
- **Subnet router** : exposer le réseau local (192.168.x.x) à tous tes appareils Tailscale

## Exit node (optionnel)

Pour que le serveur serve de passerelle VPN :

```bash
tailscale up --advertise-exit-node
```

Activer depuis l'interface Tailscale Admin Console.

## Commandes utiles

```bash
tailscale status
tailscale up --reset
caleope list            # Apparaît dans la liste avec port "-"
```
