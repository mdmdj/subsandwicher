# Subtitle conflict resolution: evidence from five Kurumi-chan episodes

## TL;DR

> Status: the two safe core fixes (role-aware anchoring and complete inline
> geometry scaling), the explicit order policy, conflict windows, suggestions
> and the per-event decision log are implemented in the Go core. Schema, levels
> and surface policy are in [`decision-log.md`](decision-log.md). Placement
> application and exact libass boxes remain future work. The structs in
> section 5 are the design sketch; the shipped names are `plannedEvent`,
> `Decision`, `Conflict` and `ConflictWindow` in `geometry.go` and
> `conflict.go`.
>
> Also implemented: title-scored track selection (full over forced/signs), a
> no-label track classifier with low-confidence surfacing (`tracks.go`), a
> bounded placement engine with keep-outs, cost, auto-apply gating and manual
> review caps (`placement.go`), per-event candidate groups, and applying a
> candidate through the `apply` CLI. The UI previews candidate results live
> and saves a full review session next to the .ass. Animated labels are no
> longer skipped: a nudge translates their whole `\move` path. Not
> implemented: exact boxes, saliency/face avoidance, retiming candidates, two
> complete sign sets.

- The hand files are not rewrites: all **3,658** generated dialogues match by `(start, end, plain text)`; there are **0** added, removed, or text-edited events. Human work is **placement and draw order**.
- The dominant edit is a **sign nudge**: **100** positioned events have a changed `\pos`, **81** moved more than 20 px, **42** more than 50 px, **22** more than 100 px. **No style field, margin, layer or plain text was changed** in any episode.
- Hypothesis 1 is **confirmed**: `remapTextAlignments`/style remapping runs on positioned events. **167** secondary signs whose original anchor was bottom (1-3) became top (7-9); the human compensated **47** of them with a vertical `\pos` correction. One correction is exactly the text-height shift (E01 0:00:00, `y 94 -> 60.67`).
- Hypothesis 2 is **confirmed but smaller**: **32** secondary middle-row signs became `\an6`; **20** were corrected with `\pos` and **1** with an `\an` edit. The phone labels at E02 0:12:26 and 0:16:28 are this class, shifted **+106 px** and **+152 px** right.
- Hypothesis 3 is **refuted for this corpus**: both source tracks are 640x360 in all five episodes, so `rescaleFactor` was 1 and no scaling ran. E01/E02 hand files were globally upscaled x3 to 1080p by the user (PlayRes 1920x1080, every geometric value x3), including `\fsp`; this is a document preference, not a conflict fix. The latent bug (inline coordinates are unscaled when PlayRes really differs) remains.
- Hypothesis 4 is **confirmed narrowly**: **26** inversions exist, only in E04 (22) and E05 (4). They change draw order (later event on top), not collision stacking; all reordered events are positioned signs. E04 puts the primary dialogue above a stale sign; E05 puts small sign labels above dialogue so they are visible.
- **58** estimated conflict windows (cross-track box overlap) exist before hand editing; the human reduces them to **39**. A deterministic classifier identifies secondary events at 99.3-99.7% accuracy, but the corpus makes it easy: every primary event is unpositioned bottom dialogue. The failure cases are instructive, not reassuring.

## 1. Corpus, streams, outputs

Every episode is 1920x1080, 23.976 fps, both text tracks `PlayResX=640 PlayResY=360`. Stream indices differ:

| Ep | English full | English signs/forced | Vietnamese | Gen events | Styles | Merge runtime |
|----|--------------|----------------------|------------|-----------:|-------:|--------------:|
| E01 | #3 (369) | #4 (91) | #19 (320) | 689 | 11 | 1900 ms |
| E02 | #3 (423) | #4 (140) | #20 (376) | 799 | 9 | 2255 ms |
| E03 | #4 (404) | #3 forced (141) | #20 (289) | 693 | 26 | 2342 ms |
| E04 | #4 (408) | #3 forced (79) | #20 (362) | 770 | 26 | 2305 ms |
| E05 | #3 (381) | #4 (95) | #20 (326) | 707 | 9 | 2466 ms |

The hand files are `primary=Vietnamese`, `secondary=English full`, confirmed by styles: Vietnamese events use `BottomCenter` (E01) or `Default` (E02-E05), font `Arial Unicode MS`, style alignment 2, **0** `\pos`, **0** `\move`, **0** `\an`, **0** drawings. English events carry `Default-eng`, `sign_* -eng`, `cr_sign_1-eng` and include 484 positioned signs. E03/E04 use #4 as full and #3 as forced; using #3 by title would merge signs only.

