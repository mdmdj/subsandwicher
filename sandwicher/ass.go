package sandwicher

import (
	"fmt"
	"math"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// canonical V4+ style field order used when writing the merged file
var canonicalStyleFields = []string{
	"Name", "Fontname", "Fontsize", "PrimaryColour", "SecondaryColour",
	"OutlineColour", "BackColour", "Bold", "Italic", "Underline", "StrikeOut",
	"ScaleX", "ScaleY", "Spacing", "Angle", "BorderStyle", "Outline", "Shadow",
	"Alignment", "MarginL", "MarginR", "MarginV", "Encoding",
}

var canonicalEventFields = []string{
	"Layer", "Start", "End", "Style", "Name", "MarginL", "MarginR", "MarginV", "Effect", "Text",
}

var styleDefaults = map[string]string{
	"name": "Default", "fontname": "Arial", "fontsize": "16",
	"primarycolour": "&H00FFFFFF", "secondarycolour": "&H000000FF",
	"outlinecolour": "&H00000000", "backcolour": "&H00000000",
	"bold": "0", "italic": "0", "underline": "0", "strikeout": "0",
	"scalex": "100", "scaley": "100", "spacing": "0", "angle": "0",
	"borderstyle": "1", "outline": "2", "shadow": "2",
	"alignment": "2", "marginl": "10", "marginr": "10", "marginv": "10",
	"encoding": "1",
}

var (
	reAn        = regexp.MustCompile(`\\an(\d+)`)
	reA         = regexp.MustCompile(`\\a(\d+)`)
	reResetRole = regexp.MustCompile(`\\r([^\\}]*)`)
)

// Dialogue is a parsed Dialogue: line.
type Dialogue struct {
	Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text string
	startCS                                                                 int
}

// ASSStyle is a parsed Style: line as a lowercased field -> raw value map.
type ASSStyle struct {
	Val map[string]string
}

func (s *ASSStyle) name() string { return strings.TrimSpace(s.Val["name"]) }

// ASSDoc is a parsed .ass document, keeping raw [Script Info] verbatim.
type ASSDoc struct {
	InfoRaw   []string
	Info      map[string]string
	Styles    []*ASSStyle
	Dialogues []*Dialogue
	Other     [][]string // any other sections (e.g. [Fonts]), raw, in order
}

func parseASSFile(path string) (*ASSDoc, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseASS(data), nil
}

func parseASS(data []byte) *ASSDoc {
	text := string(data)
	text = strings.TrimPrefix(text, "\uFEFF")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	doc := &ASSDoc{Info: map[string]string{}}
	section := ""
	var fields []string
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			name := strings.ToLower(strings.TrimSpace(trimmed[1 : len(trimmed)-1]))
			switch {
			case strings.Contains(name, "script info"):
				section = "info"
			case strings.Contains(name, "styles"):
				section = "styles"
				fields = nil
			case strings.Contains(name, "events"):
				section = "events"
				fields = nil
			default:
				section = "other"
				doc.Other = append(doc.Other, []string{trimmed})
			}
			continue
		}
		switch section {
		case "info":
			doc.InfoRaw = append(doc.InfoRaw, line)
			if k, v, ok := splitKeyVal(trimmed); ok {
				doc.Info[strings.ToLower(k)] = v
			}
		case "styles":
			lower := strings.ToLower(trimmed)
			switch {
			case strings.HasPrefix(lower, "format:"):
				fields = splitCSV(lower[len("format:"):])
			case strings.HasPrefix(lower, "style:"):
				vals := splitCSV(trimmed[len("style:"):])
				st := &ASSStyle{Val: map[string]string{}}
				for i, f := range fields {
					if i >= len(vals) {
						break
					}
					st.Val[f] = strings.TrimSpace(vals[i])
				}
				doc.Styles = append(doc.Styles, st)
			}
		case "events":
			lower := strings.ToLower(trimmed)
			switch {
			case strings.HasPrefix(lower, "format:"):
				fields = splitCSV(lower[len("format:"):])
			case strings.HasPrefix(lower, "dialogue:"):
				doc.Dialogues = append(doc.Dialogues, parseDialogue(fields, trimmed[len("dialogue:"):]))
			}
		case "other":
			doc.Other[len(doc.Other)-1] = append(doc.Other[len(doc.Other)-1], trimmed)
		}
	}
	return doc
}

