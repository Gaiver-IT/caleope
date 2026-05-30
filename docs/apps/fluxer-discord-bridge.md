---
title: Fluxer-Discord Bridge
description: Bot passerelle de messages bidirectionnelle entre Discord et Fluxer
published: true
date: 2026-05-30
---

# Fluxer-Discord Bridge

Bot qui synchronise les messages entre des channels **Discord** et des channels **Fluxer** en temps réel. Chaque message posté dans un channel bridgé est automatiquement relayé dans l'autre plateforme.

## Installation

```bash
caleope install fluxer-discord-bridge
```

Le CLI demande interactivement :

| Champ | Description |
|-------|-------------|
| **Token du bot Discord** | [discord.com/developers/applications](https://discord.com/developers/applications) → ton app → Bot → Token |
| **Token du bot Fluxer** | Token d'API du bot côté Fluxer |
| **Préfixe des commandes** | Défaut : `brdg;` — personnalisable (ex : `bridge!`) |

### Permissions requises sur les deux bots

| Permission | Utilité |
|---|---|
| Manage Roles | Gestion des rôles |
| Manage Webhooks | Création des webhooks de relais |
| Send Messages | Envoi des messages bridgés |
| Read Message History | Lecture des messages entrants |

## Configurer un bridge

### Via les commandes bot (recommandé)

Toutes les commandes nécessitent la permission **Manage Channels** sur le channel, sauf `help`.

| Commande | Syntaxe | Description |
|---|---|---|
| `mate` | `brdg;mate <ID_channel>` | Bridge **bidirectionnel** entre le channel courant et l'ID spécifié |
| `divorce` | `brdg;divorce <ID_channel>` | Supprime le bridge bidirectionnel |
| `listen` | `brdg;listen <ID1>, <ID2>` | Abonne le channel courant (réception uniquement) |
| `drop` | `brdg;drop <ID1>, <ID2>` | Désabonne le channel courant |
| `help` | `brdg;help` | Affiche l'aide |

### Étapes pour bridger Discord ↔ Fluxer

1. Va dans le **channel Discord** à bridger
2. Récupère l'**ID du channel Fluxer** cible (clic droit → Copier l'ID)
3. Tape dans Discord :
```
brdg;mate <ID_channel_Fluxer>
```
4. ✅ Le bridge est actif dans les deux sens

### Configuration manuelle (Bridges.yaml)

Les bridges sont persistés dans :
```
/opt/gaiver-it/caleope/app-data/fluxer-discord-bridge/db/Bridges.yaml
```

Format du fichier :

```yaml
Discord:
  "<ID_channel_Discord>":       # Channel Discord source
    - "<ID_channel_Fluxer>"     # Channel Fluxer destination

Fluxer:
  "<ID_channel_Fluxer>":        # Channel Fluxer source (retour)
    - "<ID_channel_Discord>"    # Channel Discord destination
```

> Pour un bridge bidirectionnel, il faut déclarer **chaque sens** séparément.

Après modification manuelle :
```bash
caleope restart fluxer-discord-bridge
```

## Gestion

```bash
# Voir les logs en temps réel
caleope logs fluxer-discord-bridge

# Redémarrer (relit secrets.env et Bridges.yaml)
caleope restart fluxer-discord-bridge

# Modifier les tokens
sudo nano /opt/gaiver-it/caleope/app-config/fluxer-discord-bridge/secrets.env
caleope restart fluxer-discord-bridge
```

## Sauvegardes

Les bridges configurés (`Bridges.yaml`) sont inclus dans les sauvegardes Caleope :

```bash
caleope backup fluxer-discord-bridge
caleope backups fluxer-discord-bridge   # liste les sauvegardes
caleope restore fluxer-discord-bridge   # restaure la plus récente
```

## Structure des données

```
app-data/fluxer-discord-bridge/
└── db/
    └── Bridges.yaml    ← bridges configurés (persistés)

app-config/fluxer-discord-bridge/
└── secrets.env         ← tokens Discord et Fluxer
```
