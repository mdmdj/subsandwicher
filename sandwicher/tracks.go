package sandwicher

import (
	"sort"
	"strings"
	"unicode"
)

// TrackGroup is one style's role profile, used by the no-label classifier and
// useful for explaining why a track was chosen.
type TrackGroup struct {
	Key        string   `json:"key"`
	Styles     []string `json:"styles"`
	Events     int      `json:"events"`
	Dialogue   int      `json:"dialogue"`
	Positioned int      `json:"positioned"`
	Drawings   int      `json:"drawings"`
	AlignMode  int      `json:"alignMode"`
	Font       string   `json:"font,omitempty"`
	FontSize   float64  `json:"fontSize,omitempty"`
	Script     string   `json:"script,omitempty"`
	Track      string   `json:"track,omitempty"`
}

// TrackInference is the fallback when a file has no secondary style suffix.
// Confidence below 0.5 means the UI should ask the user which track is primary.
type TrackInference struct {
	Method      string            `json:"method"` // suffix | classifier | assumed
	Confidence  float64           `json:"confidence"`
	Note        string            `json:"note"`
	Groups      []TrackGroup      `json:"groups,omitempty"`
	StyleTracks map[string]string `json:"styleTracks,omitempty"`
}

// InferTracks groups events by style and infers primary/secondary from
// geometry, font and script. It never uses labels or track tags.
func InferTracks(doc *ASSDoc) TrackInference {
	styles := map[string]*ASSStyle{}
	for _, s := range doc.Styles {
		styles[s.name()] = s
	}
	type acc struct {
		group   TrackGroup
		aligns  map[int]int
		fonts   map[string]int
		sizes   map[int]int
		scripts map[string]int
	}
	groups := map[string]*acc{}
	get := func(key string) *acc {
		if g, ok := groups[key]; ok {
			return g
		}
		g := &acc{
			group:   TrackGroup{Key: key, Styles: []string{key}},
			aligns:  map[int]int{},
			fonts:   map[string]int{},
			sizes:   map[int]int{},
			scripts: map[string]int{},
		}
		groups[key] = g
		return g
	}

	minDialogue := 3
	if len(doc.Dialogues) < 30 {
		minDialogue = 1
	}
	for _, d := range doc.Dialogues {
		g := get(d.Style)
		g.group.Events++
		st := styles[d.Style]
		align := styleInt(st, "alignment")
		if n, ok := inlineAlign(d.Text); ok {
			align = n
		}
		g.aligns[align]++
		role := classifyRole(d, d.Style)
		if role != RoleDrawing {
			g.scripts[scriptOf(assTextPreview(d.Text))]++
		}
		if st != nil {
			if f := strings.TrimSpace(st.Val["fontname"]); f != "" {
				g.fonts[strings.ToLower(f)]++
			}
			if n := styleInt(st, "fontsize"); n > 0 {
				g.sizes[n]++
			}
		}
		switch role {
		case RoleDrawing:
			g.group.Drawings++
		case RoleSign:
			g.group.Positioned++
		default:
			g.group.Dialogue++
		}
	}

	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := TrackInference{Method: "classifier"}
	var dialogue []*acc
	for _, k := range keys {
		g := groups[k]
		g.group.AlignMode = modeInt(g.aligns)
		g.group.Font = modeString(g.fonts)
		g.group.FontSize = float64(modeInt(g.sizes))
		g.group.Script = modeString(g.scripts)
		out.Groups = append(out.Groups, g.group)
		if g.group.Dialogue >= minDialogue && !strings.Contains(strings.ToLower(g.group.Key), "sign") {
			dialogue = append(dialogue, g)
		}
	}
	sort.SliceStable(dialogue, func(i, j int) bool {
		if dialogue[i].group.Dialogue != dialogue[j].group.Dialogue {
			return dialogue[i].group.Dialogue > dialogue[j].group.Dialogue
		}
		return dialogue[i].group.Key < dialogue[j].group.Key
	})

	var bottom, top []*acc
	for _, g := range dialogue {
		switch {
		case isBottom(g.group.AlignMode):
			bottom = append(bottom, g)
		case isTop(g.group.AlignMode):
			top = append(top, g)
		}
	}
	conf := 0.30
	note := "no clear dialogue split; treating everything as primary"
	switch {
	case len(bottom) > 0 && len(top) > 0:
		for _, g := range bottom {
			g.group.Track = "primary"
		}
		for _, g := range top {
			g.group.Track = "secondary"
		}
		conf = 0.85
		note = "bottom-aligned families are primary; top-aligned families are secondary"
	case len(bottom) > 0:
		bottom[0].group.Track = "primary"
		for _, g := range bottom[1:] {
			g.group.Track = "secondary"
		}
		conf = 0.35
		if len(bottom) == 1 {
			note = "one bottom-aligned dialogue family; treating it as primary"
		} else {
			note = "several bottom-aligned dialogue families; larger family assumed primary"
		}
	case len(top) > 0:
		top[0].group.Track = "primary"
		for _, g := range top[1:] {
			g.group.Track = "secondary"
		}
		conf = 0.30
		note = "top-aligned dialogue families only; larger family assumed primary"
	}

	// Remaining dialogue families: font match first, then count, then lane.
	// A font shared by both tracks cannot identify a family.
	fontTracks := map[string]map[string]bool{}
	for _, g := range dialogue {
		if g.group.Track == "" || g.group.Font == "" {
			continue
		}
		set := fontTracks[g.group.Font]
		if set == nil {
			set = map[string]bool{}
			fontTracks[g.group.Font] = set
		}
		set[g.group.Track] = true
	}
	fontTrack := map[string]string{}
	for f, set := range fontTracks {
		if len(set) == 1 {
			for tr := range set {
				fontTrack[f] = tr
			}
		}
	}
	for _, g := range dialogue {
		if g.group.Track != "" {
			continue
		}
		if t, ok := fontTrack[g.group.Font]; ok {
			g.group.Track = t
			continue
		}
		g.group.Track = "secondary"
		conf = minFloat(conf, 0.45)
	}

	// Sign-only families inherit the track whose font matches, then whose
	// script differs from the primary dialogue, then their lane.
	primaryScript, secondaryScript := "", ""
	for _, g := range dialogue {
		switch g.group.Track {
		case "primary":
			if primaryScript == "" {
				primaryScript = g.group.Script
			}
		case "secondary":
			if secondaryScript == "" {
				secondaryScript = g.group.Script
			}
		}
	}
	for _, k := range keys {
		g := groups[k]
		if g.group.Track != "" {
			continue
		}
		if t, ok := fontTrack[g.group.Font]; ok && g.group.Font != "" {
			g.group.Track = t
			continue
		}
		if secondaryScript != "" && g.group.Script != "" && g.group.Script != primaryScript {
			g.group.Track = "secondary"
			continue
		}
		if isTop(g.group.AlignMode) && len(top) > 0 {
			g.group.Track = "secondary"
		} else {
			g.group.Track = "primary"
		}
	}

	out.Groups = nil
	out.StyleTracks = map[string]string{}
	for _, k := range keys {
		g := groups[k]
		out.Groups = append(out.Groups, g.group)
		for _, st := range g.group.Styles {
			out.StyleTracks[st] = g.group.Track
		}
	}
	// Styles with no events still need a track for the report and UI pickers.
	for _, s := range doc.Styles {
		name := s.name()
		if _, ok := out.StyleTracks[name]; ok {
			continue
		}
		track := "primary"
		if t, ok := fontTrack[strings.ToLower(strings.TrimSpace(s.Val["fontname"]))]; ok {
			track = t
		} else if isTop(styleInt(s, "alignment")) && len(top) > 0 {
			track = "secondary"
		}
		out.StyleTracks[name] = track
	}
	out.Confidence = round2(conf)
	out.Note = note
	return out
}