Variant merges (E01/E02): signs-only (#4) gives 411/516 lines, 9/7 styles, 1855/2256 ms; mirror (English #3 primary, Vietnamese secondary) gives 689/799 lines, 11/9 styles, 1928/2303 ms. The mirror exposes the primary transform: 45 (E01) and 68 (E02) top-positioned English signs would have their anchor flipped from top to bottom.

## 2. Evidence table (generated vs hand, normalized)

All E01/E02 hand geometry was divided by 3 before diffing (see 3.3). "Pins" are events with no `\pos` in generated and an explicit `\pos` in hand.

| Ep | Events | Pos changed | >20 px | >50 px | >100 px | `\move` | `\an` | Pins | Inversions | Est. windows gen/hand | Style diffs |
|----|-------:|------------:|-------:|-------:|--------:|--------:|------:|-----:|-----------:|----------------------:|------------:|
| E01 | 689 | 15 | 11 | 8 | 0 | 2 | 1 | 4 | 0 | 10 / 5 | 0 |
| E02 | 799 | 28 | 25 | 18 | 15 | 1 | 0 | 2 | 0 | 15 / 11 | 0 |
| E03 | 693 | 16 | 12 | 5 | 2 | 1 | 8 | 2 | 0 | 10 / 7 | 0 |
| E04 | 770 | 20 | 16 | 6 | 3 | 0 | 0 | 4 | 22 | 13 / 12 | 0 |
| E05 | 707 | 21 | 17 | 5 | 2 | 3 | 3 | 14 | 4 | 10 / 4 | 0 |
| **All** | **3658** | **100** | **81** | **42** | **22** | **7** | **12** | **26** | **26** | **58 / 39** | **0** |

Other measured edits: `\fad` 1 (E02 0:04:04, missing `)` repaired by Aegisub), `\fs` 1 (E04 0:06:09, removed rotation and inline size on "National Tax Agency"), bold 3 (E03 0:14:38, unbolding explanatory text), color/rotation 2 (E01/E04 sign tweaks). Plain text edits: **0**. Layer/margin/style edits: **0**. Estimated cross-track box-overlap pairs: 93 -> 65 (per episode 15/22/16/20/20 -> 16/17/11/15/6; E01 rises because disclaimers move into a denser top area but no longer collide with the bottom title).

## 3. Findings

### 3.1. Alignment remapping on positioned events is the largest systematic error (hypothesis 1: confirmed)

`mapAlignment` changes the anchor, but `\pos` names a point, not a box. For a bottom-anchored sign at `\pos(x,y)`, changing `\an2 -> \an8` keeps the point and flips the box from `y-h..y` to `y..y+h`: the sign moves down by its own height without moving its coordinate. The human corrected this with `\pos`, not by restoring `\an` (0 positioned events had `\an` restored).

Measured on source tracks:

| Class | Count | Human `\pos` correction | Human `\an` correction |
|-------|------:|------------------------:|-----------------------:|
| Positioned bottom (1-3) -> top (7-9) | 167 | 47 (28.1%) | 0 |
| Positioned middle (4-6) -> 6 | 32 | 20 (62.5%) | 1 |

Example, E01 0:00:00.00, `This is a work of fiction.`:
`{\an2...\pos(321,94)}` -> generated `{\an8...\pos(321,94)}` -> hand `{\an8...\pos(323.7,60.7)}`. The anchor flip alone moves the box down by roughly one line (about 14 px at inline `\fs12`); the hand moves `y` up by 33.3 px, i.e. the flip plus a deliberate extra nudge to clear the Japanese line. It does **not** restore `\an2`. Four sibling disclaimer lines get `y -49..-68`. Rendered comparison: `frames/E01_2.0_pair.png`.

Example, E02 0:12:26.98, phone account labels. Source `{\an4...\pos(203,17)}Balance:`; generated `{\an6...\pos(203,17)}`; hand `{\an6...\pos(309,19)}` (+106 px). The generated right-anchored label is drawn leftwards over the burned-in Japanese `残高`; the hand moves it into the gap before the number. Six labels in this group and six at 0:16:28.09 get +106 / +152 px. Rendered: `frames/E02_748.5_pair.png`, `frames/E02_990.0_pair.png`.

The minimal rule: **never remap alignment on events with `\pos`, `\move`, `\org`, or `\p`**. If a sign must be re-anchored, translate the anchor point so the estimated box is preserved; then run the placement pass if the box still conflicts.

### 3.2. Middle-row signs forced to `\an6` (hypothesis 2: confirmed, smaller)

Only 32 positioned signs were middle-row. The change preserves the row but flips horizontal anchoring, so text extends left instead of right. The human fixed 20 with `\pos`, 1 with `\an5`. Example E02 0:10:15.51 `Balance:` (`\an4\pos(87,191)` -> `\an6\pos(87,191)`, unchanged by hand; five labels remain overlapping), and E04 0:02:48.73 `Send` (`\an6\pos(479,324)` -> `\an6\pos(521,324)`, +42 px to clear the Japanese `送信` button). Rendered: `frames/E04_169.2_pair.png`.

Minimal rule: same as 3.1. For unpositioned events, middle-row -> 6 is acceptable because libass collision stacking will place it; for positioned events it is a horizontal translation that must be compensated.

### 3.3. PlayRes/coordinate rescaling (hypothesis 3: refuted in this corpus)

`rescaleFactor` was 1 in all five merges (`no rescaling` lines in every stderr log); both tracks in every episode declare 640x360. The apparent 3x style diff is the user re-saving E01/E02 at PlayRes 1920x1080: every style number, `\fs`, `\fsp`, event margin, `\pos`, `\move` and `\org` value is exactly 3x, while `\fad` times stay in ms and `\fscx/\fsy` stay in percent. After dividing by 3, **zero** style fields differ. E03-E05 were saved at 640x360 and show no rescale.

So this corpus cannot validate the mismatch path. The code path is still incomplete for a real mismatch: `scaleStyle` covers `fontsize, spacing, outline, shadow, margin*`; `scaleDialogueMargins` covers event margins; but `\fs`, `\bord`, `\shad`, `\fsp`, `\pos`, `\move`, `\org`, `\blur` and drawing coordinates are not scaled. The phone signs prove why this matters: a 640x360 sign with `\move` coordinates is meaningful only in its own PlayRes.

### 3.4. Ordering and layer (hypothesis 4: confirmed, narrow)

libass semantics: Layer first; within a layer, later events draw on top. `\pos`/`\move` disable collision movement but not draw order. Collision stacking applies to unpositioned events with the same alignment. Here every sign that matters is positioned, and primary/secondary dialogue use different alignments (bottom vs top), so **collision stacking is not the mechanism used by the hand fixes**. Draw order is.

Only 26 inversions exist:

- E04, 22 inversions. All at 0:01:15.30 and 0:20:29.29. At 1:15.30 the human moved the stale `Investor Password` row (start 1:15.30, end 1:16.81) before the primary dialogue, and moved the previous shot's `Open` (start 1:14.05, end 1:15.30) to the end of the group. At 20:29.29 the primary dialogue was moved from first to last and `Completed` from last to first. Rendered: `frames/E04_75.6_pair.png` and `frames/E04_75.6_bottom_zoom.png`; generated shows the old row over the Vietnamese line, hand leaves the Vietnamese line readable.
- E05, 4 inversions at 20:08.02. `bunny`/`cat` signs were moved after the English and Vietnamese dialogue. In generated, the small `cat` at `pos(460,305)` is covered by the bottom dialogue; in hand it is drawn last and visible. Rendered: `frames/E05_1208.5_pair.png` and `frames/E05_1208.5_zoom.png`.

A deterministic policy that puts dialogue above signs (layer 1 vs 0, or sign-before-dialogue tie-break) reproduces E04's 22 meaningful inversions; it does **not** reproduce E05's 4, where a label over artwork must be above dialogue. That is a genuine per-conflict choice, not a global rule. Default to "protect dialogue", offer a one-click "raise sign labels" batch.

### 3.5. Human fixes are sign nudges, and sometimes move the primary too

100 sign `\pos` changes, 81 over 20 px, 22 over 100 px. Only 81 of 484 positioned signs (16.7%) needed moving at all. The largest single fixes are semantic: E03 0:15:03.72 `1,800,000 yen` moved `y 18 -> 228` (210 px) from over the top dialogue down to the `180万円` referent; E05 0:21:01.27 primary dialogue `SỐ DƯ TÀI KHOẢN FX...` given `pos(320,265)` so it clears the huge `12,014,110円`; E04 0:02:48.73 `Nice to meet you...` moved `(+67,-90)` out of the comment box; E01 0:19:06 `Current Price` moved `(+49,-20)` above the chart box. Rendered: `frames/E03_904.2_pair.png`, `frames/E05_1262.5_pair.png`, `frames/E04_169.2_pair.png`, `frames/E01_1147.0_pair.png`.

Pins (26) matter for the design: **9 primary dialogue events** got an explicit `\pos` (E01 4, E05 5) and **17 unpositioned sign-style events** got one. Any policy that only allows secondary movement cannot reproduce these fixes.

### 3.6. The phone-labels example

Closest match: **E02 0:01:10.63**, a phone order screen with five moving labels. Exact tags/durations:

| Text | Visibility | Motion |
|------|-----------:|--------|
| `Symbol` | 0.34 s = **8 frames** | `\move(102,32,101,-2,23,315)`, 34 px in 292 ms |
| `Volume` | 0.71 s = 17 frames | `\move(120,80,120,5)` |
| `Stop Loss` | 1.09 s = 26 frames | `\move(25,120,25,05)` (typo `05`) |
| `Take Profit` | 1.09 s = 26 frames | `\move(355,115,355,5)`, `\fs15` |
| `Order Type` | 1.50 s = 36 frames | `\move(73,157,70,8,23,1483)`, 149 px |

The human changed only the typo (`05 -> 5`), no positions. There is **no 5-frame event** in these files; the shortest is 8 frames. Treat this as evidence for "do not move short moving labels": the animation tracks a scrolling UI, and translating only the endpoints would desync it. Rendered: `frames/E02_71.2_pair.png`.

## 4. Identifying "secondary" without labels or tags

Provenance is known at merge time from the user's primary/secondary choice. It is unknown for third-party files and hand-re-saved files. The classifier below never uses style names, `-eng` suffixes, track tags, or the generated stdout.

Features: explicit `\pos`/`\move`/`\org`; drawing `\p`; effective alignment (`\an` else style); layer; font; font size; margins; text script/language; role; duration; text length; estimated bbox; overlap with events of another language. The unsupervised step infers the **base language** as the majority language among unpositioned bottom-aligned events (the local dialogue track), and the base font as the most common font on those events. Score (secondary if > 0):

```
+3.0 drawing
+2.0 positioned
+1.5 alignment 7-9          -1.5 alignment 1-3
+2.0 text language != base  -2.0 text language == base
+1.0 positioned and language != base
+0.5 font != base font      +0.5 nonzero layer
```

Evaluated on the five hand files, ground truth joined from the generated merge; style names are inputs to neither training nor rule tuning:

| Variant | Accuracy | Precision (secondary) | Recall (secondary) |
|---------|---------:|----------------------:|-------------------:|
| Full rule | **0.9945** | 0.990 | 1.000 |
| No language | **0.9973** | 0.996 | 0.9995 |
| Geometry only | 0.9926 | 0.995 | 0.991 |
| Language only | 0.9940 | 0.990 | 0.999 |

This looks excellent only because the corpus is easy: every primary event is unpositioned bottom dialogue. Failure cases:

- **Short primary lines lose their diacritics.** `Kurumi!`, `Alo?`, `Mochiko.`, `Yay!` are scored `lang=other`, and the language rule (+2) overrides bottom alignment (-1.5), producing 20 false positives. No-language avoids these but then fails on pinned dialogue.
- **Pinned primary dialogue becomes "positioned".** The human added `\pos` to 9 primary lines (E01 0:15:20, E05 0:21:01 etc). Geometry-only calls all 9 secondary; language saves them. This is exactly why re-saved files are hard.
- **A secondary sign at the bottom** (E03 0:00:44.82 `Yen`, `\an2`) is a false negative in the no-language variant: bottom alignment is a weak signal once signs can sit anywhere.
- **Font is a corpus shortcut.** `Gandhi Sans` vs `Arial Unicode MS` separates the tracks here but will not generalize.

Recommendation: use provenance when available (merge time); for third-party files run the full rule, but require **two independent signals** (e.g. foreign language + top/positioned, or foreign language + overlap with base dialogue) before calling an event secondary. Score per **track**, not per event: median alignment/position/script over the whole track is far more stable than one short line. Confidence below threshold -> ask the user to pick which track is primary (one radio, no classification).

## 5. Phase 0 design for the Go core

### 5.1. Data structures (new file `sandwicher/conflict.go`)

```go
type Role int        // RoleDialogue, RoleSign, RoleDrawing
type Track int       // TrackPrimary, TrackSecondary
type Rect struct{ X0, Y0, X1, Y1 float64 }
type Point struct{ X, Y float64 }

type MergedEvent struct {
    *Dialogue
    Track Track
    Role  Role
    Align int            // effective alignment
    Pos   *Point         // explicit pos, or nil
    Move  *[4]float64    // x1,y1,x2,y2
    Org   *Point
    BBox  Rect           // estimated; Exact bool when libass-measured
    Importance float64
}

type Conflict struct {
    StartCS, EndCS int
    PeakCS         int
    Events         []int
    Kind           string  // sign-dialogue, sign-sign, sign-edge
    Severity       float64
}

type Placement struct {
    Event   int
    Dx, Dy  float64
    Anchor  int      // new \an, 0 = keep
    Scale   float64  // 1.0 = no font shrink
    Layer   int
    Hide    bool
    Cost    float64
}
```

### 5.2. Passes

1. **Parse/normalize.** Parse all event types for analysis (`Dialogue`, `Comment`, others), but only `Dialogue` is written as today. Keep a byte-exact `[Script Info]`.
2. **Classify role** per event: drawing (`\p`), sign (explicit position, or sign style used without position), dialogue. Use provenance known at merge time; classifier only as fallback.
3. **Scale secondary geometry** when PlayRes differs: style numbers, event margins, and every inline geometric tag (`\fs`, `\bord`, `\shad`, `\blur`, `\fsp`, `\pos`, `\move`, `\org`, drawing coordinates). Never scale `\fad`, `\t` times, `\fscx/\fsy`, or colors.
4. **Role-safe alignment.** Remap alignment only for events with no `\pos/\move/\org/\p`. For styles, either clone dialogue styles per track or add an explicit `\an` to every unpositioned dialogue event and leave the style table alone. Do not rewrite `\an` inside positioned events.
5. **Estimate boxes.** Width from per-character advances (ASCII 0.5 em, CJK 1.0 em, space 0.3 em) x fontsize x `\fscx`; height fontsize x 1.2 x lines x `\fscy`; anchor from `\an`/pos/margins; for rotated text use the AABB of the rotated corners. Mark `Exact=false`.
6. **Detect conflicts.** For all pairs: time overlap > 0 and box intersection area ratio > 2% (dialogue) or > 5% (sign). Keep-out boxes: primary/secondary dialogue, already placed signs, 5% safe-area border, and optional video saliency. Union intervals with gaps <= 100 ms.
7. **Place.** Candidate list is bounded: original, `y +/- (h+gap)` for 1..3 lanes, `x +/- (w+gap)` for 1..2 lanes; never more than 25% of PlayResY from the original. Pick the lowest-cost candidate.
8. **Order/layer.** Sort `(start, layer, roleRank, end, track, originalIndex)`; assign dialogue layer 1, signs layer 0 by default; expose a per-window "raise signs" override. Pin every moved sign with explicit `\pos` so libass stacking cannot reorder it.
9. **Write.** Preserve event order chosen in pass 8; do not stable-sort again.

### 5.3. Cost function and thresholds

```
cost = 10 * dialogueOverlapArea
     +  8 * signOverlapArea
     +  6 * outOfSafeAreaArea
     +  2 * edgeEnergy(newBox)          // 0..1, from low-res frame
     +  0.5 * displacement2 / maxStep2
     +  1 * distanceFromOriginalAnchor / maxStep
     +  0.5 * (1 - importance)           // moving unimportant things is cheap/noisy
```

`edgeEnergy` is optional Phase 0: sample 4-8 frames per conflict window at 320x180, Sobel magnitude mean inside the candidate box. Prefer flatter background, but reject a candidate if it covers a face-region heuristic only when a detector exists. Without a detector, the honest claim is: this avoids **text-like and high-detail** regions, not faces specifically.

Thresholds, calibrated from the 81 human fixes:

| Rule | Value | Evidence |
|------|-------|----------|
| Do not auto-move | duration < 12 frames (0.50 s at 23.976 fps) | the shortest event the human fixed was 12 frames; none of the 11 signs at 8 frames or less was touched |
| Do not rewrite coordinates of moving labels | `\move` and group >= 3 same-start labels | E02 0:01:10 group; hand changed none |
| Auto-move only signs | `RoleSign` | 81/81 >20 px fixes were positioned signs |
| Max displacement | 160 px at 360p (25% PlayResY), 25% of width horizontally | largest accepted fixes 265 px at 360p for a full-height wall sign; require manual review above 160 px |
| Max font shrink | 80% (20% reduction) | no human style/font edits; shrinking is a last resort |
| Retime limit | +/- 6 frames (0.25 s) | no human retimes existed; keep conservative |
| Auto-apply confidence | cost improvement > 2x and displacement <= 60 px | 81 fixes have median 72 frames and 13 chars; short/small fixes are the uncertain ones |

`importance = 0.35*min(durFrames/72,1) + 0.25*min(nchars/20,1) + 0.15*min(fontsize/baseFont,1.5)/1.5 + 0.15*(RoleSign) + 0.10*(1 - moving)`. Leave it if `importance < 0.25` or duration < 12 frames. The phone `Symbol` (8 frames, 6 chars) scores ~0.08 and is left; `1,800,000 yen` (long duration, 12 chars, high referent value) scores ~0.65 and is moved.

### 5.4. Existing functions to change

| Function | Change |
|----------|--------|
| `mapAlignment(n, secondary)` (`ass.go:226`) | keep pure mapping, but call it only for unpositioned dialogue. Add `mapAlignmentPreservingAnchor` for the rare sign case. |
| `remapTextAlignments(text, secondary)` (`ass.go:286`) | add `role`/`positioned` argument; return text unchanged when the event has `\pos/\move/\org/\p`. Do not add `\an` to signs. |
| `transformStyleAlignment` (`ass.go:324`) | only for styles whose events are dialogue (or clone sign styles and leave alignment). |
| `scaleStyle` / `scaleDialogueMargins` (`ass.go:342`, `350`) | replace with `scaleEventGeometry` covering all tags in 5.2 step 3; share one scalar helper. |
| `mergeAndWrite` (`ass.go:424`) | split into `prepareEvents` (classify, scale, align), `resolveConflicts` (detect/place/order), `writeASS`. The current stable-sort by `startCS` becomes the tuple sort in 5.2 step 8. |
| `rescaleFactor` (`core.go:367`) | keep, but pass both PlayResX and PlayResY and warn when aspect ratios differ; currently only Y is used. |

## 6. Conflict-window UX

**Compute.** Union time intervals of conflicting pairs (5.2 step 6), split at event boundaries, merge gaps <= 100 ms. Representative frame = the time inside the window maximizing `sum(overlapArea * importance)`; clamp to `start+0.15s .. end-0.15s`. Each window carries: start/end, peak time, event IDs, kind, severity, current boxes, candidate placements with costs, recommended index.

**Screen.** A two-pane "Review conflicts" view after merge:

```
+---------------------------------------------------------------+
| 12 conflicts found   [Auto-fix safe ones]   [Export anyway]   |
+----------------------------+----------------------------------+
|                            |  Conflict 4/12   0:21:01.61      |
|   video frame at peak      |  Sign over dialogue (severity 82)|
|   with generated subs      |  [Before] [After]                |
|   (mpv, paused)            |  Recommended: move sign up 46 px  |
|                            |  [ Fix ]  [ Other options v ]    |
|                            |  [ ] Apply to all sign/dialogue  |
|                            |  [ ] Apply to all episodes       |
+----------------------------+----------------------------------+
| 1 0:00:00  disclaimer  4  |  2 0:12:26 phone labels  6  ...  |
+---------------------------------------------------------------+
```

States: `scanning` (progress from Go), `review` (window selected), `preview` (before/after toggled), `applied` (undo stack), `exported`. The list is sorted by severity; high-confidence auto-fixes are pre-checked.

**Fix options**, all computed by Go and previewed on the same frame: nudge lane (up/down/left/right), flip anchor (preserve box), shrink font (80/90%), retime (+/-1..6 frames), hide one event, accept. Batch checkboxes: same class ("all sign overlaps"), same style/font, same episode, whole series. "Auto-fix safe ones" applies every placement with `cost improvement > 2x` and `displacement <= 60 px`, then leaves the rest.

**Keyboard:** Up/Down window, Left/Right candidate, Enter apply, Space before/after, U undo, A apply-all-similar, S skip, Esc close. First-run default: one Enter on the recommended fix. The common case is **one click**.

**Go -> UI contract:** `conflicts: [{start,end,peak,kind,severity,events:[{id,track,role,bbox,text}],candidates:[{dx,dy,anchor,scale,cost,recommended}],diff:{before,after}}]`, plus `apply: [{windowID, candidateIndex}]` for the write-back. The UI never edits ASS text; it selects placement IDs, and Go re-renders the final file deterministically.

## 7. Two complete sets (both languages with dialogue + signs)

The model generalizes if role/provenance are per event and placement is per track:

- **Lane ownership.** Reserve the bottom band for the primary dialogue, top band for the secondary dialogue, and side/top sign lanes for signs. If both tracks carry signs, each sign is owned by the track that carries it; do not duplicate the same translation. If both translate the same burned-in sign, assign by nearest referent (same time, nearest original anchor) and show only the higher-priority track's sign, or offset the other by one lane.
- **Sign-to-referent assignment.** Match sign to referent by temporal overlap and proximity to the original anchor; a sign that moves should keep its relation to the same scene text. The phone labels show the failure mode: moving the translation right is correct only because the referent (Japanese label) is left of the number.
- **Priority.** Viewer picks language A (primary, bottom, larger) and B (secondary, top); optional "signs from A only" toggle. Per-language lanes and a priority number replace the hardcoded primary/secondary map.
- **What survives unchanged:** parser, PlayRes normalization, role classifier, conflict detection, placement search, cost function, ordering/layer policy, and the entire conflict-window UX. What changes: lane configuration, sign ownership, and the duplicate-sign policy.
- **Extra data needed:** per-track role labels (or a reliable classifier), the viewer's language pair/priority, and optionally audio language to infer the "local" track. Without provenance, sign ownership is the hard case: two tracks of signs at the same timestamps require a user choice or a conservative "show both, offset" default.

## 8. Open questions

- Does the user always want E01/E02-style 1080p output? The hand files upscale 640x360 to 1920x1080 by ×3; this is not a code bug but a document preference. Add an output PlayRes setting; do not infer it from the secondary track.
- The exact "~5 frame" phone sign was not found. Closest is E02 0:01:10.63 (`Symbol`, 8 frames). Confirm whether the user meant that group or another file.
- Cost weights are calibrated on 81 fixes from one show. Validate on the Cyberpunk hand files in the same media directory before trusting the auto-apply thresholds.
- Exact bboxes via libass are not implemented. Phase 0 estimates; a later pass can render each event on a transparent canvas and detect ink bounds, or bind libass `ass_render_event` via cgo.
- Face/important-object avoidance is not solved. Low-res edge energy and safe areas are honest but insufficient; a face detector is a later plugin, not a Phase 0 promise.
- Draw-order default (dialogue over signs) matches E04 but not E05. The per-window override is required; no global rule fits both.

## 9. Reproduction

```bash
cd /home/mdm/dev/subsandwicher
go build -o /tmp/opencode/kurumi/subsandwicher .

V='/mnt/l/!today/[Trix] FX Fighter Kurumi-chan S01E01 [WEBRip 1080p AV1] (Dual Audio, Multi Subs).mkv'
/tmp/opencode/kurumi/subsandwicher merge --video="$V" \
  --primary-index=19 --secondary-index=3 --out=/tmp/opencode/kurumi/E01.generated.ass

# E02: 20/3; E03: 20/4; E04: 20/4; E05: 20/3 (verify with probe first)
/tmp/opencode/kurumi/subsandwicher probe --video="$V"

cd /tmp/opencode/kurumi
python3 analyze.py E01.generated.ass E01.hand.ass E01.generated.stdout.json E01
python3 stats.py                 # report.json, summary.csv
python3 classifier_eval.py       # classifier.json, classifier.csv
python3 render_pairs.py          # frames/*.png, generated vs hand
```

Artifacts: `analyze.py`, `stats.py`, `classifier_eval.py`, `render_pairs.py`, `report.json`, `classifier.json`, `conflict_windows.json`, `summary.csv`, `E0*.generated.ass`, `E0*.generated.stdout.json`, `E0*.signs.ass`, `E0*.mirror.ass`, `E0*.hand.ass` (copies, read-only source), `frames/*_pair.png` under `/tmp/opencode/kurumi/`. Media files were never written.
