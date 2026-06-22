---
title: Pterodactyl Panel
description: Panel de gestion de serveurs de jeux
published: true
date: 2026-06-19
---

# Pterodactyl Panel

Interface web de gestion de serveurs de jeux (Minecraft, CS2, Valheim, Rust…). Gère les instances, les mods, les backups et les utilisateurs.

## Installation

> Installer **Pterodactyl Panel avant Wings**. Wings se connecte au Panel pour récupérer sa configuration.

```bash
caleope install pterodactyl-panel --domain panel.monserveur.fr
```

## Accès

- **Interface** : `https://panel.monserveur.fr`
- **Login admin** : `admin`
- **Mot de passe** : généré à l'installation, affiché dans le terminal et dans `app-config/pterodactyl-panel/secrets.env`

## Ajouter un nœud Wings

Après installation du Panel :

1. **Admin → Nodes → Créer un nœud**
2. Renseigner le FQDN du serveur Wings
3. **Générer le token** de connexion
4. Installer Wings : `caleope install pterodactyl-wings --param NODE_FQDN=<ip-du-serveur>`

→ Voir [Pterodactyl Wings](/apps/pterodactyl-wings)

## Créer un serveur de jeu

1. **Admin → Servers → Créer**
2. Choisir l'**Egg** (type de serveur : Minecraft Java, Bedrock, CS2…)
3. Assigner à un nœud Wings et configurer les ressources (CPU, RAM, disque)
4. Le serveur démarre automatiquement

## Commandes utiles

```bash
caleope logs pterodactyl-panel
caleope backup pterodactyl-panel
caleope restart pterodactyl-panel
```
