package sandwicher

import (
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// SpoilerInfo records the visual-only scramble used by No Spoilers mode.
// The merged file and decision log keep the original text; the preview file is
// the only artifact whose subtitle text is replaced.
type SpoilerInfo struct {
	Enabled bool   `json:"enabled"`
	Seed    int64  `json:"seed"`
	Preview string `json:"preview,omitempty"`
}

// spoilerRNG is a splitmix64 generator so the scramble is stable across
// reloads and does not depend on the process or the math/rand version.
type spoilerRNG struct{ state uint64 }

func (r *spoilerRNG) next() uint64 {
	r.state += 0x9E3779B97F4A7C15
	z := r.state
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

func (r *spoilerRNG) intn(n int) int {
	if n <= 1 {
		return 0
	}
	return int(r.next() % uint64(n))
}

// Consonants only: a run of these never forms a readable English word, so a
// scrambled line cannot invent a plot beat by accident.
var spoilerLetters = []rune("bcdfghjklmnpqrstvwxyz")

// Combining marks add visual noise on top of the letters without advancing
// the pen, so they obscure glyph silhouettes without changing line length.
var spoilerMarks = []rune{
	'\u0300', '\u0301', '\u0302', '\u0303', '\u0304', '\u0306', '\u0307',
	'\u0308', '\u030A', '\u030B', '\u030C', '\u0311', '\u0323', '\u0327',
	'\u0331', '\u0332', '\u0335', '\u0336', '\u0337', '\u0338',
}

// Arial-style advance widths in 1/1000 em. Widths only need to be relatively
// right: the merged and scrambled text are drawn by the same libass font, and
// the scramble matches em widths, so both scale together.
var spoilerAdvance = map[rune]int{
	' ': 278, '\t': 278, '\u00A0': 278,
	'a': 556, 'b': 556, 'c': 500, 'd': 556, 'e': 556, 'f': 278, 'g': 556,
	'h': 556, 'i': 222, 'j': 222, 'k': 500, 'l': 222, 'm': 833, 'n': 556,
	'o': 556, 'p': 556, 'q': 556, 'r': 333, 's': 500, 't': 278, 'u': 556,
	'v': 500, 'w': 722, 'x': 500, 'y': 500, 'z': 500,
	'A': 667, 'B': 667, 'C': 722, 'D': 722, 'E': 667, 'F': 611, 'G': 778,
	'H': 722, 'I': 278, 'J': 500, 'K': 667, 'L': 556, 'M': 833, 'N': 722,
	'O': 778, 'P': 667, 'Q': 778, 'R': 722, 'S': 667, 'T': 611, 'U': 722,
	'V': 667, 'W': 944, 'X': 667, 'Y': 667, 'Z': 611,
	'0': 556, '1': 556, '2': 556, '3': 556, '4': 556,
	'5': 556, '6': 556, '7': 556, '8': 556, '9': 556,
	'.': 278, ',': 278, '!': 278, '?': 556, '\'': 191, '"': 355,
	'-': 333, ':': 278, ';': 278, '(': 333, ')': 333, '/': 278,
	'&': 667, '%': 889, '#': 556, '@': 1015,
}

func spoilerAdvanceFor(r rune) int {
	if r >= 0x0300 && r <= 0x036F {
		return 0
	}
	if w, ok := spoilerAdvance[r]; ok {
		return w
	}
	if r >= 0x1100 && r <= 0x11FF {
		return 1000
	}
	if r >= 0x2E80 {
		return 1000
	}
	return 556
}

func spoilerEventSeed(seed int64, event int) uint64 {
	if seed == 0 {
		seed = 1
	}
	return uint64(seed) ^ (uint64(event+1) * 0x9E3779B97F4A7C15)
}

var reSpoilerDrawing = regexp.MustCompile(`\\p\s*(\d+)`)

// ScrambleText replaces the visible runs of an ASS event with width-matched
// gibberish. Override blocks, line breaks, spaces, tabs and drawing events are
// preserved; spaces therefore keep their approximate x position because each
// word is filled to the original's advance width.
//
// The event index is part of the seed so every event scrambles differently but
// the same event always scrambles to the same text across reloads.
func ScrambleText(text string, seed int64, event int) string {
	if text == "" {
		return text
	}
	rng := &spoilerRNG{state: spoilerEventSeed(seed, event)}
	var out strings.Builder
	lines := splitSpoilerLines(text)
	for i, line := range lines {
		if i > 0 {
			out.WriteString(`\N`)
		}
		out.WriteString(scrambleSpoilerLine(line, rng))
	}
	return out.String()
}

// splitSpoilerLines separates hard/soft ASS line breaks while keeping the rest
// of the text intact. The breaks are re-emitted by ScrambleText.
func splitSpoilerLines(text string) []string {
	var lines []string
	var b strings.Builder
	rs := []rune(text)
	for i := 0; i < len(rs); i++ {
		if rs[i] == '\\' && i+1 < len(rs) && (rs[i+1] == 'N' || rs[i+1] == 'n') {
			lines = append(lines, b.String())
			b.Reset()
			i++
			continue
		}
		b.WriteRune(rs[i])
	}
	lines = append(lines, b.String())
	return lines
}

func scrambleSpoilerLine(line string, rng *spoilerRNG) string {
	rs := []rune(line)
	var out strings.Builder
	targetCursor, outCursor := 0, 0
	drawing := false
	for i := 0; i < len(rs); {
		r := rs[i]
		if r == '{' {
			if j := strings.IndexRune(string(rs[i:]), '}'); j >= 0 {
				block := string(rs[i : i+j+1])
				out.WriteString(block)
				// \p1..\p9 switch to vector-drawing coordinates; \p0 returns to
				// ordinary text. Coordinates are not prose, so preserve them.
				for _, m := range reSpoilerDrawing.FindAllStringSubmatch(block, -1) {
					if n, err := strconv.Atoi(m[1]); err == nil {
						drawing = n > 0
					}
				}
				if !drawing {
					targetCursor, outCursor = 0, 0
				}
				i += j + 1
				continue
			}
		}
		if drawing {
			out.WriteRune(r)
			i++
			continue
		}
		if r == '\\' && i+1 < len(rs) && rs[i+1] == 'h' {
			out.WriteString(`\h`)
			w := spoilerAdvanceFor(' ')
			targetCursor += w
			outCursor += w
			i += 2
			continue
		}
		if r == ' ' || r == '\t' || r == '\u00A0' {
			out.WriteRune(r)
			w := spoilerAdvanceFor(r)
			targetCursor += w
			outCursor += w
			i++
			continue
		}
		// A visible run: letters, punctuation and any stray characters up to
		// the next space, tag block or line break.
		start := i
		width := 0
		for i < len(rs) {
			c := rs[i]
			if c == ' ' || c == '\t' || c == '\u00A0' || c == '{' {
				break
			}
			if c == '\\' && i+1 < len(rs) && (rs[i+1] == 'N' || rs[i+1] == 'n' || rs[i+1] == 'h') {
				break
			}
			width += spoilerAdvanceFor(c)
			i++
		}
		if i == start {
			out.WriteRune(r)
			i++
			continue
		}
		// Fill to the original word's end position, not just its width. That
		// corrects accumulated drift at every space, so space positions stay
		// inside the 1-2% buffer instead of sliding across a long line.
		fill := targetCursor + width - outCursor
		gen, genWidth := fillSpoilerRun(fill, rng)
		out.WriteString(gen)
		targetCursor += width
		outCursor += genWidth
	}
	return out.String()
}

// unqiue-width coin set for the width solver. The gcd is 1, so nearly every
// target width is reachable exactly.
var spoilerWidths = []int{222, 278, 333, 500, 556, 722, 833}

func lettersForWidth(w int) []rune {
	out := make([]rune, 0, 4)
	for _, c := range spoilerLetters {
		if spoilerAdvanceFor(c) == w {
			out = append(out, c)
		}
	}
	return out
}

// fillSpoilerRun builds a run whose advance is as close to target as the
// available glyph advances allow. A small DP over the unique widths reaches
// the exact target for most words; only very short runs keep a residual.
func fillSpoilerRun(target int, rng *spoilerRNG) (string, int) {
	if target <= 0 {
		return "", 0
	}
	limit := target + 900
	reach := make([]bool, limit+1)
	reach[0] = true
	for s := 0; s <= limit; s++ {
		if !reach[s] {
			continue
		}
		for _, w := range spoilerWidths {
			if s+w <= limit {
				reach[s+w] = true
			}
		}
	}
	best := -1
	for s := 1; s <= limit; s++ {
		if !reach[s] {
			continue
		}
		if best < 0 {
			best = s
			continue
		}
		d, bd := absInt(s-target), absInt(best-target)
		if d < bd || (d == bd && rng.intn(2) == 0) {
			best = s
		}
	}
	if best < 0 {
		return "", 0
	}
	// Reconstruct from the top. Prefer wide glyphs so a normal word does not
	// turn into a long string of narrow letters, but allow randomness so the
	// character count differs from the original.
	widths := make([]int, 0, 16)
	rem := best
	for rem > 0 {
		var fits []int
		for _, w := range spoilerWidths {
			if w <= rem && reach[rem-w] {
				fits = append(fits, w)
			}
		}
		if len(fits) == 0 {
			break
		}
		sort.Sort(sort.Reverse(sort.IntSlice(fits)))
		w := fits[0]
		if len(fits) > 1 && rng.intn(100) < 30 {
			w = fits[rng.intn(len(fits))]
		}
		widths = append(widths, w)
		rem -= w
	}
	var b strings.Builder
	total := 0
	for _, w := range widths {
		letters := lettersForWidth(w)
		if len(letters) == 0 {
			continue
		}
		b.WriteRune(letters[rng.intn(len(letters))])
		total += w
		if rng.intn(100) < 26 {
			b.WriteRune(spoilerMarks[rng.intn(len(spoilerMarks))])
		}
	}
	return b.String(), total
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// WriteSpoilerASS parses an already-written merged file and writes a preview
// copy whose visible text is scrambled. It returns the readable display form
// of each scrambled event, in the same order as the report's events.
func WriteSpoilerASS(srcPath, outPath string, seed int64) ([]string, error) {
	doc, err := parseASSFile(srcPath)
	if err != nil {
		return nil, err
	}
	display := make([]string, len(doc.Dialogues))
	for i, d := range doc.Dialogues {
		d.Text = ScrambleText(d.Text, seed, i)
		display[i] = assTextPreview(d.Text)
	}
	if err := writeDoc(doc, outPath); err != nil {
		return nil, err
	}
	return display, nil
}

// scrambleDoc rewrites a parsed document in place, skipping events listed in
// reveal. Used by apply so a preview reload stays scrambled.
func scrambleDoc(doc *ASSDoc, seed int64, reveal map[int]bool) {
	for i, d := range doc.Dialogues {
		if reveal[i] {
			continue
		}
		d.Text = ScrambleText(d.Text, seed, i)
	}
}

// spoilerWidth returns the advance width of an already-scrambled line, used by
// tests to assert the width invariant.
func spoilerWidth(line string) int {
	w := 0
	inTag := false
	rs := []rune(line)
	for i := 0; i < len(rs); i++ {
		switch {
		case rs[i] == '{':
			inTag = true
		case rs[i] == '}':
			inTag = false
		case inTag:
		case rs[i] == '\\' && i+1 < len(rs) && (rs[i+1] == 'N' || rs[i+1] == 'n'):
			i++
		case rs[i] == '\\' && i+1 < len(rs) && rs[i+1] == 'h':
			w += spoilerAdvanceFor(' ')
			i++
		default:
			w += spoilerAdvanceFor(rs[i])
		}
	}
	return w
}

// spoilerLineWidth is a small helper for tests: it strips tags/breaks and
// reports the widest visual line in a (possibly multi-line) event.
func spoilerLineWidths(text string) []int {
	lines := splitSpoilerLines(text)
	out := make([]int, len(lines))
	for i, line := range lines {
		line = stripSpoilerTags(line)
		out[i] = spoilerWidth(line)
	}
	return out
}

func stripSpoilerTags(text string) string {
	var b strings.Builder
	inTag := false
	for _, r := range text {
		switch {
		case r == '{':
			inTag = true
		case r == '}':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func spoilerWidthDelta(a, b int) float64 {
	if a <= 0 {
		return math.Abs(float64(b))
	}
	return math.Abs(float64(a-b)) / float64(a)
}
