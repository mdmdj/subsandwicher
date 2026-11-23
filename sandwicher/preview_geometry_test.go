package sandwicher

import (
	"math"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func geomStyle() *ASSStyle {
	return &ASSStyle{Val: map[string]string{
		"fontsize": "40", "alignment": "2", "marginl": "10", "marginr": "10", "marginv": "20",
		"outline": "1", "shadow": "0", "scalex": "100", "scaley": "100",
	}}
}

func TestTransformInfoFlags3DAndShear(t *testing.T) {
	d := &Dialogue{Text: `{\frz20\frx15\fax0.4}hello`}
	g := measureEvent(d, geomStyle(), 640, 360)
	info := g.transformInfo()
	if info == nil {
		t.Fatal("expected transform info")
	}
	if info.RotZ != 20 || info.RotX != 15 || info.SkewX != 0.4 {
		t.Fatalf("transform = %+v", info)
	}
	if !strings.Contains(info.Tags, `\frx15`) || !strings.Contains(info.Tags, `\fax0.4`) {
		t.Errorf("tags = %q", info.Tags)
	}
}

func TestRotatedBoxExpandsBeyondLayoutBox(t *testing.T) {
	plain := measureEvent(&Dialogue{Text: `hello`}, geomStyle(), 640, 360)
	rot := measureEvent(&Dialogue{Text: `{\frz45}hello`}, geomStyle(), 640, 360)
	lb := plain.layoutBoxAt(0, 0, 1, 640, 360)
	rb := rot.boxAt(0, 0, 1, 640, 360)
	if rb.W() <= lb.W() || rb.H() <= lb.H() {
		t.Fatalf("rotated box %+v did not expand layout box %+v", rb, lb)
	}
	// A rotation must keep the layout anchor, because \pos positions the
	// unrotated alignment point.
	anchor, ok := rot.layoutAnchorAt(0, 0, 1, 640, 360)
	if !ok || math.Abs(anchor.X-320) > 0.01 || math.Abs(anchor.Y-340) > 0.01 {
		t.Fatalf("layout anchor = %+v, %v; want 320,340", anchor, ok)
	}
}

func TestPerspectiveBoxFinite(t *testing.T) {
	g := measureEvent(&Dialogue{Text: `{\frx35\fry20}hello`}, geomStyle(), 640, 360)
	b := g.boxAt(0, 0, 1, 640, 360)
	if !b.Valid() || math.IsNaN(b.X0) || math.IsInf(b.Y1, 0) {
		t.Fatalf("perspective box = %+v", b)
	}
}

func TestEffectMotion(t *testing.T) {
	for _, s := range []string{"Banner;5;0;50", "Scroll up;0;100;5", "scroll down;0;100;5"} {
		if !effectMotion(s) {
			t.Errorf("effectMotion(%q) = false", s)
		}
	}
	for _, s := range []string{"", "karaoke", "fade"} {
		if effectMotion(s) {
			t.Errorf("effectMotion(%q) = true", s)
		}
	}
}

func TestEstimatedSamplesTrackMoveInsteadOfLifetimeUnion(t *testing.T) {
	d := &Dialogue{Text: `{\move(100,100,500,100)}hello`}
	g := measureEvent(d, geomStyle(), 640, 360)
	ev := &plannedEvent{
		d: d, geom: g, moving: true, startSec: 0, endSec: 4,
		order: 0, finalAlign: 2, bbox: g.lifetimeBox(0, 4, 640, 360),
	}
	samples := estimatedSamples(ev, 640, 360)
	if len(samples) < 3 {
		t.Fatalf("samples = %+v", samples)
	}
	first, last := samples[0], samples[len(samples)-1]
	if !first.Estimated || !last.Estimated {
		t.Fatal("estimated samples must be marked")
	}
	if math.Abs(first.Box.X0-last.Box.X0) < 300 {
		t.Fatalf("samples do not follow move: %+v -> %+v", first.Box, last.Box)
	}
	if ev.bbox.W() <= first.Box.W() {
		t.Fatalf("lifetime union %+v should be wider than one sample %+v", ev.bbox, first.Box)
	}
}

func TestApplyNudgeMovesRotationOrigin(t *testing.T) {
	st := geomStyle()
	d := &Dialogue{Text: `{\org(300,200)\frz30\pos(320,300)}hi`, Style: "Default"}
	applyPosition(d, Suggestion{Kind: "nudge", Dx: 10, Dy: -5, At: &Point{X: 330, Y: 295}}, st, 640, 360)
	if !strings.Contains(d.Text, `\pos(330,295)`) {
		t.Errorf("pos not moved: %s", d.Text)
	}
	if !strings.Contains(d.Text, `\org(310,195)`) {
		t.Errorf("org not moved with the text: %s", d.Text)
	}
}

func TestApplyMoveTranslatesRotationOrigin(t *testing.T) {
	st := geomStyle()
	d := &Dialogue{Text: `{\org(100,100)\move(200,200,300,250)}hi`, Style: "Default"}
	applyPosition(d, Suggestion{Kind: "nudge", Dx: 10, Dy: 20}, st, 640, 360)
	if !strings.Contains(d.Text, `\move(210,220,310,270)`) {
		t.Errorf("move not translated: %s", d.Text)
	}
	if !strings.Contains(d.Text, `\org(110,120)`) {
		t.Errorf("org not translated with move: %s", d.Text)
	}
}

func TestPreviewBoxesIncludeCollisionStacking(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not on PATH")
	}
	doc := assDoc("PlayResX: 640\nPlayResY: 360\n",
		"Top,Arial,40,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,0,0,7,20,20,40,1\n",
		"Dialogue: 0,0:00:01.00,0:00:05.00,Top,,0,0,0,,first label\n"+
			"Dialogue: 0,0:00:01.00,0:00:05.00,Top,,0,0,0,,second label\n")
	out := filepath.Join(t.TempDir(), "stack.ass")
	if err := writeDoc(doc, out); err != nil {
		t.Fatal(err)
	}
	boxes, err := PreviewBoxes(out, 2.0)
	if err != nil {
		t.Skipf("preview masks unavailable: %v", err)
	}
	if len(boxes) != 2 {
		t.Fatalf("boxes = %+v, want 2", boxes)
	}
	if boxes[0].ID != 0 || boxes[1].ID != 1 {
		t.Fatalf("box order = %+v", boxes)
	}
	// A solo render would put both at the same margin. Collision handling must
	// separate them vertically.
	if math.Abs(boxes[0].Box.Y0-boxes[1].Box.Y0) < 10 {
		t.Fatalf("collision stacking not applied: first %+v second %+v", boxes[0].Box, boxes[1].Box)
	}
	if boxes[1].Box.Y1 > boxes[0].Box.Y0 && boxes[0].Box.Y1 > boxes[1].Box.Y0 {
		t.Fatalf("stacked boxes still overlap vertically: first %+v second %+v", boxes[0].Box, boxes[1].Box)
	}
}
