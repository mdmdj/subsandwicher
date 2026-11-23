package sandwicher

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Role is what an event does on screen. It drives which transforms are safe:
// dialogue may be re-anchored, signs and drawings may not.
type Role int

const (
	RoleDialogue Role = iota
	RoleSign
	RoleDrawing
)

func (r Role) String() string {
	switch r {
	case RoleSign:
		return "sign"
	case RoleDrawing:
		return "drawing"
	default:
		return "dialogue"
	}
}

// Track is the merge provenance of an event.
type Track int

const (
	TrackPrimary Track = iota
	TrackSecondary
)

func (t Track) String() string {
	if t == TrackSecondary {
		return "secondary"
	}
	return "primary"
}

// Point is a script-space coordinate.
type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Box is an axis-aligned rectangle in script space.
type Box struct {
	X0 float64 `json:"x0"`
	Y0 float64 `json:"y0"`
	X1 float64 `json:"x1"`
	Y1 float64 `json:"y1"`
}

func (b Box) Valid() bool { return b.X1 > b.X0 && b.Y1 > b.Y0 }
func (b Box) W() float64  { return b.X1 - b.X0 }
func (b Box) H() float64  { return b.Y1 - b.Y0 }
func (b Box) Area() float64 {
	if !b.Valid() {
		return 0
	}
	return b.W() * b.H()
}

func (b Box) Intersect(o Box) Box {
	return Box{
		X0: math.Max(b.X0, o.X0),
		Y0: math.Max(b.Y0, o.Y0),
		X1: math.Min(b.X1, o.X1),
		Y1: math.Min(b.Y1, o.Y1),
	}
}

func (b Box) Union(o Box) Box {
	if !b.Valid() {
		return o
	}
	if !o.Valid() {
		return b
	}
	return Box{
		X0: math.Min(b.X0, o.X0),
		Y0: math.Min(b.Y0, o.Y0),
		X1: math.Max(b.X1, o.X1),
		Y1: math.Max(b.Y1, o.Y1),
	}
}

func (b Box) Inflate(d float64) Box {
	return Box{b.X0 - d, b.Y0 - d, b.X1 + d, b.Y1 + d}
}

func (b Box) Translate(dx, dy float64) Box {
	return Box{b.X0 + dx, b.Y0 + dy, b.X1 + dx, b.Y1 + dy}
}

// Move is an ASS \move target.
type Move struct {
	X1, Y1, X2, Y2 float64
	T1, T2         int // milliseconds from the event start; 0 means the whole line
}

func (m Move) at(tMs, durMs int) (float64, float64) {
	t1, t2 := m.T1, m.T2
	if t2 <= t1 {
		t1, t2 = 0, durMs
	}
	if t2 <= 0 {
		return m.X2, m.Y2
	}
	f := float64(tMs-t1) / float64(t2-t1)
	f = math.Max(0, math.Min(1, f))
	return m.X1 + (m.X2-m.X1)*f, m.Y1 + (m.Y2-m.Y1)*f
}

