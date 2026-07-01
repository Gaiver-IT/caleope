# Process de release Caleope

## Source de vérité : `pkg/version/version.go`

Le CI GitHub Actions (`.github/workflows/release.yml`) est le **process officiel**.
Il se déclenche sur **push vers `main`** (canal stable) ou **`alpha`** (pré-release).

### Release STABLE
1. Bumper la constante dans `module/pkg/version/version.go` :
   ```go
   Version = "v0.6.4"   // la version qu'on publie
   ```
2. Commit + `git push origin main`.
3. Le CI lit `version.go`, build les 3 binaires linux/amd64, et crée/ met à jour
   la release `vX.Y.Z` avec les assets. Rien d'autre à faire.

> ⚠️ **Toujours bumper `version.go` avant de pousser `main`.** Le CI publie la
> version qu'il y lit. Si on oublie, il ré-écrase la release existante (ce qui
> est arrivé avec v0.6.0 tant que la constante était restée à v0.6.0).

### Release ALPHA
- `git push origin alpha` → le CI met à jour la rolling `alpha-latest`
  (prerelease, titre `Caleope alpha (<commit>)`). Comparaison par commit côté upgrade.

## Ne PAS mélanger avec des releases manuelles
`release.sh` (interactif) et les `gh release create` à la main **court-circuitent**
le CI et désalignent `version.go`. À réserver aux cas exceptionnels (CI cassé).
En temps normal : **bump `version.go` + push `main`**.

## Vérifier une release
```bash
gh release list --repo gaiver-it/caleope
gh run list --repo gaiver-it/caleope --workflow release.yml   # état du CI
```

## Côté serveurs
`caleope upgrade` (canal stable → dernière release non-prerelease ;
`--alpha` → dernière prerelease `alpha-latest`).