func splitKeyVal(s string) (string, string, bool) {
	i := strings.Index(s, ":")
	if i < 0 {
		return "", "", false
	}
	return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+1:]), true
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func parseDialogue(fields []string, rest string) *Dialogue {
	if len(fields) == 0 {
		fields = []string{"layer", "start", "end", "style", "name", "marginl", "marginr", "marginv", "effect", "text"}
	}
	vals := strings.SplitN(rest, ",", len(fields))
	get := func(name string) string {
		for i, f := range fields {
			if f == name && i < len(vals) {
				return strings.TrimSpace(vals[i])
			}
		}
		return ""
	}
	d := &Dialogue{
		Layer:   get("layer"),
		Start:   get("start"),
		End:     get("end"),
		Style:   get("style"),
		Name:    get("name"),
		MarginL: get("marginl"),
		MarginR: get("marginr"),
		MarginV: get("marginv"),
	}
	if d.Layer == "" {
		d.Layer = get("marked") // SSA v4 events use "Marked" instead of "Layer"
	}
	if d.Layer == "" {
		d.Layer = "0"
	}
	if d.Style == "" {
		d.Style = "Default"
	}
	// text is the last field and may contain commas: join any overflow back
	for i, f := range fields {
		if f == "text" && i < len(vals) {
			d.Text = strings.TrimSpace(vals[i])
		}
	}
	d.startCS = parseTimeCS(d.Start)
	return d
}

func parseTimeCS(t string) int {
	t = strings.TrimSpace(t)
	if t == "" {
		return 0
	}
	neg := strings.HasPrefix(t, "-")
	t = strings.TrimPrefix(t, "-")
	parts := strings.Split(t, ":")
	var h, m int
	for i := 0; i < len(parts)-1; i++ {
		n, _ := strconv.Atoi(strings.TrimSpace(parts[i]))
		switch i {
		case len(parts) - 2:
			m = n
		case len(parts) - 3:
			h = n
		}
	}
	secs, _ := strconv.ParseFloat(strings.TrimSpace(parts[len(parts)-1]), 64)
	cs := int(math.Round(((float64(h)*60+float64(m))*60 + secs) * 100))
	if neg {
		cs = -cs
	}
	return cs
}

// mapAlignment re-targets a V4+ \an style alignment (1-3 bottom, 4-6 middle, 7-9 top),
// keeping the horizontal anchor except for middle rows, which get forced to the
// left (primary) or right (secondary) column.
func mapAlignment(n int, secondary bool) int {
	if n < 1 || n > 9 {
		return n
	}
	if secondary {
		if n <= 3 {
			return n + 6 // bottom -> top
		}
		if n <= 6 {
			return 6 // middle -> middle-right
		}
		return n
	}
	if n >= 7 {
		return n - 6 // top -> bottom
	}
	if n >= 4 {
		return 4 // middle -> middle-left
	}
	return n
}

// mapLegacyAlignment handles the SSA \a tag encoding (1-3 bottom, 5-7 middle, 9-11 top).
func mapLegacyAlignment(n int, secondary bool) int {
	hor, vert := 0, 0 // vert: 0 bottom, 1 middle, 2 top
	switch {
	case n >= 1 && n <= 3:
		hor = n
	case n >= 5 && n <= 7:
		hor, vert = n-4, 1
	case n >= 9 && n <= 11:
		hor, vert = n-8, 2
	default:
		return n
	}
	if secondary {
		if vert == 0 {
			vert = 2
		}
		if vert == 1 {
			hor = 3
		}
	} else {
		if vert == 2 {
			vert = 0
		}
		if vert == 1 {
			hor = 1
		}
	}
	switch vert {
	case 1:
		return hor + 4
	case 2:
		return hor + 8
	default:
		return hor
	}
}

