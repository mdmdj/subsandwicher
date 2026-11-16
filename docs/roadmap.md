# Roadmap

Status after the E06 review sessions (2026-11-11). See
[`decision-log.md`](decision-log.md) for the report/decision-log design,
[`review-feedback.md`](review-feedback.md) for the session notes, and
[`subtitle-conflict-analysis.md`](subtitle-conflict-analysis.md) for the
original evidence.

## Done

### Core merge and analysis

- [x] Positioned signs and drawings are exempt from alignment remapping;
      sign-only styles keep their anchor; shared styles get an explicit `\an`
      pin.
- [x] Complete inline geometry scaling to the primary PlayRes (`\fs`, `\bord`,
      `\shad`, `\blur`, `\fsp`, `\pos`, `\move`, `\org`, drawing coordinates).
- [x] Deterministic order: `(start, layer, sign-before-dialogue, end, track,
      original index)`, logged per event.
- [x] Title-scored track selection: full dialogue beats signs/forced/SDH.
- [x] No-label track classifier (lane, unique font, script, size) with
      low-confidence surfacing; corpus regression test passes at 100% on used
      `-eng` styles.
- [x] Estimated boxes: character advances, explicit breaks, wrapping, anchors,
      `\pos`/`\move` sampling, rotation AABB.
- [x] Exact libass ink masks via ffmpeg: per-event alpha render, exact bbox,
      mask-based conflict validation and nudge cost; estimated fallback and
      `--no-exact` escape hatch.
- [x] Conflict detection: time + area thresholds, causes, severity, peak,
      edge/safe-area checks, short/low-importance suppression.
- [x] Per-event candidate groups for every conflicting event (including
      primary dialogue, review-only), plus **every other event active in the
      window** for spacing nudges, and a per-event `seek` inside the actual
      conflict interval.
- [x] Combined x+y nudge candidates, with zero-dialogue-overlap candidates
      ranked ahead of cheaper colliding ones.
- [x] Paired moving signs expose `related` event IDs, keep the sibling's
      single-axis moves past the top-six cut, and let the UI promote the exact
      move chosen for the sibling (`same move as #N`).
- [x] Video-frame readability: downscaled RGB sampling at each group's seek
      combines edge density with the event's text colour, so candidates can
      overlap shapes when contrast is good while low-contrast or very busy
      placements remain penalised.
- [x] Sign clusters: `related` now spans same-style, same-lane signs in the
      window; cluster-wide moves are retained and marked `sharedWith`, so the
      same delta (for example `dy +44`) is available for every label.
- [x] Pause control that holds the current frame and reloads subtitle fixes
      against it without seeking.
- [x] Loading progress covers the whole pipeline (probe, both extractions,
      merge, conflict analysis, exact masks, decision log, review); stages
      without a reliable fraction use an animated busy bar instead of 100%.
- [x] Placement: bounded nudges, font shrink, hide, raise; whole-`\move` path
      translation; keep-outs from all active events; cost/improvement;
      auto-apply gate (60 px, 2x) and manual-review cap (160 px).
- [x] Applying a candidate: `apply` CLI for nudge/shrink/hide/raise; no fixes
      copies byte-for-byte.

### Decision log and review workflow

- [x] Per-event decision log with kinds, rules, levels, surface policy,
      confidence, before/after and reasons; summaries by level/surface/kind/
      rule/cause.
- [x] `merge --report`, `report --ass`, and `apply` CLI commands.
- [x] Saved review session next to the video: report, chosen fixes, applied
      changes, comments and statuses.
- [x] Load state is a full-window drop target; probe failures roll back to it
      with a friendly inline message and no modal.
- [x] Setup flow: drop zone replaced by a filename/Eject bar; language summary
      with Change languages; Continue action.
- [x] Review mode fills the window; sticky playback controls.
- [x] Per-event fixes with Original; per-event preview seek; preview working
      copy reloaded through `sub-reload`.
- [x] Playback: Play window/Stop, loop, mute by default with unmute, 0.25x-2x
      speed, Random frame.
