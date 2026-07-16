# offline-builder

Outil Go qui fabrique un **bundle Caleope offline** : binaires + store + images
Docker des apps choisies — sans démon Docker (pull via
[go-containerregistry](https://github.com/google/go-containerregistry)).

Le bundle produit sert :
- directement à une install air-gap : `install.sh --offline <bundle>` ;
- à fabriquer une **ISO offline** taillée juste : `OFFLINE_IMAGES_DIR=<bundle>/images ../build.sh`.

C'est le modèle « ISO sur mesure » : plutôt qu'une ISO ~40 Go tout-inclus,
l'utilisateur choisit ses apps sur un PC connecté et repart avec une image à jour.

## Build

```bash
cd iso/offline-builder
go build -o offline-builder .
```

## Usage

```bash
# Toutes les apps du canal stable, version v0.6.6
./offline-builder -version v0.6.6 -out ./bundle

# Sélection d'apps (air-gap taillé juste)
./offline-builder -apps jellyfin,nextcloud,immich -channel alpha -out ./bundle

# Réutiliser un store déjà cloné
./offline-builder -store /chemin/vers/caleope-store -out ./bundle
```

Flags : `-apps` (CSV, vide = toutes), `-version`, `-channel` (stable|alpha),
`-store` (chemin local, sinon clone auto), `-out`, `-arch` (défaut amd64).

## Layout produit

```
bundle/binaries/{caleoped,caleope,caleope-ui}
bundle/store.tar.gz
bundle/images/<image>.tar        # docker load-compatible
bundle/pack-info.json            # version, canal, apps, images
```

> Les images sont découvertes en parsant les `image:` des `docker-compose.yml`
> du store (placeholders Go-template et images `caleope-*` locales exclus).
