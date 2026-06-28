---
title: Scrutiny
description: Monitoring de santé des disques durs via S.M.A.R.T.
published: true
date: 2026-06-28
---

# Scrutiny

Monitoring de santé des disques durs via S.M.A.R.T. Collecte les données SMART de tous les disques, affiche leur état de santé, historique et envoie des alertes en cas de problème détecté.

## Installation

```bash
caleope install scrutiny --domain scrutiny.monserveur.fr
```

## Configuration

| Paramètre | Description | Défaut |
|-----------|-------------|--------|
| `SCRUTINY_PORT` | Port web | `8086` |

## Accès

```
https://scrutiny.monserveur.fr/
```

## Commandes utiles

```bash
caleope logs scrutiny       # Logs
caleope restart scrutiny    # Redémarrer
```

## Notes

- Scrutiny nécessite un accès privilégié aux disques pour lire les données SMART — le container est configuré automatiquement avec les permissions nécessaires.
- Les disques sont détectés automatiquement au démarrage.
