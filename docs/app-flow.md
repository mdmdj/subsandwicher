# GUI flows

What each step in the app does, and what the failure mode looks like in the
log when it does not.

## 1. Drop a video

The load state is a full-window drop target. Renderer gets a `drop` event;
`webUtils.getPathForFile()` (in preload, contract-isolated) resolves the real
path. The controller then runs `subsandwicher-cli probe --video=<path>` and
renders one radio per
text subtitle stream, labeled from mkv tags. Image-based streams
(PGS / VobSub / DVB) are filtered out — the Go core refuses them too
("image-based ... out of scope"), they only ever show up as hints.

The probe also finds external subtitle files beside the video (same base name,
e.g. `Film.en.srt`, `Film.English.forced.srt`, or under `Subs/`) and offers
`.srt`, `.ass`, `.ssa` and `.vtt` sidecars in the same pickers. Their language
comes from the filename; qualifiers such as `forced`/`SDH` become the title.
The merge reads the chosen file directly instead of extracting an embedded
stream.

When the language form picks a track automatically, titles are scored:
full/dialogue/main/complete are preferred, signs/forced/SDH/CC are
deprioritized. This matters in shows where the "English" stream list is
`#3 English (Signs)` and `#4 English (Full Subtitle)`.

Failure: cli exits non-zero or the file has no usable text tracks → the app
rolls back to the full-window drop state and shows a friendly inline message
("Couldn't read ... as a video"), with the raw error only in the log. No modal;
drop another file to retry.

## 2. Merge

User picks a primary and a secondary; renderer calls
`merge-subtitles {videoPath, primaryIndex, secondaryIndex}`. Controller runs:

```
subsandwicher-cli merge --video <path> --primary-index N --secondary-index M \
                        --out <temp dir>\merge-N-M.ass
```

The transaction is merge-in-TEMP and save-on-confirm: nothing is ever written
next to the video until the user confirms.

The CLI's stderr carries machine lines the controller parses: throttled
`{"stage","role","message","overall":0..1,"step","seconds","total_seconds"}`
progress, and one `{"event":"bin","path":...}` line proving the exact vendored
binary used. `overall` is weighted across the whole pipeline, not just
extraction:

| stage | overall |
|---|---|
| probe | 0.00–0.02 |
| primary extraction | 0.02–0.31 |
| secondary extraction | 0.31–0.62 |
| merge/write | 0.62 |
| conflict analysis | 0.66 |
| exact ink masks | 0.70 |
| decision log | 0.94 |
| review loading | 0.97 |
| done | 1.00 |

Stages without a reliable fraction (`indeterminate: true`) keep the bar at
their stage floor with a shimmer, instead of pretending to be finished. The
exact stage also reports the number of masks rendered. The stdout payload is:

```
{"out":"...ass","styles":3,"lines":522,
 "report_path":"...ass.report.json","conflicts":12,"conflict_windows":4,"review_required":1,
 "events":[{"start":62.62,"end":63.75,"lang":"primary","text":"Xong!"}, ...]}
```

`events` is what the preview picker samples; `lang` distinguishes primary vs
secondary rows in the UI if you ever want a per-track breakdown in the UI.

`report_path` points at the job's decision log: one entry per event (including
unchanged events), the conflict pairs and the review windows. See
`docs/decision-log.md`. Unless `--no-exact` is passed, the CLI also renders
libass ink masks for conflict-involved events and uses them to drop box-padding
false positives and to cost placement candidates; this adds a few seconds and
emits `{"stage":"exact","message":"rendering exact ink masks (N)…"}`
progress lines. The log is written next to `--out` unless `--report`
names another path; `subsandwicher report --ass <file>` rescans an existing
merged file without rewriting it.

## 3. Preview

`launch-preview { videoPath, mergedPath, seekTo }` — asks mpv to open the
video and jump (frame-exact) into a dialogue line. See docs/mpv-preview.md.

seekTo = start + min(0.3s, line_duration/2) of a random event — past fades,
inside visible text, biased toward longer events (more reliable showcase).

## 4. Save

`confirm-save { mergedPath, reportPath, videoPath, primaryLang, secondaryLang,
fixes, comments }`. If fixes were chosen, the CLI `apply` command writes a temp
file with them applied; the result is then copied (not moved) to:

```
<video dir>\<video base>.<primaryLang>-<secondaryLang>.ass
```

matching the original CLI naming convention. Language tags come from the two
selected streams; empty tags degrade gracefully to `-merged.ass` with a warn
log. The temp copy stays in %TEMP% — safer against mid-save crashes than a
move.

The full review session is written next to it as
`<video base>.<primaryLang>-<secondaryLang>.review.json`: the decision report,
the chosen suggestions, the concrete changes `apply` made, and the per-window
comments. If that log cannot be written, the subtitles are still saved and the
UI surfaces the log error.

## Review UX note