var (
	reOverrideBlock = regexp.MustCompile(`\{[^}]*\}`)
	reInlineAn      = regexp.MustCompile(`\\an(\d+)`)
	reInlineA       = regexp.MustCompile(`\\a(\d+)`)
	rePosTag        = regexp.MustCompile(`\\pos\(\s*(-?[\d.]+)\s*,\s*(-?[\d.]+)\s*\)`)
	reMoveTag       = regexp.MustCompile(`\\move\(\s*(-?[\d.]+)\s*,\s*(-?[\d.]+)\s*,\s*(-?[\d.]+)\s*,\s*(-?[\d.]+)(?:\s*,\s*(-?[\d.]+)\s*,\s*(-?[\d.]+))?\s*\)`)
	reOrgTag        = regexp.MustCompile(`\\org\(\s*(-?[\d.]+)\s*,\s*(-?[\d.]+)\s*\)`)
	reFsTag         = regexp.MustCompile(`\\fs(-?[\d.]+)`)
	reBordTag       = regexp.MustCompile(`\\b(?:ord|xbord|ybord)(-?[\d.]+)`)
	reShadTag       = regexp.MustCompile(`\\shad(-?[\d.]+)`)
	reBlurTag       = regexp.MustCompile(`\\blur(-?[\d.]+)`)
	reFspTag        = regexp.MustCompile(`\\fsp(-?[\d.]+)`)
	reFscxTag       = regexp.MustCompile(`\\fscx(-?[\d.]+)`)
	reFscyTag       = regexp.MustCompile(`\\fscy(-?[\d.]+)`)
	reFrzTag        = regexp.MustCompile(`\\f(?:rz|r)(-?[\d.]+)`)
	reFrxTag        = regexp.MustCompile(`\\frx(-?[\d.]+)`)
	reFryTag        = regexp.MustCompile(`\\fry(-?[\d.]+)`)
	reFaxTag        = regexp.MustCompile(`\\fax(-?[\d.]+)`)
	reFayTag        = regexp.MustCompile(`\\fay(-?[\d.]+)`)
	reAnimatedTag   = regexp.MustCompile(`\\t\s*\(`)
	reQTag          = regexp.MustCompile(`\\q(\d+)`)
	reDrawingTag    = regexp.MustCompile(`\\p(\d+)`)

	// scale-side patterns keep the tag name so the replacement can preserve it
	reScaleSizeTag = regexp.MustCompile(`\\(bord|xbord|ybord|shad|blur|fsp)(-?[\d.]+)`)
	reScaleFsTag   = regexp.MustCompile(`\\fs(-?[\d.]+)`)
	reDrawingNum   = regexp.MustCompile(`-?\d+(?:\.\d+)?`)
)

// legacyToNumpad converts an SSA \a code to the V4+ numpad alignment.
func legacyToNumpad(n int) int {
	switch {
	case n >= 1 && n <= 3:
		return n
	case n >= 5 && n <= 7:
		return n - 1 // 5,6,7 -> 4,5,6
	case n >= 9 && n <= 11:
		return n - 2 // 9,10,11 -> 7,8,9
	}
	return 0
}

func inlineAlign(text string) (int, bool) {
	if m := reInlineAn.FindStringSubmatch(text); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil && n >= 1 && n <= 9 {
			return n, true
		}
	}
	if m := reInlineA.FindStringSubmatch(text); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil {
			if nn := legacyToNumpad(n); nn != 0 {
				return nn, true
			}
		}
	}
	return 0, false
}

func hasInlineAlign(text string) bool {
	_, ok := inlineAlign(text)
	return ok
}

func styleInt(st *ASSStyle, key string) int {
	if st == nil {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(st.Val[key]))
	if err != nil {
		return 0
	}
	return n
}

func styleFloat(st *ASSStyle, key string, def float64) float64 {
	if st == nil {
		return def
	}
	v := strings.TrimSpace(st.Val[key])
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return f
}

func tagFloat(re *regexp.Regexp, text string, def float64) float64 {
	ms := re.FindAllStringSubmatch(text, -1)
	if len(ms) == 0 {
		return def
	}
	f, err := strconv.ParseFloat(ms[len(ms)-1][1], 64)
	if err != nil {
		return def
	}
	return f
}

func tagPoint(re *regexp.Regexp, text string) *Point {
	m := re.FindStringSubmatch(text)
	if m == nil {
		return nil
	}
	x, err1 := strconv.ParseFloat(m[1], 64)
	y, err2 := strconv.ParseFloat(m[2], 64)
	if err1 != nil || err2 != nil {
		return nil
	}
	return &Point{X: x, Y: y}
}

func tagMove(text string) *Move {
	m := reMoveTag.FindStringSubmatch(text)
	if m == nil {
		return nil
	}
	vals := make([]float64, 4)
	for i := 0; i < 4; i++ {
		f, err := strconv.ParseFloat(m[i+1], 64)
		if err != nil {
			return nil
		}
		vals[i] = f
	}
	mv := &Move{X1: vals[0], Y1: vals[1], X2: vals[2], Y2: vals[3]}
	if m[5] != "" && m[6] != "" {
		if t1, err := strconv.Atoi(m[5]); err == nil {
			mv.T1 = t1
		}
		if t2, err := strconv.Atoi(m[6]); err == nil {
			mv.T2 = t2
		}
	}
	return mv
}

