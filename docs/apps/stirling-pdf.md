---
title: Stirling PDF
description: Suite d'outils PDF self-hosted
published: true
date: 2026-06-28
---

# Stirling PDF

Suite complète d'outils PDF self-hosted. Fusion, division, compression, conversion, OCR, rotation, signature, extraction de pages et bien plus — sans envoyer de documents à des services tiers.

## Installation

```bash
caleope install stirling-pdf --domain pdf.monserveur.fr
```

## Configuration

| Paramètre | Description | Défaut |
|-----------|-------------|--------|
| `STIRLING_PDF_PORT_WEB` | Port web | `8088` |

## Accès

```
https://pdf.monserveur.fr/
```

Aucun compte requis — accès direct à tous les outils.

## Outils disponibles

- Fusion et division de PDF
- Compression et optimisation
- Conversion (PDF → Word, images → PDF, etc.)
- OCR (reconnaissance de texte)
- Rotation, recadrage, extraction de pages
- Suppression de métadonnées
- Signature et watermark

## Commandes utiles

```bash
caleope logs stirling-pdf       # Logs
caleope restart stirling-pdf    # Redémarrer
```
