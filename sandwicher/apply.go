package sandwicher

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// Fix selects one candidate from a review window. SuggestionIndex -1 means
// "keep the original", which is accepted and ignored.
//
// Override is the UI's manual escape hatch: it replaces individual dimensions
// of the chosen candidate, so a candidate that only moves x can gain a y move,
// or a shrink can be made larger/smaller than the generated options.
type Fix struct {
	WindowID        int          `json:"windowId"`
	SuggestionIndex int          `json:"suggestionIndex"`
	Override        *FixOverride `json:"override,omitempty"`
	Comment         string       `json:"comment,omitempty"`
}

// FixOverride carries manually edited candidate dimensions. Nil fields keep the
// candidate's generated value, so an override only records what the user
// changed. At is an absolute anchor; when it is set it wins over dx/dy for
// static events.
type FixOverride struct {
	Kind      string   `json:"kind,omitempty"`
	Dx        *float64 `json:"dx,omitempty"`
	Dy        *float64 `json:"dy,omitempty"`
	FontScale *float64 `json:"fontScale,omitempty"`
	At        *Point   `json:"at,omitempty"`
}

// AppliedFix describes what a fix did.
type AppliedFix struct {
	WindowID        int          `json:"windowId"`
	SuggestionIndex int          `json:"suggestionIndex"`
	Event           int          `json:"event"`
	Kind            string       `json:"kind"`
	Before          string       `json:"before,omitempty"`
	After           string       `json:"after,omitempty"`
	Reason          string       `json:"reason,omitempty"`
	Override        *FixOverride `json:"override,omitempty"`
	Comment         string       `json:"comment,omitempty"`
	// New geometry, so the UI can warn when a manual or generated fix creates
	// a new safe-area or frame problem.
	BBox                Box     `json:"bbox,omitempty"`
	SafeOutside         float64 `json:"safeOutside,omitempty"`
	OriginalSafeOutside float64 `json:"originalSafeOutside,omitempty"`
	IntroducedSafeArea  bool    `json:"introducedSafeArea,omitempty"`
	FrameOutside        float64 `json:"frameOutside,omitempty"`
	IntroducedFrame     bool    `json:"introducedFrame,omitempty"`
	OutOfFrame          bool    `json:"outOfFrame,omitempty"`
}

// ApplyResult is the CLI `apply` payload.
type ApplyResult struct {
	Out     string       `json:"out"`
	Applied []AppliedFix `json:"applied"`
	// RemainingConflicts is an estimate-only recheck of the written file,
	// filtered to conflicts that touch an event changed by this apply call.
	RemainingConflicts []Conflict `json:"remainingConflicts,omitempty"`
}

var (
	reAlphaTag = regexp.MustCompile(`\\alpha[^\\}]*`)
	reLayer    = regexp.MustCompile(`^-?\d+$`)
)