func isDrawing(text string) bool {
	ms := reDrawingTag.FindAllStringSubmatch(text, -1)
	for _, m := range ms {
		if n, err := strconv.Atoi(m[1]); err == nil && n > 0 {
			return true
		}
	}
	return false
}

// classifyRole decides what an event is, without relying on track labels.
// Positions and drawings are unambiguous; a sign style name is the remaining
// reliable hint. Everything else is dialogue.
func classifyRole(d *Dialogue, styleName string) Role {
	if isDrawing(d.Text) {
		return RoleDrawing
	}
	if tagPoint(rePosTag, d.Text) != nil || tagMove(d.Text) != nil || tagPoint(reOrgTag, d.Text) != nil {
		return RoleSign
	}
	if strings.Contains(strings.ToLower(styleName), "sign") {
		return RoleSign
	}
	return RoleDialogue
}

// eventGeom is a cheap approximation of libass's layout. It is deliberately
// deterministic and dependency-free; Exact stays false in the report.
type eventGeom struct {
	align       int
	fontSize    float64
	scaleX      float64
	scaleY      float64
	spacing     float64
	outline     float64
	shadow      float64
	angle       float64
	rotX        float64
	rotY        float64
	skewX       float64
	skewY       float64
	animated    bool
	pos         *Point
	move        *Move
	org         *Point
	marginL     float64
	marginR     float64
	marginV     float64
	lines       []string
	plain       string
	charCount   int
	width       float64
	height      float64
	measuredFor int // alignment used for the width/height-independent fields
}

func stripTags(text string) string {
	return reOverrideBlock.ReplaceAllString(text, "")
}

func textLines(text string) []string {
	s := stripTags(text)
	s = strings.ReplaceAll(s, `\N`, "\n")
	s = strings.ReplaceAll(s, `\n`, "\n")
	s = strings.ReplaceAll(s, `\h`, " ")
	s = strings.ReplaceAll(s, "\r", "")
	raw := strings.Split(s, "\n")
	out := make([]string, 0, len(raw))
	for _, l := range raw {
		out = append(out, strings.TrimRight(l, " "))
	}
	if len(out) == 0 {
		out = []string{""}
	}
	return out
}

func runeAdvance(r rune) float64 {
	switch {
	case r == ' ' || r == '\t':
		return 0.30
	case r >= 0x1100 && r <= 0x11FF:
		return 1.0
	case r >= 0x2E80:
		return 1.0
	default:
		return 0.50
	}
}