func remapTextAlignments(text string, secondary bool) string {
	text = reAn.ReplaceAllStringFunc(text, func(m string) string {
		n, err := strconv.Atoi(m[3:])
		if err != nil {
			return m
		}
		return `\an` + strconv.Itoa(mapAlignment(n, secondary))
	})
	return reA.ReplaceAllStringFunc(text, func(m string) string {
		n, err := strconv.Atoi(m[2:])
		if err != nil {
			return m
		}
		return `\a` + strconv.Itoa(mapLegacyAlignment(n, secondary))
	})
}

func renameResetStyles(text string, renames map[string]string) string {
	return reResetRole.ReplaceAllStringFunc(text, func(m string) string {
		name := strings.TrimSpace(m[2:])
		if name == "" {
			return m
		}
		if nn, ok := renames[name]; ok {
			return `\r` + nn
		}
		return m
	})
}

func styleAlignment(s *ASSStyle) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(s.Val["alignment"]))
	if err != nil {
		return 0, false
	}
	return n, true
}

func transformStyleAlignment(s *ASSStyle, secondary bool) {
	if n, ok := styleAlignment(s); ok {
		s.Val["alignment"] = strconv.Itoa(mapAlignment(n, secondary))
	}
}

func scaleNum(v string, f float64) string {
	x, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil {
		return v
	}
	n := int(math.Round(x * f))
	if n < 0 {
		n = 0
	}
	return strconv.Itoa(n)
}

func scaleStyle(s *ASSStyle, f float64) {
	for _, k := range []string{"fontsize", "spacing", "outline", "shadow", "marginl", "marginr", "marginv"} {
		if v, ok := s.Val[k]; ok && strings.TrimSpace(v) != "" {
			s.Val[k] = scaleNum(v, f)
		}
	}
}

func scaleDialogueMargins(d *Dialogue, f float64) {
	for _, p := range []*string{&d.MarginL, &d.MarginR, &d.MarginV} {
		*p = strings.TrimSpace(*p)
		if *p == "" {
			continue
		}
		if n, err := strconv.Atoi(*p); err == nil && n != 0 {
			*p = scaleNum(*p, f)
		}
	}
}

func playResY(doc *ASSDoc) int {
	n, _ := strconv.Atoi(strings.TrimSpace(doc.Info["playresy"]))
	return n
}

func rescaleFactor(prim, sec *ASSDoc) float64 {
	py := playResY(prim)
	if py <= 0 {
		py = 288 // renderer default
	}
	sy := playResY(sec)
	if sy <= 0 {
		sy = 288
	}
	f := float64(py) / float64(sy)
	if math.Abs(f-1) < 0.001 {
		return 1
	}
	return f
}

// buildSecondaryRenames computes old -> new style names for the secondary track,
// resolving collisions against primary names, other secondary names and
// already-assigned suffixes. It also renames the styles in place.
func buildSecondaryRenames(prim, sec *ASSDoc, suffix string) map[string]string {
	taken := map[string]bool{}
	for _, s := range prim.Styles {
		if n := s.name(); n != "" {
			taken[strings.ToLower(n)] = true
		}
	}
	for _, s := range sec.Styles {
		if n := s.name(); n != "" {
			taken[strings.ToLower(n)] = true
		}
	}
	renames := map[string]string{}
	for _, s := range sec.Styles {
		old := s.name()
		if old == "" {
			continue
		}
		nn := old + suffix
		for taken[strings.ToLower(nn)] {
			nn += suffix
		}
		renames[old] = nn
		taken[strings.ToLower(nn)] = true
		s.Val["name"] = nn
	}
	return renames
}

// LineEvent is a merged dialogue line exposed to GUI clients, in seconds.
type LineEvent struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Lang  string  `json:"lang"` // which track the line came from
	Text  string  `json:"text,omitempty"`
}

