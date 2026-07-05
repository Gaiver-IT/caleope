# Caleope — Export / Import d'applications (« submarine restore »)

Sauvegarder une app **et pouvoir la restaurer des mois plus tard**, même si :
- l'app a **disparu du store** (ou sa définition a changé),
- il n'y a **pas d'accès internet / de registre** (air-gap),
- Caleope a **évolué** entre-temps.

## Export

```bash
caleope export <app> [<dest.tar.gz>] [--no-images]
```

Produit une archive **auto-suffisante** :

```
payload/data.tar.gz            données (app-data/<app>)
payload/config.tar.gz          secrets (app-config/<app>)
payload/definition/installed   compose résolu + app.env (apps-installed/<app>)
payload/definition/store       définition d'origine (app.json, setup.sh, params.json)
payload/definition/runtime.json  état runtime
payload/images/*.tar           images Docker COMPLÈTES (couches) — restore hors-ligne
payload/export-manifest.json   version Caleope, app, images, date
```

> Les images sont exportées via **skopeo** (couches complètes) — fiable avec le store containerd, où
> `docker save` n'exporte que les manifests. `--no-images` produit une archive légère (re-pull au restore).

Depuis l'UI : bouton **EXPORT** sur la carte de l'app (affiche le chemin de l'archive sur le serveur).

## Import

```bash
caleope import <archive.tar.gz> [--legacy | --migrate]
```

- **`--legacy`** (défaut) : recrée l'app **à l'identique** (images figées) → marche toujours, même des années après.
- **`--migrate`** : si le store fournit un `migrate.sh` pour cette app, l'exécute pour porter les données vers le
  standard courant ; sinon **fallback legacy** automatique.

Depuis l'UI : carte **« Importer une app »** dans *Paramètres* (chemin de l'archive sur le serveur + mode).

L'import est **indépendant du store et d'internet** : `docker load` des images embarquées, restauration de la
définition + des données, puis `docker compose up`.

## Convention `migrate.sh` (mode `--migrate`)

Une app peut fournir `apps/<app>/migrate.sh` dans le store. `caleope import --migrate` le lance **avant** de
démarrer l'app, avec :

| Variable | Contenu |
|---|---|
| `CALEOPE_BASE_DIR` | racine Caleope (`/opt/gaiver-it/caleope`) |
| `CALEOPE_APP_ID` | id de l'app |
| `CALEOPE_FROM_VERSION` | version Caleope de l'archive |

Le script tourne dans `apps-installed/<app>/` (compose restauré). Exemple minimal :

```bash
#!/bin/bash
set -euo pipefail
# Ex: migration d'un schéma de données entre deux versions majeures d'une app.
# En cas d'échec (exit != 0), l'import retombe automatiquement en legacy.
echo "Migration ${CALEOPE_APP_ID} depuis Caleope ${CALEOPE_FROM_VERSION}…"
# … transformations sur ${CALEOPE_BASE_DIR}/app-data/${CALEOPE_APP_ID} …
```

Sans `migrate.sh`, `--migrate` se comporte comme `--legacy`.
