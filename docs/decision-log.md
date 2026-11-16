# Decision log and conflict windows

Every merge is now a job that records **one decision entry per event**, plus
the conflict pairs and the review windows derived from them. The log is the
data the review UI will render: it can filter by change type, level and
surfacing policy instead of re-deriving intent from the .ass diff.

Code: `sandwicher/geometry.go` (roles, boxes, wrapping), `sandwicher/tracks.go`
(no-label track inference), `sandwicher/conflict.go` (report, conflicts, windows),
`sandwicher/placement.go` (candidates and cost), `sandwicher/ass.go` (pipeline).

## Producing a log

`merge` always writes a log. Default path: `<out>.report.json`.

```sh
subsandwicher merge --video=<mkv> --primary-index=19 --secondary-index=3 \
  --out=/tmp/merged.ass --report=/tmp/job.report.json
```

`merge` stdout gains `report_path`, `conflicts`, `conflict_windows` and
`review_required`; the existing `out/styles/lines/events` fields are unchanged.

Scan an existing merged file without rewriting it (works on hand-edited files):

```sh
subsandwicher report --ass=merged.ass --out=job.report.json \
  --secondary-suffix=-eng
```

`--secondary-suffix` defaults to `-eng`, the suffix the merge adds to the
secondary track's style names. Pass `--secondary-suffix=` to skip suffix
matching and run the no-label track classifier. A scan has no transform
decisions, but it still classifies roles and tracks, estimates boxes and
produces conflicts and windows.

## Levels

`level` is an integer plus `levelName`:

| Level | Name | Meaning |
| --- | --- | --- |
| 0 | `info` | transform or classification, no visual risk |
| 1 | `notice` | applied safely, or a suppressed/ignored issue |
| 2 | `warning` | worth showing in review; layout may be wrong |
| 3 | `critical` | collision or off-screen state that needs a decision |

Severity (0-100) is computed from overlap ratio, participant importance and
overlap duration:

```
severity = 100 * (0.55*min(ratio*4,1) + 0.25*importance + 0.20*min(duration,1))
```

Levels are derived from severity: 20 notice, 40 warning, 65 critical.

## Surface policy

`surface` says what the UI should do with a decision:

| Surface | Meaning |
| --- | --- |
| `none` | log only |
| `auto` | applied, safe to hide behind a summary |
| `review` | show in the conflict review list |
| `required` | must be acknowledged before export |

Policy, in order:

- Suppressed conflicts are `none`. A conflict is suppressed when every
  non-dialogue participant is shorter than 0.5 s or below importance 0.25.
  Animated labels are not suppressed: their whole `\move` path can be
  translated, which keeps them in sync with the thing they follow.
- `dialogue-dialogue` with severity at least 65 is `required`. This is the only
  blocking case: two main dialogue lanes colliding.
- `dialogue-dialogue` at severity 20+ is `review`.
- `sign-dialogue` at severity 30+ is `review` (never `required`).
- `sign-sign` at severity 40+ is `review`.
- `edge` (a sign outside the 5% safe area) at severity 70+ is `review`;
  otherwise log-only.
- Transforms that were applied (dialogue re-anchoring, ordering, geometry
  scaling) are `auto`.

Only `review` and `required` conflicts create windows. Edge conflicts stay in
the conflict list so they can be listed separately.

## Decision rules

`decisions[].kind` groups rules; `rule` is the stable identifier.

| Kind | Rule | Level | Surface | Meaning |
| --- | --- | --- | --- | --- |
| `role` | `role.classified` | info | none | dialogue/sign/drawing from position and style name |
| `alignment` | `align.dialogue-remapped` | info | auto | dialogue moved into its track lane |
| `alignment` | `align.positioned-preserved` | info | none | a positioned/drawing event kept its anchor |
| `alignment` | `align.sign-style-preserved` | notice | auto | a sign in a shared style got an explicit `\an` pin |
| `scale` | `scale.inline-geometry` | notice | auto | inline `\pos`/`\move`/`\fs`/... scaled to the primary PlayRes |
| `scale` | `scale.track` | info | auto | secondary track normalized by the PlayRes ratio |
| `order` | `order.role-priority` | notice | auto | sign placed before dialogue at the same start |
| `conflict` | `conflict.<kind>` | warning/critical | review/required | event participates in a conflict |
| `conflict` | `conflict.suppressed-low-importance` | notice | none | conflict recorded but not surfaced |
| `conflict` | `window.review` | warning/critical | review/required | event is part of a review window |
| `none` | `event.unchanged` | info | none | no transform, no conflict |

