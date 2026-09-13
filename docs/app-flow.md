# GUI flows

What each step in the app does, and what the failure mode looks like in the
log when it does not.

## 1. Drop a video

Renderer gets a `drop` event; `webUtils.getPathForFile()` (in preload,
contract-isolated) resolves the real path. The controller then runs
`subsandwicher-cli probe --video=<path>` and renders one radio per
text subtitle stream, labeled from mkv tags. Image-based streams
(PGS / VobSub / DVB) are filtered out — the Go core refuses them too
("image-based ... out of scope"), they only ever show up as hints.

Failure: cli exits non-zero → controller logs argv, exit code and all stderr;
renderer surfaces the message verbatim under `#status`.

## 2. Merge

User picks a primary and a secondary; renderer calls
`merge-subtitles {videoPath, primaryIndex, secondaryIndex}`. Controller runs:

```
subsandwicher-cli merge --video <path> --primary-index N --secondary-index M \
                        --out <temp dir>\merge-N-M.ass
```

The transaction is merge-in-TEMP and 
-on-confirm: nothing is ever written
next to the video until the user confirms.

The CLI's stderr carries machine lines the controller parses: throttled
`{"role"...,"overall":0..1,"step","seconds","total_seconds"}` progress
(merged extraction steps: primary first half, secondary second half; UI
redraws at ~10fps), and one `{"event":"bin","path":...}` line proving the
exact vendored binary used. The stdout payload is:

```
{"out":"...ass","styles":3,"lines":522,
 "events":[{"start":62.62,"end":63.75,"lang":"primary","text":"Xong!"}, ...]}
```

`events` is what the preview picker samples; `lang` distinguishes primary vs
secondary rows in the UI if you ever want a per-track breakdown in the UI.

## 3. Preview

`launch-preview { videoPath, mergedPath, seekTo }` — asks mpv to open the
video and jump (frame-exact) into a dialogue line. See docs/mpv-preview.md.

seekTo = start + min(0.3s, line_duration/2) of a random event — past fades,
inside visible text, biased toward longer events (more reliable showcase).

## 4. Save

`confirm-save { mergedPath, videoPath, primaryLang, secondaryLang }`.
Copy (not move) temp file to:

```
<video dir>\<video base>.<primaryLang>-<secondaryLang>.ass
```

matching the original CLI naming convention. Language tags come from the two
selected streams; empty tags degrade gracefully to `-merged.ass` with a warn
log. The temp copy stays in %TEMP% — safer against mid-save crashes than a
move, and it's the same data byte-for-byte.

## Review UX note

Currently one "Another frame" jump button; there is no per-stream time context
or seek slider. If users report wanting precise verification, the plan path is
a small timeline in the browser (that's the same "events" array) — mpv stays
a dumb pixel renderer under controller command.

## Kiosk policy and input

mpv window has no hotkeys, no OSD, no right-click menu; we deliberate so that
all behavior is driven from this app. If someone requests manual keyboard
control back in the mpv window, apply ui routes through the app, not through
mpv hotkeys — the app should stay the only thing controlling this instance.
