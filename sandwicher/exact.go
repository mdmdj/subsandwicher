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
	"sort"
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
	// includeActive renders every event that is on screen at the seek. libass
	// uses those events for collision stacking, so a sign rendered alone sits at
	// the margin instead of where it is actually drawn.
	includeActive bool
	// preserveTargetOpacity keeps the target's fade/alpha, so an invisible event
	// produces no ink. Merge-time masks strip opacity to measure shape.
	preserveTargetOpacity bool
	// coordScale converts render pixels back to script/PlayRes units.
	coordScale float64
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
	// Coverage is a coarse hex occupancy grid (16x8 cells, hex byte per cell)
	// used by the preview to distinguish ink from the empty corners of a
	// rotated/skewed bounding box.
	Coverage string
}

const (
	exactCell = 1
	coverCols = 16
	coverRows = 8
)

func newExactContext(assPath string, playW, playH float64) *exactContext {
	return newExactContextOpts(assPath, playW, playH, false, false)
}

func newPreviewExactContext(assPath string, playW, playH float64) *exactContext {
	// Preview hit boxes do not need full video resolution. Rendering at about
	// 960x540 is several times faster and stays well within a few script pixels.
	scale := 1.0
	if playW > 960 {
		scale = math.Min(scale, 960/playW)
	}
	if playH > 540 {
		scale = math.Min(scale, 540/playH)
	}
	if scale < 0.25 {
		scale = 0.25
	}
	ex := newExactContextOpts(assPath, math.Round(playW*scale), math.Round(playH*scale), true, true)
	if ex != nil {
		ex.coordScale = 1 / scale
	}
	return ex
}