// ApplyFixes reads a merged .ass and a report, applies the selected candidate
// for each window, and writes the result. With no fixes it copies the input
// byte-for-byte.
func ApplyFixes(assPath, reportPath, outPath string, fixes []Fix) (*ApplyResult, error) {
	if len(fixes) == 0 {
		data, err := os.ReadFile(assPath)
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(outPath, data, 0o644); err != nil {
			return nil, err
		}
		return &ApplyResult{Out: outPath}, nil
	}
	if reportPath == "" {
		return nil, fmt.Errorf("apply requires a report")
	}
	data, err := os.ReadFile(reportPath)
	if err != nil {
		return nil, fmt.Errorf("reading report: %w", err)
	}
	var rep Report
	if err := json.Unmarshal(data, &rep); err != nil {
		return nil, fmt.Errorf("parsing report: %w", err)
	}
	doc, err := parseASSFile(assPath)
	if err != nil {
		return nil, err
	}
	styles := map[string]*ASSStyle{}
	for _, s := range doc.Styles {
		styles[s.name()] = s
	}
	byID := map[int]*ConflictWindow{}
	for i := range rep.Windows {
		byID[rep.Windows[i].ID] = &rep.Windows[i]
	}

	res := &ApplyResult{Out: outPath}
	playW, playH := rep.Job.PlayResX, rep.Job.PlayResY
	if playW <= 0 || playH <= 0 {
		playW, playH = playRes(doc)
	}
	for _, fix := range fixes {
		if fix.SuggestionIndex < 0 {
			continue
		}
		w := byID[fix.WindowID]
		if w == nil {
			return nil, fmt.Errorf("window %d not found in report", fix.WindowID)
		}
		if fix.SuggestionIndex >= len(w.Suggestions) {
			return nil, fmt.Errorf("window %d has no suggestion %d", fix.WindowID, fix.SuggestionIndex)
		}
		s := w.Suggestions[fix.SuggestionIndex]
		if s.Event < 0 || s.Event >= len(doc.Dialogues) {
			return nil, fmt.Errorf("window %d suggestion %d references event %d outside the file", fix.WindowID, fix.SuggestionIndex, s.Event)
		}
		s = applyOverride(s, fix.Override)
		d := doc.Dialogues[s.Event]
		before := d.Text
		st := styles[d.Style]
		switch s.Kind {
		case "nudge":
			if s.At == nil && !reMoveTag.MatchString(d.Text) {
				return nil, fmt.Errorf("window %d suggestion %d has no target anchor", fix.WindowID, fix.SuggestionIndex)
			}
			applyPosition(d, s, st, playW, playH)
		case "shrink":
			if s.FontScale <= 0 {
				return nil, fmt.Errorf("window %d suggestion %d has invalid font scale", fix.WindowID, fix.SuggestionIndex)
			}
			fs := tagFloat(reFsTag, d.Text, styleFloat(styles[d.Style], "fontsize", 20))
			d.Text = setFs(d.Text, fs*s.FontScale)
			if s.At != nil {
				d.Text = setPos(d.Text, *s.At)
				d.Text = scaleOrg(d.Text, *s.At, s.FontScale)
			}
		case "hide":
			d.Text = setAlphaHidden(d.Text)
		case "raise":
			layer := atoiDefault(d.Layer, 0)
			d.Layer = strconv.Itoa(layer + 1)
			applyPosition(d, s, st, playW, playH)
		case "top":
			d.Layer = strconv.Itoa(topLayer(doc.Dialogues) + 1)
			applyPosition(d, s, st, playW, playH)
		default:
			return nil, fmt.Errorf("window %d suggestion %d has unknown kind %q", fix.WindowID, fix.SuggestionIndex, s.Kind)
		}
		res.Applied = append(res.Applied, AppliedFix{
			WindowID: fix.WindowID, SuggestionIndex: fix.SuggestionIndex, Event: s.Event,
			Kind: s.Kind, Before: before, After: d.Text, Reason: s.Reason,
			Override: fix.Override, Comment: fix.Comment,
		})
	}
	if len(res.Applied) == 0 {
		// Everything was "original"; keep the file byte-identical.
		orig, err := os.ReadFile(assPath)
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(outPath, orig, 0o644); err != nil {
			return nil, err
		}
		return res, nil
	}

	// Measure the result so the UI can warn about fixes that leave the safe
	// area or the video frame. The report's bbox is the pre-fix geometry.
	original := map[int]Box{}
	for _, e := range rep.Events {
		original[e.ID] = e.BBox
	}
	changed := map[int]bool{}
	for i := range res.Applied {
		af := &res.Applied[i]
		changed[af.Event] = true
		if af.Event < 0 || af.Event >= len(doc.Dialogues) {
			continue
		}
		d := doc.Dialogues[af.Event]
		g := measureEvent(d, styles[d.Style], playW, playH)
		start := float64(d.startCS) / 100
		end := float64(parseTimeCS(d.End)) / 100
		b := g.lifetimeBox(start, end, playW, playH)
		af.BBox = roundBox(b)
		af.SafeOutside = round4(safeOutsideFraction(b, playW, playH))
		af.FrameOutside = round4(frameOutsideFraction(b, playW, playH))
		af.OutOfFrame = af.FrameOutside >= 0.999
		if ob, ok := original[af.Event]; ok && ob.Valid() {
			af.OriginalSafeOutside = round4(safeOutsideFraction(ob, playW, playH))
			oldFrame := frameOutsideFraction(ob, playW, playH)
			af.IntroducedSafeArea = af.OriginalSafeOutside <= 0.02 && af.SafeOutside > 0.02
			af.IntroducedFrame = oldFrame <= 0.005 && af.FrameOutside > 0.005
		} else {
			af.IntroducedSafeArea = af.SafeOutside > 0.02
			af.IntroducedFrame = af.FrameOutside > 0.005
		}
	}
	res.RemainingConflicts = recheckConflicts(doc, &rep, changed)

	if err := writeDoc(doc, outPath); err != nil {
		return nil, err
	}
	return res, nil
}

// applyPosition applies the optional positional part of a fix. Top/raise
// candidates normally have no position, but a manual override can combine them
// with a move so dragging an already-raised event still works.
//
// If the event has an \org, the rotation origin must move with the text.
// Changing \pos alone would leave the rendered shape rotating around the old
// origin, so a rotated/skewed label jumps or skews instead of translating.
func applyPosition(d *Dialogue, s Suggestion, st *ASSStyle, playW, playH float64) {
	if reMoveTag.MatchString(d.Text) {
		if s.Dx != 0 || s.Dy != 0 {
			d.Text = moveBy(d.Text, s.Dx, s.Dy)
			d.Text = translateOrg(d.Text, s.Dx, s.Dy)
		}
		return
	}
	if s.At == nil {
		return
	}
	old, ok := eventAnchor(d, st, playW, playH)
	if ok {
		d.Text = translateOrg(d.Text, s.At.X-old.X, s.At.Y-old.Y)
	}
	d.Text = setPos(d.Text, *s.At)
}

