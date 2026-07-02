#!/usr/bin/env bash
#
# iso/build.sh — Construit l'ISO d'installation Caleope (Debian 13 Trixie).
#
# Prend une netinst Debian 13, y injecte le preseed (install auto) + le payload
# Caleope (binaires + install.sh + cache store), et repack une ISO bootable
# BIOS+UEFI. Résultat : une clé/ISO qui installe Caleope sans interaction.
#
# PRÉREQUIS (hôte Linux) : xorriso, wget, gzip, cpio, fdisk (paquet: xorriso).
#   sudo apt install -y xorriso wget cpio gzip
#
# USAGE :
#   ./build.sh                       # ISO online (apps pull depuis le registre miroir)
#   CALEOPE_VERSION=v0.6.3 ./build.sh
#
# NOTE : pour l'ISO OFFLINE (images Docker bundlées), voir l'outil builder
#        séparé (iso/offline-builder/) — pas ce script.

set -euo pipefail

# ── Paramètres ──────────────────────────────────────────────────────────────
DEBIAN_VER="${DEBIAN_VER:-13.0.0}"
ARCH="amd64"
CALEOPE_VERSION="${CALEOPE_VERSION:-v0.6.3}"
REPO="Gaiver-IT/caleope"

HERE="$(cd "$(dirname "$0")" && pwd)"
WORK="${WORK:-$HERE/build}"
CACHE="${CACHE:-$HERE/cache}"
NETINST="$CACHE/debian-${DEBIAN_VER}-${ARCH}-netinst.iso"
NETINST_URL="https://cdimage.debian.org/debian-cd/${DEBIAN_VER}/${ARCH}/iso-cd/debian-${DEBIAN_VER}-${ARCH}-netinst.iso"
EXTRACT="$WORK/iso-extract"
OUT="${OUT:-$WORK/caleope-installer-${CALEOPE_VERSION}.iso}"

need() { command -v "$1" >/dev/null 2>&1 || { echo "❌ manquant: $1 (apt install $2)"; exit 1; }; }
need xorriso xorriso; need wget wget; need cpio cpio; need gzip gzip

mkdir -p "$WORK" "$CACHE"

# ── 1. Récupérer la netinst Debian ──────────────────────────────────────────
if [[ ! -f "$NETINST" ]]; then
  echo "⬇️  Téléchargement Debian ${DEBIAN_VER} netinst..."
  wget -O "$NETINST" "$NETINST_URL"
fi

# ── 2. Extraire l'ISO (xorriso -osirrox, pas besoin de root/mount) ──────────
echo "📂 Extraction de l'ISO..."
rm -rf "$EXTRACT"; mkdir -p "$EXTRACT"
xorriso -osirrox on -indev "$NETINST" -extract / "$EXTRACT" 2>/dev/null
chmod -R u+w "$EXTRACT"

# ── 3. Injecter le preseed dans l'initrd (d-i le charge automatiquement) ────
echo "💉 Injection du preseed dans l'initrd..."
for INITRD in "$EXTRACT/install.amd/initrd.gz" "$EXTRACT/install.amd/gtk/initrd.gz"; do
  [[ -f "$INITRD" ]] || continue
  TMP="$(mktemp -d)"
  ( cd "$TMP" && gzip -d < "$INITRD" > initrd.cpio \
    && cp "$HERE/preseed.cfg" preseed.cfg \
    && echo preseed.cfg | cpio -H newc -o -A -F initrd.cpio 2>/dev/null \
    && gzip -9 < initrd.cpio > "$INITRD" )
  rm -rf "$TMP"
done

# ── 4. Copier le payload Caleope (binaires + install.sh + cache store) ──────
echo "📦 Assemblage du payload Caleope (${CALEOPE_VERSION})..."
PAYLOAD="$EXTRACT/caleope"; mkdir -p "$PAYLOAD/bin"
BASE="https://github.com/${REPO}/releases/download/${CALEOPE_VERSION}"
for b in caleoped caleope caleope-ui; do
  wget -qO "$PAYLOAD/bin/$b" "$BASE/${b}-linux-amd64"
done
# install.sh depuis la branche correspondante (ou embarqué)
wget -qO "$PAYLOAD/install.sh" "https://raw.githubusercontent.com/${REPO}/main/install.sh"
# Cache du store (clone shallow — pour l'offline des définitions d'apps)
if command -v git >/dev/null 2>&1; then
  git clone --depth 1 "https://github.com/gaiver-it/caleope-store.git" "$PAYLOAD/store" 2>/dev/null || \
    echo "⚠️  clone store échoué (le cache store sera sync au 1er boot)"
fi

# ── 5. Patcher les bootloaders pour l'install auto (BIOS + UEFI) ────────────
echo "⚙️  Patch des bootloaders (auto-install)..."
BOOT_ARGS="auto=true priority=critical"
# isolinux (BIOS)
if [[ -f "$EXTRACT/isolinux/txt.cfg" ]]; then
  sed -i "s#\(append.*vmlinuz.*\)#\1 ${BOOT_ARGS}#" "$EXTRACT/isolinux/txt.cfg" || true
fi
# grub (UEFI) — booter direct sur l'install auto
if [[ -f "$EXTRACT/boot/grub/grub.cfg" ]]; then
  sed -i "s#\(linux.*vmlinuz.*\)#\1 ${BOOT_ARGS}#" "$EXTRACT/boot/grub/grub.cfg" || true
  sed -i 's/set default=.*/set default=0/; s/set timeout=.*/set timeout=3/' "$EXTRACT/boot/grub/grub.cfg" || true
fi

# ── 6. Recalculer md5 + repack ISO hybride (BIOS+UEFI) ──────────────────────
echo "🔧 Repack de l'ISO..."
( cd "$EXTRACT" && find . -type f ! -name md5sum.txt -exec md5sum {} \; > md5sum.txt 2>/dev/null || true )

# Récupère les infos de boot El Torito depuis la netinst d'origine
xorriso -indev "$NETINST" -report_el_torito as_mkisofs > "$WORK/eltorito.txt" 2>/dev/null || true

xorriso -as mkisofs \
  -r -V "CALEOPE_${CALEOPE_VERSION}" \
  -o "$OUT" \
  -J -joliet-long \
  -isohybrid-mbr /usr/lib/ISOLINUX/isohdpfx.bin \
  -c isolinux/boot.cat \
  -b isolinux/isolinux.bin -no-emul-boot -boot-load-size 4 -boot-info-table \
  -eltorito-alt-boot -e boot/grub/efi.img -no-emul-boot -isohybrid-gpt-basdat \
  "$EXTRACT" 2>/dev/null

echo ""
echo "✅ ISO construite : $OUT"
ls -lh "$OUT"
echo "   Grave-la (dd/balena) ou boote-la en VM. Elle installe Caleope sans interaction."
