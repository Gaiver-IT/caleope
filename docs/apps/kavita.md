---
title: Kavita
description: Serveur de lecture pour mangas, comics, ebooks et BD
published: true
date: 2026-06-28
---

# Kavita

Serveur de lecture auto-hébergé pour mangas, comics, ebooks et BD. Interface web moderne avec progression de lecture, métadonnées automatiques et application mobile.

## Installation

```bash
caleope install kavita --domain kavita.monserveur.fr
```

## Configuration

| Paramètre | Description | Défaut |
|-----------|-------------|--------|
| `KAVITA_PORT_WEB` | Port web | `5001` |

## Accès

```
https://kavita.monserveur.fr/
```

Un wizard de premier accès te guide pour créer le compte admin et ajouter les bibliothèques.

## Commandes utiles

```bash
caleope logs kavita       # Logs
caleope restart kavita    # Redémarrer
caleope backup kavita     # Sauvegarder
```

## Structure des données

```
app-data/kavita/
├── config/    ← configuration et base de données
└── data/      ← bibliothèques de contenu
```

## Notes

- Ajouter les bibliothèques depuis **Server Settings → Libraries** en pointant vers les dossiers de contenu dans le container.
