# mpv preview protocol

How the app controls its mpv instance. Reference: the mpv manual
(`--input-ipc-server`, JSON IPC section in `mpv/DOCS`).

## Launch (per preview)

For each preview session, the controller spawns **one** mpv process:

```
mpv.exe
  --no-config                          (deterministic instance, no user profiles)
  --input-ipc-server=<pipe path>       (host-unique: pid + counter + random)
  --vo=gpu-next --hwdec=auto-safe
  --keep-open=always --pause
  --sub-auto=no                        (do not steal embedded subs)
  --osd-level=0 --osc=no               (kiosk-ish: no mpv UI over our control)
  --input-default-bindings=no --window-dragging=no
  <video path>
```

Automatically retried with a **minimal** arg set (IPC + `--pause` +
`--keep-open=always` + file only) if mpv exits within 3s — used to bisect an
argument problem from a playback problem. `hwdec` is then restored over IPC,
which can be set at runtime.

## IPC sequence

mpv creates the pipe before we can connect. The controller then semaphores
this order (replies use `request_id`, matching the JSON line):

```
sub-add <merged .ass path> select      # attach our external merged subs
seek <seconds> absolute+exact          # frame-exact seek, no keyframe snaps
set pause yes
```

`absolute+exact` is mandatory for preview correctness; `absolute+keyframes`
snaps to nearby keyframes and routinely lands *outside* a short subtitle
line's window (that was a real bug).

Subsequent replays: `seek <t> absolute+exact` with a new `t` chosen from the
merged line table (random dialogue, sampling ~1/3 into the line so fades are
over).

## Instance policy

- Pipe name is unique per app instance and per session: collisions with
  unrelated mpv runs are impossible. On unix the socket file is unlinked at
  quit; on Windows the pipe dies with the mpv process itself.
- `--no-config` guards against ~/.config/mpv / %APPDATA% mpv profiles and any
  lua autoload scripts the user may have installed.

## mpv versions

Pinned by shinchiro build (`tools/fetch-windows-bins.sh`). Note that these
builds are git snapshots; a **date changing pin is a version change** — if a
new build changes behavior, investigate the mpv changelog between the git
revs listed in the release description before bumping.

## Lifecycle ownership

The CLI is a separate child of Electron. On POSIX it is spawned as a
separate process group; on Windows it creates a named kill-on-close Job
Object (`subsandwicher-job-<pid>`, see `sandwicher/job_windows.go`) containing
the CLI and its `ffmpeg`/`ffprobe` children. At quit, the controller sends
`SIGTERM` to the CLI process group on POSIX or runs `taskkill /F /T` for the
CLI PID on Windows.

mpv is also a separate direct child of Electron, but it is not in the CLI's
process group or Job Object. `MpvSession.quit()` closes the IPC connection and
calls the mpv child's plain `kill()`; it does not tree-kill mpv descendants.

TODO: decide whether mpv needs tree-aware cleanup, since plain child killing
does not explicitly reap descendants.

TODO: the early-exit minimal-argument retry currently calls `spawn(true)` on
the same session after `spawn(false)`, but `spawn()` rejects while `proc` is
still set; reset the session or create a new one before relying on that
fallback.

## Auto-relaunch

If the user (or anything) kills mpv, the session object marks itself
`dead`; the next controller command that needs a window (`jump-random-frame`,
review-next-frame) transparently relaunches and seeks straight to that
command's timestamp instead of erroring.
