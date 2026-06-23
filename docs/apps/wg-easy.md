---
title: WG-Easy
description: Interface web WireGuard pour VPN personnel
published: true
date: 2026-06-19
---

# WG-Easy

Interface web pour gérer un serveur WireGuard. Crée et distribue des configs VPN pour tes appareils (téléphone, laptop, tablette) en quelques clics.

## Installation

```bash
caleope install wg-easy --domain vpn.monserveur.fr
```

> WireGuard ouvre le port UDP **51820** dans UFW automatiquement.

## Accès

- **Interface admin** : `https://vpn.monserveur.fr`
- **Mot de passe** : généré à l'installation, affiché dans le terminal et dans `app-config/wg-easy/secrets.env`

## Ajouter un appareil

1. **Ouvrir l'interface** → `+ Nouveau client`
2. Donner un nom (ex: `iphone-ewen`, `laptop-bureau`)
3. **Télécharger le fichier** `.conf` ou **scanner le QR code**
4. Importer dans l'app WireGuard sur l'appareil

## Applications WireGuard

| Plateforme | Application |
|------------|-------------|
| iOS | WireGuard (App Store) |
| Android | WireGuard (Play Store) |
| Windows | WireGuard for Windows |
| macOS | WireGuard (App Store) |
| Linux | `wg-quick` |

## Accéder aux services internes via VPN

Une fois connecté au VPN, tu accèdes directement au réseau de ton serveur. Utile pour :
- Accéder aux apps sans les exposer sur Internet
- Accéder à l'IP locale du serveur depuis n'importe où

## Commandes utiles

```bash
caleope logs wg-easy
caleope restart wg-easy
caleope backup wg-easy      # Sauvegarde les configs clients
```
