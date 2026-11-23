#!/usr/bin/env bash
# Build a portable Windows dist of SubSandwicher from Linux (or any shell).
# Output: dist/SubSandwicher-win-x64/ and dist/SubSandwicher-win-x64.zip
#
# Copy that folder anywhere on Windows and run SubSandwicher.exe.
#
# Layout produced:
#   SubSandwicher.exe       (electron shell)
#   resources/app/           (the app: package.json + src/ + third_party/bin)
#   resources/app/third_party/bin/windows/{ffmpeg,ffprobe,mpv}.exe
#
# IDEMPOTENT: staging happens in a fresh temp dir each run and the output
# folder/zip are always replaced. Agents should call this repeatedly without
# worrying about leftovers.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
EUI="$ROOT/electron-ui"
OUT="$ROOT/dist/SubSandwicher-win-x64"
CACHE="$ROOT/.cache"
ELECTRON_VERSION="44.3.0"
ELECTRON_ZIP="electron-v${ELECTRON_VERSION}-win32-x64.zip"

STAGE="$(mktemp -d -t subsandwicher-build-XXXXXX)"
trap 'rm -rf "$STAGE"' EXIT

mkdir -p "$(dirname "$OUT")"

echo "==> building subsandwicher-cli for windows"
(
  cd "$ROOT"
  GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-s -w" \
    -o "$STAGE/resources/app/subsandwicher-cli.exe" .   # lives beside third_party/ so sibling resolution works
)

echo "==> staging gui"
mkdir -p "$STAGE/resources/app"
cp -r "$EUI/src" "$STAGE/resources/app/src"
cp "$EUI/package.json" "$STAGE/resources/app/package.json"

# Build number = commit count, so the UI reports a version that increments with
# every commit and matches the source revision. A dirty tree is marked so a
# package built from uncommitted code cannot be mistaken for a clean release.
COMMITS="$(git -C "$ROOT" rev-list --count HEAD 2>/dev/null || echo 0)"
COMMIT="$(git -C "$ROOT" rev-parse --short HEAD 2>/dev/null || echo unknown)"
BUILD_VERSION="0.1.${COMMITS}"
if [ -n "$(git -C "$ROOT" status --porcelain --untracked-files=no 2>/dev/null)" ]; then
  BUILD_VERSION="${BUILD_VERSION}-dirty"
fi
BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
python3 - "$STAGE/resources/app/package.json" "$BUILD_VERSION" "$COMMITS" "$COMMIT" "$BUILD_TIME" <<'PY'
import json, sys
path, version, number, commit, built = sys.argv[1:6]
with open(path) as f:
    pkg = json.load(f)
pkg["version"] = version
pkg["buildNumber"] = int(number)
pkg["buildCommit"] = commit
pkg["buildTime"] = built
with open(path, "w") as f:
    json.dump(pkg, f, indent=2)
    f.write("\n")
print(f"    version {version} ({commit}) built {built}")
PY

echo "==> vendoring media binaries"
mkdir -p "$STAGE/resources/app/third_party/bin/windows"
bash "$ROOT/tools/fetch-windows-bins.sh"   # no-op when already present
cp "$ROOT/third_party/bin/windows/"*.exe "$STAGE/resources/app/third_party/bin/windows/"

echo "==> fetching electron ${ELECTRON_VERSION}"
mkdir -p "$CACHE"
if [ ! -f "$CACHE/$ELECTRON_ZIP" ]; then
  curl -fSL --retry 3 -o "$CACHE/$ELECTRON_ZIP" \
    "https://github.com/electron/electron/releases/download/v${ELECTRON_VERSION}/${ELECTRON_ZIP}"
fi
echo "==> extracting electron"
EX="$STAGE/ex"
rm -rf "$EX"
mkdir -p "$EX"
python3 -c "import zipfile; zipfile.ZipFile('$CACHE/$ELECTRON_ZIP').extractall('$EX')"
# Some electron zips unpack flat; others nest everything under a dir.
if [ -d "$EX/electron" ]; then EX="$EX/electron"; fi
cp -r "$EX/." "$STAGE/"
rm -rf "$STAGE/ex" "$STAGE/electron"   # remove loose extraction dirs
mv "$STAGE/electron.exe" "$STAGE/SubSandwicher.exe"

echo "==> branding metadata (go-winres: FileDescription etc. => SubSandwicher)"
if [ ! -x "$HOME/go/bin/go-winres" ]; then
  go install github.com/tc-hib/go-winres@latest
fi
"$HOME/go/bin/go-winres" patch --in "$ROOT/tools/app-winres.json" --no-backup "$STAGE/SubSandwicher.exe"

if [ -e "$OUT" ]; then rm -rf "$OUT"; fi
cp -r "$STAGE" "$OUT"
find "$OUT" -name ".*" -type d ! -path "*/locales/*" -exec rm -rf {} + 2>/dev/null || true

# (re)write the portable zip — make_archive replaces an existing file
python3 - <<PY
import shutil, os
os.chdir("$ROOT/dist")
if os.path.exists("SubSandwicher-win-x64.zip"):
    os.remove("SubSandwicher-win-x64.zip")
shutil.make_archive("SubSandwicher-win-x64", "zip", ".", "SubSandwicher-win-x64")
PY
echo "==> done: $OUT (+ zip)"
