---
title: Pterodactyl Wings
description: Daemon d'exécution des serveurs de jeux Pterodactyl
published: true
date: 2026-06-19
---

# Pterodactyl Wings

Daemon qui exécute les serveurs de jeux pilotés par le Panel Pterodactyl. Peut tourner sur le même serveur que le Panel ou sur des machines dédiées.

## Prérequis

**Pterodactyl Panel doit être installé et accessible** avant d'installer Wings.

## Installation

```bash
caleope install pterodactyl-wings \
  --param NODE_FQDN=<ip-ou-domaine-du-serveur>
```

Le setup se connecte automatiquement au Panel pour :
1. Créer le nœud Wings dans le Panel
2. Télécharger la configuration `config.yml`
3. Démarrer le daemon

## Vérification

```bash
# Wings doit apparaître en "actif" dans le Panel (Admin → Nodes)
caleope logs pterodactyl-wings

# Vérifier la connexion Panel ↔ Wings
curl http://localhost:8080
```

## Port SFTP des serveurs de jeux

Le port **2022** est ouvert pour le SFTP Pterodactyl (accès aux fichiers des serveurs de jeux depuis FileZilla ou WinSCP).

```
Hôte   : <ip-du-serveur>
Port   : 2022
Login  : <user>.<server-uuid>
Passe  : mot de passe Panel
```

## Ajouter des Eggs (types de serveurs)

Depuis le Panel : **Admin → Nests → Import Egg**

Eggs communautaires : [github.com/pterodactyl/eggs](https://github.com/pterodactyl/eggs)

| Jeu | Egg |
|-----|-----|
| Minecraft Java | `minecraft/java` |
| Minecraft Bedrock | `minecraft/bedrock` |
| CS2 | `games/source/cs2` |
| Valheim | `games/valheim` |
| Rust | `games/rust` |

## Commandes utiles

```bash
caleope logs pterodactyl-wings
caleope restart pterodactyl-wings
```