// eventAnchor is the alignment point the event currently uses, before a fix.
func eventAnchor(d *Dialogue, st *ASSStyle, playW, playH float64) (Point, bool) {
	if p := tagPoint(rePosTag, d.Text); p != nil {
		return *p, true
	}
	if playW <= 0 || playH <= 0 {
		return Point{}, false
	}
	align := styleInt(st, "alignment")
	if n, ok := inlineAlign(d.Text); ok {
		align = n
	}
	if align < 1 || align > 9 {
		align = 2
	}
	ml := styleFloat(st, "marginl", 10)
	mr := styleFloat(st, "marginr", 10)
	mv := styleFloat(st, "marginv", 10)
	if v, err := strconv.Atoi(strings.TrimSpace(d.MarginL)); err == nil && v != 0 {
		ml = float64(v)
	}
	if v, err := strconv.Atoi(strings.TrimSpace(d.MarginR)); err == nil && v != 0 {
		mr = float64(v)
	}
	if v, err := strconv.Atoi(strings.TrimSpace(d.MarginV)); err == nil && v != 0 {
		mv = float64(v)
	}
	var x, y float64
	switch align % 3 {
	case 1:
		x = ml
	case 0:
		x = playW - mr
	default:
		x = playW / 2
	}
	switch {
	case align <= 3:
		y = playH - mv
	case align <= 6:
		y = playH / 2
	default:
		y = mv
	}
	return Point{X: x, Y: y}, true
}

// translateOrg shifts a \org tag by the same delta as the text. Events without
// \org are returned unchanged.
func translateOrg(text string, dx, dy float64) string {
	if dx == 0 && dy == 0 {
		return text
	}
	return reOrgTag.ReplaceAllStringFunc(text, func(m string) string {
		s := reOrgTag.FindStringSubmatch(m)
		if len(s) < 3 {
			return m
		}
		x, err1 := strconv.ParseFloat(s[1], 64)
		y, err2 := strconv.ParseFloat(s[2], 64)
		if err1 != nil || err2 != nil {
			return m
		}
		return fmt.Sprintf(`\org(%s,%s)`, formatNum(x+dx), formatNum(y+dy))
	})
}

// scaleOrg keeps a rotation origin in the same position relative to the text
// when a shrink fix changes the font size around anchor.
func scaleOrg(text string, anchor Point, scale float64) string {
	if scale <= 0 || scale == 1 {
		return text
	}
	return reOrgTag.ReplaceAllStringFunc(text, func(m string) string {
		s := reOrgTag.FindStringSubmatch(m)
		if len(s) < 3 {
			return m
		}
		x, err1 := strconv.ParseFloat(s[1], 64)
		y, err2 := strconv.ParseFloat(s[2], 64)
		if err1 != nil || err2 != nil {
			return m
		}
		return fmt.Sprintf(`\org(%s,%s)`,
			formatNum(anchor.X+(x-anchor.X)*scale), formatNum(anchor.Y+(y-anchor.Y)*scale))
	})
}

// applyOverride merges a manual edit into a generated candidate. Position edits
// translate the candidate's anchor by the change in dx/dy, so adding a y move to
// an x-only candidate behaves the way the editor shows it.
func applyOverride(s Suggestion, ov *FixOverride) Suggestion {
	if ov == nil {
		return s
	}
	if ov.Kind != "" {
		s.Kind = ov.Kind
	}
	baseDx, baseDy := s.Dx, s.Dy
	if ov.Dx != nil {
		s.Dx = *ov.Dx
	}
	if ov.Dy != nil {
		s.Dy = *ov.Dy
	}
	if ov.FontScale != nil {
		s.FontScale = *ov.FontScale
	}
	if ov.At != nil {
		p := *ov.At
		s.At = &p
	} else if s.At != nil && (s.Dx != baseDx || s.Dy != baseDy) {
		p := Point{X: s.At.X + (s.Dx - baseDx), Y: s.At.Y + (s.Dy - baseDy)}
		s.At = &p
	}
	return s
}

// topLayer returns the highest layer in use, so the caller can draw above it.
func topLayer(dialogues []*Dialogue) int {
	max := 0
	seen := false
	for _, d := range dialogues {
		l := atoiDefault(d.Layer, 0)
		if !seen || l > max {
			max = l
			seen = true
		}
	}
	return max
}

