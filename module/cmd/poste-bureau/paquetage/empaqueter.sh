#!/usr/bin/env bash
# Fabrique ce qui se double-clique : un bundle .app pour macOS, un raccourci
# .desktop pour les bureaux Linux.
#
# POURQUOI un bundle : sur macOS, un binaire nu se lance depuis un terminal, pas
# depuis le Finder — et « installer un exécutable et se connecter » ne doit pas
# commencer par ouvrir un terminal. Le bundle n'est qu'une arborescence de
# fichiers, aucun outil Apple n'est nécessaire pour la construire.
set -euo pipefail
DEST="${1:-dist}"
VERSION="${2:-0.0.0}"

# ── macOS : Poste.app ───────────────────────────────────────────────────────
for arch in arm64 amd64; do
    BIN="${DEST}/poste-bureau-macos-${arch}"
    [ -f "${BIN}" ] || { echo "  (pas de binaire ${arch}, on saute)"; continue; }
    APP="${DEST}/Poste-macos-${arch}.app"
    rm -rf "${APP}"
    mkdir -p "${APP}/Contents/MacOS" "${APP}/Contents/Resources"
    cp "${BIN}" "${APP}/Contents/MacOS/Poste"
    chmod +x "${APP}/Contents/MacOS/Poste"
    cat > "${APP}/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>CFBundleName</key><string>Poste</string>
  <key>CFBundleDisplayName</key><string>Poste</string>
  <key>CFBundleIdentifier</key><string>fr.gaiver-it.caleope.poste</string>
  <key>CFBundleExecutable</key><string>Poste</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>${VERSION}</string>
  <key>CFBundleVersion</key><string>${VERSION}</string>
  <!-- Agent : l'application n'a pas d'icône dans le Dock ni de menu — son
       interface est la fenêtre qu'elle ouvre. Sans ça, macOS afficherait une
       application vide en plus de la fenêtre. -->
  <key>LSUIElement</key><true/>
  <key>NSHighResolutionCapable</key><true/>
</dict></plist>
PLIST
    # ── Signature du BUNDLE, pas seulement du binaire ───────────────────────
    # L'éditeur de liens Go signe déjà l'exécutable (adhoc). Mais on vient de
    # l'envelopper : la signature ne couvre pas Info.plist ni la structure, et
    # macOS refuse alors l'ouverture en annonçant une application ENDOMMAGÉE,
    # sans proposer de passer outre. Mesuré en v0.9.15 :
    #   spctl → « code has no resources but signature indicates they must be present »
    # Re-signer l'ensemble crée Contents/_CodeSignature/CodeResources.
    if command -v codesign >/dev/null 2>&1; then
        codesign --force --deep --sign - "${APP}"
        codesign --verify --deep --strict "${APP}"
    else
        echo "  ✗ codesign absent : ce bundle serait refusé par macOS." >&2
        echo "    Construis les cibles darwin sur un Mac." >&2
        exit 1
    fi
    ( cd "${DEST}" && zip -qry "Poste-macos-${arch}.app.zip" "Poste-macos-${arch}.app" )
    rm -rf "${APP}"
    echo "  ✓ Poste-macos-${arch}.app.zip"
done

# ── Linux : raccourci de bureau ─────────────────────────────────────────────
# Écrit systématiquement : c'est un simple fichier texte qui décrit OÙ le binaire
# sera installé, pas un empaquetage de celui-ci. Le conditionner à la présence du
# binaire le faisait disparaître dès que les deux cibles étaient construites par
# des machines différentes — et la publication échouait sur un fichier manquant.
if true; then
    cat > "${DEST}/poste.desktop" <<'DESK'
[Desktop Entry]
Type=Application
Name=Poste
Comment=Retrouver ses logiciels et ses dossiers sur cette machine
Exec=/usr/local/bin/poste-bureau
Icon=preferences-desktop
Terminal=false
Categories=System;Settings;
DESK
    echo "  ✓ poste.desktop"
fi
