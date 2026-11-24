package sandwicher

import (
	"fmt"
	"math"
	"sort"
)

const (
	costDialogueOverlap = 10.0
	costSignOverlap     = 8.0
	costSafeArea        = 6.0
	costDisplacement    = 2.0
	placementGap        = 8.0

	// A candidate may not move a fully in-frame subtitle more than this much
	// of its area out of the frame. A subtitle that was already partially
	// outside may only get this many percentage points worse.
	frameOutLimitInFrame = 0.20
	frameOutLimitPartial = 0.05
)

// boxOutsideFraction reports how much of box lies outside keep.
func boxOutsideFraction(b, keep Box) float64 {
	a := b.Area()
	if a <= 0 {
		return 1
	}
	inside := b.Intersect(keep).Area()
	if inside < 0 {
		inside = 0
	}
	if inside > a {
		inside = a
	}
	return (a - inside) / a
}

// frameOutsideFraction reports how much of box lies outside the video frame.
func frameOutsideFraction(b Box, playW, playH float64) float64 {
	return boxOutsideFraction(b, Box{X0: 0, Y0: 0, X1: playW, Y1: playH})
}

// safeOutsideFraction reports how much of box lies outside the 5% safe area.
func safeOutsideFraction(b Box, playW, playH float64) float64 {
	return boxOutsideFraction(b, Box{X0: playW * 0.05, Y0: playH * 0.05, X1: playW * 0.95, Y1: playH * 0.95})
}

// keepsInFrame rejects candidates that push a subtitle completely out of the
// frame, or that lose substantially more of it than the original.
//
// For animated (\\move) subtitles the comparison uses the lifetime union, so a
// label that originally scrolled on and off screen keeps doing so: it is only
// rejected when the candidate makes its overall visible share worse.
func keepsInFrame(orig, cand Box, playW, playH float64) bool {
	if playW <= 0 || playH <= 0 {
		return true
	}
	if !cand.Valid() {
		return false
	}
	frame := Box{X0: 0, Y0: 0, X1: playW, Y1: playH}
	if cand.Intersect(frame).Area() <= 0 {
		return false // fully out of frame
	}
	origOut := frameOutsideFraction(orig, playW, playH)
	candOut := frameOutsideFraction(cand, playW, playH)
	if origOut < 0.005 {
		return candOut <= frameOutLimitInFrame
	}
	return candOut <= origOut+frameOutLimitPartial
}

// pairOverlapRatio compares two events, using ink masks when both are available
// at this time and falling back to box intersection otherwise.
func pairOverlapRatio(box Box, ev, o *plannedEvent, masks map[int]*inkMask, at, dx, dy, playW, playH float64) float64 {
	if masks != nil {
		if me, mo := masks[ev.order], masks[o.order]; me != nil && mo != nil {
			return maskOverlapRatio(me, mo, dx, dy)
		}
	}
	ob := o.geom.boxAt(at, o.startSec, o.endSec, playW, playH)
	inter := box.Intersect(ob)
	if !inter.Valid() {
		return 0
	}
	den := math.Min(box.Area(), ob.Area())
	if den <= 0 {
		return 0
	}
	return inter.Area() / den
}

// dialogueOverlapRatio is the worst remaining overlap with any active dialogue
// line. Candidates that clear dialogue entirely sort ahead of cheaper but still
// colliding ones.
func dialogueOverlapRatio(box Box, ev *plannedEvent, active []*plannedEvent, masks map[int]*inkMask, at, dx, dy, playW, playH float64) float64 {
	worst := 0.0
	for _, o := range active {
		if o == ev || o.role != RoleDialogue {
			continue
		}
		if r := pairOverlapRatio(box, ev, o, masks, at, dx, dy, playW, playH); r > worst {
			worst = r
		}
	}
	return worst
}

// placementCost scores a candidate box at time t. It uses every event that is
// active around t as a keep-out, not just the conflict partner.
func placementCost(box Box, ev *plannedEvent, active []*plannedEvent, masks map[int]*inkMask, at, playW, playH, dx, dy float64, video *videoContext) float64 {
	if !box.Valid() {
		return math.Inf(1)
	}
	cost := 0.0
	for _, o := range active {
		if o == ev {
			continue
		}
		r := pairOverlapRatio(box, ev, o, masks, at, dx, dy, playW, playH)
		if r <= 0 {
			continue
		}
		if o.role == RoleDialogue {
			cost += costDialogueOverlap * r
		} else {
			cost += costSignOverlap * r
		}
	}
	safe := Box{X0: playW * 0.05, Y0: playH * 0.05, X1: playW * 0.95, Y1: playH * 0.95}
	if a := box.Area(); a > 0 {
		outside := a - box.Intersect(safe).Area()
		if outside > 0 {
			cost += costSafeArea * outside / a
		}
	}
	maxStep := math.Hypot(playW, playH)
	if maxStep > 0 {
		cost += costDisplacement * math.Hypot(dx, dy) / maxStep
	}
	cost += videoReadabilityCost(video, at, box, ev.textColor, playW, playH)
	return cost
}