func newExactContextOpts(assPath string, playW, playH float64, includeActive, preserveOpacity bool) *exactContext {
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
		doc:                   doc,
		playW:                 playW,
		playH:                 playH,
		ffmpeg:                ff,
		dir:                   dir,
		cache:                 map[string]*inkMask{},
		failed:                map[string]bool{},
		sem:                   make(chan struct{}, 4),
		includeActive:         includeActive,
		preserveTargetOpacity: preserveOpacity,
		coordScale:            1,
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
	dur, rel, doc := e.renderDocFor(eventID, seek)

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

// renderDocFor builds the temporary document used for one event mask. In
// preview mode every event on screen is included so libass collision stacking
// picks the same vertical position as the real frame; other events are made
// transparent so only the target contributes ink.
func (e *exactContext) renderDocFor(eventID int, seek float64) (dur, rel float64, doc *ASSDoc) {
	src := e.doc.Dialogues[eventID]
	if !e.includeActive {
		start := float64(src.startCS) / 100
		end := float64(parseTimeCS(src.End)) / 100
		dur = math.Max(1.0, end-start)
		rel = math.Max(0.05, math.Min(seek-start, dur-0.05))
		cp := *src
		cp.Start = "0:00:00.00"
		cp.End = formatCS(dur)
		cp.startCS = 0
		cp.Text = stripOpacityTags(cp.Text)
		return dur, rel, &ASSDoc{InfoRaw: e.doc.InfoRaw, Info: e.doc.Info, Styles: e.doc.Styles, Dialogues: []*Dialogue{&cp}, Other: e.doc.Other}
	}

	type activeEvent struct {
		d          *Dialogue
		start, end float64
	}
	active := make([]activeEvent, 0, 16)
	base := math.Inf(1)
	maxEnd := 0.0
	for _, d := range e.doc.Dialogues {
		start := float64(d.startCS) / 100
		end := float64(parseTimeCS(d.End)) / 100
		if seek < start-0.01 || seek > end+0.01 {
			continue
		}
		active = append(active, activeEvent{d: d, start: start, end: end})
		if start < base {
			base = start
		}
		if end > maxEnd {
			maxEnd = end
		}
	}
	if math.IsInf(base, 1) {
		start := float64(src.startCS) / 100
		end := float64(parseTimeCS(src.End)) / 100
		base = start
		maxEnd = end
		active = append(active, activeEvent{d: src, start: start, end: end})
	}
	dur = math.Max(1.0, maxEnd-base)
	rel = math.Max(0.02, math.Min(seek-base, dur-0.02))

	dialogues := make([]*Dialogue, 0, len(active))
	targetFound := false
	for _, a := range active {
		cp := *a.d
		cp.Start = formatCS(a.start - base)
		cp.End = formatCS(a.end - base)
		cp.startCS = int(math.Round((a.start - base) * 100))
		if cp.startCS < 0 {
			cp.startCS = 0
		}
		if a.d == src {
			targetFound = true
			if !e.preserveTargetOpacity {
				cp.Text = stripOpacityTags(cp.Text)
			}
		} else {
			// Keep the event in layout (collision stacking) but hide its ink.
			cp.Text = "{\\alpha&HFF&}" + stripOpacityTags(cp.Text)
		}
		dialogues = append(dialogues, &cp)
	}
	if !targetFound {
		start := float64(src.startCS) / 100
		end := float64(parseTimeCS(src.End)) / 100
		cp := *src
		cp.Start = formatCS(start - base)
		cp.End = formatCS(end - base)
		cp.startCS = int(math.Round((start - base) * 100))
		cp.Text = stripOpacityTags(cp.Text)
		dialogues = append(dialogues, &cp)
	}
	return dur, rel, &ASSDoc{InfoRaw: e.doc.InfoRaw, Info: e.doc.Info, Styles: e.doc.Styles, Dialogues: dialogues, Other: e.doc.Other}
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
	// Encode occupancy relative to the ink bbox, one hex byte per grid cell.
	// The preview maps a click into this same bbox, so the grid must not use the
	// full canvas (a small subtitle would otherwise occupy a fraction of a cell).
	bw := maxX + 1 - minX
	bh := maxY + 1 - minY
	cov := make([]byte, coverCols*coverRows*2)
	const hexDigits = "0123456789abcdef"
	for gy := 0; gy < coverRows; gy++ {
		y0 := minY + gy*bh/coverRows
		y1 := minY + (gy+1)*bh/coverRows
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for gx := 0; gx < coverCols; gx++ {
			x0 := minX + gx*bw/coverCols
			x1 := minX + (gx+1)*bw/coverCols
			if x1 <= x0 {
				x1 = x0 + 1
			}
			count := 0
			for y := y0; y < y1 && y <= maxY; y++ {
				for x := x0; x < x1 && x <= maxX; x++ {
					if m.bit(x, y) {
						count++
					}
				}
			}
			area := (x1 - x0) * (y1 - y0)
			v := 0
			if area > 0 {
				v = count * 255 / area
			}
			if v > 255 {
				v = 255
			}
			pos := (gy*coverCols + gx) * 2
			cov[pos] = hexDigits[v>>4]
			cov[pos+1] = hexDigits[v&0xf]
		}
	}
	m.Coverage = string(cov)
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

// samplesFor returns ink boxes across an animated event's lifetime. A single
// mask cannot describe \t transforms, because the glyph outline changes while
// the event is on screen. Static events keep one sample.
func (e *exactContext) samplesFor(ev *plannedEvent, primarySeek float64, primary *inkMask) []BoxSample {
	if e == nil || ev == nil || ev.geom == nil || primary == nil {
		return nil
	}
	// \move is translated analytically by the preview. \t transforms and
	// Effect-column motion (Banner/Scroll) need real boxes across the lifetime.
	// Effect motion is not `moving`: it has no \move tag and positional fixes
	// still work the same way.
	if !ev.geom.animated && !effectMotion(ev.d.Effect) {
		return nil
	}
	out := []BoxSample{{Time: round2(primarySeek), Box: roundBox(primary.Box), Coverage: primary.Coverage}}
	start, end := ev.startSec, ev.endSec
	if end-start < 0.12 {
		return out
	}
	for _, f := range []float64{0.05, 0.15, 0.3, 0.45, 0.6, 0.75, 0.9, 0.97} {
		t := start + (end-start)*f
		if t < start+0.03 {
			t = start + 0.03
		}
		if t > end-0.03 {
			t = end - 0.03
		}
		if math.Abs(t-primarySeek) < 0.05 {
			continue
		}
		m := e.maskFor(ev.order, t)
		if m == nil || !m.Box.Valid() {
			continue
		}
		out = append(out, BoxSample{Time: round2(t), Box: roundBox(m.Box), Coverage: m.Coverage})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Time < out[j].Time })
	return out
}

// PreviewBox is one current-frame event box for preview hit-testing.
type PreviewBox struct {
	ID       int    `json:"id"`
	Box      Box    `json:"box"`
	Coverage string `json:"coverage,omitempty"`
}

// PreviewBoxes renders the events visible at time in the given .ass and returns
// their libass-correct boxes. Unlike the merge-time masks these include
// collision stacking with other events, fades/alpha, and Effect-column motion.
func PreviewBoxes(assPath string, time float64) ([]PreviewBox, error) {
	doc, err := parseASSFile(assPath)
	if err != nil {
		return nil, err
	}
	playW, playH := playRes(doc)
	ex := newPreviewExactContext(assPath, playW, playH)
	if ex == nil {
		return nil, fmt.Errorf("exact preview rendering is unavailable")
	}
	defer ex.close()

	var ids []int
	for i, d := range doc.Dialogues {
		start := float64(d.startCS) / 100
		end := float64(parseTimeCS(d.End)) / 100
		if time >= start-0.01 && time <= end+0.01 {
			ids = append(ids, i)
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	masks := ex.masksAt(ids, time)
	out := make([]PreviewBox, 0, len(ids))
	s := ex.coordScale
	if s <= 0 {
		s = 1
	}
	for _, id := range ids {
		m := masks[id]
		if m == nil || !m.Box.Valid() {
			continue
		}
		b := m.Box
		if s != 1 {
			b = Box{X0: b.X0 * s, Y0: b.Y0 * s, X1: b.X1 * s, Y1: b.Y1 * s}
		}
		out = append(out, PreviewBox{ID: id, Box: roundBox(b), Coverage: m.Coverage})
	}
	return out, nil
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
