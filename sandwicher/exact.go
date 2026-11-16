package sandwicher

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"math"
	"math/bits"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

// exactContext renders individual events through ffmpeg's libass filter and
// caches their alpha masks. It is optional: every caller falls back to the
// estimated boxes when a render fails.
type exactContext struct {
	doc          *ASSDoc
	playW        float64
	playH        float64
	ffmpeg       string
	dir          string
	mu           sync.Mutex
	cache        map[string]*inkMask
	failed       map[string]bool
	sem          chan struct{}
	rendered     int
	progress     func(rendered int)
	lastProgress time.Time
}

// inkMask is a downsampled alpha coverage mask plus the exact pixel bbox.
// Cells are CELL x CELL pixels; bits are absolute canvas cells (row-major).
type inkMask struct {
	Cell     int
	W, H     int // canvas in pixels
	MW, MH   int // canvas in cells
	Box      Box // exact ink bbox in pixels
	Bits     []uint64
	InkCells int
}

const exactCell = 1

func newExactContext(assPath string, playW, playH float64) *exactContext {
	doc, err := parseASSFile(assPath)
	if err != nil {
		return nil
	}
	ff := resolveBin("ffmpeg")
	if p, err := exec.LookPath(ff); err == nil {
		ff = p
	} else {
		return nil
	}
	dir, err := os.MkdirTemp("", "subsandwicher-exact-*")
	if err != nil {
		return nil
	}
	return &exactContext{
		doc:    doc,
		playW:  playW,
		playH:  playH,
		ffmpeg: ff,
		dir:    dir,
		cache:  map[string]*inkMask{},
		failed: map[string]bool{},
		sem:    make(chan struct{}, 4),
	}
}

func (e *exactContext) setProgress(fn func(rendered int)) {
	if e == nil {
		return
	}
	e.mu.Lock()
	e.progress = fn
	e.mu.Unlock()
}

func (e *exactContext) noteRendered() {
	if e == nil {
		return
	}
	e.mu.Lock()
	n := e.rendered
	emit := e.progress != nil && time.Since(e.lastProgress) > 120*time.Millisecond
	if emit {
		e.lastProgress = time.Now()
	}
	fn := e.progress
	e.mu.Unlock()
	if emit {
		fn(n)
	}
}

func (e *exactContext) close() {
	if e != nil && e.dir != "" {
		_ = os.RemoveAll(e.dir)
	}
}

// masksAt renders (or reads from cache) the masks for the given events at a
// single time. Every remaining event of the window is included so placement
// sees real ink, not boxes, for all participants.
func (e *exactContext) masksAt(ids []int, seek float64) map[int]*inkMask {
	if e == nil {
		return nil
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	out := map[int]*inkMask{}
	for _, id := range ids {
		if id < 0 || id >= len(e.doc.Dialogues) {
			continue
		}
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			if m := e.maskFor(id, seek); m != nil {
				mu.Lock()
				out[id] = m
				mu.Unlock()
			}
		}(id)
	}
	wg.Wait()
	if len(out) == 0 {
		return nil
	}
	return out
}

func (e *exactContext) maskFor(eventID int, seek float64) *inkMask {
	key := fmt.Sprintf("%d:%.2f", eventID, seek)
	e.mu.Lock()
	if m, ok := e.cache[key]; ok {
		e.mu.Unlock()
		return m
	}
	if e.failed[key] {
		e.mu.Unlock()
		return nil
	}
	e.mu.Unlock()

	e.sem <- struct{}{}
	defer func() { <-e.sem }()

	m, err := e.renderEvent(eventID, seek)
	e.mu.Lock()
	if err != nil {
		e.failed[key] = true
		e.mu.Unlock()
		return nil
	}
	e.cache[key] = m
	e.rendered++
	e.mu.Unlock()
	e.noteRendered()
	return m
}

