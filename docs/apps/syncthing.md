---
title: Syncthing
description: Synchronisation de fichiers peer-to-peer
published: true
date: 2026-06-28
---

# Syncthing

Synchronisation de fichiers peer-to-peer, chiffrée et décentralisée. Synchronise des dossiers entre plusieurs appareils (serveur, PC, mobile) sans passer par un cloud tiers.

## Installation

```bash
caleope install syncthing --domain sync.monserveur.fr
```

## Configuration

| Paramètre | Description | Défaut |
|-----------|-------------|--------|
| `SYNCTHING_PORT_WEB` | Port interface web | `8384` |

Ports supplémentaires exposés (fixes) :
- **22000** TCP/UDP — synchronisation entre devices
- **21027** UDP — découverte locale

## Accès

```
https://sync.monserveur.fr/
```

L'interface est accessible sans mot de passe par défaut. Configurer un mot de passe dans **Actions → Settings → GUI**.

## Ajouter des appareils

1. Ouvrir Syncthing sur les deux appareils
2. Copier l'**ID de l'appareil** depuis **Actions → Show ID**
3. Sur l'autre appareil, **Add Remote Device** et coller l'ID
4. Partager les dossiers souhaités

## Commandes utiles

```bash
caleope logs syncthing       # Logs
caleope restart syncthing    # Redémarrer
caleope backup syncthing     # Sauvegarder la config
```

## Structure des données

```
app-data/syncthing/
└── config/    ← configuration et certificats TLS
```

## Notes

- S'assurer que les ports 22000 (TCP/UDP) et 21027 (UDP) sont accessibles si la synchronisation se fait depuis l'extérieur du réseau local.
