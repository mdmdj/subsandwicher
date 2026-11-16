# Evaluating Rune as the UI layer

Assessment of moving the app's shell from Electron to
[Rune](https://github.com/anomalyco/rune), and what that buys or costs for the
two planned enhancements: preview inside the main window, and interactive
subtitle layout (moving overlapping lines).

Date: 2026-11-11. Sources: Rune `dev` at 6.1k stars / pre-release
(`crates/rune/README.md`, `crates/rune-kit/README.md`, `AGENTS.md`, CI
workflows), mpv manual (`screenshot-raw`, render API), and this repo.

## TL;DR

- Rune is a good fit for the *shell*: the current UI is ~930 lines of
  JavaScript wrapping a drop zone, two radio lists, four buttons and a progress
  bar, rendered by a 150 MB Chromium runtime. Rune covers every control this
  app uses, plus native file dialogs, external file drops, `fit_content_height`
  (replacing the `fit-window` JS hack) and window-title control.
- Rune does **not** by itself deliver either enhancement. Both are
  *video-compositing* and *subtitle-data* problems, not UI-framework problems.
  Electron can do most of the same things; Rune makes the overlay variant
  nicer and cheaper, and its headless tooling is a genuine step up for this
  repo's agent-driven workflow.
- The hard constraint is z-order: you cannot draw UI on top of an embedded
  native video window. Any "UI over video" design needs either a separate
  transparent top-level window, or video rendered into the app's own surface
  (libmpv), or a still-frame preview.
- Biggest risks, in order: Rust + Windows cross-compilation added to a Linux
  dist build; Rune's Windows surface is CI-tested only by `cargo xwin clippy`
  (the only CI workload is macOS); pre-release API you pin and cannot upstream.
- Recommendation: **do not rewrite for the enhancements**. Ship the
  subtitle-overlap work first in the Go core + current UI (it is pure data and
  survives any UI choice). Run a 1–2 day Rune spike on Windows for the shell
  and a still-frame preview before committing to a port.

## What the app is today

Three processes, three owners (`DEVELOPMENT.md`):

| Owner | Job | Size |
| --- | --- | --- |
| Electron main | window, drag/drop, spawning, IPC plumbing | ~940 lines JS total |
| Go CLI | all subtitle logic; JSON on stdout, progress on stderr | ~1.1k lines Go |
| mpv | playback + libass rendering in its own kiosk window, JSON IPC | vendored binary |

The renderer is 36 lines of HTML, 229 lines of JS, 56 lines of CSS. Electron
44 ships a full Chromium + Node for that. The dist is Windows-first:
`tools/build-windows.sh` builds everything on Linux and zips a portable
folder.

## What Rune is

Native GPU UI in Rust (wgpu + winit, with a native Metal backend on macOS),
own renderer, text, layout (Taffy), springs. Pre-release; apps pin a `dev` rev
(revs are never rewritten). MIT. The repo is explicitly built around coding
agents: apps run headless, render deterministic PNGs, dump their element tree
and replay scripted input.

Relevant capabilities, verified in the `dev` docs:

- **Components** (`rune-kit`): Button, IconButton, Switch, Checkbox,
  **RadioGroup/RadioItem**, TextInput, Select, Popover, Menu, Tooltip, Toast,
  Card, List, Accordion, Badge, Divider, Spinner, **Progress**, ProgressCircle,
  SegmentedControl, Tree, Table, diff view, Markdown.
- **Input**: pointer capture, drags with per-gesture state, double press, drop
  files (`on_drop_files`, `on_drop_hover`), keyboard focus/Tab/modal handling.
- **Windows**: multiple windows and overlays on one event loop; per-window
  state types; `WindowOptions { transparent, borderless, always_on_top,
  position, visible }`; `WindowOptions::overlay(display)` = transparent,
  borderless, topmost, **click-through** full-display surface; `WindowHandle`
  can toggle click-through, visibility, topmost, position and resize; overlays
  pass clicks through unless `interactive`.
- **Shell bits**: native file dialogs (`native-file-dialog`, rfd on
  Windows/macOS), `fit_content_height`, `Tx::set_title`, clipboard, log-path
  style text.
- **Headless tooling**: `--snap` PNG, `--tree` element dump, `--script`
  scripted input, `--video` capture; `--desktop` composites several windows.
  Deterministic; same code path as the real app.
- **Not available / not useful here**: `Player` video and `NativeSurface`
  frame import are macOS-only; system materials are macOS-only. No HTML/CSS,
  no DevTools; iteration is compile + run + inspect tree/snapshot.

## Fit for the current UI

Everything maps cleanly:

| Today (Electron) | Rune |
| --- | --- |
| drop zone + hidden file input | `on_drop_files` + `FileDialogs::open` |
| `probe-video`, `merge-subtitles`, `confirm-save` IPC | spawn Go CLI, read stdout JSON / stderr progress |
| radio lists per stream | `RadioGroup` / `RadioItem` |
| `merge-progress` IPC event | child stderr line parser + `poll` wake |
| `fit-window` via `executeJavaScript(scrollHeight)` | `App::fit_content_height()` |
| log path in window title | `Tx::set_title` |
| "open logs folder" | shell crate / `std::process::Command` (small gap) |

Estimated port: 1,200–1,600 lines of Rust for parity, plus process/IPC
plumbing. Two languages either way (Go + JS today, Go + Rust after).

## Enhancement 1: preview in the main window / a controlled window with UI on top

The blocker is native z-order, not the UI toolkit. A child window
(`mpv --wid=<hwnd>`) is always composited above its parent's client area, so
Rune (or Electron, or anything) cannot draw over an embedded mpv surface in
the same window.

Options, with real costs:

**A. Keep the separate mpv window, add a Rune overlay above it.** Position mpv
borderless at a known rect (`--no-border`, `--geometry`), put a transparent
always-on-top Rune window at the same rect (`WindowOptions` with
`transparent: true, borderless: true, always_on_top: true, position`), drive
all input there (mpv stays kiosk, so nothing needs to reach it). Rune can
toggle click-through per window handle. This is the cheapest path to "UI over
video" and keeps libass fidelity. Fragile parts: keeping the two rects in sync
on move/resize/DPI/multi-monitor; Wayland cannot place or stack windows, and
interactive overlays are unsupported there (Windows/X11/macOS are fine).

**B. Embed mpv in the Rune window and draw UI only outside the video rect.**
`--wid` with Rune's raw window handle; perfect pairing, no stacking problem,
but no overlays over the picture. Best if "main window" means a normal
video + sidebar layout rather than HUD-on-video.

**C. libmpv in-process, rendered into the app.** mpv render APIs are OpenGL,
D3D11 (0.42+) and software. Rune draws with wgpu (Metal/DX12/Vulkan), so every
variant needs interop or readback: GL→wgpu has no Windows GL backend; D3D11
textures can be shared to DX12/Vulkan but that is custom `wgpu-hal` work;
software (`MPV_RENDER_API_TYPE_SW`) gives a CPU RGBA buffer that can be
uploaded as a Rune image — simple and portable, single-threaded and fine for a
paused/stepped preview, not for 60 fps playback. Also needs a vendored
`libmpv.dll` (the shinchiro release ships `mpv.exe`, not libmpv) and an FFI
crate. This is the only path to true in-window playback *with* UI on top, and
it is a multi-week project.

**D. Treat the preview as a still.** The app pauses on a line and steps
frames; it does not need continuous playback. mpv already exposes
`screenshot-to-file <path> [video|subtitles|window]` over JSON IPC (note:
`screenshot-raw` returns a byte array and works only through the client API or
an mpv script, not plain JSON IPC). Flow: seek `absolute+exact`, wait for the
frame, screenshot without subtitles, show it as a Rune image, draw subtitle
overlays and handles as real elements. No second window, no z-order games,
exact hit boxes for interaction, works on every target.
Caveats to validate: screenshots come from the VO at window scale unless
`--screenshot-sw=yes` is used to render from the decoded frame; seek+encode
cost is ~100–200 ms per frame at 1080p (JPEG is faster than PNG).

Recommended shape: **B or D for the main window**, **A** only if HUD-on-live-
video is required, **C** deferred until playback-in-UI is a stated goal.

## Enhancement 2: interacting with subtitles (moving overlapping lines)

The interaction does not require taking over subtitle rendering. Moving a line
is a *data* change, and mpv's render is the feedback:

1. **Know what is on screen.** The merged events are already in the CLI's
   stdout (`events: [{start,end,lang,text}]`); mpv also reports `sub-text`,
   `sub-start`, `sub-end` live.
2. **Hit boxes.** Approximate is enough for a grab handle and a vertical drag:
   PlayRes scale, alignment, `MarginV`, line count. Exact boxes would mean
   running libass in-process (`ASS_Image` lists) or reimplementing its layout;
   both are costly and only worth it if you want pixel-perfect outlines drawn
   over the video.
3. **Mutation.** Per-track `MarginV` / `\an` override, or a sparse per-event
   `\pos`/`\an8` override written into the merged ASS. Overlap detection and
   the "which lines collide" decision belong in the Go core, not the UI.
4. **Feedback.** Rewrite the temp ASS, then `sub-reload` (or `sub-add` again)
   and re-seek; the user sees libass's real output immediately.

That sequence works in Electron today and in Rune later, and it keeps libass as
the single source of truth for what the saved file will look like. The only
thing Rune adds is a nicer transparent overlay/drag surface over the video
(option A or D above).

Important scope note: today the merge is extraction + concatenation; "move
overlapping subtitles" turns the tool into an editor (per-track style, per-event
overrides, an undo story, and a decision about how source tracks stay
traceable). That is a Go-core and product change first. Do not pick a UI
framework to solve it.