func (e *exactContext) renderEvent(eventID int, seek float64) (*inkMask, error) {
	if eventID < 0 || eventID >= len(e.doc.Dialogues) {
		return nil, fmt.Errorf("event %d out of range", eventID)
	}
	src := e.doc.Dialogues[eventID]
	start := float64(src.startCS) / 100
	end := float64(parseTimeCS(src.End)) / 100
	dur := math.Max(1.0, end-start)
	rel := math.Max(0.05, math.Min(seek-start, dur-0.05))

	copyD := *src
	copyD.Start = "0:00:00.00"
	copyD.End = formatCS(dur)
	copyD.startCS = 0
	copyD.Text = stripOpacityTags(copyD.Text)
	doc := &ASSDoc{InfoRaw: e.doc.InfoRaw, Info: e.doc.Info, Styles: e.doc.Styles, Dialogues: []*Dialogue{&copyD}, Other: e.doc.Other}

	n := eventID
	assName := fmt.Sprintf("e%d-%03d.ass", n, int(math.Round(rel*100)))
	pngName := fmt.Sprintf("e%d-%03d.png", n, int(math.Round(rel*100)))
	assPath := filepath.Join(e.dir, assName)
	if err := writeDoc(doc, assPath); err != nil {
		return nil, err
	}
	defer os.Remove(assPath)

	args := []string{
		"-hide_banner", "-loglevel", "error", "-nostdin",
		"-f", "lavfi", "-i",
		fmt.Sprintf("color=c=black:s=%dx%d:r=24:d=%s", int(e.playW), int(e.playH), formatNum(dur+1)),
		"-vf", fmt.Sprintf("format=rgba,colorchannelmixer=aa=0,ass=%s:alpha=1", assName),
		"-ss", fmt.Sprintf("%.3f", rel),
		"-frames:v", "1", "-update", "1", "-y", pngName,
	}
	cmd := exec.Command(e.ffmpeg, args...)
	cmd.Dir = e.dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg exact render: %w: %s", err, stderr.String())
	}
	defer os.Remove(filepath.Join(e.dir, pngName))

	return readInkMask(filepath.Join(e.dir, pngName))
}

func readInkMask(path string) (*inkMask, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	alphaAt := func(x, y int) uint8 {
		_, _, _, a := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
		return uint8(a >> 8)
	}
	switch p := img.(type) {
	case *image.NRGBA:
		alphaAt = func(x, y int) uint8 {
			return p.Pix[(y+b.Min.Y-p.Rect.Min.Y)*p.Stride+(x+b.Min.X-p.Rect.Min.X)*4+3]
		}
	case *image.RGBA:
		alphaAt = func(x, y int) uint8 {
			return p.Pix[(y+b.Min.Y-p.Rect.Min.Y)*p.Stride+(x+b.Min.X-p.Rect.Min.X)*4+3]
		}
	}
	m := &inkMask{
		Cell: exactCell,
		W:    w,
		H:    h,
		MW:   (w + exactCell - 1) / exactCell,
		MH:   (h + exactCell - 1) / exactCell,
	}
	m.Bits = make([]uint64, (m.MW*m.MH+63)/64)
	minX, minY := w, h
	maxX, maxY := -1, -1
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if alphaAt(x, y) <= 8 {
				continue
			}
			if x < minX {
				minX = x
			}
			if y < minY {
				minY = y
			}
			if x > maxX {
				maxX = x
			}
			if y > maxY {
				maxY = y
			}
			idx := (y/exactCell)*m.MW + (x / exactCell)
			m.Bits[idx/64] |= 1 << (idx % 64)
		}
	}
	if maxX < 0 {
		return nil, fmt.Errorf("no ink")
	}
	m.Box = Box{X0: float64(minX), Y0: float64(minY), X1: float64(maxX + 1), Y1: float64(maxY + 1)}
	for _, w := range m.Bits {
		m.InkCells += bits.OnesCount64(w)
	}
	return m, nil
}

func (m *inkMask) bit(cx, cy int) bool {
	if m == nil || cx < 0 || cy < 0 || cx >= m.MW || cy >= m.MH {
		return false
	}
	idx := cy*m.MW + cx
	return m.Bits[idx/64]&(1<<(idx%64)) != 0
}

// maskOverlap counts overlapping ink cells after translating a by (dx,dy)
// pixels. It only visits the intersection of the two bboxes.
func maskOverlap(a, b *inkMask, dx, dy float64) int {
	if a == nil || b == nil || a.Cell != b.Cell {
		return 0
	}
	cell := float64(a.Cell)
	ax := int(math.Round(dx / cell))
	ay := int(math.Round(dy / cell))
	ab := a.Box.Translate(dx, dy)
	inter := ab.Intersect(b.Box)
	if !inter.Valid() {
		return 0
	}
	x0 := int(math.Floor(inter.X0 / cell))
	y0 := int(math.Floor(inter.Y0 / cell))
	x1 := int(math.Ceil(inter.X1 / cell))
	y1 := int(math.Ceil(inter.Y1 / cell))
	n := 0
	for cy := y0; cy < y1; cy++ {
		for cx := x0; cx < x1; cx++ {
			if a.bit(cx-ax, cy-ay) && b.bit(cx, cy) {
				n++
			}
		}
	}
	return n
}

