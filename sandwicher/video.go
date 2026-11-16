package sandwicher

import (
	"bytes"
	"fmt"
	"math"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// videoContext measures the underlying video frame so placement can judge
// readability, not just subtitle-ink overlap. Frames are decoded once per seek
// into a small RGB grid. For a candidate box it reports:
//
//   - busyness: normalised edge density (text over very detailed footage is
//     hard to read even when it technically contrasts);
//   - background colour: compared with the event's own text colour to estimate
//     contrast, so overlaying a shape is allowed when the text still stands out.
//
// The context is optional; without ffmpeg or a video path every cost is 0.
type videoContext struct {
	path   string
	playW  float64
	playH  float64
	ffmpeg string

	mu     sync.Mutex
	cache  map[string]*frameStats
	failed map[string]bool
}

const (
	videoGridW      = 160
	videoGridH      = 90
	costVideoBusy   = 5.0
	costVideoWeak   = 3.5
	videoBusyCap    = 0.35
	videoMaxBusyCut = 0.65
)

// RGB is a display colour in 0..255 space.
type RGB struct {
	R, G, B float64
}

type frameStats struct {
	W, H  int
	Luma  []float64 // integral images, (W+1)*(H+1)
	Luma2 []float64
	R     []float64
	G     []float64
	B     []float64
	Busy  []float64
}

func newVideoContext(path string, playW, playH float64) *videoContext {
	if path == "" {
		return nil
	}
	ff := resolveBin("ffmpeg")
	if p, err := exec.LookPath(ff); err == nil {
		ff = p
	} else {
		return nil
	}
	return &videoContext{
		path:   path,
		playW:  playW,
		playH:  playH,
		ffmpeg: ff,
		cache:  map[string]*frameStats{},
		failed: map[string]bool{},
	}
}

func (v *videoContext) statsAt(t float64) *frameStats {
	if v == nil {
		return nil
	}
	key := fmt.Sprintf("%.1f", math.Round(math.Max(0, t)*10)/10)
	v.mu.Lock()
	if f, ok := v.cache[key]; ok {
		v.mu.Unlock()
		return f
	}
	if v.failed[key] {
		v.mu.Unlock()
		return nil
	}
	v.mu.Unlock()

	f, err := v.extract(math.Max(0, t))
	v.mu.Lock()
	defer v.mu.Unlock()
	if err != nil {
		v.failed[key] = true
		return nil
	}
	v.cache[key] = f
	return f
}

func (v *videoContext) extract(t float64) (*frameStats, error) {
	args := []string{
		"-hide_banner", "-loglevel", "error", "-nostdin",
		"-ss", fmt.Sprintf("%.3f", t),
		"-i", v.path,
		"-frames:v", "1",
		"-vf", fmt.Sprintf("scale=%d:%d:flags=bilinear,format=rgb24", videoGridW, videoGridH),
		"-f", "rawvideo", "-",
	}
	cmd := exec.Command(v.ffmpeg, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("video frame: %w: %s", err, stderr.String())
	}
	want := videoGridW * videoGridH * 3
	if len(out) < want {
		return nil, fmt.Errorf("video frame: short raw frame (%d bytes)", len(out))
	}
	return newFrameStats(out[:want]), nil
}

// newFrameStats turns an RGB frame into integral images of luma, squared luma,
// colour and edge density. Edge density is box-blurred so a text box spanning a
// few glyph strokes sees the surrounding region rather than one sharp edge.
func newFrameStats(rgb []byte) *frameStats {
	w, h := videoGridW, videoGridH
	luma := make([]float64, w*h)
	luma2 := make([]float64, w*h)
	r := make([]float64, w*h)
	g := make([]float64, w*h)
	b := make([]float64, w*h)
	for i := 0; i < w*h; i++ {
		rr := float64(rgb[i*3])
		gg := float64(rgb[i*3+1])
		bb := float64(rgb[i*3+2])
		l := 0.2126*rr + 0.7152*gg + 0.0722*bb
		r[i], g[i], b[i] = rr, gg, bb
		luma[i], luma2[i] = l, l*l
	}
	edge := make([]float64, w*h)
	for y := 1; y < h-1; y++ {
		for x := 1; x < w-1; x++ {
			i := y*w + x
			gx := math.Abs(luma[i+1] - luma[i-1])
			gy := math.Abs(luma[i+w] - luma[i-w])
			e := (gx + gy) / 120.0
			if e > 1 {
				e = 1
			}
			edge[i] = e
		}
	}
	busy := make([]float64, w*h)
	for y := 1; y < h-1; y++ {
		for x := 1; x < w-1; x++ {
			s := 0.0
			for yy := y - 1; yy <= y+1; yy++ {
				for xx := x - 1; xx <= x+1; xx++ {
					s += edge[yy*w+xx]
				}
			}
			busy[y*w+x] = s / 9
		}
	}
	sw := w + 1
	mk := func(vals []float64) []float64 {
		sum := make([]float64, sw*(h+1))
		for y := 0; y < h; y++ {
			row := 0.0
			for x := 0; x < w; x++ {
				row += vals[y*w+x]
				sum[(y+1)*sw+(x+1)] = sum[y*sw+(x+1)] + row
			}
		}
		return sum
	}
	return &frameStats{
		W: w, H: h,
		Luma: mk(luma), Luma2: mk(luma2),
		R: mk(r), G: mk(g), B: mk(b), Busy: mk(busy),
	}
}

type boxStats struct {
	Luma, Luma2 float64
	R, G, B     float64
	Busy        float64
	Area        float64
}

func (f *frameStats) sample(b Box, playW, playH float64) boxStats {
	if f == nil || playW <= 0 || playH <= 0 {
		return boxStats{}
	}
	clamp := func(v, hi int) int {
		if v < 0 {
			return 0
		}
		if v > hi {
			return hi
		}
		return v
	}
	x0 := clamp(int(math.Floor(b.X0/playW*float64(f.W))), f.W)
	y0 := clamp(int(math.Floor(b.Y0/playH*float64(f.H))), f.H)
	x1 := clamp(int(math.Ceil(b.X1/playW*float64(f.W))), f.W)
	y1 := clamp(int(math.Ceil(b.Y1/playH*float64(f.H))), f.H)
	if x1 <= x0 || y1 <= y0 {
		return boxStats{}
	}
	sw := f.W + 1
	area := float64((x1 - x0) * (y1 - y0))
	mean := func(s []float64) float64 {
		at := func(x, y int) float64 { return s[y*sw+x] }
		return (at(x1, y1) - at(x0, y1) - at(x1, y0) + at(x0, y0)) / area
	}
	return boxStats{
		Luma: mean(f.Luma), Luma2: mean(f.Luma2),
		R: mean(f.R), G: mean(f.G), B: mean(f.B),
		Busy: mean(f.Busy), Area: area,
	}
}

// relLum is the WCAG relative luminance of an 0..255 RGB colour.
func relLum(c RGB) float64 {
	lin := func(v float64) float64 {
		v /= 255
		if v <= 0.03928 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(c.R) + 0.7152*lin(c.G) + 0.0722*lin(c.B)
}

// readability estimates how well text holds up over a sampled background.
// Luma contrast and colour distance are combined, so light text over a dark
// shape (or a saturated colour against a different hue) is not punished just
// because the area is detailed.
func readability(fg RGB, bg boxStats) float64 {
	bgCol := RGB{R: bg.R, G: bg.G, B: bg.B}
	fgL, bgL := relLum(fg), relLum(bgCol)
	hi, lo := math.Max(fgL, bgL), math.Min(fgL, bgL)
	contrast := (hi + 0.05) / (lo + 0.05)
	// 1:1 .. 7:1 mapped to 0..1.
	contrastNorm := math.Max(0, math.Min(1, (contrast-1)/6))
	dist := math.Hypot(math.Hypot(fg.R-bgCol.R, fg.G-bgCol.G), fg.B-bgCol.B) / 441.7
	distNorm := math.Min(1, dist/0.65)
	read := 0.55*contrastNorm + 0.55*distNorm
	// High local variance means only part of the text has this contrast.
	if bg.Area > 0 {
		variance := math.Max(0, bg.Luma2-bg.Luma*bg.Luma)
		read *= 1 - 0.30*math.Min(1, math.Sqrt(variance)/80)
	}
	return math.Max(0, math.Min(1, read))
}

// videoReadabilityCost charges for weak contrast and, more mildly, for busy
// footage. High-contrast text can overlap shapes; a flat but similarly coloured
// background is still penalised.
func videoReadabilityCost(v *videoContext, at float64, box Box, text RGB, playW, playH float64) float64 {
	if v == nil {
		return 0
	}
	st := v.statsAt(at)
	if st == nil {
		return 0
	}
	bg := st.sample(box, playW, playH)
	if bg.Area <= 0 {
		return 0
	}
	read := readability(text, bg)
	busy := math.Min(bg.Busy, videoBusyCap)
	return costVideoBusy*busy*(1-videoMaxBusyCut*read) + costVideoWeak*(1-read)
}

var reInlineColor = regexp.MustCompile(`\\[1-4]?c&H([0-9A-Fa-f]{6})&`)

// parseASSColor reads an ASS &HAABBGGRR / &HBBGGRR value as RGB.
func parseASSColor(v string) (RGB, bool) {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(strings.TrimPrefix(v, "&H"), "&h")
	v = strings.TrimSuffix(v, "&")
	if len(v) > 6 {
		v = v[len(v)-6:]
	}
	if len(v) != 6 {
		return RGB{}, false
	}
	n, err := strconv.ParseUint(v, 16, 32)
	if err != nil {
		return RGB{}, false
	}
	return RGB{
		R: float64((n >> 0) & 0xFF),
		G: float64((n >> 8) & 0xFF),
		B: float64((n >> 16) & 0xFF),
	}, true
}

// eventTextColor prefers an inline \c override, then the style's primary
// colour. Signs are usually colourful, so this is what contrast should use.
func eventTextColor(d *Dialogue, st *ASSStyle) RGB {
	if d != nil {
		if m := reInlineColor.FindStringSubmatch(d.Text); m != nil {
			if c, ok := parseASSColor(m[1]); ok {
				return c
			}
		}
	}
	if st != nil {
		if c, ok := parseASSColor(st.Val["primarycolour"]); ok {
			return c
		}
	}
	return RGB{R: 255, G: 255, B: 255}
}