The merge returns a decision log with conflict windows (`docs/decision-log.md`),
and the renderer shows a **Review conflicts** panel: windows filtered by
surface and cause, events with boxes/decisions/raw lines, ranked fix
candidates, a comment box, and **Copy state for report** (JSON for a selected
window) / **Copy all state**. "Preview window" seeks mpv to the window's peak;
choosing a fix seeks to that event's own conflict time and previews it.
Every event active in the window gets its own group; same-style, same-lane signs
carry a `related` list, cluster-wide moves are marked `sharedWith`, and the UI
promotes the move chosen for a sibling (`same move as #N`). Placement samples
the video frame in RGB and weighs edge density against the event's text colour,
so readable overlap with a shape is allowed.
Each window can be played on loop (muted by default, unmute and 0.25x-2x speed
controls), with a progress bar showing the current position inside the loop.
The pause button holds the current frame; choosing a fix while paused reloads
subtitles in place, without seeking away from that frame. A chosen nudge/shrink
candidate can be edited manually (dx, dy, font %, position); edits update the
preview and are recorded with the fix in the review log. Tick event headings and
use **Group move** to apply one shared dx/dy across a cluster, including
non-conflicting signs. Every event heading also has **Move to top** to raise its
layer above the other subtitles.

You can also click/drag in the mpv preview: mousedown pauses and picks the
topmost subtitle under the cursor, a translucent ghost shows the target, and
release commits the move through the same override path. Shift locks the
dominant axis, Alt moves at quarter speed, and arrow keys nudge the selected
event (Shift+arrows move 10 px); `R` resets the selected event to its original.
Drags snap to the frame, the 5% safe area, and (for single events) other visible
subtitle boxes. Snap distance is measured in screen pixels, not PlayRes units,
so it stays consistent when the subtitle script is smaller than the video.

Animated subtitles translate their whole `\move` path. The exact ink box is
translated along the parsed move path, so a moving event can be picked and
dragged at any paused frame inside its lifetime, not only at its conflict seek.

**Referent base:** select a subtitle, then Ctrl+drag in the preview to mark the
in-video text/object it is translating. Ctrl+drag a corner to make the quad
perspective, or Ctrl+drag the red handle to rotate it. The app draws nine green
anchor points around the base (above, corners, left/right, below, centre);
clicking one places the selected subtitle there using its current box size and
alignment; the review pane also shows the same anchors as arrow buttons. If no
subtitle is selected, the base becomes pending until you click one. `B` or
**Clear base** removes it. Base quads are saved in the review
session log as `bases`.

After applying, the app warns about fixes that
leave the safe area/frame and shows an estimated re-check of conflicts still
touching changed events. Applying a candidate for preview and save goes through the CLI
`apply` command; mpv remains a dumb pixel renderer under controller command.

## No Spoilers mode

Optional at language finalization, before the merge/preview starts. It is a
presentation layer, not a data lock: the merged `.ass`, the decision log and
the saved review session keep the original text; opening them will spoil.

- **Video**: mpv gets a `gblur` video filter sized from the probed video height
  (default 85% of a `height * 0.10` sigma). The filter is in the launch args so
  the first frame never appears sharp. A top-bar **Default blur** slider sets
  the level for every window that does not have its own override; the toolbar
  slider stores a per-window override, and **Use default** clears it so the
  window follows the global value again.
- **No subtitle flash**: mpv starts with `--sid=no --sub-auto=no
  --sub-visibility=no`, so an embedded or sidecar track cannot render on the
  first frame. The scrambled file is added with `sub-add … select` and only
  then are subtitles made visible. Switching the global veil on or off hides
  the old track before loading the new one for the same reason.
- **Text in mpv**: `merge --no-spoilers` writes a sibling
  `<out>.spoiler.ass` whose visible text is replaced by width-matched
  consonant gibberish with combining marks. Override tags, `\N` breaks, spaces
  and vector drawings are preserved; each word is filled to the original's
  advance width (Arial-style table; Latin text is the supported target, and
  decorative or monospaced fonts will drift), so spaces and line lengths stay
  put while character counts change. The seed is per merge and stored in the report;
  `apply --no-spoilers` reuses it so preview reloads stay identical. The
  report's `events[].displayText` is the same scrambled string for the review
  headings.
- **Text in the review UI**: headings, event details and the raw ASS text use
  `displayText`; the raw line keeps its metadata and only the Text field is
  replaced.
- **Reveal**: a 0.9 s click-and-hold on **Hold to reveal window** rewrites the
  preview with only that window's events unscrambled and removes the blur;
  selecting another window re-locks it. The top-bar global switch has the veil
  on at the right: slide left to turn it off (reload the original text and
  remove the filter) and slide right to turn it back on (reload the scrambled
  preview at the current window's blur level). Both gestures are deliberately
  more effort than a click, not a security boundary.
- **Audio is locked muted** while the mode is on.
- **Save** never passes `--no-spoilers`: the final file is the original text
  with fixes applied. The normal apply path is byte-identical when no fixes are
  chosen.

Failure: if the scrambled preview cannot be produced or copied, the preview
fails instead of falling back to the plaintext merged file.

## Kiosk policy and input

mpv window has no hotkeys, no OSD, no right-click menu; we deliberate so that
all behavior is driven from this app. If someone requests manual keyboard
control back in the mpv window, apply ui routes through the app, not through
mpv hotkeys — the app should stay the only thing controlling this instance.
