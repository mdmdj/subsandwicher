# Windows release packaging

`tools/build-windows.sh` — single entrypoint, runs from Linux (or any shell).
Agents call it repeatedly; it is **idempotent** (never needs a manual rm):
staging happens in a mktemp scratch dir with a trap, the output dir is
replaced wholesale, and the zip is deleted-then-recreated.

```
dist/SubSandwicher-win-x64/
  SubSandwicher.exe                            # electron shell
  resources/app/subsandwicher-cli.exe          # Go core (cross-built)
  resources/app/src/...                        # electron app (main/preload/renderer)
  resources/app/third_party/bin/windows/       # ffmpeg, ffprobe, mpv
dist/SubSandwicher-win-x64.zip                 # portable, no installer
```

## What each tool does

| script | responsibility |
|---|---|
| `tools/fetch-windows-bins.sh` | populate `third_party/bin/windows/` from pinned asset URLs (skips work when already present) |
| `tools/build-windows.sh` | cross-build CLI (GOOS=windows), stage electron, vendor bins, produce dist/ + zip |
| `electron-ui/*` | the app source; `npm install` in electron-ui/ for dev runs |

## Binary resolution order (in the packaged app)

`src/bins.js` searches, in order:

1. env overrides `SUBS_BIN_FFMPEG` / `SUBS_BIN_FFPROBE` / `SUBS_BIN_MPV` /
   `SUBS_BIN_CLI` (dev/testing convenience)
2. `resources/app/` and `resources/app/third_party/bin/windows/` (next to the
   app source in the packed layout)
3. `<electron exe dir>` and repo-adjacent `third_party/bin/windows`
4. bare names on `PATH` — last resort; if you see this in the log
   (`"ffmpeg":"ffmpeg.exe"`), the packaging went wrong somewhere

## Version pins (do not upgrade casually)

| component | version | source |
|---|---|---|
| Go toolchain | 1.27.x | system |
| electron | 44.3.0 | `electron/electron` GitHub release (pinned zip URL) |
| ffmpeg / ffprobe | BtbN `N-126497-g5b614efc7e` (9.x master; no stable 9.1 release exists yet) | `BtbN/FFmpeg-Builds` |
| mpv | shinchiro `20260903-git-69e63f425a` | `shinchiro/mpv-winbuild-cmake` |

Any version bump = editing the pinned URL constants in the tool scripts, then
a fresh `build-windows.sh` run on a clean machine to confirm reproducibility.

## Honing the artifact size (optional, not done)

ffmpeg.exe ships all codecs; a barebones audio/srt/ass + whatever-decode-only
custom build would cut ~150MB from the zip but adds a build we must maintain.
Only worth it if distribution size matters; leave as-is for now.