func isBottom(align int) bool { return align >= 1 && align <= 3 }
func isTop(align int) bool    { return align >= 7 && align <= 9 }

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func modeInt(m map[int]int) int {
	best, n := 0, 0
	for k, c := range m {
		if c > n || (c == n && k > best) {
			best, n = k, c
		}
	}
	return best
}

func modeString(m map[string]int) string {
	best, n := "", 0
	for k, c := range m {
		if c > n || (c == n && k > best) {
			best, n = k, c
		}
	}
	return best
}

// scriptOf is a cheap script profile, not language detection. It separates
// scripts well enough for the classifier to agree or disagree.
func scriptOf(s string) string {
	var letters, diacritic, cjk, hangul, thai, cyr, arab int
	for _, r := range s {
		if !unicode.IsLetter(r) {
			continue
		}
		switch {
		case r >= 0x3040 && r <= 0x9FFF:
			cjk++
		case r >= 0xAC00 && r <= 0xD7AF:
			hangul++
		case r >= 0x0E00 && r <= 0x0E7F:
			thai++
		case r >= 0x0400 && r <= 0x04FF:
			cyr++
		case r >= 0x0600 && r <= 0x06FF:
			arab++
		default:
			letters++
			if r > 0x7F {
				diacritic++
			}
		}
	}
	total := letters + cjk + hangul + thai + cyr + arab
	if total == 0 {
		return "unknown"
	}
	switch {
	case cjk*2 > total:
		return "cjk"
	case hangul*2 > total:
		return "hangul"
	case thai*2 > total:
		return "thai"
	case cyr*2 > total:
		return "cyrillic"
	case arab*2 > total:
		return "arabic"
	case diacritic*8 >= total:
		return "latin-diacritic"
	default:
		return "latin"
	}
}