func measureEvent(d *Dialogue, st *ASSStyle, playW, playH float64) *eventGeom {
	align := styleInt(st, "alignment")
	if n, ok := inlineAlign(d.Text); ok {
		align = n
	}
	if align < 1 || align > 9 {
		align = 2
	}

	g := &eventGeom{
		align:    align,
		fontSize: tagFloat(reFsTag, d.Text, styleFloat(st, "fontsize", 20)),
		scaleX:   tagFloat(reFscxTag, d.Text, styleFloat(st, "scalex", 100)),
		scaleY:   tagFloat(reFscyTag, d.Text, styleFloat(st, "scaley", 100)),
		spacing:  tagFloat(reFspTag, d.Text, styleFloat(st, "spacing", 0)),
		outline:  tagFloat(reBordTag, d.Text, styleFloat(st, "outline", 0)),
		shadow:   tagFloat(reShadTag, d.Text, styleFloat(st, "shadow", 0)),
		angle:    tagFloat(reFrzTag, d.Text, styleFloat(st, "angle", 0)),
		rotX:     tagFloat(reFrxTag, d.Text, 0),
		rotY:     tagFloat(reFryTag, d.Text, 0),
		skewX:    tagFloat(reFaxTag, d.Text, 0),
		skewY:    tagFloat(reFayTag, d.Text, 0),
		animated: reAnimatedTag.MatchString(d.Text),
		pos:      tagPoint(rePosTag, d.Text),
		move:     tagMove(d.Text),
		org:      tagPoint(reOrgTag, d.Text),
		marginL:  styleFloat(st, "marginl", 10),
		marginR:  styleFloat(st, "marginr", 10),
		marginV:  styleFloat(st, "marginv", 10),
	}
	if v, err := strconv.Atoi(strings.TrimSpace(d.MarginL)); err == nil && v != 0 {
		g.marginL = float64(v)
	}
	if v, err := strconv.Atoi(strings.TrimSpace(d.MarginR)); err == nil && v != 0 {
		g.marginR = float64(v)
	}
	if v, err := strconv.Atoi(strings.TrimSpace(d.MarginV)); err == nil && v != 0 {
		g.marginV = float64(v)
	}

	g.lines = textLines(d.Text)
	if tagInt(reQTag, d.Text, 0) != 2 && playW > 0 {
		maxW := playW - g.marginL - g.marginR
		g.lines = wrapLines(g.lines, maxW, g.fontSize, g.spacing, g.scaleX)
	}
	g.plain = assTextPreview(d.Text)
	for _, l := range g.lines {
		g.charCount += len([]rune(strings.ReplaceAll(strings.TrimSpace(l), " ", "")))
	}

	em := g.fontSize
	lineH := em * 1.2
	var maxLine float64
	for _, l := range g.lines {
		var w float64
		runes := []rune(l)
		for _, r := range runes {
			w += runeAdvance(r) * em
		}
		if n := len(runes); n > 1 {
			w += g.spacing * float64(n-1)
		}
		w *= g.scaleX / 100
		if w > maxLine {
			maxLine = w
		}
	}
	pad := math.Max(0, g.outline) + math.Max(0, g.shadow)
	g.width = maxLine + 2*pad
	g.height = lineH*float64(len(g.lines))*g.scaleY/100 + 2*pad
	_ = playH
	return g
}

// anchorAt returns the alignment anchor point for a given time.
func (g *eventGeom) anchorAt(tSec, startSec, endSec float64) (x, y float64, anchored bool) {
	if g.move != nil {
		durMs := int(math.Round((endSec - startSec) * 1000))
		tMs := int(math.Round((tSec - startSec) * 1000))
		x, y := g.move.at(tMs, durMs)
		return x, y, true
	}
	if g.pos != nil {
		return g.pos.X, g.pos.Y, true
	}
	return 0, 0, false
}

// layoutBoxAt is the box libass would use before any rotation, shear, or 3D
// transform. Position, alignment, margins, line wrapping, and inline scaling
// are already folded into it.
func (g *eventGeom) layoutBoxAt(tSec, startSec, endSec, playW, playH float64) Box {
	w, h := g.width, g.height
	var x0, y0 float64
	if px, py, ok := g.anchorAt(tSec, startSec, endSec); ok {
		switch g.align % 3 {
		case 1:
			x0 = px
		case 0:
			x0 = px - w
		default:
			x0 = px - w/2
		}
		switch {
		case g.align <= 3:
			y0 = py - h
		case g.align <= 6:
			y0 = py - h/2
		default:
			y0 = py
		}
	} else {
		switch g.align % 3 {
		case 1:
			x0 = g.marginL
		case 0:
			x0 = playW - g.marginR - w
		default:
			x0 = (playW - w) / 2
		}
		switch {
		case g.align <= 3:
			y0 = playH - g.marginV - h
		case g.align <= 6:
			y0 = (playH - h) / 2
		default:
			y0 = g.marginV
		}
	}
	return Box{X0: x0, Y0: y0, X1: x0 + w, Y1: y0 + h}
}

