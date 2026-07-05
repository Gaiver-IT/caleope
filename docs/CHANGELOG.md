# Caleope — Journal des versions

## v0.6.10 (2026-07-05)
- **UI** : sélecteur de **mode registre** (fallback/mirror/upstream) dans la carte Registre.
- **UI** : carte **« Importer une app »** (Paramètres) — recrée une app depuis une archive d'export.

## v0.6.9 (2026-07-05)
- **Fix** : le champ **« Stockage » (NAS)** n'apparaissait pas à l'install pour les apps sans `params.json`
  (ex: immich). Le modal fusionne désormais les params du store avec les params intégrés de l'UI.

## v0.6.8 (2026-07-04)
- **`caleope export` / `import`** — archive **auto-suffisante** (« submarine restore ») : données + config +
  définition figée (compose, app.json, setup.sh) + **images Docker complètes** + manifest. Restaurable des mois
  plus tard, **même hors-ligne** ou si l'app a quitté le store. Voir [EXPORT-IMPORT.md](EXPORT-IMPORT.md).
- **Mode fallback registre** (`CALEOPE_REGISTRY_MODE`) : `fallback` (upstream puis miroir), `mirror`
  (confiance Caleope), `upstream` (origine).

## v0.6.7 (2026-07-03)
- **Fix routing NPM (404)** : en mode proxy `npm`/`standalone`, les routers des apps passent sur l'entrypoint
  `web` (au lieu de `websecure`) — sinon 404 public. ⚠️ Réinstaller les apps existantes pour appliquer.
- **Fix UI** : les boutons d'action des cartes d'app ne « fuient » plus au survol (taille fixe).

## v0.6.1 → v0.6.6
Licence Ed25519, réorg UI, sessions persistantes, `install --alpha`, registre miroir, gestion des dépôts.
Voir l'historique git et `module/RELEASING.md`.

---

## Process de release
Bumper `module/pkg/version/version.go` + push `main` → la CI publie la release stable. Ne jamais faire de
`gh release create` manuel. Push `alpha` → rolling `alpha-latest`.
