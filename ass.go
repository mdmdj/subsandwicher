package main

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

// mergeAndWrite applies all transformations and writes the merged .ass file.
func mergeAndWrite(prim, sec *ASSDoc, lang2 string, scale float64, outPath string) (nStyles, nLines int, err error) {
	// Primary: top-aligned -> bottom-aligned (horizontal kept), middle -> middle-left.
	for _, s := range prim.Styles {
		transformStyleAlignment(s, false)
	}
	for _, d := range prim.Dialogues {
		d.Text = remapTextAlignments(d.Text, false)
	}

	// Secondary: force top alignment (middle -> middle-right), rescale to
	// primary PlayRes, suffix style names and update all references.
	renames := buildSecondaryRenames(prim, sec, "-"+lang2)
	for _, s := range sec.Styles {
		transformStyleAlignment(s, true)
		if scale != 1 {
			scaleStyle(s, scale)
		}
	}
	for _, d := range sec.Dialogues {
		if nn, ok := renames[d.Style]; ok {
			d.Style = nn
		}
		d.Text = renameResetStyles(d.Text, renames)
		d.Text = remapTextAlignments(d.Text, true)
		if scale != 1 {
			scaleDialogueMargins(d, scale)
		}
	}

	lines := make([]*Dialogue, 0, len(prim.Dialogues)+len(sec.Dialogues))
	lines = append(lines, prim.Dialogues...)
	lines = append(lines, sec.Dialogues...)
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].startCS < lines[j].startCS })

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
	for _, d := range lines {
		fmt.Fprintf(&b, "Dialogue: %s,%s,%s,%s,%s,%s,%s,%s,%s,%s\n",
			d.Layer, d.Start, d.End, d.Style, d.Name, d.MarginL, d.MarginR, d.MarginV, d.Effect, d.Text)
	}
	for _, other := range prim.Other {
		b.WriteString("\n" + strings.Join(other, "\n") + "\n")
	}

	if err := os.WriteFile(outPath, []byte(b.String()), 0o644); err != nil {
		return 0, 0, err
	}
	return len(prim.Styles) + len(sec.Styles), len(lines), nil
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