func anchorForBox(b Box, align int) Point {
	var x, y float64
	switch align % 3 {
	case 1:
		x = b.X0
	case 0:
		x = b.X1
	default:
		x = (b.X0 + b.X1) / 2
	}
	switch {
	case align <= 3:
		y = b.Y1
	case align <= 6:
		y = (b.Y0 + b.Y1) / 2
	default:
		y = b.Y0
	}
	return Point{X: x, Y: y}
}

func boxFromAnchor(p Point, w, h float64, align int) Box {
	var x0, y0 float64
	switch align % 3 {
	case 1:
		x0 = p.X
	case 0:
		x0 = p.X - w
	default:
		x0 = p.X - w/2
	}
	switch {
	case align <= 3:
		y0 = p.Y - h
	case align <= 6:
		y0 = p.Y - h/2
	default:
		y0 = p.Y
	}
	return Box{X0: x0, Y0: y0, X1: x0 + w, Y1: y0 + h}
}

func activeAt(events []*plannedEvent, t, margin float64) []*plannedEvent {
	var out []*plannedEvent
	for _, e := range events {
		if e.startSec-margin <= t && e.endSec+margin >= t {
			out = append(out, e)
		}
	}
	return out
}

// candidateSteps builds a deduplicated list of move distances: small absolute
// steps for fine adjustments, then multiples of the box size for larger jumps.
func candidateSteps(size, gap, scale, max float64, fine, multiples []float64) []float64 {
	var out []float64
	add := func(v float64) {
		if v <= 0 || v > max {
			return
		}
		for _, x := range out {
			if math.Abs(x-v) < 0.6 {
				return
			}
		}
		out = append(out, v)
	}
	for _, f := range fine {
		add(f * scale)
	}
	for _, m := range multiples {
		add(size*m + gap)
	}
	return out
}