// recheckConflicts re-detects estimated overlaps in the written file and keeps
// only conflicts that touch an event changed by this apply call. It uses the
// report's track/role metadata and the same geometry estimator as merge.
func recheckConflicts(doc *ASSDoc, rep *Report, changed map[int]bool) []Conflict {
	if len(changed) == 0 {
		return nil
	}
	styles := map[string]*ASSStyle{}
	for _, s := range doc.Styles {
		styles[s.name()] = s
	}
	playW, playH := rep.Job.PlayResX, rep.Job.PlayResY
	if playW <= 0 || playH <= 0 {
		playW, playH = playRes(doc)
	}
	events := make([]*plannedEvent, len(doc.Dialogues))
	for i, d := range doc.Dialogues {
		p := &plannedEvent{d: d, styleName: d.Style, order: i, origOrder: i}
		if i < len(rep.Events) {
			p.track = trackFromReport(rep.Events[i].Track)
			p.role = roleFromReport(rep.Events[i].Role)
		} else {
			p.role = classifyRole(d, d.Style)
		}
		events[i] = p
	}
	measurePlan(events, styles, playW, playH, map[Track]float64{})
	all := detectConflicts(events, playW, playH)
	var out []Conflict
	for _, c := range all {
		for _, id := range c.Events {
			if changed[id] {
				out = append(out, c)
				break
			}
		}
	}
	return out
}

func roleFromReport(s string) Role {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "sign":
		return RoleSign
	case "drawing":
		return RoleDrawing
	default:
		return RoleDialogue
	}
}

func trackFromReport(s string) Track {
	if strings.EqualFold(strings.TrimSpace(s), "secondary") {
		return TrackSecondary
	}
	return TrackPrimary
}

func setPos(text string, p Point) string {
	tag := fmt.Sprintf(`\pos(%s,%s)`, formatNum(p.X), formatNum(p.Y))
	if rePosTag.MatchString(text) {
		return rePosTag.ReplaceAllString(text, tag)
	}
	return "{" + tag + "}" + text
}

func setFs(text string, fs float64) string {
	tag := `\fs` + formatNum(fs)
	if reScaleFsTag.MatchString(text) {
		return reScaleFsTag.ReplaceAllString(text, tag)
	}
	return "{" + tag + "}" + text
}

func setAlphaHidden(text string) string {
	if reAlphaTag.MatchString(text) {
		return reAlphaTag.ReplaceAllString(text, `\alpha&HFF&`)
	}
	return `{\alpha&HFF&}` + text
}

func writeDoc(doc *ASSDoc, outPath string) error {
	var b strings.Builder
	b.WriteString("[Script Info]\n")
	for _, l := range doc.InfoRaw {
		if strings.TrimSpace(l) == "" {
			continue
		}
		b.WriteString(l)
		b.WriteByte('\n')
	}
	b.WriteString("\n[V4+ Styles]\n")
	b.WriteString("Format: " + strings.Join(canonicalStyleFields, ", ") + "\n")
	for _, s := range doc.Styles {
		writeStyleLine(&b, s)
	}
	b.WriteString("\n[Events]\n")
	b.WriteString("Format: " + strings.Join(canonicalEventFields, ", ") + "\n")
	for _, d := range doc.Dialogues {
		fmt.Fprintf(&b, "Dialogue: %s,%s,%s,%s,%s,%s,%s,%s,%s,%s\n",
			d.Layer, d.Start, d.End, d.Style, d.Name, d.MarginL, d.MarginR, d.MarginV, d.Effect, d.Text)
	}
	for _, other := range doc.Other {
		b.WriteString("\n" + strings.Join(other, "\n") + "\n")
	}
	return os.WriteFile(outPath, []byte(b.String()), 0o644)
}

// moveBy translates a whole \move path, preserving its timing. Moving both
// endpoints keeps an animated label in sync with the thing it follows.
func moveBy(text string, dx, dy float64) string {
	return reMoveTag.ReplaceAllStringFunc(text, func(m string) string {
		s := reMoveTag.FindStringSubmatch(m)
		vals := make([]float64, 4)
		for i := 0; i < 4; i++ {
			f, err := strconv.ParseFloat(s[i+1], 64)
			if err != nil {
				return m
			}
			vals[i] = f
		}
		x1 := formatNum(vals[0] + dx)
		y1 := formatNum(vals[1] + dy)
		x2 := formatNum(vals[2] + dx)
		y2 := formatNum(vals[3] + dy)
		if s[5] != "" && s[6] != "" {
			return `\move(` + x1 + `,` + y1 + `,` + x2 + `,` + y2 + `,` + s[5] + `,` + s[6] + `)`
		}
		return `\move(` + x1 + `,` + y1 + `,` + x2 + `,` + y2 + `)`
	})
}