Every event has at least `role.classified` and `event.unchanged` is only added
when nothing else applies, so a log always accounts for all events.

## Track inference

Merge jobs know provenance: the user picked the two streams, so
`job.trackSource` is `provenance` and confidence is 1.

`report` has no provenance. It uses `--secondary-suffix` when that suffix
matches at least one style. Otherwise it runs a no-label classifier
(`tracks.go`) that groups events by style and infers the tracks from:

1. lane: bottom-aligned dialogue families are primary, top-aligned families
   are secondary;
2. font: a sign family inherits the track whose dialogue font it shares, when
   that font belongs to exactly one track;
3. script: a sign family whose script differs from the primary dialogue
   (for example Latin vs Latin-with-diacritics) follows the secondary;
4. otherwise the larger family is assumed primary.

The result is in `report.tracks`:

```json
"tracks": {
  "method": "classifier",
  "confidence": 0.85,
  "note": "bottom-aligned families are primary; top-aligned families are secondary",
  "groups": [ { "key": "BottomCenter", "dialogue": 320, "alignMode": 2,
                "font": "arial unicode ms", "script": "latin-diacritic",
                "track": "primary" } ],
  "styleTracks": { "BottomCenter": "primary", "Default-eng": "secondary" }
}
```

Confidence below 0.5 adds a `track.low-confidence` decision at `warning` /
`review` on every event, telling the UI to ask which track is primary instead
of guessing. A classifier run over the ten merged files in the test corpus
assigned 100% of the used `-eng` styles to secondary (suffix used only as
ground truth):

```sh
SUBS_CORPUS_DIR="/path/to/media" go test ./sandwicher -run Corpus -v
```

## Reading the report

```json
{
  "version": 1,
  "job": { "id": "...", "video": "...", "primaryLang": "vie",
           "secondaryLang": "eng", "playResX": 640, "playResY": 360,
           "exact": true, "exactEvents": 108 },
  "summary": {
    "events": 689, "conflicts": 79, "windows": 26,
    "reviewEvents": 77, "requiredEvents": 7,
    "byLevel": { "info": 1054, "notice": 52, "warning": 13, "critical": 187 },
    "bySurface": { "none": 813, "auto": 300, "review": 176, "required": 17 },
    "byKind": { "role": 689, "alignment": 365, "order": 26, "conflict": 226 },
    "byRule": { "align.dialogue-remapped": 274, ... },
    "byCause": { "same-track-dialogue": 5, "secondary-sign-vs-dialogue": 34, "secondary-sign-vs-primary-dialogue": 15, "secondary-signs": 2, "sign-outside-safe-area": 23 }
  },
  "events": [
    {
      "id": 4, "track": "secondary", "role": "sign", "style": "sign_Arial-eng",
      "start": 0, "end": 4.98, "layer": 0, "align": 8, "moving": false,
      "bbox": { "x0": 316, "y0": 309, "x1": 495, "y1": 338 },
      "exactBBox": { "x0": 320, "y0": 306, "x1": 490, "y1": 333 },
      "exactSeek": 3.32,
      "importance": 0.898, "level": 3, "levelName": "critical",
      "surface": "review",
      "decisions": [ { "rule": "conflict.sign-dialogue", ... } ]
    }
  ],
  "conflicts": [
    {
      "id": 1, "kind": "sign-dialogue", "cause": "secondary-sign-vs-primary-dialogue",
      "start": 0, "end": 1.08, "peak": 0,
      "events": [4, 5], "overlapRatio": 0.9241, "severity": 97,
      "level": 3, "levelName": "critical", "surface": "review",
      "suppressed": false, "box": { ... }, "reason": "..."
    }
  ],
  "windows": [
    {
      "id": 1, "start": 0, "end": 4.96, "peak": 0,
      "kind": "sign-dialogue", "cause": "secondary-sign-vs-primary-dialogue",
      "causes": ["secondary-sign-vs-primary-dialogue"],
      "severity": 97, "level": 3, "levelName": "critical",
      "surface": "review", "events": [4, 5, 6, 7],
      "byEvent": [ { "event": 7, "seek": 3.32, "suggestionIds": [18, 19] } ],
      "conflicts": [1, 4, 5],
      "autoApplyCount": 3, "manualReviewCount": 0,
      "suggestions": [
        { "event": 4, "kind": "nudge", "dy": -37.2, "at": { "x": 316, "y": 271.8 },
          "cost": 0.1, "originalCost": 9.24, "improvement": 91.2,
          "autoApply": true, "manualReview": false, "recommended": true,
          "reason": "move up 37 px clear of the overlap" }
      ]
    }
  ]
}
```

