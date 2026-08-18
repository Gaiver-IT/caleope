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
    ( cd "${DEST}" && zip -qry "Poste-macos-${arch}.app.zip" "Poste-macos-${arch}.app" )
    rm -rf "${APP}"
    echo "  ✓ Poste-macos-${arch}.app.zip"
done

# ── Linux : raccourci de bureau ─────────────────────────────────────────────
if [ -f "${DEST}/poste-bureau-linux-amd64" ]; then
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