// layoutAnchorAt returns the alignment point libass uses for \pos. It is
// computed from the untransformed layout box, so a rotated/skewed event still
// reports the same anchor the renderer will place when a fix adds \pos.
func (g *eventGeom) layoutAnchorAt(tSec, startSec, endSec, playW, playH float64) (Point, bool) {
	b := g.layoutBoxAt(tSec, startSec, endSec, playW, playH)
	if !b.Valid() {
		return Point{}, false
	}
	return anchorForBox(b, g.align), true
}

func (g *eventGeom) hasTransform() bool {
	return g.angle != 0 || g.rotX != 0 || g.rotY != 0 || g.skewX != 0 || g.skewY != 0
}

// transformInfo summarizes the tags that make the estimate diverge from what
// libass draws, and whether \t can change the shape over the event's lifetime.
func (g *eventGeom) transformInfo() *TransformInfo {
	if g == nil || (!g.hasTransform() && g.org == nil && !g.animated) {
		return nil
	}
	var tags []string
	add := func(format string, v float64) {
		if v != 0 {
			tags = append(tags, fmt.Sprintf(format, v))
		}
	}
	add(`\frx%g`, g.rotX)
	add(`\fry%g`, g.rotY)
	add(`\frz%g`, g.angle)
	add(`\fax%g`, g.skewX)
	add(`\fay%g`, g.skewY)
	if g.org != nil {
		tags = append(tags, fmt.Sprintf(`\org(%g,%g)`, g.org.X, g.org.Y))
	}
	if g.animated {
		tags = append(tags, `\t(...)`)
	}
	if len(tags) == 0 {
		return nil
	}
	return &TransformInfo{
		RotX: g.rotX, RotY: g.rotY, RotZ: g.angle,
		SkewX: g.skewX, SkewY: g.skewY,
		Org: g.org, Animated: g.animated, Tags: strings.Join(tags, " "),
	}
}

// transformCenter is the point libass transforms around. \org is absolute and
// wins; otherwise the alignment anchor of the layout box is used.
func (g *eventGeom) transformCenter(b Box) (float64, float64) {
	if g.org != nil {
		return g.org.X, g.org.Y
	}
	cx, cy := (b.X0+b.X1)/2, (b.Y0+b.Y1)/2
	switch g.align % 3 {
	case 1:
		cx = b.X0
	case 0:
		cx = b.X1
	}
	switch {
	case g.align <= 3:
		cy = b.Y1
	case g.align <= 6:
		cy = (b.Y0 + b.Y1) / 2
	default:
		cy = b.Y0
	}
	return cx, cy
}

// projectPoint applies the same transform order libass documents: shear, then
// Z rotation, then X/Y rotation with a pinhole perspective. This is an
// approximation (per-glyph inline transforms are not modelled), but it keeps
// the estimated box close for files without an exact ink mask.
func projectPoint(dx, dy, cx, cy, skewX, skewY, angle, rotX, rotY, playH float64) (float64, float64) {
	// Shear: x' = x + y*factor, y' = y + x*factor.
	if skewX != 0 || skewY != 0 {
		dx, dy = dx+dy*skewX, dy+dx*skewY
	}
	// \frz / \fr.
	if angle != 0 {
		rad := angle * math.Pi / 180
		c, s := math.Cos(rad), math.Sin(rad)
		dx, dy = dx*c-dy*s, dx*s+dy*c
	}
	if rotX != 0 || rotY != 0 {
		// libass uses a fixed camera distance scaled from the script height.
		cam := math.Max(1, playH) * (312.5 / 288.0)
		rx := rotX * math.Pi / 180
		ry := rotY * math.Pi / 180
		sx, cxr := -math.Sin(rx), math.Cos(rx)
		sy, cyr := math.Sin(ry), math.Cos(ry)
		z3 := dy * sx
		y3 := dy * cxr
		x4 := dx*cyr - z3*sy
		z4 := dx*sy + z3*cyr
		zf := z4 + cam
		if zf < 0.1 {
			zf = 0.1
		}
		return cx + x4*cam/zf, cy + y3*cam/zf
	}
	return cx + dx, cy + dy
}