`bbox` is an estimate (`bboxExact: false` in estimate-only mode): character
advances, explicit line breaks, simple wrapping at the margins, style and
inline sizes, anchor or `\pos`/`\move`, and rotation AABB. It is deterministic
and dependency-free, not libass-exact.

### Exact ink masks

By default the CLI now adds an exact pass (`--no-exact` turns it off):

- Each conflict-involved event is rendered alone, on a transparent frame, by
  the vendored `ffmpeg` through its libass `ass` filter (`alpha=1`). The event
  is re-timed to start at zero; relative `\move`/`\t` timing is preserved and
  opacity tags are stripped so a fade does not erase the shape.
- Alpha coverage is read per pixel (a 1-bit mask). The report gets `exactBBox`
  and `exactSeek`; the mask itself stays in memory and is not serialized.
- Conflicts are re-checked at their peak with mask intersections. A pair whose
  boxes overlap but whose ink does not (for example text padding around a
  sign) is dropped before windows are built. In the E06 session this removed 10
  of 53 box-based candidates and 3 windows.
- Placement cost uses mask intersections for nudges: the moving event's mask is
  translated by the candidate offset and compared with the obstacle's mask, so
  box padding no longer counts as a collision. Boxes remain the fallback for
  moving/estimated events and for font shrinks.
- `job.exact`, `job.exactEvents` and `job.exactNote` record whether masks were
  produced. If ffmpeg/libass is unavailable or a render fails, that event falls
  back to the estimator and the review still completes.
- Cost: about 4-5 seconds for E06 (108 renders, four in parallel) on top of the
  merge. `--no-exact` skips it.

`summary.byLevel`, `bySurface`, `byKind` and `byRule` count decisions.
`summary.byCause` counts conflicts, so it answers "how many problems of this
shape" rather than "how many log lines".

`conflicts[].events` and `windows[].events` are event IDs (`events[].id`), not
array indices, although they currently coincide.

## Placement candidates

Suggestions are deterministic candidates, not applied changes.

- **Keep-outs:** every event active within 0.2 s of the window peak, not just
  the conflict partner.
- **Candidates:** fine vertical/horizontal steps (6/12/20 pt at 360p), box
  multiples at 0.5/1/1.5/2/3/4 heights and 0.5/1/1.5/2/3 widths, **combined x+y
  diagonals**, font shrinks to 90% and 80%, `hide`, `raise` (draw order) for
  sign/dialogue windows, and `top` (move the event above every other subtitle)
  for every event group. Non-conflicting active events offer nudges (plus
  `top`). Candidates that clear dialogue are ranked ahead of cheaper
  still-overlapping ones. Trimming preserves the largest move in each
  direction, so a small label can still jump several box heights even when
  cheap fine moves fill the top of the list. For `\move` events a nudge
  translates both endpoints and preserves the timing instead of writing
  `\pos`.
