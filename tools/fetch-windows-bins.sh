#!/usr/bin/env bash
# Fetch pinned Windows binaries (ffmpeg suite + mpv) into third_party/bin/windows
# so the packaged app has everything it needs locally.
#
# Versions are PINNED (exact release assets). To upgrade, verify the new
# version's asset URLs and update the constants below.
#
# Requires curl + a 7z extractor: prefers `7z`/`7za`/`7zr` on PATH,
# falls back to `python3 -m py7zr`.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DEST="$ROOT/third_party/bin"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

# --- pinned versions ---------------------------------------------------
FFMPEG_TAG="autobuild-2026-09-11-13-20"
FFMPEG_FILE="ffmpeg-N-126497-g5b614efc7e-win64-gpl.zip"
FFMPEG_RELEASE_NOTE="BtbN has no 9.1 release yet; this is 9.x master (newer than 9.1), pinned by asset name."
FFMPEG_URL="https://github.com/BtbN/FFmpeg-Builds/releases/download/${FFMPEG_TAG}/${FFMPEG_FILE}"

MPV_TAG="20260903"
MPV_FILE="mpv-x86_64-20260903-git-69e63f425a.7z"
MPV_URL="https://github.com/shinchiro/mpv-winbuild-cmake/releases/download/${MPV_TAG}/${MPV_FILE}"
# -----------------------------------------------------------------------

extract() { # ext, file, destdir
	local ext="$1" file="$2" dir="$3"
	case "$ext" in
		zip) unzip -q "$file" -d "$dir" ;;
		7z)
			if command -v 7z >/dev/null; then 7z x -y -o"$dir" "$file" >/dev/null
			elif command -v 7za >/dev/null; then 7za x -y -o"$dir" "$file" >/dev/null
			elif command -v 7zr >/dev/null; then 7zr x -y -o"$dir" "$file" >/dev/null
			else
				# fallback: 7zz binary from the 7zip-bin-full npm package
				Z7=""
				if node -e "require('7zip-bin-full')" 2>/dev/null; then
					Z7="$(node -e "console.log(require('7zip-bin-full').path7zzs||require('7zip-bin-full').path7z)")"
				elif npm --prefix "$ROOT/electron-ui" install 7zip-bin-full@26.3.1 >/dev/null 2>&1; then
					Z7="$(NEW_SHIM=1 node -e "module.paths.unshift('$ROOT/electron-ui/node_modules'); console.log(require('7zip-bin-full').path7zzs||require('7zip-bin-full').path7z)")"
				fi
				if [ -n "$Z7" ] && [ -x "$Z7" ]; then
					"$Z7" x -y -o"$dir" "$file" >/dev/null
				else
					python3 -m py7zr x "$file" "$dir"
				fi
			fi
			;;
	esac
}

fetch() { # url
	local url="$1" out
	out="$TMP/$(basename "$url")"
	if [ -f "$out" ]; then echo "cached: $out"; return; fi
	echo "downloading: $url"
	curl -fSL --retry 3 -o "$out" "$url"
}

mkdir -p "$DEST"

# ffmpeg (provides ffmpeg + ffprobe)
if [ ! -x "$DEST/windows/ffmpeg.exe" ]; then
	echo "fetching ffmpeg ${FFMPEG_RELEASE_NOTE:-($FFMPEG_FILE)}..."
	fetch "$FFMPEG_URL" >/dev/null
	extract zip "$TMP/$FFMPEG_FILE" "$TMP/ffmpeg" >/dev/null
	bindir="$(find "$TMP/ffmpeg" -maxdepth 2 -type d -name bin | head -1)"
	mkdir -p "$DEST/windows"
	cp -v "$bindir"/ffmpeg.exe "$bindir"/ffprobe.exe "$DEST/windows/"
fi

# mpv (includes its own ffmpeg/libass; used for preview playback)
if [ ! -x "$DEST/windows/mpv.exe" ]; then
	echo "fetching mpv ($MPV_FILE)..."
	fetch "$MPV_URL" >/dev/null
	extract 7z "$TMP/$MPV_FILE" "$TMP/mpv" >/dev/null
	mkdir -p "$DEST/windows"
	cp -v "$TMP/mpv"/mpv.exe "$DEST/windows/"
fi

echo "done. contents:"
ls -la "$DEST/windows"