## Cost and risk of Rune

**Build and dist.** The Windows dist is built on Linux. Rune needs a Rust
toolchain; your Windows artifact must come from `cargo xwin` (MSVC) or a
`*-pc-windows-gnu` target cross-link, or from a Windows CI runner. Rune's own
CI runs macOS tests only, and its Windows overlay path is explicitly "checked
with `cargo xwin clippy` only". Rune's macOS-only features (video, native
surfaces, materials) do not help this app anyway. Expect first-build times of
minutes (wgpu tree) and a new class of toolchain failures in `build-windows.sh`.

**Dependency posture.** Pre-release, pin-a-rev, "we don't take pull requests".
Pinning is safe (revs are immutable) and upgrading is deliberate, but a bug you
hit must be worked around or patched in a local fork. MIT makes that legally
fine, not cheap.

**Iteration and debugging.** No DevTools, no live CSS. You get `--tree`,
`--snap`, traces and a debug inspector; styling changes are edit → compile →
look. For a form this size that is fine; for the overlay/drag work it is the
main friction.

**What you gain.** The dist drops Electron (roughly 150 MB unpacked and a
Chromium process tree) and keeps a few-MB UI binary next to Go, ffmpeg and mpv.
Native overlays/transparency with a real API. Springs and drag primitives for
the subtitle handles. Deterministic headless snapshots for CI and for agents,
which matches how this repo already works.

