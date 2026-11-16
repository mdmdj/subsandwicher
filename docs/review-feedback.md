# Review feedback backlog

Notes from the E06 review session (2025-11-11), kept separate from the
implemented decision-log design.

## Worked well

- Per-event fix groups: a window with `Japanese yen`, `US dollar`, `Euro` and
  dialogue now lets each line be resolved independently.
- Whole-path translation for `\move` signs keeps animated labels in sync.

## Requested behaviour

- **Timing snap between the two language tracks.** Windows 9 and 11: "the viet
  subs should snap timing to the english subs", and "the two language dialog
  lines should have synced timing." Likely a per-event retime candidate
  (`start/end` matched to the overlapping partner) with a small frame cap.
- **Colour-aware sign placement.** Window 11: the sign is styled to match a
  coloured box; the ideal space is below the original text once the transform
  is taken into account. A later placement pass could sample the source frame
  around the sign and prefer background regions whose colour matches the sign
  style, instead of only edge energy. Exact libass ink masks now prevent padding
  from counting as an overlap; colour matching is still open.
- **Adjacent-line spacing.** Window 1: nudging a line up should optionally move
  the neighbouring line so spacing stays consistent. This needs a group nudge
  (event + adjacent events) rather than a single-event candidate.
- **Importance decisions.** Window 10: "voice call" was not worth saving, and a
  redundant name translation was accepted for reading consistency. The current
  importance score and "Original" option cover this, but a future "ignore this
  window" status could make it explicit.
- **Status flow.** Windows 14/15 were "no conflict, looks perfect"; the Done /
  Check again statuses in the UI are the first version of that workflow.

## Known gaps in this pass

- No retiming candidates yet.
- Placement samples background RGB and text colour for contrast; deliberately matching a sign to a coloured box (colour harmony) is still open.
- Adjacent-line grouping is not modelled.
- Exact ink masks exist and drop box-padding false positives; they render
  through ffmpeg's libass, so a missing style font can change the mask slightly.
- Sign ownership/priority for two complete sign sets is still deferred.

## E06 window 4 — paired moving signs (2025-11-11)

- `#31` (`US dollar`) and `#32` (`Euro`) are animated siblings with matching
  `\move` timing. Moving `#31` alone broke the pair, and `#32`'s useful vertical
  steps were cut from the top-six list, so there was no way to copy `#31`'s
  move. Fixed by linking the events (`related`), retaining sibling single-axis
  steps, and promoting the matching candidate with `same move as #31`.
- Candidates that used to land on the flags, the Japanese labels, or other busy
  footage were ranked highly because only subtitle ink was modelled. Placement
  now samples the video frame and charges for edge density under the candidate,
  so clear background wins over dense artwork. A later colour-matching pass can
  replace edge energy for signs that deliberately match a coloured box.

## E06 window 4 — colour awareness and cluster moves (2025-11-12)

- Edge-only saliency was too aggressive: it ruled out readable overlap with the
  flags and pushed candidates into distant open space. Placement now samples
  RGB and compares the event's text colour with the background, so detailed but
  high-contrast areas stay usable. Edge density is capped and reduced when
  contrast is good.
- The requested `dy +44` compromise is now a cluster move: `#26`, `#27`, `#31`
  and `#32` are related by style/lane, all retain the vertical steps, and
  candidates shared by the whole cluster are badged `shared by 4 signs`.
  Choosing `+44` for one promotes the same move at the top of every sibling
  group (`same move as #N`).
- Added a playback pause button. Pausing clears the loop and holds the current
  frame; choosing a fix while paused reloads the subtitle file without seeking,
  so the same image shows every candidate.

## E01 review session — manual overrides and group layout (2026-11-16)

Session output: `[Trix] FX Fighter Kurumi-chan S01E01 ...vie-eng.review.json`
(22 windows, 28 fixes, 14 manual overrides, all fixes applied).

- Half the selected fixes were manually edited. Static signs often needed an
  absolute position (window 6 moved to `230,241` to sit on the in-video
  “Financial Crisis” text); moving signs often needed a different delta or a
  larger jump than the candidate envelope offered (`-64` became `-101` in the
  end-credit group).
- Fine steps and larger multiples are now generated (6/12/20 pt plus 3x/4x box
  heights), and trimming keeps the largest move in each direction.
- Window 22 (“move group together”) needed moving six credit signs as one
  layout; related-sign linking only covers same-style/same-lane pairs. Added
  the multi-event Group move control with per-event overrides.
- Windows 18/19 were low-priority signs “already behind the dialog”. Do not
  suppress that case: a sign fully painted over by a later dialogue layer is
  now escalated to `required`. Signs that remain under review are unchanged.
- An unchanged or deliberately re-positioned fix can still conflict; apply
  results now include an estimated re-check of conflicts touching changed
  events, and the UI warns when a fix introduces a new safe-area/frame problem.
- Stacked dialogue in windows 3/16 is intentional for overlapping voices; the
  re-check is advisory, not an error.

## Preview click/drag (2026-11-16)

- mpv is a separate process, so Electron cannot read its mouse events directly.
  `mpv-drag.lua` uses forced `MBTN_LEFT`/`MOUSE_MOVE` bindings and
  `script-message` → JSON IPC `client-message` events.
- Mousedown pauses the preview, the app hit-tests the report boxes and sends the
  box back to the Lua script, which draws a translucent ghost. Release commits
  through the existing manual-override path.
- Shift locks the dominant axis, Alt scales the drag, arrows nudge, `R` resets
  the selected event. Drags snap to the frame, safe area, and neighboring
  subtitle boxes for single-event drags.
- Animated events are hit-tested with their exact ink box at the paused event
  seek. Dragging a moving event translates the whole `\move` path.