- **Frame retention:** candidates that move a subtitle completely out of the
  frame are rejected. A subtitle that starts fully in-frame may lose at most
  20% of its area; one that starts partially outside may only get 5 percentage
  points worse. Animated `\move` subtitles are compared by their lifetime
  union, so a label that originally scrolled on and off screen can keep doing
  so; only a candidate that pushes substantially more of it off screen is
  rejected.
- **Hidden signs:** a sign-dialogue conflict where the sign draws beneath the
  dialogue (lower layer, or earlier at the same layer) and is at least 90%
  covered is escalated to `required`. Hidden text is a hard review item, not a
  low-importance conflict to suppress.
- **Sign clusters:** signs in the same style and lane inside one window are
  linked as `byEvent[].related`, even when their times only touch the same
  review window rather than each other. Each group keeps the cluster's
  single-axis moves even when they rank below the normal top six, so one label
  can always copy the move chosen for another. Suggestions carry `sharedWith`
  when the same delta is available for every related sign; the review UI
  reorders the chosen sibling's delta to the top (`same move as #N`) and
  badges cluster-wide moves `shared by N signs`.
- **Video readability:** when a video path is available, `sandwicher/video.go`
  samples the frame at each group's seek with ffmpeg as a downscaled RGB grid.
  Candidate cost combines:
  - normalised edge density, capped at 0.35 so busy shapes do not dominate;
  - the event's own text colour (inline `\c` then style primary) against the
    sampled background, using luma contrast plus colour distance.
  High-contrast text can therefore overlap a shape; a flat but similarly
  coloured background is still penalised. This replaced the earlier
  edge-only keep-out, which pushed every option into distant empty space.
- **Bounds:** no candidate beyond 50% of the PlayRes side. `manualReview` is
  set above 160 px at 360p. `autoApply` is only for non-dialogue nudges and
  requires a cost improvement of at least 2x and displacement of at most 60 px
  at 360p. Whole-path `\move` translations can be auto-applied too. Dialogue
  moves and font shrinks are always review-only.
- **Cost:** `10 ×` dialogue overlap ratio, `8 ×` sign overlap ratio, `6 ×`
  out-of-safe-area ratio, `2 ×` displacement / diagonal, plus video
  readability: up to `5 ×` capped busyness (reduced by up to 65% when contrast
  is good) and `3.5 ×` weak text/background contrast. `originalCost` and
  `improvement` let the UI rank and explain.
- **Apply payload:** `at` is the anchor point for a `\pos` write; `fontScale`
  is the factor for an `\fs`/style rewrite. Applying is not implemented yet.

Every conflicting event gets its own candidate group, including primary
dialogue. Dialogue moves are review-only: `autoApply` is never set for them.
The engine is intentionally conservative and never moves primary dialogue
automatically. `recommended` marks the lowest cost; `autoApplyCount` and
`manualReviewCount` let the UI pre-check the safe ones.

## Filtering

Only blocking windows:

```sh
jq '.windows[] | select(.surface=="required")' job.report.json
```

All sign-over-dialogue conflicts, grouped by cause:

```sh
jq -r '.conflicts[] | select(.kind=="sign-dialogue") | .cause' job.report.json | sort | uniq -c
```

Everything the job changed silently:

```sh
jq '.events[] | select(any(.decisions[]; .surface=="auto")) | {id,start,text,decisions:[.decisions[]|select(.surface=="auto")|.rule]}' job.report.json
```

Suppressed issues (the "leave it" set):

```sh
jq '[.conflicts[] | select(.suppressed)] | length' job.report.json
```

Events with no transform at all:

```sh
jq '[.events[] | select(.decisions|length==1 and .decisions[0].rule=="role.classified")] | length' job.report.json
```

## Review UI

After the video is probed, the drop zone is replaced by a small filename bar
with **Eject**. The language selector uses one scroll area with sticky
Primary/Secondary headers; choosing both languages collapses the selectors to a
summary with **Change languages**, and the primary button becomes **Continue**. After
the merge the app enters review mode: the setup is hidden, the review fills the
window, and **Save merged subs** / **Change languages** move to the filename bar.

The **Review conflicts** panel:

- Left: windows filtered by surface, cause and review status. Each row shows
  time, kind, cause, severity, event count, and status badges:
  `open`, `changed` (has chosen fixes), `done`, or `check again`.
  **Mark done** advances to the next open window; **Check again** flags it;
  reopening a done window clears the status. Statuses are saved and copied.
- Right: **one candidate group for every event active during the window**, not
  only the conflict participants. Non-conflicting active events are labelled
  `no direct conflict; manual spacing nudge`, so adjacent lines can still be
  moved for spacing. Each group is led by the event text in a larger,
  sticky heading with inline colour swatches (parsed from the event's `\c` /
  `\1c`-`\4c` tags). The heading stays visible while its fix options scroll;
  event metadata, exact bbox, decisions and raw ASS sit behind a collapsed
  **Event details** expander on each group. Each group has its own Original
  option.
- **Clicking a window seeks mpv to its peak automatically.**
- **Every event group has its own `seek` time** inside the interval where that
  event actually conflicts. Selecting a fix for a later line (for example #7 at
  3.02s in a 0-5s window) seeks to that line, not the window start.
- **Selecting a candidate previews it immediately**: the app writes a working
  copy with every chosen fix, reloads it in mpv (`sub-reload`) and seeks to that
  event's `seek` time. **Original (no fix)** removes that event's fix and
  restores the unmodified preview. Nothing touches the original merged file.
- **Manual edit escape hatch:** a chosen nudge/shrink candidate gets a small
  editor with `dx`, `dy`, font percent and (for static events) `pos x/y`.
  Editing any field updates the preview after a short debounce, even when the
  candidate originally moved only one axis. The override and the window
  comment are recorded with the applied fix in the review log. **Reset to
  candidate** removes the edit.
- **Group move:** tick one or more event headings and enter a shared `dx/dy`.
  Each selected event gets its own override composed with its current fix, so
  a cluster of signs can keep its relative layout. The selection works across
  styles and includes non-conflicting active events.
- **Move to top** in each event heading sets the event's layer above every
  other subtitle without changing its position.
- **Preview drag:** clicking the mpv window pauses it and selects the topmost
  subtitle under the cursor. Dragging shows a translucent ghost and commits the
  override on release (Shift locks an axis, Alt is quarter-speed, arrow keys
  nudge, `R` resets the selected event). Static events get a new `\pos`; `\move` events translate the whole
  path. Drags snap to the frame, the 5% safe-area edges, and, for single events, other visible subtitle boxes. Moving-event hit-testing
  uses the paused event seek and its exact ink box, so pause at the event seek
  before dragging.
- **Warnings and re-check:** after applying fixes, the app warns when a fix
  moves an event outside the 5% safe area or the video frame. It also shows an
  estimate-only re-check listing conflicts that still touch changed events;
  intentional overlaps can be accepted.
- **Sticky toolbar** per window: Play/Stop, Loop, Unmute (audio muted by
  default), speed (0.25x-2x), Random frame, plus the window status badge and
  icon-only **Done** / **Check again** actions. Playback loops between
  `window.start` and `window.end`; with Loop off it pauses at the end. A thin
  progress bar at the bottom shows the current position inside the window
  loop.
- **Changing a candidate while a window is playing reloads the subtitle file
  without stopping the loop.** Changing windows, pressing Preview window, or
  stopping playback pauses it; the preview then seeks to the relevant time.
- **Preview window** re-applies the current chosen fixes and seeks there.
- **Copy state for report** copies a JSON payload for the selected window and
  chosen candidate. **Copy all state** copies every window, conflict and event.
- The comment box is included as `comment` in either payload.

The payload is `kind: "subsandwicher.review-context"` with `app`, `video`,
`paths` (`mergedAss`, `report`, `log`), `job`, `summary`, `tracks`, `chosen`
(all selected fixes), `comments` (per-window notes), `statuses`
(per-window review state), and then:

- window copy: `windowIndex`, `windowCount`, `window`, `status`, `playback`,
  `suggestions`, `conflicts` (only that window's), `events` (only that
  window's, each with `raw`), plus `comment`;
- copy all: `windows`, `conflicts`, `events`.

Feedback and follow-ups from a real review session are in
[`review-feedback.md`](review-feedback.md).

Paste the payload with a note when reporting a case. It contains everything
needed to reproduce the decision: the report header, the selected window, the
score/improvement, the raw `Dialogue:` lines, and the app/log paths.

### Applying candidates

```sh
subsandwicher apply --ass=merged.ass --report=job.report.json --out=out.ass \
  --fix=3:0 --fix=7:2
```

`window:candidate` pairs refer to `windows[].id` and
`windows[].suggestions[]`. `-1` keeps the original. The command writes a new
file; with no fixes it copies the input byte-for-byte.

- `nudge` replaces or inserts `\pos(at)`; for `\move` events it translates
  both endpoints by `dx/dy` and leaves the times untouched.
- `shrink` writes `\fs` and pins the box with `\pos(at)`.
- `hide` writes `\alpha&HFF&`.
- `raise` increments `Layer`.
- `top` sets `Layer` above every other event.

`--fix-json` accepts the same fix object with optional `override` and `comment`
fields, used by the UI's manual editor:

```sh
subsandwicher apply ... --fix-json='{"windowId":3,"suggestionIndex":0,
  "override":{"kind":"nudge","dx":10,"dy":-24},
  "comment":"moved a little further up"}'
```

Nil override fields keep the generated candidate's value. `dx`/`dy` shift the
candidate's anchor, `fontScale` replaces the shrink factor, and `at` sets an
absolute anchor for static events. The applied override and comment are
recorded on `applied[]` and in the saved review session.

`applied[]` also carries the new `bbox`, `safeOutside`, `originalSafeOutside`,
`introducedSafeArea`, `frameOutside`, `introducedFrame` and `outOfFrame`, so
the UI can warn about fixes that leave the safe area or frame. Apply results
include `remainingConflicts`, an estimate-only recheck of the written file
filtered to conflicts that touch an event changed by that call.

The app uses this for the preview working copy on every candidate click, and
again at save time. **Save merged subs** applies the chosen fixes, copies the
result next to the video, and writes a self-contained session log next to it:

```
<video>.<primaryLang>-<secondaryLang>.ass
<video>.<primaryLang>-<secondaryLang>.review.json
```

The review log is `kind: "subsandwicher.review-session"` with `app`, `video`,
`mergedAss`, `finalAss`, `reportPath`, the chosen `fixes` (with the suggestion
that was applied, plus any manual `override` and `comment`), `applied` (what
the Go core changed, including the override and comment), `comments`,
`statuses`, and the full `report` including raw `Dialogue:` lines. If the log cannot be written the
subtitles are still saved and the UI reports the log error.

## Estimate-only baseline numbers

Generated from the five Kurumi episodes (primary Vietnamese, secondary English
full). "Windows" are review units after suppression.

| Ep | Events | Conflicts | Windows | Review events | Required events |
| --- | ---: | ---: | ---: | ---: | ---: |
| E01 | 689 | 79 | 26 | 75 | 7 |
| E02 | 799 | 93 | 29 | 84 | 5 |
| E03 | 693 | 102 | 24 | 95 | 4 |
| E04 | 770 | 60 | 25 | 71 | 8 |
| E05 | 707 | 63 | 28 | 75 | 5 |

These five-episode counts were taken with `--no-exact`; the exact mask pass
drops box-padding false positives (E06: 53 conflicts / 15 windows estimate vs
43 / 12 exact). The hand-adjusted files reduce review pressure when rescanned: E05 drops from
28 to 18 windows and 5 required events to 3; E04 from 25 to 19 windows; E01
from 26 to 21. E03 rises by one window (24 to 25) where a moved sign created a
new small overlap.

## Not implemented yet

- Exact boxes from libass (`ass_render_event` ink bounds). Estimates are
  honest about being estimates.
- Face or important-object avoidance. Edge-energy saliency is not wired in;
  the safe-area and active-event keep-outs are.
- Retiming candidates. The model can express them; the current engine only
  offers nudge, shrink, hide and raise.
- Two complete sign sets. The conflict and placement model survives; sign
  ownership and priority policy do not exist yet.
