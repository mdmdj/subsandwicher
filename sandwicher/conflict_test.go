package sandwicher

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func assSource(info, styles, events string) string {
	return "[Script Info]\n" + info +
		"\n[V4+ Styles]\nFormat: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding\n" +
		styles + "\n[Events]\nFormat: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\n" + events + "\n"
}

func assDoc(info, styles, events string) *ASSDoc {
	return parseASS([]byte(assSource(info, styles, events)))
}

func TestClassifyRole(t *testing.T) {
	cases := []struct {
		name  string
		style string
		text  string
		want  Role
	}{
		{"positioned", "Default", `{\pos(10,20)}sign`, RoleSign},
		{"moved", "Default", `{\move(1,2,3,4)}sign`, RoleSign},
		{"drawing", "Default", `{\p1}m 0 0 l 10 0{\p0}`, RoleDrawing},
		{"sign style", "sign_Arial", `caption`, RoleSign},
		{"dialogue", "Default", `hello`, RoleDialogue},
		{"an only", "Default", `{\an8}hello`, RoleDialogue},
	}
	for _, c := range cases {
		d := &Dialogue{Text: c.text}
		if got := classifyRole(d, c.style); got != c.want {
			t.Errorf("%s: classifyRole = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestScaleInlineGeometry(t *testing.T) {
	in := `{\fs20\bord2\shad1\blur0.5\fsp1\pos(10,20)\move(1,2,3,4,0,500)\org(5,6)\an2\fad(100,200)\t(0,500,\fs20)}x{\p1}m 0 0 l 100 0{\p0}`
	out, tags := scaleInlineGeometry(in, 3)
	for _, want := range []string{
		`\fs60`, `\bord6`, `\shad3`, `\blur1.5`, `\fsp3`,
		`\pos(30,60)`, `\move(3,6,9,12,0,500)`, `\org(15,18)`,
		`\an2`, `\fad(100,200)`, `\t(0,500,\fs60)`, `m 0 0 l 300 0`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("scaled output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, `\fs20`) || strings.Contains(out, `\pos(10,20)`) {
		t.Errorf("unscaled value survived: %s", out)
	}
	if len(tags) == 0 {
		t.Error("expected scaled tag names")
	}
}

func TestPositionedSignKeepsAnchor(t *testing.T) {
	prim := assDoc("PlayResX: 640\nPlayResY: 360\n",
		"Style: Bottom,Arial,20,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,0,0,2,10,10,20,1\n",
		"Dialogue: 0,0:00:01.00,0:00:03.00,Bottom,,0,0,0,,hello\n")
	sec := assDoc("PlayResX: 640\nPlayResY: 360\n",
		"Style: sign_Arial,Arial,20,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,0,0,2,10,10,10,1\n",
		"Dialogue: 0,0:00:01.00,0:00:02.00,sign_Arial,,0,0,0,,{\\pos(320,330)}SIGN\n")

	out := filepath.Join(t.TempDir(), "out.ass")
	_, _, _, rep, err := mergeAndWrite(prim, sec, "eng", 1, out, JobInfo{}, false)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, "Style: sign_Arial-eng,Arial,20") &&
		!strings.Contains(text, ",2,10,10,10,1") {
		t.Errorf("sign style alignment was rewritten:\n%s", text)
	}
	if !strings.Contains(text, `{\pos(320,330)}SIGN`) {
		t.Errorf("positioned sign lost its anchor/tags:\n%s", text)
	}
	if strings.Contains(text, `{\an8}`) {
		t.Errorf("sign was re-anchored to top:\n%s", text)
	}
	if rep == nil || len(rep.Events) != 2 {
		t.Fatalf("report events = %v, want 2", rep)
	}
	for _, ev := range rep.Events {
		if len(ev.Decisions) == 0 {
			t.Errorf("event %d has no decisions", ev.ID)
		}
	}
}

func TestOrderAndDecisions(t *testing.T) {
	prim := assDoc("PlayResX: 640\nPlayResY: 360\n",
		"Style: Bottom,Arial,20,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,0,0,2,10,10,20,1\n",
		"Dialogue: 0,0:00:01.00,0:00:03.00,Bottom,,0,0,0,,hello\n")
	sec := assDoc("PlayResX: 640\nPlayResY: 360\n",
		"Style: sign_Arial,Arial,20,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,0,0,2,10,10,10,1\n",
		"Dialogue: 0,0:00:01.00,0:00:02.00,sign_Arial,,0,0,0,,{\\pos(320,330)}SIGN\n")

	out := filepath.Join(t.TempDir(), "out.ass")
	_, _, _, rep, err := mergeAndWrite(prim, sec, "eng", 1, out, JobInfo{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rep.Events[0].Text, "SIGN") {
		t.Errorf("sign should sort before dialogue at the same start, got %q first", rep.Events[0].Text)
	}
	foundOrder := false
	for _, ev := range rep.Events {
		for _, d := range ev.Decisions {
			if d.Rule == "order.role-priority" {
				foundOrder = true
			}
		}
	}
	if !foundOrder {
		t.Error("expected an order.role-priority decision")
	}
}

func TestConflictWindowAndSuggestions(t *testing.T) {
	prim := assDoc("PlayResX: 640\nPlayResY: 360\n",
		"Style: Bottom,Arial,20,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,0,0,2,10,10,20,1\n",
		"Dialogue: 0,0:00:10.00,0:00:14.00,Bottom,,0,0,0,,a long primary dialogue line\n")
	sec := assDoc("PlayResX: 640\nPlayResY: 360\n",
		"Style: sign_Arial,Arial,20,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,0,0,2,10,10,10,1\n",
		"Dialogue: 0,0:00:10.50,0:00:13.00,sign_Arial,,0,0,0,,{\\pos(320,330)}SIGN\n")

	out := filepath.Join(t.TempDir(), "out.ass")
	_, _, _, rep, err := mergeAndWrite(prim, sec, "eng", 1, out, JobInfo{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Conflicts) == 0 || len(rep.Windows) == 0 {
		t.Fatalf("expected a conflict and a window, got %d/%d", len(rep.Conflicts), len(rep.Windows))
	}
	w := rep.Windows[0]
	if w.Surface != SurfaceReview && w.Surface != SurfaceRequired {
		t.Errorf("window surface = %q, want review/required", w.Surface)
	}
	if len(w.Suggestions) == 0 || !w.Suggestions[0].Recommended {
		t.Errorf("expected a ranked suggestion, got %+v", w.Suggestions)
	}
	if w.LevelName == "" {
		t.Error("window levelName is empty")
	}
	if rep.Summary.ReviewEvents == 0 {
		t.Error("expected review events in summary")
	}
}

func TestMovingLabelIsSuppressed(t *testing.T) {
	prim := assDoc("PlayResX: 640\nPlayResY: 360\n",
		"Style: Bottom,Arial,20,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,0,0,2,10,10,20,1\n",
		"Dialogue: 0,0:00:10.00,0:00:14.00,Bottom,,0,0,0,,a long primary dialogue line\n")
	sec := assDoc("PlayResX: 640\nPlayResY: 360\n",
		"Style: sign_Arial,Arial,20,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,0,0,2,10,10,10,1\n",
		"Dialogue: 0,0:00:10.80,0:00:11.10,sign_Arial,,0,0,0,,{\\move(320,330,320,90,0,300)}LABEL\n")

	out := filepath.Join(t.TempDir(), "out.ass")
	_, _, _, rep, err := mergeAndWrite(prim, sec, "eng", 1, out, JobInfo{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Conflicts) == 0 {
		t.Fatal("expected a conflict for the moving label")
	}
	c := rep.Conflicts[0]
	if !c.Suppressed || c.Surface != SurfaceNone {
		t.Errorf("moving label conflict should be suppressed, got suppressed=%v surface=%q", c.Suppressed, c.Surface)
	}
	if len(rep.Windows) != 0 {
		t.Errorf("suppressed conflict should not create a window, got %d", len(rep.Windows))
	}
}

func TestScanASSReport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "merged.ass")
	data := assSource("PlayResX: 640\nPlayResY: 360\n",
		"Style: Bottom,Arial,20,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,0,0,2,10,10,20,1\n"+
			"Style: sign_Arial-eng,Arial,20,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,0,0,2,10,10,10,1\n",
		"Dialogue: 0,0:00:01.00,0:00:03.00,Bottom,,0,0,0,,hello\n"+
			"Dialogue: 0,0:00:01.50,0:00:02.50,sign_Arial-eng,,0,0,0,,{\\pos(320,330)}SIGN\n")
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err := ScanASS(path, ScanOptions{SecondarySuffix: "-eng"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Events) != 2 {
		t.Fatalf("events = %d, want 2", len(rep.Events))
	}
	byText := map[string]EventDecision{}
	for _, e := range rep.Events {
		byText[e.Text] = e
	}
	if byText["SIGN"].Track != "secondary" {
		t.Errorf("SIGN track = %q, want secondary", byText["SIGN"].Track)
	}
	if byText["hello"].Track != "primary" {
		t.Errorf("hello track = %q, want primary", byText["hello"].Track)
	}
	if len(rep.Conflicts) == 0 {
		t.Error("expected a conflict in the scan")
	}
	for _, e := range rep.Events {
		if len(e.Decisions) == 0 {
			t.Errorf("event %d has no decisions", e.ID)
		}
	}
}

func TestReportJSONMarshals(t *testing.T) {
	prim := assDoc("PlayResX: 640\nPlayResY: 360\n",
		"Style: Bottom,Arial,20,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,0,0,2,10,10,20,1\n",
		"Dialogue: 0,0:00:01.00,0:00:03.00,Bottom,,0,0,0,,hello\n")
	sec := assDoc("PlayResX: 640\nPlayResY: 360\n",
		"Style: sign_Arial,Arial,20,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,0,0,2,10,10,10,1\n",
		"Dialogue: 0,0:00:01.20,0:00:02.60,sign_Arial,,0,0,0,,{\\pos(320,330)}SIGN\n")
	out := filepath.Join(t.TempDir(), "out.ass")
	_, _, _, rep, err := mergeAndWrite(prim, sec, "eng", 1, out, JobInfo{}, false)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if rep.Summary.ByCause == nil {
		t.Error("byCause should be populated")
	}
	for _, c := range rep.Conflicts {
		if c.LevelName == "" || c.Cause == "" {
			t.Errorf("conflict %d missing levelName/cause: %+v", c.ID, c)
		}
	}
}

func TestPickTrackPrefersFullOverPartial(t *testing.T) {
	streams := []Stream{
		{Index: 3, CodecName: "ass", Tags: map[string]string{"language": "eng", "title": "English (Signs) [Generated]"}},
		{Index: 4, CodecName: "ass", Tags: map[string]string{"language": "eng", "title": "English (Full Subtitle)"}},
		{Index: 5, CodecName: "ass", Tags: map[string]string{"language": "eng", "title": "English (Forced)"}},
	}
	s, err := PickTrack(streams, "en", -1, "secondary")
	if err != nil {
		t.Fatal(err)
	}
	if s.Index != 4 {
		t.Errorf("PickTrack chose #%d, want #4 (full) over signs/forced", s.Index)
	}
	// An explicit override still wins.
	if s, err := PickTrack(streams, "en", 3, "secondary"); err != nil || s.Index != 3 {
		t.Errorf("override should win, got %v, %v", s, err)
	}
}

func TestInferTracks(t *testing.T) {
	doc := assDoc("PlayResX: 640\nPlayResY: 360\n",
		"Style: BottomCenter,Arial,20,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,0,0,2,10,10,20,1\n"+
			"Style: Default-eng,Gandhi Sans,20,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,0,0,8,10,10,20,1\n"+
			"Style: sign_Arial-eng,Gandhi Sans,20,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,0,0,2,10,10,10,1\n",
		"Dialogue: 0,0:00:01.00,0:00:02.00,BottomCenter,,0,0,0,,one\n"+
			"Dialogue: 0,0:00:03.00,0:00:04.00,BottomCenter,,0,0,0,,two\n"+
			"Dialogue: 0,0:00:05.00,0:00:06.00,BottomCenter,,0,0,0,,three\n"+
			"Dialogue: 0,0:00:02.00,0:00:03.00,Default-eng,,0,0,0,,alpha\n"+
			"Dialogue: 0,0:00:04.00,0:00:05.00,Default-eng,,0,0,0,,beta\n"+
			"Dialogue: 0,0:00:06.00,0:00:07.00,Default-eng,,0,0,0,,gamma\n"+
			"Dialogue: 0,0:00:07.00,0:00:08.00,sign_Arial-eng,,0,0,0,,{\\pos(320,330)}SIGN\n")
	inf := InferTracks(doc)
	if inf.Confidence < 0.5 {
		t.Fatalf("confidence = %.2f, want >= 0.5 (%s)", inf.Confidence, inf.Note)
	}
	if got := inf.StyleTracks["BottomCenter"]; got != "primary" {
		t.Errorf("BottomCenter = %q, want primary", got)
	}
	if got := inf.StyleTracks["Default-eng"]; got != "secondary" {
		t.Errorf("Default-eng = %q, want secondary", got)
	}
	if got := inf.StyleTracks["sign_Arial-eng"]; got != "secondary" {
		t.Errorf("sign = %q, want secondary", got)
	}
}

func TestInferTracksLowConfidence(t *testing.T) {
	doc := assDoc("PlayResX: 640\nPlayResY: 360\n",
		"Style: A,Arial,20,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,0,0,2,10,10,20,1\n"+
			"Style: B,Arial,22,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,0,0,2,10,10,20,1\n",
		"Dialogue: 0,0:00:01.00,0:00:02.00,A,,0,0,0,,one\n"+
			"Dialogue: 0,0:00:03.00,0:00:04.00,A,,0,0,0,,two\n"+
			"Dialogue: 0,0:00:05.00,0:00:06.00,A,,0,0,0,,three\n"+
			"Dialogue: 0,0:00:02.00,0:00:03.00,B,,0,0,0,,alpha\n"+
			"Dialogue: 0,0:00:04.00,0:00:05.00,B,,0,0,0,,beta\n")
	inf := InferTracks(doc)
	if inf.Confidence >= 0.5 {
		t.Fatalf("confidence = %.2f, want < 0.5 for two same-lane families", inf.Confidence)
	}
	if inf.StyleTracks["A"] != "primary" || inf.StyleTracks["B"] != "secondary" {
		t.Errorf("assignments = %v", inf.StyleTracks)
	}
}

func TestScanASSUsesClassifierWithoutSuffix(t *testing.T) {
	path := filepath.Join(t.TempDir(), "merged.ass")
	data := assSource("PlayResX: 640\nPlayResY: 360\n",
		"Style: BottomCenter,Arial,20,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,0,0,2,10,10,20,1\n"+
			"Style: Default-eng,Gandhi Sans,20,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,0,0,8,10,10,20,1\n",
		"Dialogue: 0,0:00:01.00,0:00:03.00,BottomCenter,,0,0,0,,hello\n"+
			"Dialogue: 0,0:00:01.50,0:00:02.50,Default-eng,,0,0,0,,world\n")
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err := ScanASS(path, ScanOptions{SecondarySuffix: ""})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Job.TrackSource != "classifier" {
		t.Errorf("track source = %q, want classifier", rep.Job.TrackSource)
	}
	if rep.Tracks == nil || rep.Tracks.Confidence < 0.5 {
		t.Errorf("track inference = %+v", rep.Tracks)
	}
	byText := map[string]EventDecision{}
	for _, e := range rep.Events {
		byText[e.Text] = e
	}
	if byText["hello"].Track != "primary" || byText["world"].Track != "secondary" {
		t.Errorf("tracks = hello:%q world:%q", byText["hello"].Track, byText["world"].Track)
	}
	found := false
	for _, e := range rep.Events {
		for _, d := range e.Decisions {
			if d.Rule == "track.source" {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected track.source decisions")
	}
}

func TestPlacementCostAndAnchor(t *testing.T) {
	if got := anchorForBox(Box{0, 0, 100, 20}, 2); got.X != 50 || got.Y != 20 {
		t.Errorf("anchor an2 = %+v, want (50,20)", got)
	}
	if got := anchorForBox(Box{0, 0, 100, 20}, 8); got.X != 50 || got.Y != 0 {
		t.Errorf("anchor an8 = %+v, want (50,0)", got)
	}
	if got := boxFromAnchor(Point{50, 20}, 100, 20, 2); got != (Box{0, 0, 100, 20}) {
		t.Errorf("boxFromAnchor = %+v", got)
	}

	st := &ASSStyle{Val: map[string]string{
		"alignment": "2", "fontsize": "20", "marginl": "10", "marginr": "10",
		"marginv": "20", "outline": "0", "shadow": "0", "scalex": "100", "scaley": "100",
	}}
	d := &Dialogue{Text: "hello", Start: "0:00:00.00", End: "0:00:10.00"}
	g := measureEvent(d, st, 640, 360)
	ob := &plannedEvent{role: RoleDialogue, geom: g, startSec: 0, endSec: 10}
	ev := &plannedEvent{role: RoleSign, geom: g, startSec: 0, endSec: 10}
	box := g.boxAt(1, 0, 10, 640, 360)
	overlap := placementCost(box, ev, []*plannedEvent{ob}, nil, 1, 640, 360, 0, 0, nil)
	clear := placementCost(box.Translate(0, -200), ev, []*plannedEvent{ob}, nil, 1, 640, 360, 0, 0, nil)
	if clear >= overlap {
		t.Errorf("clear cost %.2f should be lower than overlap cost %.2f", clear, overlap)
	}
}

func TestPlacementSuggestionsRanked(t *testing.T) {
	prim := assDoc("PlayResX: 640\nPlayResY: 360\n",
		"Style: Bottom,Arial,20,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,0,0,2,10,10,20,1\n",
		"Dialogue: 0,0:00:10.00,0:00:14.00,Bottom,,0,0,0,,a long primary dialogue line\n")
	sec := assDoc("PlayResX: 640\nPlayResY: 360\n",
		"Style: sign_Arial,Arial,20,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,0,0,2,10,10,10,1\n",
		"Dialogue: 0,0:00:10.50,0:00:13.00,sign_Arial,,0,0,0,,{\\pos(320,330)}SIGN\n")
	out := filepath.Join(t.TempDir(), "out.ass")
	_, _, _, rep, err := mergeAndWrite(prim, sec, "eng", 1, out, JobInfo{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Windows) == 0 {
		t.Fatal("no windows")
	}
	w := rep.Windows[0]
	if len(w.Suggestions) == 0 {
		t.Fatal("no suggestions")
	}
	best := w.Suggestions[0]
	if !best.Recommended {
		t.Error("first suggestion should be recommended")
	}
	if best.At == nil {
		t.Error("suggestion should carry an anchor point")
	}
	if best.OriginalCost <= 0 {
		t.Errorf("original cost = %.2f, want > 0", best.OriginalCost)
	}
	maxAuto := 60.0 * (360.0 / 360.0)
	for _, s := range w.Suggestions {
		if s.AutoApply && (math.Abs(s.Dx) > maxAuto || math.Abs(s.Dy) > maxAuto) {
			t.Errorf("auto-apply outside the 60 px gate: %+v", s)
		}
		if s.AutoApply && s.Kind != "nudge" {
			t.Errorf("only nudges may auto-apply: %+v", s)
		}
		if s.ManualReview && math.Abs(s.Dy) <= 160 && math.Abs(s.Dx) <= 160 {
			t.Errorf("manual review below the 160 px cap: %+v", s)
		}
	}
	if w.AutoApplyCount == 0 && len(w.Suggestions) > 1 {
		t.Log("no auto-apply candidates in this fixture; gate still exercised")
	}
}