// placeCandidates generates bounded translations and font shrinks for one
// event, scores them against every active event, and marks which are safe
// enough to apply without asking.
func placeCandidates(ev *plannedEvent, at float64, active []*plannedEvent, playW, playH float64, masks map[int]*inkMask, video *videoContext) []Suggestion {
	if ev.geom == nil {
		return nil
	}
	b0 := ev.geom.boxAt(at, ev.startSec, ev.endSec, playW, playH)
	if !b0.Valid() {
		return nil
	}
	orig := placementCost(b0, ev, active, masks, at, playW, playH, 0, 0, video)

	autoPX := 60.0 * (playH / 360.0)
	manualPX := 160.0 * (playH / 360.0)
	autoPXx := 60.0 * (playW / 640.0)
	manualPXx := 160.0 * (playW / 640.0)

	var out []Suggestion
	add := func(kind string, box Box, dx, dy float64, scale float64, reason string) {
		var cmasks map[int]*inkMask
		if kind == "nudge" {
			cmasks = masks
		}
		cost := placementCost(box, ev, active, cmasks, at, playW, playH, dx, dy, video)
		dialogue := dialogueOverlapRatio(box, ev, active, cmasks, at, dx, dy, playW, playH)
		if math.IsInf(cost, 1) {
			return
		}
		// Keep the subtitle visible. The lifetime box covers \move paths; for
		// static events it is just the box under review.
		origLife, candLife := b0, box
		if ev.moving && ev.bbox.Valid() {
			origLife = ev.bbox
			candLife = ev.bbox
			if kind == "nudge" {
				candLife = ev.bbox.Translate(dx, dy)
			}
		}
		if !keepsInFrame(origLife, candLife, playW, playH) {
			return
		}
		dist := math.Hypot(dx, dy)
		improvement := 0.0
		if cost > 0 && orig > 0 {
			improvement = orig / cost
		}
		var anchor *Point
		if !ev.moving {
			if p, ok := ev.geom.layoutAnchorAt(at, ev.startSec, ev.endSec, playW, playH); ok {
				p.X = round2(p.X + dx)
				p.Y = round2(p.Y + dy)
				anchor = &p
			}
		}
		s := Suggestion{
			Event:           ev.order,
			Kind:            kind,
			Dx:              round2(dx),
			Dy:              round2(dy),
			At:              anchor,
			FontScale:       scale,
			Cost:            round2(cost),
			OriginalCost:    round2(orig),
			Improvement:     round2(improvement),
			ManualReview:    dist > manualPX || math.Abs(dx) > manualPXx,
			DialogueOverlap: round4(dialogue),
			Reason:          reason,
		}
		if kind == "nudge" && ev.role != RoleDialogue {
			s.AutoApply = improvement >= 2 && dist <= autoPX && math.Abs(dx) <= autoPXx
		}
		out = append(out, s)
	}

	h, w := b0.H(), b0.W()
	maxY := playH * 0.5
	maxX := playW * 0.5
	// Fine steps cover "just a little more room" edits; the larger multiples
	// cover small labels that need a big jump (end credits, wall signs).
	stepsY := candidateSteps(h, placementGap, playH/360, maxY, []float64{6, 12, 20}, []float64{0.5, 1, 1.5, 2, 3, 4})
	stepsX := candidateSteps(w, placementGap, playW/640, maxX, []float64{6, 12, 20}, []float64{0.5, 1, 1.5, 2, 3})
	majorY := []float64{h*0.5 + placementGap, h + placementGap}
	majorX := []float64{w*0.5 + placementGap, w + placementGap}
	for _, d := range stepsY {
		add("nudge", b0.Translate(0, -d), 0, -d, 1, fmt.Sprintf("move up %.0f px clear of the overlap", d))
		add("nudge", b0.Translate(0, d), 0, d, 1, fmt.Sprintf("move down %.0f px clear of the overlap", d))
	}
	for _, d := range stepsX {
		add("nudge", b0.Translate(-d, 0), -d, 0, 1, fmt.Sprintf("move left %.0f px clear of the overlap", d))
		add("nudge", b0.Translate(d, 0), d, 0, 1, fmt.Sprintf("move right %.0f px clear of the overlap", d))
	}
	for _, dyStep := range majorY {
		for _, dxStep := range majorX {
			for _, sx := range []float64{-1, 1} {
				for _, sy := range []float64{-1, 1} {
					dx, dy := sx*dxStep, sy*dyStep
					if math.Abs(dx) > maxX || math.Abs(dy) > maxY {
						continue
					}
					add("nudge", b0.Translate(dx, dy), dx, dy, 1,
						fmt.Sprintf("move by %.0f,%.0f px clear of the overlap", dx, dy))
				}
			}
		}
	}
	for _, scale := range []float64{0.9, 0.8} {
		anchor, ok := ev.geom.layoutAnchorAt(at, ev.startSec, ev.endSec, playW, playH)
		if !ok {
			continue
		}
		nb := boxFromAnchor(anchor, w*scale, h*scale, ev.finalAlign)
		add("shrink", nb, 0, 0, scale, fmt.Sprintf("shrink text to %.0f%%", scale*100))
	}

	// Hide is only cheap for events the user was unlikely to read anyway.
	hideCost := 500.0
	if ev.importance < 0.30 || ev.durSec < 0.5 {
		hideCost = 60
	}
	out = append(out, Suggestion{
		Event: ev.order, Kind: "hide", Cost: hideCost, OriginalCost: round2(orig),
		Reason: "hide this event for the conflict window",
	})

	sort.SliceStable(out, func(i, j int) bool {
		if d := out[i].Cost - out[j].Cost; math.Abs(d) > 1e-9 {
			return d < 0
		}
		zi, zj := out[i].DialogueOverlap == 0, out[j].DialogueOverlap == 0
		if zi != zj {
			return zi
		}
		return out[i].Event < out[j].Event
	})
	return out
}

// signsRelated reports whether two signs belong to the same visual cluster:
// same style and lane inside one review window. Animated and static labels can
// be related, and they do not have to overlap in time; the point is that the
// same move can keep the cluster's relative layout intact.
func signsRelated(a, b *plannedEvent) bool {
	if a == nil || b == nil || a == b {
		return false
	}
	if a.role != RoleSign || b.role != RoleSign {
		return false
	}
	if a.styleName != b.styleName {
		return false
	}
	height := math.Abs(a.bbox.Y1 - a.bbox.Y0)
	if math.Abs(a.bbox.Y0-b.bbox.Y0) > math.Max(12, height) {
		return false
	}
	return true
}

func axisNudge(s Suggestion) bool {
	return s.Kind == "nudge" && ((s.Dx == 0) != (s.Dy == 0))
}