// mergeAndWrite applies role-safe transformations, orders the events, writes
// the merged .ass and returns the per-event decision log for the job.
func mergeAndWrite(prim, sec *ASSDoc, lang2 string, scale float64, outPath string, job JobInfo, exact bool, progress ...func(Progress)) (nStyles, nLines int, events []LineEvent, rep *Report, err error) {
	var reportProgress func(Progress)
	if len(progress) > 0 {
		reportProgress = progress[0]
	}
	playW, playH := playRes(prim)

	primStyles := map[string]*ASSStyle{}
	for _, s := range prim.Styles {
		primStyles[s.name()] = s
	}
	secStyles := map[string]*ASSStyle{}
	for _, s := range sec.Styles {
		secStyles[s.name()] = s
	}

	type preEvent struct {
		d          *Dialogue
		track      Track
		role       Role
		oldStyle   string
		finalStyle string
		origAlign  int
	}

	// Pass 1: classify before any transform, and remember which styles carry
	// dialogue. A style is re-anchored only if dialogue uses it.
	var pres []*preEvent
	styleHasDialogue := map[string]bool{}
	for _, d := range prim.Dialogues {
		st := primStyles[d.Style]
		orig := styleInt(st, "alignment")
		if n, ok := inlineAlign(d.Text); ok {
			orig = n
		}
		role := classifyRole(d, d.Style)
		pres = append(pres, &preEvent{d: d, track: TrackPrimary, role: role, oldStyle: d.Style, finalStyle: d.Style, origAlign: orig})
		if role == RoleDialogue {
			styleHasDialogue[d.Style] = true
		}
	}
	for _, d := range sec.Dialogues {
		st := secStyles[d.Style]
		orig := styleInt(st, "alignment")
		if n, ok := inlineAlign(d.Text); ok {
			orig = n
		}
		role := classifyRole(d, d.Style)
		pres = append(pres, &preEvent{d: d, track: TrackSecondary, role: role, oldStyle: d.Style, finalStyle: d.Style, origAlign: orig})
		if role == RoleDialogue {
			styleHasDialogue[d.Style] = true
		}
	}

	renames := buildSecondaryRenames(prim, sec, "-"+lang2)
	for old, nn := range renames {
		if styleHasDialogue[old] {
			styleHasDialogue[nn] = true
		}
	}

	// Pass 2: transform styles. Dialogue styles move; sign-only styles keep
	// their anchor. Secondary geometry is rescaled either way.
	for _, s := range prim.Styles {
		if styleHasDialogue[s.name()] {
			transformStyleAlignment(s, false)
		}
	}
	for _, s := range sec.Styles {
		if styleHasDialogue[s.name()] {
			transformStyleAlignment(s, true)
		}
		if scale != 1 {
			scaleStyle(s, scale)
		}
	}

	styles := map[string]*ASSStyle{}
	for _, s := range prim.Styles {
		styles[s.name()] = s
	}
	for _, s := range sec.Styles {
		styles[s.name()] = s
	}

	// Pass 3: write event text with role-safe rules.
	plan := make([]*plannedEvent, 0, len(pres))
	for _, p := range pres {
		d := p.d
		isSec := p.track == TrackSecondary
		if isSec {
			if nn, ok := renames[p.oldStyle]; ok {
				d.Style = nn
				p.finalStyle = nn
			}
		}
		pe := &plannedEvent{
			d: d, track: p.track, role: p.role, styleName: p.finalStyle,
			origAlign: p.origAlign,
		}

		if p.role == RoleDialogue {
			before := p.origAlign
			after := mapAlignment(before, isSec)
			if before >= 1 && before <= 9 && before != after {
				pe.add(Decision{
					Rule: "align.dialogue-remapped", Kind: KindAlignment,
					Level: LevelInfo, Action: "remapped",
					Before: strconv.Itoa(before), After: strconv.Itoa(after),
					Reason: "dialogue is re-anchored into its track's lane", Confidence: 0.95,
					Surface: SurfaceAuto,
				})
			}
			d.Text = remapTextAlignments(d.Text, isSec)
		} else {
			// Positioned and drawing events keep their author's anchor. If the
			// style itself was re-anchored for dialogue, pin the original.
			if styleHasDialogue[p.finalStyle] && !hasInlineAlign(d.Text) && p.origAlign >= 1 && p.origAlign <= 9 {
				d.Text = fmt.Sprintf(`{\an%d}`, p.origAlign) + d.Text
				pe.add(Decision{
					Rule: "align.sign-style-preserved", Kind: KindAlignment,
					Level: LevelNotice, Action: "pinned",
					Before: strconv.Itoa(p.origAlign), After: strconv.Itoa(p.origAlign),
					Reason:     "style is shared with dialogue; the sign keeps its original anchor",
					Confidence: 0.9, Surface: SurfaceAuto,
				})
			} else {
				pe.add(Decision{
					Rule: "align.positioned-preserved", Kind: KindAlignment,
					Level: LevelInfo, Action: "kept",
					Reason:     "signs and drawings are exempt from alignment remapping",
					Confidence: 0.95, Surface: SurfaceNone,
				})
			}
		}

		if isSec {
			d.Text = renameResetStyles(d.Text, renames)
			if scale != 1 {
				var tags []string
				d.Text, tags = scaleInlineGeometry(d.Text, scale)
				if len(tags) > 0 {
					pe.add(Decision{
						Rule: "scale.inline-geometry", Kind: KindScale,
						Level: LevelNotice, Action: "scaled",
						After:      strings.Join(tags, ","),
						Reason:     fmt.Sprintf("inline geometry scaled by %.4f to the primary PlayRes", scale),
						Confidence: 0.9, Surface: SurfaceAuto,
					})
				}
				pe.add(Decision{
					Rule: "scale.track", Kind: KindScale,
					Level: LevelInfo, Action: "scaled",
					Reason:     fmt.Sprintf("secondary geometry normalized by %.4f", scale),
					Confidence: 0.9, Surface: SurfaceAuto,
				})
				scaleDialogueMargins(d, scale)
			}
		}

		pe.add(Decision{
			Rule: "role.classified", Kind: KindRole, Level: LevelInfo, Action: "kept",
			Reason: fmt.Sprintf("classified as %s on the %s track from position/style, not labels",
				p.role.String(), p.track.String()),
			Confidence: 0.85, Surface: SurfaceNone,
		})
		plan = append(plan, pe)
	}

	// Old order (stable by start, primary first) is the baseline for logging.
	old := make([]*plannedEvent, len(plan))
	copy(old, plan)
	sort.SliceStable(old, func(i, j int) bool { return old[i].d.startCS < old[j].d.startCS })
	for i, p := range old {
		p.origOrder = i
	}

	// New order: explicit and deterministic. Signs draw below dialogue at the
	// same start, which reproduces the E04 hand fix.
	sort.SliceStable(plan, func(i, j int) bool {
		a, b := plan[i], plan[j]
		if a.d.startCS != b.d.startCS {
			return a.d.startCS < b.d.startCS
		}
		al, bl := atoiDefault(a.d.Layer, 0), atoiDefault(b.d.Layer, 0)
		if al != bl {
			return al < bl
		}
		if roleRank(a.role) != roleRank(b.role) {
			return roleRank(a.role) < roleRank(b.role)
		}
		ae, be := parseTimeCS(a.d.End), parseTimeCS(b.d.End)
		if ae != be {
			return ae < be
		}
		if a.track != b.track {
			return a.track < b.track
		}
		return a.origOrder < b.origOrder
	})
	for i, p := range plan {
		p.order = i
		if p.order != p.origOrder {
			p.add(Decision{
				Rule: "order.role-priority", Kind: KindOrder,
				Level: LevelNotice, Action: "moved",
				Before: strconv.Itoa(p.origOrder), After: strconv.Itoa(p.order),
				Reason:     "deterministic tie-break: signs before dialogue at the same start",
				Confidence: 0.8, Surface: SurfaceAuto,
			})
		}
	}

	// Base font per track, for the importance score.
	baseFont := map[Track]float64{}
	countsBy := map[Track]map[int]int{TrackPrimary: {}, TrackSecondary: {}}
	for _, p := range plan {
		if p.role != RoleDialogue {
			continue
		}
		fs := int(styleFloat(styles[p.styleName], "fontsize", 20))
		countsBy[p.track][fs]++
	}
	for tr, counts := range countsBy {
		best, n := 0, 0
		for fs, c := range counts {
			if c > n || (c == n && fs > best) {
				best, n = fs, c
			}
		}
		if best > 0 {
			baseFont[tr] = float64(best)
		}
	}
	measurePlan(plan, styles, playW, playH, baseFont)

	// Line events for the GUI preview picker.
	for _, p := range plan {
		lang := "primary"
		if p.track == TrackSecondary {
			lang = "secondary"
		}
		events = append(events, LineEvent{
			Start: float64(p.d.startCS) / 100,
			End:   float64(parseTimeCS(p.d.End)) / 100,
			Lang:  lang,
			Text:  assTextPreview(p.d.Text),
		})
	}

	job.PlayResX, job.PlayResY = playW, playH
	job.Scale = scale
	job.Styles = len(prim.Styles) + len(sec.Styles)
	job.Lines = len(plan)

	if err := writeMergedASS(prim, sec, plan, outPath); err != nil {
		return 0, 0, nil, nil, err
	}
	emitProgress(reportProgress, Progress{Stage: "analyze", Overall: stageAnalyzeStart, Indeterminate: true,
		Message: "detecting conflicts and scoring placements…"})
	var ex *exactContext
	if exact {
		ex = newExactContext(outPath, playW, playH)
		ex.setProgress(func(n int) {
			emitProgress(reportProgress, Progress{Stage: "exact", Overall: stageExactStart, Indeterminate: true,
				Message: fmt.Sprintf("rendering exact ink masks (%d)…", n)})
		})
		defer ex.close()
	}
	rep = buildReport(plan, job, playW, playH, ex)
	if ex != nil {
		ok, n, note := ex.exactSummary()
		rep.Job.Exact, rep.Job.ExactEvents, rep.Job.ExactNote = ok, n, note
	}
	return len(prim.Styles) + len(sec.Styles), len(plan), events, rep, nil
}