## Alternatives, briefly

- **Stay on Electron.** Lowest risk. A transparent `alwaysOnTop`
  `BrowserWindow` with `setIgnoreMouseEvents(true, { forward: true })` gives the
  same overlay trick, and the still-frame preview is an `<img>`. You keep
  DevTools and the 100 MB runtime.
- **Tauri.** Rust shell + system webview. Most of Rune's dist win, keeps the
  web UI and skill set, more mature. Weaker than Rune on multi-window/overlay
  ergonomics and headless inspection.
- **Gio / Wails.** Gio keeps everything in Go and is GPU-native, but
  multi-window and overlay control are poor; Wails is Tauri-like with a Go
  backend.

## Recommendation

1. **Phase 0 — now, UI-independent.** Add overlap analysis and a nudge model
   (per-track margin, sparse per-event overrides) to the Go core and wire a
   simple version into the current Electron UI. This is the actual feature;
   it is cheap, testable in Go, and portable to any shell.
2. **Phase 1 — Rune spike, 1–2 days.** One Rune window: drop file → run
   `probe` → radio lists → `merge` with stderr progress → show a
   `screenshot-to-file` still → save. Build it on **Windows** early (that is the
   dist target), and cross-build from Linux as part of the spike. Compare
   launch time, dist size and lines of code against Electron.
3. **Phase 2 — overlay proof, 2–3 days.** mpv at a fixed borderless rect, a
   Rune transparent always-on-top window over it, approximate subtitle boxes,
   drag → margin rewrite → `sub-reload`. Validate on Windows at 100% and 150%
   DPI, and with two monitors.
4. **Phase 3 — port or stop.** Only port the shell if Phase 1 matches the
   Electron UI functionally and Phase 2 feels precise. Otherwise keep
   Electron, keep the Phase 0 work, and revisit when Rune ships a release and
   has Windows CI.

**Kill criteria for the port:** cross-compiled Windows build not reproducible
from `build-windows.sh`; overlay/DPI drift visible at 150 %; or the Phase 0
feature proves sufficient and the overlay never gets built.

## Decision summary

| | Electron (today) | Rune |
| --- | --- | --- |
| effort to parity | 0 | 2–4 weeks + toolchain |
| dist size / RAM | ~150 MB, Chromium tree | few MB, one native process |
| UI iteration | devtools, instant | compile + snapshot |
| main-window preview | `<img>` / `<video>` | image element / Player (macOS only) |
| UI over video | transparent always-on-top window | same, with first-class window API |
| subtitle drag | DOM boxes | element boxes + drag primitive |
| headless tests | browser smoke test | `--snap` / `--tree` / `--script` |
| Windows confidence | high | medium (clippy-only CI) |
| dependency risk | Electron (stable, huge) | pre-release, pinned rev, patch locally |
| solves the two enhancements | partly | partly |

Rune is the better long-term shell for this app, but it is an
infrastructure-and-polish decision, not a prerequisite for the next feature.
Sequence the work accordingly: overlap model first, Rune spike second, port
only if the spike earns it.