// suggestForWindow picks the movable participants in a window, generates
// candidates for each, and dedupes them into one ranked list.
func suggestForWindow(w ConflictWindow, byID map[int]*plannedEvent, conflicts map[int]Conflict, playW, playH float64, exact *exactContext, video *videoContext) ([]Suggestion, []EventSuggestions, int, int) {
	all := make([]*plannedEvent, 0, len(byID))
	for _, e := range byID {
		all = append(all, e)
	}
	// Every event that is on screen during the window can interact with the
	// fix, even when it does not directly overlap another event. Offer a group
	// for each so spacing nudges are possible.
	idSet := map[int]bool{}
	for _, e := range all {
		if e.endSec > w.Start && e.startSec < w.End {
			idSet[e.order] = true
		}
	}
	for _, id := range w.Events {
		idSet[id] = true
	}
	ids := make([]int, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		a, b := byID[ids[i]], byID[ids[j]]
		if a.startSec != b.startSec {
			return a.startSec < b.startSec
		}
		return a.order < b.order
	})

	type eventWork struct {
		ev         *plannedEvent
		seek       float64
		inConflict bool
		cands      []Suggestion
	}
	work := make([]*eventWork, 0, len(ids))
	deltas := map[int]map[[2]float64]bool{}
	for _, id := range ids {
		ev := byID[id]
		if ev == nil {
			continue
		}
		inConflict := containsInt(w.Events, id)
		seek := eventSeek(ev, w, conflicts)
		active := activeAt(all, seek, 0.2)
		maskIDs := append([]int{}, w.Events...)
		if !inConflict {
			maskIDs = append(maskIDs, id)
		}
		var masks map[int]*inkMask
		if exact != nil {
			masks = exact.masksAt(maskIDs, seek)
			if m := masks[ev.order]; m != nil {
				ev.exactBox = m.Box
				ev.exactSeek = seek
				ev.exactValid = true
				ev.exactCov = m.Coverage
				ev.samples = exact.samplesFor(ev, seek, m)
			}
		}
		cands := placeCandidates(ev, seek, active, playW, playH, masks, video)
		if inConflict && w.Kind == "sign-dialogue" && ev.role != RoleDialogue {
			cands = append(cands, Suggestion{
				Event: ev.order, Kind: "raise", Cost: 40,
				Reason: "draw the sign above dialogue; order-only fix",
			})
		}
		seen := map[string]bool{}
		var deduped []Suggestion
		for _, s := range cands {
			if !inConflict && s.Kind != "nudge" {
				continue
			}
			key := fmt.Sprintf("%s|%.1f|%.1f|%.2f", s.Kind, s.Dx, s.Dy, s.FontScale)
			if seen[key] {
				continue
			}
			seen[key] = true
			deduped = append(deduped, s)
		}
		deltas[ev.order] = map[[2]float64]bool{}
		for _, s := range deduped {
			if s.Kind == "nudge" {
				deltas[ev.order][[2]float64{round2(s.Dx), round2(s.Dy)}] = true
			}
		}
		work = append(work, &eventWork{ev: ev, seek: seek, inConflict: inConflict, cands: deduped})
	}

	related := map[int][]int{}
	for i := 0; i < len(work); i++ {
		for j := i + 1; j < len(work); j++ {
			a, b := work[i].ev, work[j].ev
			if signsRelated(a, b) {
				related[a.order] = append(related[a.order], b.order)
				related[b.order] = append(related[b.order], a.order)
			}
		}
	}

	var flat []Suggestion
	var groups []EventSuggestions
	for _, wk := range work {
		ev := wk.ev
		kept := append([]Suggestion{}, wk.cands...)
		// Record which related signs offer the same move. A candidate shared
		// by the whole cluster keeps every label's relative layout intact.
		for i := range kept {
			if kept[i].Kind != "nudge" {
				continue
			}
			key := [2]float64{round2(kept[i].Dx), round2(kept[i].Dy)}
			for _, rid := range related[ev.order] {
				if deltas[rid][key] {
					kept[i].SharedWith = append(kept[i].SharedWith, rid)
				}
			}
		}
		sharedAll := func(s Suggestion) bool {
			return len(related[ev.order]) > 0 && len(s.SharedWith) >= len(related[ev.order])
		}
		sort.SliceStable(kept, func(i, j int) bool {
			si, sj := sharedAll(kept[i]), sharedAll(kept[j])
			if si != sj {
				return si
			}
			if d := kept[i].Cost - kept[j].Cost; math.Abs(d) > 1e-9 {
				return d < 0
			}
			zi, zj := kept[i].DialogueOverlap == 0, kept[j].DialogueOverlap == 0
			if zi != zj {
				return zi
			}
			return kept[i].Event < kept[j].Event
		})
		if len(kept) > 6 {
			top := append([]Suggestion{}, kept[:6]...)
			chosen := map[[2]float64]bool{}
			add := func(s Suggestion) {
				key := [2]float64{round2(s.Dx), round2(s.Dy)}
				if chosen[key] {
					return
				}
				chosen[key] = true
				top = append(top, s)
			}
			for _, s := range top {
				chosen[[2]float64{round2(s.Dx), round2(s.Dy)}] = true
			}
			// Preserve the largest move in each direction even when it ranks
			// below cheaper small nudges. Small labels may need a large jump,
			// while the top six stay focused on "just clear the overlap".
			type direction struct{ x, y float64 }
			extremes := map[direction]Suggestion{}
			for _, s := range kept[6:] {
				if s.Kind != "nudge" || !axisNudge(s) {
					continue
				}
				sx := math.Copysign(1, s.Dx)
				if s.Dx == 0 {
					sx = 0
				}
				sy := math.Copysign(1, s.Dy)
				if s.Dy == 0 {
					sy = 0
				}
				d := direction{sx, sy}
				cur, ok := extremes[d]
				if !ok || math.Hypot(s.Dx, s.Dy) > math.Hypot(cur.Dx, cur.Dy) {
					extremes[d] = s
				}
			}
			for _, d := range []direction{{0, -1}, {0, 1}, {-1, 0}, {1, 0}} {
				if len(top) >= 18 {
					break
				}
				if s, ok := extremes[d]; ok {
					add(s)
				}
			}
			// Keep every single-axis move the cluster shares, not only those
			// that happen to rank in the top six. This is what makes the same
			// vertical nudge available for every label in a sign cluster.
			for _, s := range kept[6:] {
				if len(top) >= 18 {
					break
				}
				if !axisNudge(s) || len(s.SharedWith) == 0 {
					continue
				}
				add(s)
			}
			kept = top
		}
		group := EventSuggestions{Event: ev.order, Seek: wk.seek, Related: related[ev.order]}
		if !wk.inConflict {
			group.Note = "no direct conflict; manual spacing nudge"
		}
		if len(kept) == 0 {
			group.Note = "no automatic candidate for this event"
		} else {
			kept[0].Recommended = true
		}
		// "Move to top" changes draw order rather than position, so it stays
		// out of the cost ranking and the UI renders it as a button.
		kept = append(kept, Suggestion{
			Event: ev.order, Kind: "top", Cost: 0,
			Reason: "draw this event above every other subtitle",
		})
		for _, s := range kept {
			s.ID = len(flat)
			s.Seek = wk.seek
			flat = append(flat, s)
			group.SuggestionIDs = append(group.SuggestionIDs, s.ID)
		}
		groups = append(groups, group)
	}
	auto, manual := 0, 0
	for i := range flat {
		if flat[i].AutoApply {
			auto++
		}
		if flat[i].ManualReview {
			manual++
		}
	}
	return flat, groups, auto, manual
}

