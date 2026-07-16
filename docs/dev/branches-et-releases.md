---
title: Branches et releases
description: Modèle de branches Caleope, pièges connus, et checklist avant de builder une ISO
published: true
date: 2026-07-16
---

# Branches et releases

## Le modèle

| Branche | Rôle | Ce que la CI en fait (`.github/workflows/release.yml`) |
|---|---|---|
| `main` | canal **stable** | release taggée à partir du tag git |
| `alpha` | canal **alpha** | release `alpha-latest`, version `alpha-<sha court>` |

La CI se déclenche sur **push vers `main` ou `alpha`** — pas sur les tags. Un push
sur l'une de ces deux branches **publie une release**. Ce n'est pas anodin : il n'y a
pas d'étape manuelle entre le `git push` et l'artefact que les gens téléchargent.

Côté store (dépôt `caleope-store`), même découpage : `main` = stable, `alpha` = staging.
Une app se développe sur `alpha` puis est promue sur `main`.

## ⚠️ Deux pièges qui coûtent cher

### 1. `iso/` n'existe QUE sur `main`

Le dossier `iso/` (dont `preseed.cfg` et `build.sh`) et le `install.sh` racine ne sont
**pas** sur `alpha`. Un `git ls-tree alpha` ne les trouve pas — ce qui ne veut pas dire
qu'ils sont perdus, seulement qu'ils vivent ailleurs.

**Avant tout build d'ISO :**
```bash
git fetch origin
git diff --stat origin/main -- iso/ install.sh   # doit être VIDE
```

### 2. `alpha` ne se resynchronise pas toute seule

`alpha` est une branche de longue durée, pas une branche jetable. Si on lui empile des
features sans jamais y remonter `main`, elle **dérive** — et comme un push sur `alpha`
publie `alpha-latest`, on publie alors une **régression** : les utilisateurs du canal
alpha reçoivent moins récent que le stable, tout en croyant tester plus récent.

*Incident 16/07 : `alpha` avait forké le 30/06 (base v0.6.0) et pris 5 commits de
features. Pendant ce temps `main` avançait de 77 commits (v0.7.1 → v0.7.7). La release
`alpha-latest` publiée perdait donc, entre autres, l'activation de licence résiliente
(v0.7.7) — ce qui a fait passer des heures à soupçonner une clé publique corrompue,
alors que le binaire testé était simplement d'avant le correctif.*

**Réflexe, à chaque fois qu'on travaille sur `alpha` :**
```bash
git rev-list --left-right --count origin/main...origin/alpha
#            ^ main seul      ^ alpha seul
# Si "main seul" n'est pas 0 → remonter main dans alpha AVANT de pousser :
git checkout alpha && git merge origin/main
```

Un `git merge origin/main` (pas un rebase, pas un force-push) : `alpha` est publique,
son historique ne se réécrit pas.

## Checklist avant de pousser sur `alpha` ou `main`

1. `git rev-list --left-right --count origin/main...origin/alpha` → « main seul » = 0
2. `go build ./... && go vet ./... && go test ./...` → verts
3. Si l'UI est touchée : vérifier que les features sont **dans le binaire**, pas
   seulement dans les sources — `make build` (pas `go build`, qui écrit ailleurs), puis
   `strings build/caleope-ui | grep -c loadShares`
4. Se rappeler que le push **publie**. Pas de « je pousse pour voir ».

> **Le `strings` de `.10` ment.** Il a rendu des faux `0` sur des chaînes réellement
> présentes (vérifié : le `strings` du Mac les trouve). Ne jamais conclure « absent »
> sur la foi d'un `strings` exécuté sur `.10`.
