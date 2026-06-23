---
title: GLPI
description: Gestion de parc informatique et helpdesk
published: true
date: 2026-06-19
---

# GLPI

Solution ITSM open-source : inventaire du parc informatique, gestion des tickets, helpdesk, gestion des contrats et fournisseurs.

## Installation

```bash
caleope install glpi --domain itsm.monserveur.fr
```

## Accès

- **Interface** : `https://itsm.monserveur.fr`
- **Login** : `glpi`
- **Mot de passe** : généré à l'installation, affiché dans le terminal et dans `app-config/glpi/secrets.env`

> Changer le mot de passe admin depuis **Administration → Utilisateurs** après le premier accès.

## Fonctionnalités

| Module | Description |
|--------|-------------|
| **Assets** | Inventaire ordinateurs, serveurs, réseau |
| **Helpdesk** | Tickets utilisateurs, SLA, escalade |
| **Contrats** | Licences, garanties, fournisseurs |
| **CMDB** | Gestion des configurations |
| **Plugins** | +200 plugins disponibles |

## Import automatique (agent GLPI)

Installer **glpi-agent** sur les machines à inventorier :

```bash
# Sur Debian/Ubuntu
wget https://github.com/glpi-project/glpi-agent/releases/latest/...
dpkg -i glpi-agent_*.deb
glpi-agent --server=https://itsm.monserveur.fr
```

## Commandes utiles

```bash
caleope logs glpi
caleope backup glpi
caleope restart glpi
```
