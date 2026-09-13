# Development

## Build and run

```sh
go build ./... && go test ./...        # go core + tests
bash tools/build-windows.sh            # full Windows dist + zip in dist/
```

Dev-run the GUI from source (needs `electron-ui/node_modules`):

```sh
cd electron-ui
npm install
npm start                              # uses SUBS_BIN_* overrides or PATH
```

## Layout

- `main.go` — CLI (`probe`, named args; `merge`, named args; classic `video lang lang` form). JSON on stdout is the GUI contract.
- `sandwicher/` — subtitle logic (parse, track pick, merge, ffmpeg extraction + progress). Tests in `ass_test.go`.
- `electron-ui/src/` — `main.js` (orchestration), `preload.js` (bindings whitelist), `renderer/` (UI), `mpv-ipc.js` + `ipc-paths.js` (mpv control), `bins.js` (binary lookup), `logger.js`.
- `tools/` — `fetch-windows-bins.sh` (pinned ffmpeg/mpv downloads), `build-windows.sh` (idempotent Windows packaging; staging in mktemp, output replaced, no manual cleanup needed).
- `third_party/bin/windows/` — vendored binaries (git-ignored).

## Architecture

Three processes, three owners:

- **Electron main**: window + drag/drop + spawning; thin orchestrator, no logic.
- **Go CLI**: all subtitle logic. `probe` and `merge` are JSON on stdout; progress lines on stderr. Called as a subprocess; runs in a kill-on-close Job Object on Windows so closing the UI reaps ffmpeg/mpv too.
- **mpv**: playback + libass rendering in its own window, driven over JSON IPC (`--input-ipc-server=<pipe>`; use the `=` form — newer mpv rejects the space form on Windows). `--no-config` so user profiles can't interfere; kiosk-ish (`--osd-level=0 --osc=no --input-default-bindings=no`).

Never do subtitle logic in JS, never do UI logic in Go. The Electron GUI resolves its CLI/mpv paths via `SUBS_BIN_*` -> app tree -> `PATH`; it does not pass
  its ffmpeg/ffprobe paths to the CLI. The Go CLI resolves ffprobe/ffmpeg relative to its own executable
  (vendored tree -> executable directory -> `PATH`). TODO: make the CLI honor/pass overrides and emit one
  `{"event":"bin",...}` record per tool; currently overrides are ignored by the CLI and logging is once-only.

## Binaries

Versions are pinned to exact URLs in `tools/fetch-windows-bins.sh`. Bump = edit
the URL, verify, rebuild. `bins.js` lookup order: `SUBS_BIN_*` env →
`resources/app/` → `third_party/bin/` → `PATH`.

## Logging / bug reports

Everything a child process does (argv, exit code, stderr/stdout) lands in the
stamped log (`%APPDATA%\SubSandwicher\logs\`, path also in the window title).
When debugging remotely, the whole log file is the artifact — send it entire.

## Conventions

- `gofmt -l .` empty, `go vet ./...` clean; `node --check` each JS file.
- Preload exposes a named whitelist only; no nodeIntegration in renderer.
- No emojis; short imperative commit messages.