// eventSeek picks a moment inside the event where its conflict is actually
// visible. It uses the longest interval the event shares with one of the
// window's conflicts, so a later line does not preview at the window start.
func eventSeek(ev *plannedEvent, w ConflictWindow, conflicts map[int]Conflict) float64 {
	bestStart, bestEnd, bestDur := 0.0, 0.0, -1.0
	for _, cid := range w.Conflicts {
		c, ok := conflicts[cid]
		if !ok || !containsInt(c.Events, ev.order) {
			continue
		}
		start := math.Max(ev.startSec, c.Start)
		end := math.Min(ev.endSec, c.End)
		if end-start > bestDur {
			bestDur, bestStart, bestEnd = end-start, start, end
		}
	}
	if bestDur <= 0 {
		bestStart = math.Max(ev.startSec, w.Start)
		bestEnd = math.Min(ev.endSec, w.End)
	}
	if bestEnd <= bestStart {
		bestStart, bestEnd = ev.startSec, ev.endSec
	}
	span := bestEnd - bestStart
	if span <= 0 {
		return round2(bestStart)
	}
	t := bestStart + math.Min(0.3, span/2)
	t = math.Max(bestStart+0.03, math.Min(t, bestEnd-0.03))
	if t < 0 {
		t = 0
	}
	return round2(t)
}