func (g *eventGeom) boxAt(tSec, startSec, endSec, playW, playH float64) Box {
	b := g.layoutBoxAt(tSec, startSec, endSec, playW, playH)
	if !g.hasTransform() {
		return b
	}
	cx, cy := g.transformCenter(b)
	corners := [][2]float64{{b.X0, b.Y0}, {b.X1, b.Y0}, {b.X1, b.Y1}, {b.X0, b.Y1}}
	out := Box{X0: math.Inf(1), Y0: math.Inf(1), X1: math.Inf(-1), Y1: math.Inf(-1)}
	for _, c := range corners {
		rx, ry := projectPoint(c[0]-cx, c[1]-cy, cx, cy, g.skewX, g.skewY, g.angle, g.rotX, g.rotY, playH)
		out.X0 = math.Min(out.X0, rx)
		out.Y0 = math.Min(out.Y0, ry)
		out.X1 = math.Max(out.X1, rx)
		out.Y1 = math.Max(out.Y1, ry)
	}
	return out
}

// lifetimeBox unions samples across the event, so \move is represented.
func (g *eventGeom) lifetimeBox(startSec, endSec, playW, playH float64) Box {
	var out Box
	steps := []float64{0, 0.1, 0.5, 0.9, 1}
	for _, f := range steps {
		t := startSec + (endSec-startSec)*f
		out = out.Union(g.boxAt(t, startSec, endSec, playW, playH))
	}
	if g.move != nil && g.move.T1 > 0 {
		t1 := startSec + float64(g.move.T1)/1000
		out = out.Union(g.boxAt(t1, startSec, endSec, playW, playH))
	}
	if g.move != nil && g.move.T2 > 0 {
		t2 := startSec + float64(g.move.T2)/1000
		out = out.Union(g.boxAt(t2, startSec, endSec, playW, playH))
	}
	return out
}

func tagInt(re *regexp.Regexp, text string, def int) int {
	ms := re.FindAllStringSubmatch(text, -1)
	if len(ms) == 0 {
		return def
	}
	n, err := strconv.Atoi(ms[len(ms)-1][1])
	if err != nil {
		return def
	}
	return n
}

func textWidth(s string, em, spacing, fscx float64) float64 {
	runes := []rune(s)
	var w float64
	for _, r := range runes {
		w += runeAdvance(r) * em
	}
	if len(runes) > 1 {
		w += spacing * float64(len(runes)-1)
	}
	return w * fscx / 100
}

type wrapToken struct {
	s     string
	space bool // needs a space before it when appended
}

func containsCJK(s string) bool {
	for _, r := range s {
		if r >= 0x2E80 {
			return true
		}
	}
	return false
}

func wrapTokens(line string) []wrapToken {
	var out []wrapToken
	for _, field := range strings.Fields(line) {
		if containsCJK(field) {
			for _, r := range field {
				out = append(out, wrapToken{s: string(r), space: false})
			}
			continue
		}
		out = append(out, wrapToken{s: field, space: true})
	}
	return out
}

// wrapLines approximates libass's smart wrapping so a long line is measured as
// several narrow lines instead of one very wide one.
func wrapLines(lines []string, maxW, em, spacing, fscx float64) []string {
	if maxW <= 0 {
		return lines
	}
	var out []string
	for _, line := range lines {
		toks := wrapTokens(line)
		if len(toks) == 0 {
			out = append(out, "")
			continue
		}
		var cur string
		var curW float64
		for _, t := range toks {
			tw := textWidth(t.s, em, spacing, fscx)
			spaceW := runeAdvance(' ')*em*fscx/100 + spacing
			add := tw
			if cur != "" && t.space {
				add += spaceW
			}
			if cur != "" && curW+add > maxW {
				out = append(out, cur)
				cur, curW = "", 0
				add = tw
			}
			if cur != "" && t.space {
				cur += " "
				curW += spaceW
			}
			cur += t.s
			curW += tw
		}
		out = append(out, cur)
	}
	return out
}