func maskOverlapRatio(a, b *inkMask, dx, dy float64) float64 {
	if a == nil || b == nil {
		return 0
	}
	den := a.InkCells
	if b.InkCells < den {
		den = b.InkCells
	}
	if den <= 0 {
		return 0
	}
	return float64(maskOverlap(a, b, dx, dy)) / float64(den)
}

var (
	reFadeTag = regexp.MustCompile(`\\fad\([^)]*\)|\\fade\([^)]*\)`)
	reColorA  = regexp.MustCompile(`\\[1-4]a[^\\}]*`)
)

// stripOpacityTags removes fade and alpha overrides for the mask render. The
// shape of the text is what matters; a fade at the seek time would otherwise
// erase the ink we are trying to measure.
func stripOpacityTags(text string) string {
	text = reFadeTag.ReplaceAllString(text, "")
	text = reAlphaTag.ReplaceAllString(text, "")
	text = reColorA.ReplaceAllString(text, "")
	return text
}

// formatCS renders seconds as an ASS timestamp.
func formatCS(seconds float64) string {
	cs := int(math.Round(seconds * 100))
	if cs < 0 {
		cs = 0
	}
	return fmt.Sprintf("%d:%02d:%02d.%02d", cs/360000, (cs%360000)/6000, (cs%6000)/100, cs%100)
}

// exactSummary is used to report whether masks were actually produced.
func (e *exactContext) exactSummary() (bool, int, string) {
	if e == nil {
		return false, 0, "disabled"
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.rendered == 0 {
		return false, 0, "no masks rendered (ffmpeg/libass unavailable or failed)"
	}
	return true, e.rendered, ""
}

// filterConflicts re-checks every candidate conflict at its peak with real ink
// masks. Bounding boxes can overlap while the rendered glyphs have padding;
// those false positives are dropped before any window is shown.
func (e *exactContext) filterConflicts(conflicts []Conflict, events []*plannedEvent) []Conflict {
	if e == nil {
		return conflicts
	}
	byID := map[int]*plannedEvent{}
	for _, ev := range events {
		byID[ev.order] = ev
	}
	out := make([]Conflict, 0, len(conflicts))
	for _, c := range conflicts {
		if c.Kind == "edge" || len(c.Events) != 2 {
			out = append(out, c)
			continue
		}
		a, b := byID[c.Events[0]], byID[c.Events[1]]
		if a == nil || b == nil {
			out = append(out, c)
			continue
		}
		masks := e.masksAt(c.Events, c.Peak)
		ma, mb := masks[a.order], masks[b.order]
		if ma == nil || mb == nil {
			out = append(out, c)
			continue
		}
		ratio := maskOverlapRatio(ma, mb, 0, 0)
		minRatio := 0.02
		if a.role != RoleDialogue && b.role != RoleDialogue {
			minRatio = 0.05
		}
		if ratio < minRatio {
			continue
		}
		imp := math.Max(a.importance, b.importance)
		sev := severityOf(ratio, imp, c.End-c.Start)
		suppressed, why := lowImportance([]*plannedEvent{a, b})
		surface := surfaceFor(c.Kind, sev, suppressed, mustSee(c.Kind, sev))
		reason := fmt.Sprintf("%s pixel-mask overlap %.1f%% for %.2fs", c.Kind, ratio*100, c.End-c.Start)
		if c.Kind == "sign-dialogue" {
			sign, dialogue := a, b
			signMask, dialogueMask := ma, mb
			if a.role == RoleDialogue {
				sign, dialogue = b, a
				signMask, dialogueMask = mb, ma
			}
			if drawsBehind(sign, dialogue) && signMask.InkCells > 0 {
				cov := float64(maskOverlap(signMask, dialogueMask, 0, 0)) / float64(signMask.InkCells)
				if cov >= hiddenSignCoverage {
					suppressed = false
					why = ""
					surface = SurfaceRequired
					reason += fmt.Sprintf("; sign is drawn beneath the dialogue and %.0f%% of its ink is covered", cov*100)
				}
			}
		}
		lvl := levelForSeverity(sev)
		c.OverlapRatio = round4(ratio)
		c.Severity = sev
		c.Level = lvl
		c.LevelName = levelName(lvl)
		c.Surface = surface
		c.Suppressed = suppressed
		c.SuppressReason = why
		c.Box = roundBox(ma.Box.Intersect(mb.Box))
		c.Reason = reason
		out = append(out, c)
	}
	return out
}
