---
title: Prometheus + Grafana
description: Supervision graphique du serveur et des applications
published: true
date: 2026-05-25
---

# Prometheus + Grafana

Stack de supervision complète : métriques en temps réel et historiques pour le serveur et toutes les applications Caleope.

## Installation

```bash
caleope install prometheus-grafana --domain metrics.monserveur.fr
```

Les identifiants Grafana s'affichent à la fin de l'installation.

## Accès

```
https://metrics.monserveur.fr    ← Grafana (dashboard)
```

## Dashboard préconfiguré : Caleope Overview

Le dashboard **Caleope Overview** est installé automatiquement avec :

| Panneau | Contenu |
|---------|---------|
| **Apps actives** | Nombre d'applications en cours d'exécution |
| **RAM système** | Utilisation mémoire totale (jauge) |
| **Disque système** | Espace utilisé/disponible (jauge) |
| **CPU par app** | Courbe temporelle par application |
| **RAM par app** | Courbe temporelle par application |
| **État des apps** | Tableau avec statut de chaque app |

## Métriques disponibles

Prometheus scrape les métriques depuis `caleoped` (port 9100) :

- CPU et RAM par container Docker
- Espace disque du serveur
- État (running/stopped) de chaque application
- Historique sur 15 jours par défaut

## Ajouter ses propres dashboards

Grafana → Dashboards → New → Import  
→ Utiliser un ID depuis [grafana.com/dashboards](https://grafana.com/grafana/dashboards/)

Dashboards utiles :
- **1860** — Node Exporter Full (métriques système détaillées)
- **893** — Docker monitoring

## Changer le mot de passe Grafana

Grafana → Profile → Change Password

Ou via CLI :
```bash
docker exec grafana grafana-cli admin reset-admin-password NOUVEAU_MOT_DE_PASSE
```

## Récupérer les identifiants

```bash
cat /opt/gaiver-it/caleope/app-config/prometheus-grafana/secrets.env
```

## Sauvegardes

```bash
caleope backup prometheus-grafana    # sauvegarde dashboards et config
```
