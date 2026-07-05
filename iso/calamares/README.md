# Caleope — Installeur graphique (Calamares)

Objectif : un installeur **graphique, guidé façon Windows 11**, aux couleurs de
Caleope, en alternative au mode netinst/preseed (`iso/build.sh`).

## Deux ISOs, deux publics
| ISO | Techno | Public |
|---|---|---|
| `iso/build.sh` (netinst+preseed) | Debian d-i auto | serveurs headless, install silencieuse, **mode legacy/texte** |
| `iso/calamares/` (live + Calamares) | **live ISO graphique** | postes/écran, install **guidée graphique** brandée |

## Comment ça marche (live + Calamares)
1. **Live ISO** : un système Debian *live* (squashfs) démarre en RAM avec un
   serveur X minimal + un WM léger (openbox) et lance **Calamares** au boot.
2. **Calamares** déroule un parcours guidé et **thémé Caleope** (dark + violet) :
   `welcome → locale → keyboard → partition → users → install → caleope → finished`.
3. Le module `install` déploie le système Debian de base ; puis notre étape
   **`caleope`** (shellprocess) lance `install.sh --iso` dans le système cible
   (Docker + Caleope). Au reboot : le **wizard de 1er démarrage** (console **ou**
   web `http://<IP>:8766`) finalise domaine/proxy/mots de passe.

## Auto-détection GUI vs legacy
Le menu de boot (isolinux/grub) propose :
- **Installation graphique (Calamares)** — défaut si écran/carte graphique OK ;
- **Installation legacy (texte)** — bascule vers le flux netinst pour vieux matériel / headless.
(La détection fine « écran présent → GUI, sinon TUI » se fait via un petit script
de boot qui teste la présence d'un GPU/écran ; à défaut, l'utilisateur choisit.)

## Design (façon Windows 11)
- `branding/caleope/branding.desc` — produit, couleurs, slideshow.
- `branding/caleope/stylesheet.qss` — thème Caleope (fond sombre, accent violet `#7c6cff`, coins arrondis).
- `branding/caleope/show.qml` — slideshow pendant l'install (présentation de Caleope).

## Build
```bash
cd iso/calamares
sudo ./build-live-iso.sh        # nécessite live-build (lb), debootstrap, xorriso
# → build/caleope-live-<version>.iso
```

## Statut
Itération 1 — scaffold complet (branding + séquence + build). À itérer :
tuning des modules partition/users pour l'appliance, test de boot réel en VM,
et le script d'auto-détection GPU du menu de boot.