- [x] Playback continues while changing candidates; changing windows or
      Preview window pauses and seeks.
- [x] Window status: open/changed/done/check again, status filter, Mark done
      auto-advance, per-window comments.
- [x] Copy state / copy all with chosen fixes, comments, statuses, report and
      raw ASS lines.
- [x] Portable Windows build via `tools/build-windows.sh`.

### Testing

- [x] Go unit tests for parsing/merge/apply/boxes/conflicts/groups/seeks.
- [x] Opt-in corpus test (`SUBS_CORPUS_DIR`) for the track classifier.
- [x] jsdom renderer harness covering setup, review, multi-event fixes,
      playback, statuses, copy and save.

## Polish — from the real review feedback

Priority order. These are refinements to the current model, not new
architecture.

1. **Retime candidates.** Windows 9 and 11: Vietnamese dialogue should snap to
   the overlapping English line's timing. Add a `retime` suggestion that
   adjusts `start`/`end` within a small cap (proposed ±6 frames), previews it,
   and applies it through `apply`. Show original/current timings in the group.
2. **Colour-aware placement and saliency.** Window 11: the sign is styled to
   match a coloured box, and the best space is below the original text once the
   transform is considered. Sample a few frames near the event, prefer regions
   whose colour/high-frequency content matches the sign's style, and add the
   edge-energy term already sketched in the cost function. Keep safe areas and
   active-event keep-outs. Face detection stays a separate optional plugin.
3. **Adjacent-line group nudge.** Window 1: nudging one line should optionally
   move its neighbour so spacing stays consistent. Add a "move with adjacent
   line(s)" candidate that translates a small set of events together and costs
   the group as a whole.
4. **Batch and auto-apply.** Add an "Auto-fix safe ones" action that applies all
   `autoApply` candidates, and batch-by-cause actions ("all secondary sign vs
   dialogue"). The flags and per-event groups already exist; this is UI plus a
   fix list.
5. **Ignore / not important.** Window 10: "voice call" was not worth saving.
   Add an explicit `ignored` review status (or an importance override) so a
   window can be dismissed without pretending it was fixed.
6. **Review-session persistence.** Statuses/comments/chosen fixes currently
   live in memory until save or copy. Autosave a draft `.review.json` (or
   restore from the last session) so an interrupted review can resume.
7. **Playback polish.** Recompute the play-once timer when speed changes
   mid-play; remember mute/speed/loop between windows; optional frame-step
   buttons while paused; keyboard shortcuts for play/stop and next window.
8. **Review-list polish.** Sort by status/severity/cause, collapse done
   windows, keep filters across selections, j/k navigation, and a compact
   "N of M" position indicator. Candidate-panel scroll is remembered per
   window and a first visit starts at the top.
9. **Track-selection UI.** Surface the title score/reason and let the user
    pick the signs-only track deliberately (the merge core already accepts
    explicit indices).

## Next — larger efforts

- **Two complete sign sets.** Both languages carrying dialogue and signs needs
  per-language lanes, sign ownership (nearest referent), and a priority policy
  for duplicate translations. The conflict and placement models should survive.
- **Main-window preview / overlay.** The open architectural decision from
  [`rune-evaluation.md`](rune-evaluation.md): still-frame preview inside the
  app, or a transparent UI layer over mpv. The per-event seek/box data is
  already the right input for either.
- **Subtitle rendering independence.** If the product needs interactive
  subtitle text in-app (drag, inline editing), move subtitle rendering to a
  libass-backed layer rather than relying on mpv's pixels. Deferred until the
  editing scope is confirmed.
- **Apply history / undo.** Keep an ordered fix log in the session so any
  applied candidate can be reverted individually; the `applied` array is the
  seed.
- **Cross-platform packaging.** Linux/macOS builds once the Windows flow is
  stable; the mpv control path is already platform-neutral.

## Deliberately deferred

- Rune port (pre-release UI framework) — revisit after the review workflow is
  stable; see `rune-evaluation.md` for the spike/kill criteria.
- Heavy face/object detection.
- Automatic correct placement without user confirmation.
- Full ASS style/effect authoring.