func roleRank(r Role) int {
	if r == RoleDialogue {
		return 1
	}
	return 0
}

func atoiDefault(s string, def int) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return def
	}
	return n
}

func writeMergedASS(prim, sec *ASSDoc, plan []*plannedEvent, outPath string) error {
	var b strings.Builder
	b.WriteString("[Script Info]\n")
	for _, l := range prim.InfoRaw {
		if strings.TrimSpace(l) == "" {
			continue
		}
		b.WriteString(l)
		b.WriteByte('\n')
	}

	b.WriteString("\n[V4+ Styles]\n")
	b.WriteString("Format: " + strings.Join(canonicalStyleFields, ", ") + "\n")
	for _, s := range prim.Styles {
		writeStyleLine(&b, s)
	}
	for _, s := range sec.Styles {
		writeStyleLine(&b, s)
	}

	b.WriteString("\n[Events]\n")
	b.WriteString("Format: " + strings.Join(canonicalEventFields, ", ") + "\n")
	for _, p := range plan {
		d := p.d
		fmt.Fprintf(&b, "Dialogue: %s,%s,%s,%s,%s,%s,%s,%s,%s,%s\n",
			d.Layer, d.Start, d.End, d.Style, d.Name, d.MarginL, d.MarginR, d.MarginV, d.Effect, d.Text)
	}
	for _, other := range prim.Other {
		b.WriteString("\n" + strings.Join(other, "\n") + "\n")
	}

	return os.WriteFile(outPath, []byte(b.String()), 0o644)
}

