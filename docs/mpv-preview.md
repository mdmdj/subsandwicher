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

## Preview geometry and hit-testing

Mouse messages are normalized by `mpv-drag.lua` against `osd-dimensions`
(window size minus the `ml/mr/mt/mb` video margins). Deltas use the start and
current *normalized* positions, so resizing the mpv window during a drag does
not stretch or shift the result. Every phase also carries the raw OSD size,
margins, mouse position and `par`; those values are written to the app log.

The controller hit-tests with the exact libass ink box from the decision log
when available. Before a drag starts, the CLI `boxes` command re-renders the
current preview frame and returns the real on-screen boxes: events are rendered
together (so libass collision stacking matches mpv), other events are made
transparent so only the target contributes ink, and fades/alpha and Effect-column
motion are honored. This also fixes signs that share a style and are stacked
vertically by libass but were rendered alone in the decision log. A coarse 16x8
ink-coverage grid travels with each exact box, so clicking an empty corner of a
rotated/skewed bounding box does not select it.
`\t` animated transforms and Effect-column motion (`Banner`, `Scroll up`,
`Scroll down`) get several `boxSamples` across the event, and moving events
translate the nearest `\move` sample. Events without an exact mask get
time-specific estimated samples instead of the whole-lifetime union box, so a
moving label is not clickable across its entire path. The estimator models
`\frz`, `\frx`, `\fry`, `\fax` and `\fay` plus `\org`. A click logs the
play-res coordinate, every candidate box, distance in screen pixels, ink hit,
layer, text, moving/effect flags and the transform tags, so a bad hit can be
reported from one log file.

Positional fixes translate `\org` together with `\pos`/`\move`. Changing only
the position of a rotated label would otherwise make it rotate around a stale
origin and appear to jump.

Known limits: per-glyph inline transforms are still estimated, and `\frx`/
`\fry` perspective uses libass's camera-distance approximation rather than a
full glyph render when no exact mask exists.