var (
	reTagBlock = regexp.MustCompile(`\{\\[^}]*\}`)
	reTag      = regexp.MustCompile(`\\[nN]`)
)

// assTextPreview strips ASS override tags and converts line breaks for a
// human-readable preview shown in the UI stream list.
func assTextPreview(text string) string {
	text = reTagBlock.ReplaceAllString(text, "")
	text = reTag.ReplaceAllString(text, " ")
	return strings.TrimSpace(text)
}

func writeStyleLine(b *strings.Builder, s *ASSStyle) {
	vals := make([]string, len(canonicalStyleFields))
	for i, f := range canonicalStyleFields {
		v, ok := s.Val[strings.ToLower(f)]
		if !ok || v == "" {
			v = styleDefaults[strings.ToLower(f)]
		}
		vals[i] = v
	}
	b.WriteString("Style: ")
	b.WriteString(strings.Join(vals, ","))
	b.WriteByte('\n')
}

// scaleInlineGeometry multiplies every inline geometric override value by f.
// ASS coordinates and sizes live in script space; when a track is normalized
// from another PlayRes, all of them must move together. Timing, percentages
// and colours are left alone.
func scaleInlineGeometry(text string, f float64) (string, []string) {
	var scaled []string
	seen := map[string]bool{}
	mark := func(name string) {
		if !seen[name] {
			seen[name] = true
			scaled = append(scaled, name)
		}
	}
	scaleNum := func(s string) string {
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return s
		}
		return formatNum(v * f)
	}

	var b strings.Builder
	last := 0
	inDrawing := false
	for _, loc := range reOverrideBlock.FindAllStringIndex(text, -1) {
		payload := text[last:loc[0]]
		if inDrawing {
			if np := reDrawingNum.ReplaceAllStringFunc(payload, scaleNum); np != payload {
				payload = np
				mark("drawing")
			}
		}
		b.WriteString(payload)

		block := text[loc[0]:loc[1]]
		block = rePosTag.ReplaceAllStringFunc(block, func(m string) string {
			s := rePosTag.FindStringSubmatch(m)
			mark("pos")
			return `\pos(` + scaleNum(s[1]) + `,` + scaleNum(s[2]) + `)`
		})
		block = reOrgTag.ReplaceAllStringFunc(block, func(m string) string {
			s := reOrgTag.FindStringSubmatch(m)
			mark("org")
			return `\org(` + scaleNum(s[1]) + `,` + scaleNum(s[2]) + `)`
		})
		block = reMoveTag.ReplaceAllStringFunc(block, func(m string) string {
			s := reMoveTag.FindStringSubmatch(m)
			mark("move")
			if s[5] != "" && s[6] != "" {
				return `\move(` + scaleNum(s[1]) + `,` + scaleNum(s[2]) + `,` +
					scaleNum(s[3]) + `,` + scaleNum(s[4]) + `,` + s[5] + `,` + s[6] + `)`
			}
			return `\move(` + scaleNum(s[1]) + `,` + scaleNum(s[2]) + `,` +
				scaleNum(s[3]) + `,` + scaleNum(s[4]) + `)`
		})
		block = reScaleSizeTag.ReplaceAllStringFunc(block, func(m string) string {
			s := reScaleSizeTag.FindStringSubmatch(m)
			mark(s[1])
			return `\` + s[1] + scaleNum(s[2])
		})
		block = reScaleFsTag.ReplaceAllStringFunc(block, func(m string) string {
			s := reScaleFsTag.FindStringSubmatch(m)
			mark("fs")
			return `\fs` + scaleNum(s[1])
		})
		if m := reDrawingTag.FindStringSubmatch(block); m != nil {
			if n, err := strconv.Atoi(m[1]); err == nil {
				inDrawing = n > 0
			}
		}
		b.WriteString(block)
		last = loc[1]
	}
	tail := text[last:]
	if inDrawing {
		if np := reDrawingNum.ReplaceAllStringFunc(tail, scaleNum); np != tail {
			tail = np
			mark("drawing")
		}
	}
	b.WriteString(tail)
	return b.String(), scaled
}

func formatNum(v float64) string {
	s := strconv.FormatFloat(v, 'f', 3, 64)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	if s == "" || s == "-" {
		return "0"
	}
	return s
}
