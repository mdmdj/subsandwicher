package sandwicher

import (
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func writeMergedFixture(t *testing.T) (assPath, reportPath string, rep *Report) {
	t.Helper()
	prim := assDoc("PlayResX: 640\nPlayResY: 360\n",
		"Style: Bottom,Arial,20,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,0,0,2,10,10,20,1\n",
		"Dialogue: 0,0:00:10.00,0:00:14.00,Bottom,,0,0,0,,a long primary dialogue line\n")
	sec := assDoc("PlayResX: 640\nPlayResY: 360\n",
		"Style: sign_Arial,Arial,20,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,0,0,2,10,10,10,1\n",
		"Dialogue: 0,0:00:10.50,0:00:13.00,sign_Arial,,0,0,0,,{\\pos(320,330)}SIGN\n")
	dir := t.TempDir()
	assPath = filepath.Join(dir, "merged.ass")
	reportPath = filepath.Join(dir, "merged.report.json")
	_, _, _, rep, err := mergeAndWrite(prim, sec, "eng", 1, assPath, JobInfo{}, false)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(reportPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return assPath, reportPath, rep
}

func TestApplyNudge(t *testing.T) {
	assPath, reportPath, rep := writeMergedFixture(t)
	if len(rep.Windows) == 0 || len(rep.Windows[0].Suggestions) == 0 {
		t.Fatal("fixture has no candidates")
	}
	w := rep.Windows[0]
	s := w.Suggestions[0]
	if s.Kind != "nudge" || s.At == nil {
		t.Fatalf("first candidate is %q at %v, want a nudge", s.Kind, s.At)
	}
	out := filepath.Join(t.TempDir(), "out.ass")
	res, err := ApplyFixes(assPath, reportPath, out, []Fix{{WindowID: w.ID, SuggestionIndex: 0}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Applied) != 1 || res.Applied[0].Event != s.Event {
		t.Fatalf("applied = %+v", res.Applied)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	lines := dialogueLinesForTest(string(data))
	if s.Event >= len(lines) {
		t.Fatalf("event %d outside output", s.Event)
	}
	got := lines[s.Event]
	want := `\pos(` + formatNum(s.At.X) + `,` + formatNum(s.At.Y) + `)`
	if !strings.Contains(got, want) {
		t.Errorf("output line missing %q:\n%s", want, got)
	}
	if strings.Contains(got, `\pos(320,330)`) {
		t.Errorf("old position survived:\n%s", got)
	}
}

func TestApplyNoFixesCopiesExactly(t *testing.T) {
	assPath, reportPath, _ := writeMergedFixture(t)
	out := filepath.Join(t.TempDir(), "out.ass")
	if _, err := ApplyFixes(assPath, reportPath, out, nil); err != nil {
		t.Fatal(err)
	}
	a, _ := os.ReadFile(assPath)
	b, _ := os.ReadFile(out)
	if string(a) != string(b) {
		t.Error("empty fix list should copy the input byte-for-byte")
	}
}

func TestApplyOriginalAndInvalid(t *testing.T) {
	assPath, reportPath, rep := writeMergedFixture(t)
	out := filepath.Join(t.TempDir(), "out.ass")
	// -1 means keep the original.
	res, err := ApplyFixes(assPath, reportPath, out, []Fix{{WindowID: rep.Windows[0].ID, SuggestionIndex: -1}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Applied) != 0 {
		t.Errorf("applied = %+v, want none", res.Applied)
	}
	if _, err := ApplyFixes(assPath, reportPath, out, []Fix{{WindowID: 999, SuggestionIndex: 0}}); err == nil {
		t.Error("unknown window should fail")
	}
	if _, err := ApplyFixes(assPath, reportPath, out, []Fix{{WindowID: rep.Windows[0].ID, SuggestionIndex: 99}}); err == nil {
		t.Error("unknown candidate should fail")
	}
}

func TestTextHelpers(t *testing.T) {
	if got := setPos(`{\an2}x`, Point{10.5, 20}); got != `{\pos(10.5,20)}{\an2}x` {
		t.Errorf("prepend pos = %q", got)
	}
	if got := setPos(`{\pos(1,2)\an2}x`, Point{10, 20}); got != `{\pos(10,20)\an2}x` {
		t.Errorf("replace pos = %q", got)
	}
	if got := setFs(`{\fs20}x`, 18); got != `{\fs18}x` {
		t.Errorf("replace fs = %q", got)
	}
	if got := setFs(`x`, 18); got != `{\fs18}x` {
		t.Errorf("prepend fs = %q", got)
	}
	if got := setAlphaHidden(`{\alpha&H80&}x`); got != `{\alpha&HFF&}x` {
		t.Errorf("replace alpha = %q", got)
	}
	if got := setAlphaHidden(`x`); got != `{\alpha&HFF&}x` {
		t.Errorf("prepend alpha = %q", got)
	}
}

func dialogueLinesForTest(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "Dialogue:") {
			out = append(out, line)
		}
	}
	return out
}

func TestMoveBy(t *testing.T) {
	in := `{\move(460,70,405,70,1526,1670)}US dollar`
	want := `{\move(460,100,405,100,1526,1670)}US dollar`
	if got := moveBy(in, 0, 30); got != want {
		t.Errorf("moveBy = %q, want %q", got, want)
	}
	in2 := `{\move(1,2,3,4)}x`
	if got := moveBy(in2, 10, -1); got != `{\move(11,1,13,3)}x` {
		t.Errorf("moveBy 4-arg = %q", got)
	}
}

func TestWindowGroupsEveryEvent(t *testing.T) {
	prim := assDoc("PlayResX: 640\nPlayResY: 360\n",
		"Style: Bottom,Arial,20,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,0,0,2,10,10,20,1\n",
		"Dialogue: 0,0:00:10.00,0:00:14.00,Bottom,,0,0,0,,a long primary dialogue line\n")
	sec := assDoc("PlayResX: 640\nPlayResY: 360\n",
		"Style: sign_Arial,Arial,20,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,0,0,2,10,10,10,1\n",
		"Dialogue: 0,0:00:10.50,0:00:13.00,sign_Arial,,0,0,0,,{\\pos(320,330)}STILL\n"+
			"Dialogue: 0,0:00:10.50,0:00:12.00,sign_Arial,,0,0,0,,{\\move(460,330,405,330,0,500)}MOVING\n")
	dir := t.TempDir()
	assPath := filepath.Join(dir, "merged.ass")
	reportPath := filepath.Join(dir, "merged.report.json")
	_, _, _, rep, err := mergeAndWrite(prim, sec, "eng", 1, assPath, JobInfo{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Windows) == 0 {
		t.Fatal("no windows")
	}
	w := rep.Windows[0]
	if len(w.ByEvent) == 0 {
		t.Fatal("window has no byEvent groups")
	}
	seen := map[int]bool{}
	for _, g := range w.ByEvent {
		seen[g.Event] = true
		if len(g.SuggestionIDs) == 0 {
			t.Errorf("event %d has no candidates: %s", g.Event, g.Note)
		}
		for _, id := range g.SuggestionIDs {
			if id < 0 || id >= len(w.Suggestions) || w.Suggestions[id].Event != g.Event {
				t.Errorf("event %d has bad suggestion id %d", g.Event, id)
			}
		}
	}
	var movingID int = -1
	for _, e := range rep.Events {
		if e.Text == "MOVING" {
			movingID = e.ID
		}
	}
	if movingID < 0 {
		t.Fatal("moving event not found")
	}
	if !seen[movingID] {
		t.Errorf("moving event %d missing from byEvent groups", movingID)
	}
	// Apply the first nudge for the moving sign and check the whole path moved.
	var chosen *Suggestion
	for _, id := range suggestionIDsFor(w, movingID) {
		s := w.Suggestions[id]
		if s.Kind == "nudge" {
			chosen = &s
			break
		}
	}
	if chosen == nil {
		t.Fatal("no nudge for the moving sign")
	}
	data, _ := json.Marshal(rep)
	if err := os.WriteFile(reportPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out.ass")
	res, err := ApplyFixes(assPath, reportPath, out, []Fix{{WindowID: w.ID, SuggestionIndex: chosen.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Applied) != 1 || res.Applied[0].Event != movingID {
		t.Fatalf("applied = %+v", res.Applied)
	}
	outData, _ := os.ReadFile(out)
	var line string
	for _, l := range dialogueLinesForTest(string(outData)) {
		if strings.Contains(l, "MOVING") {
			line = l
		}
	}
	want := `\move(` + formatNum(460+chosen.Dx) + `,` + formatNum(330+chosen.Dy) + `,` +
		formatNum(405+chosen.Dx) + `,` + formatNum(330+chosen.Dy) + `,0,500)`
	if !strings.Contains(line, want) {
		t.Errorf("moving sign not translated; want %q in\n%s", want, line)
	}
	if strings.Contains(line, `\pos(`) {
		t.Errorf("moving sign should not gain a pos tag:\n%s", line)
	}
}

func suggestionIDsFor(w ConflictWindow, event int) []int {
	for _, g := range w.ByEvent {
		if g.Event == event {
			return g.SuggestionIDs
		}
	}
	return nil
}

func TestEventSeekUsesTheEventTime(t *testing.T) {
	prim := assDoc("PlayResX: 640\nPlayResY: 360\n",
		"Style: Bottom,Arial,20,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,0,0,2,10,10,20,1\n",
		"Dialogue: 0,0:00:00.00,0:00:01.00,Bottom,,0,0,0,,FIRST\n"+
			"Dialogue: 0,0:00:01.00,0:00:03.00,Bottom,,0,0,0,,MIDDLE\n"+
			"Dialogue: 0,0:00:03.00,0:00:05.00,Bottom,,0,0,0,,LAST\n")
	sec := assDoc("PlayResX: 640\nPlayResY: 360\n",
		"Style: sign_Arial,Arial,20,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,0,0,2,10,10,10,1\n",
		"Dialogue: 0,0:00:00.00,0:00:05.00,sign_Arial,,0,0,0,,{\\pos(320,330)}SIGN\n")
	out := filepath.Join(t.TempDir(), "out.ass")
	_, _, _, rep, err := mergeAndWrite(prim, sec, "eng", 1, out, JobInfo{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Windows) == 0 {
		t.Fatal("no windows")
	}
	w := rep.Windows[0]
	byText := map[string]int{}
	for _, e := range rep.Events {
		byText[e.Text] = e.ID
	}
	for _, tc := range []struct {
		text     string
		min, max float64
	}{
		{"FIRST", 0, 0.95},
		{"MIDDLE", 1, 2.95},
		{"LAST", 3, 4.95},
	} {
		id := byText[tc.text]
		var g *EventSuggestions
		for i := range w.ByEvent {
			if w.ByEvent[i].Event == id {
				g = &w.ByEvent[i]
			}
		}
		if g == nil {
			t.Fatalf("%s event %d missing from groups", tc.text, id)
		}
		if g.Seek < tc.min || g.Seek > tc.max {
			t.Errorf("%s seek = %.2f, want %.2f..%.2f", tc.text, g.Seek, tc.min, tc.max)
		}
		for _, sid := range g.SuggestionIDs {
			if got := w.Suggestions[sid].Seek; got != g.Seek {
				t.Errorf("%s suggestion %d seek = %.2f, group seek %.2f", tc.text, sid, got, g.Seek)
			}
		}
	}
	// The long sign conflicts across all three dialogue intervals; it should
	// pick a conflict interval, not the start of the window.
	signID := byText["SIGN"]
	var signGroup *EventSuggestions
	for i := range w.ByEvent {
		if w.ByEvent[i].Event == signID {
			signGroup = &w.ByEvent[i]
		}
	}
	if signGroup == nil {
		t.Fatal("sign group missing")
	}
	if signGroup.Seek < 1.0 {
		t.Errorf("sign seek = %.2f, want a conflict interval rather than the window start", signGroup.Seek)
	}
}

func TestExactInkMask(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not on PATH")
	}
	prim := assDoc("PlayResX: 640\nPlayResY: 360\n",
		"Style: Bottom,Arial,20,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,0,0,2,10,10,20,1\n",
		"Dialogue: 0,0:00:10.00,0:00:14.00,Bottom,,0,0,0,,a long primary dialogue line\n")
	sec := assDoc("PlayResX: 640\nPlayResY: 360\n",
		"Style: sign_Arial,Arial,20,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,0,0,2,10,10,10,1\n",
		"Dialogue: 0,0:00:10.50,0:00:13.00,sign_Arial,,0,0,0,,{\\pos(320,330)}SIGN\n")
	out := filepath.Join(t.TempDir(), "out.ass")
	_, _, _, rep, err := mergeAndWrite(prim, sec, "eng", 1, out, JobInfo{}, true)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Job.Exact {
		t.Skipf("exact masks unavailable: %s", rep.Job.ExactNote)
	}
	found := false
	for _, e := range rep.Events {
		if e.ExactBBox == nil {
			continue
		}
		found = true
		if !e.ExactBBox.Valid() {
			t.Errorf("event %d exact bbox invalid: %+v", e.ID, e.ExactBBox)
		}
		if e.ExactSeek < 10 || e.ExactSeek > 14 {
			t.Errorf("event %d exact seek %.2f outside the event", e.ID, e.ExactSeek)
		}
	}
	if !found {
		t.Fatal("no event carried an exact bbox")
	}
}

func TestMaskOverlap(t *testing.T) {
	mk := func(cells ...[2]int) *inkMask {
		m := &inkMask{Cell: 2, W: 10, H: 10, MW: 5, MH: 5, Box: Box{X0: 0, Y0: 0, X1: 10, Y1: 10}}
		m.Bits = make([]uint64, 1)
		for _, c := range cells {
			idx := c[1]*m.MW + c[0]
			m.Bits[0] |= 1 << (idx % 64)
			m.InkCells++
		}
		return m
	}
	a := mk([2]int{0, 0}, [2]int{1, 0})
	b := mk([2]int{0, 0}, [2]int{3, 3})
	if got := maskOverlap(a, b, 0, 0); got != 1 {
		t.Errorf("overlap = %d, want 1", got)
	}
	if got := maskOverlap(a, b, 4, 0); got != 0 {
		t.Errorf("shifted overlap = %d, want 0", got)
	}
}

func TestWindowIncludesActiveEventsAndDiagonals(t *testing.T) {
	prim := assDoc("PlayResX: 640\nPlayResY: 360\n",
		"Style: Bottom,Arial,20,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,0,0,2,10,10,20,1\n",
		"Dialogue: 0,0:00:10.00,0:00:14.00,Bottom,,0,0,0,,a long primary dialogue line\n")
	sec := assDoc("PlayResX: 640\nPlayResY: 360\n",
		"Style: sign_Arial,Arial,20,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,0,0,2,10,10,10,1\n",
		"Dialogue: 0,0:00:10.50,0:00:13.00,sign_Arial,,0,0,0,,{\\pos(320,330)}OVERLAP\n"+
			"Dialogue: 0,0:00:10.50,0:00:13.00,sign_Arial,,0,0,0,,{\\pos(10,10)}LONELY\n")
	out := filepath.Join(t.TempDir(), "out.ass")
	_, _, _, rep, err := mergeAndWrite(prim, sec, "eng", 1, out, JobInfo{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Windows) == 0 {
		t.Fatal("no windows")
	}
	w := rep.Windows[0]
	byText := map[string]int{}
	for _, e := range rep.Events {
		byText[e.Text] = e.ID
	}
	lonely := byText["LONELY"]
	var lonelyGroup *EventSuggestions
	for i := range w.ByEvent {
		if w.ByEvent[i].Event == lonely {
			lonelyGroup = &w.ByEvent[i]
		}
	}
	if lonelyGroup == nil {
		t.Fatalf("active non-conflict event %d missing from groups", lonely)
	}
	if lonelyGroup.Note == "" || !strings.Contains(lonelyGroup.Note, "no direct conflict") {
		t.Errorf("lonely group note = %q", lonelyGroup.Note)
	}
	if len(lonelyGroup.SuggestionIDs) == 0 {
		t.Error("lonely group has no nudge candidates")
	}
	foundDiagonal := false
	for _, s := range w.Suggestions {
		if s.Kind == "nudge" && s.Dx != 0 && s.Dy != 0 {
			foundDiagonal = true
			break
		}
	}
	if !foundDiagonal {
		t.Error("expected at least one combined x+y candidate")
	}
	// The long sign conflicts across the window; its seek must be inside one
	// of those conflict intervals, not the start of the window.
	overlapID := byText["OVERLAP"]
	var signGroup *EventSuggestions
	for i := range w.ByEvent {
		if w.ByEvent[i].Event == overlapID {
			signGroup = &w.ByEvent[i]
		}
	}
	if signGroup == nil {
		t.Fatal("conflict sign group missing")
	}
	if signGroup.Seek < 3.0 {
		t.Errorf("conflict sign seek = %.2f, want a later conflict interval", signGroup.Seek)
	}
}

func TestMovingSignsKeepSiblingMoves(t *testing.T) {
	doc := assDoc("PlayResX: 640\nPlayResY: 360\n",
		"Style: Top,Arial,20,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,0,0,8,10,10,20,1\n"+
			"Style: sign_Arial-eng,Arial,20,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,0,0,8,10,10,10,1\n",
		"Dialogue: 0,0:00:10.00,0:00:14.00,Top,,0,0,0,,a primary dialogue line that spans the sign\n"+
			"Dialogue: 0,0:00:10.00,0:00:13.00,sign_Arial-eng,,0,0,0,,{\\move(315,30,175,30,66,274)\\c&HD18BE4&}US dollar\n"+
			"Dialogue: 0,0:00:10.00,0:00:13.00,sign_Arial-eng,,0,0,0,,{\\move(605,30,465,30,66,274)\\c&HD18BE4&}Euro\n")
	path := filepath.Join(t.TempDir(), "merged.ass")
	if err := writeDoc(doc, path); err != nil {
		t.Fatal(err)
	}
	rep, err := ScanASS(path, ScanOptions{SecondarySuffix: "-eng"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Windows) == 0 {
		t.Fatal("no windows")
	}
	w := rep.Windows[0]
	byText := map[string]int{}
	for _, e := range rep.Events {
		byText[e.Text] = e.ID
	}
	us, eu := byText["US dollar"], byText["Euro"]
	var gUS, gEU *EventSuggestions
	for i := range w.ByEvent {
		switch w.ByEvent[i].Event {
		case us:
			gUS = &w.ByEvent[i]
		case eu:
			gEU = &w.ByEvent[i]
		}
	}
	if gUS == nil || gEU == nil {
		t.Fatalf("missing groups: us=%v eu=%v", gUS, gEU)
	}
	if len(gUS.Related) != 1 || gUS.Related[0] != eu {
		t.Errorf("US related = %v, want [%d]", gUS.Related, eu)
	}
	if len(gEU.Related) != 1 || gEU.Related[0] != us {
		t.Errorf("Euro related = %v, want [%d]", gEU.Related, us)
	}
	vertical := func(g *EventSuggestions) map[float64]bool {
		out := map[float64]bool{}
		for _, id := range g.SuggestionIDs {
			s := w.Suggestions[id]
			if s.Kind == "nudge" && s.Dx == 0 && s.Dy != 0 {
				out[s.Dy] = true
			}
		}
		return out
	}
	vu, ve := vertical(gUS), vertical(gEU)
	if len(vu) == 0 {
		t.Error("US offers no vertical moves")
	}
	shared := 0
	for dy := range vu {
		if ve[dy] {
			shared++
		}
	}
	// Candidates that clip too much of an in-frame sign are rejected now, so
	// the exact number of steps is policy-dependent; what matters is that the
	// two related signs offer the same surviving steps.
	if shared != len(vu) || shared != len(ve) {
		t.Errorf("shared vertical moves = %d (%v vs %v), want paired signs to offer the same steps", shared, vu, ve)
	}
	for _, g := range []*EventSuggestions{gUS, gEU} {
		found := false
		for _, id := range g.SuggestionIDs {
			s := w.Suggestions[id]
			if s.Kind == "nudge" && s.Dx == 0 && s.Dy == 44 {
				found = true
				if len(s.SharedWith) == 0 {
					t.Errorf("event %d dy 44 has no sharedWith links", g.Event)
				}
			}
		}
		if !found {
			t.Errorf("event %d has no dy +44 shared move", g.Event)
		}
	}
}

func TestEventTextColor(t *testing.T) {
	st := &ASSStyle{Val: map[string]string{"primarycolour": "&H00FF8040"}}
	got := eventTextColor(&Dialogue{Text: "plain"}, st)
	if got.R != 0x40 || got.G != 0x80 || got.B != 0xFF {
		t.Errorf("style colour = %+v, want BGR &HFF8040 -> R40 G80 BFF", got)
	}
	got = eventTextColor(&Dialogue{Text: `{\c&HD18BE4&}inline`}, st)
	if got.R != 0xE4 || got.G != 0x8B || got.B != 0xD1 {
		t.Errorf("inline colour = %+v, want BGR &HD18BE4 -> RE4 G8B BD1", got)
	}
}

func suggestionOfKind(w ConflictWindow, event int, kind string) *Suggestion {
	for _, id := range suggestionIDsFor(w, event) {
		s := w.Suggestions[id]
		if s.Kind == kind {
			return &s
		}
	}
	return nil
}

func TestApplyMoveToTop(t *testing.T) {
	assPath, reportPath, rep := writeMergedFixture(t)
	w := rep.Windows[0]
	var event int = -1
	var top *Suggestion
	for _, g := range w.ByEvent {
		if s := suggestionOfKind(w, g.Event, "top"); s != nil {
			event, top = g.Event, s
			break
		}
	}
	if top == nil {
		t.Fatal("no move-to-top candidate")
	}
	out := filepath.Join(t.TempDir(), "out.ass")
	res, err := ApplyFixes(assPath, reportPath, out, []Fix{{WindowID: w.ID, SuggestionIndex: top.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Applied) != 1 || res.Applied[0].Kind != "top" {
		t.Fatalf("applied = %+v", res.Applied)
	}
	data, _ := os.ReadFile(out)
	lines := dialogueLinesForTest(string(data))
	if event >= len(lines) {
		t.Fatalf("event %d outside output", event)
	}
	layer := strings.SplitN(strings.TrimPrefix(lines[event], "Dialogue: "), ",", 2)[0]
	if layer != "1" {
		t.Errorf("layer = %q, want 1 (above the other layer-0 lines)", layer)
	}
}

func TestApplyManualOverride(t *testing.T) {
	assPath, reportPath, rep := writeMergedFixture(t)
	w := rep.Windows[0]
	var nudge *Suggestion
	for _, g := range w.ByEvent {
		if s := suggestionOfKind(w, g.Event, "nudge"); s != nil && s.At != nil {
			nudge = s
			break
		}
	}
	if nudge == nil {
		t.Fatal("no static nudge candidate")
	}
	dx, dy := nudge.Dx+5, nudge.Dy+17
	comment := "manual: add a little more y"
	fix := Fix{
		WindowID: w.ID, SuggestionIndex: nudge.ID, Comment: comment,
		Override: &FixOverride{Dx: &dx, Dy: &dy},
	}
	out := filepath.Join(t.TempDir(), "out.ass")
	res, err := ApplyFixes(assPath, reportPath, out, []Fix{fix})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Applied) != 1 {
		t.Fatalf("applied = %+v", res.Applied)
	}
	a := res.Applied[0]
	if a.Override == nil || a.Comment != comment {
		t.Errorf("applied override/comment not recorded: %+v", a)
	}
	data, _ := os.ReadFile(out)
	lines := dialogueLinesForTest(string(data))
	if nudge.Event >= len(lines) {
		t.Fatalf("event %d outside output", nudge.Event)
	}
	want := `\pos(` + formatNum(nudge.At.X+5) + `,` + formatNum(nudge.At.Y+17) + `)`
	if !strings.Contains(lines[nudge.Event], want) {
		t.Errorf("manual override missing %q:\n%s", want, lines[nudge.Event])
	}
}

func TestKeepsInFramePolicy(t *testing.T) {
	const pw, ph = 640.0, 360.0
	inside := Box{X0: 100, Y0: 100, X1: 200, Y1: 150}
	if !keepsInFrame(inside, inside.Translate(0, -10), pw, ph) {
		t.Error("a small nudge of a fully in-frame box should be allowed")
	}
	if keepsInFrame(inside, inside.Translate(0, -120), pw, ph) {
		t.Error("a box pushed mostly out of frame should be rejected")
	}
	if keepsInFrame(inside, inside.Translate(0, -400), pw, ph) {
		t.Error("a box moved fully out of frame should be rejected")
	}
	partial := Box{X0: -50, Y0: 100, X1: 50, Y1: 150}
	if !keepsInFrame(partial, partial.Translate(0, 0), pw, ph) {
		t.Error("an unchanged, originally partial box should be allowed")
	}
	if keepsInFrame(partial, partial.Translate(-40, 0), pw, ph) {
		t.Error("a partial box made much worse should be rejected")
	}
	if !keepsInFrame(partial, partial.Translate(20, 0), pw, ph) {
		t.Error("a partial box moved further in should be allowed")
	}
}

func TestCandidateStepsIncludeFineAndLargeMoves(t *testing.T) {
	steps := candidateSteps(20, 8, 1, 180, []float64{6, 12, 20}, []float64{0.5, 1, 1.5, 2, 3, 4})
	want := []float64{6, 12, 20, 18, 28, 38, 48, 68, 88}
	for _, w := range want {
		found := false
		for _, s := range steps {
			if math.Abs(s-w) < 0.01 {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("candidateSteps missing %.0f: %v", w, steps)
		}
	}
}

func TestHiddenSignBehindDialogueIsRequired(t *testing.T) {
	path := filepath.Join(t.TempDir(), "merged.ass")
	doc := assSource("PlayResX: 640\nPlayResY: 360\n",
		"Style: sign_Arial-eng,Arial,20,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,0,0,2,10,10,10,1\n"+
			"Style: Default-eng,Arial,20,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,0,0,2,10,10,10,1\n",
		"Dialogue: 0,0:00:10.00,0:00:13.00,sign_Arial-eng,,0,0,0,,{\\pos(320,348)}SIGN\n"+
			"Dialogue: 0,0:00:10.00,0:00:14.00,Default-eng,,0,0,0,,a long dialogue line covering the sign\n")
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err := ScanASS(path, ScanOptions{SecondarySuffix: "-eng"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Windows) == 0 {
		t.Fatal("expected a review window")
	}
	w := rep.Windows[0]
	if w.Surface != SurfaceRequired {
		t.Errorf("surface = %q, want required for a fully covered sign", w.Surface)
	}
	if !strings.Contains(w.Reason, "beneath") {
		t.Errorf("reason = %q, want a draw-order explanation", w.Reason)
	}
}

func TestApplyReportsRemainingAndOffFrame(t *testing.T) {
	assPath, reportPath, rep := writeMergedFixture(t)
	w := rep.Windows[0]
	var nudge *Suggestion
	var sign EventDecision
	for _, e := range rep.Events {
		if e.Role == "sign" {
			sign = e
		}
	}
	for _, g := range w.ByEvent {
		if g.Event == sign.ID {
			nudge = suggestionOfKind(w, g.Event, "nudge")
		}
	}
	if nudge == nil || sign.Pos == nil {
		t.Fatal("no static nudge candidate")
	}

	// Put the sign back exactly where it started: the conflict must remain.
	out := filepath.Join(t.TempDir(), "out.ass")
	orig := Point{X: sign.Pos.X, Y: sign.Pos.Y}
	res, err := ApplyFixes(assPath, reportPath, out, []Fix{{
		WindowID: w.ID, SuggestionIndex: nudge.ID,
		Override: &FixOverride{At: &orig},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.RemainingConflicts) == 0 {
		t.Error("an unchanged sign should still report its remaining conflict")
	}

	// A real nudge should clear it.
	out2 := filepath.Join(t.TempDir(), "out2.ass")
	res2, err := ApplyFixes(assPath, reportPath, out2, []Fix{{WindowID: w.ID, SuggestionIndex: nudge.ID}})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range res2.RemainingConflicts {
		if containsInt(c.Events, nudge.Event) {
			t.Errorf("resolved fix still reports conflict %+v", c)
		}
	}

	// A deliberately out-of-frame override is reported, not silently accepted.
	out3 := filepath.Join(t.TempDir(), "out3.ass")
	bad := Point{X: 320, Y: -1000}
	res3, err := ApplyFixes(assPath, reportPath, out3, []Fix{{
		WindowID: w.ID, SuggestionIndex: nudge.ID,
		Override: &FixOverride{At: &bad},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res3.Applied) != 1 || !res3.Applied[0].OutOfFrame {
		t.Fatalf("out-of-frame fix not flagged: %+v", res3.Applied)
	}
	if !res3.Applied[0].IntroducedFrame {
		t.Error("new frame problem not marked as introduced")
	}

	// A position just inside the frame but outside the 5% safe area is a
	// safe-area warning, not an out-of-frame error.
	out4 := filepath.Join(t.TempDir(), "out4.ass")
	edge := Point{X: 25, Y: sign.Pos.Y}
	res4, err := ApplyFixes(assPath, reportPath, out4, []Fix{{
		WindowID: w.ID, SuggestionIndex: nudge.ID,
		Override: &FixOverride{At: &edge},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res4.Applied) != 1 || !res4.Applied[0].IntroducedSafeArea {
		t.Fatalf("safe-area warning not reported: %+v", res4.Applied)
	}
	if res4.Applied[0].OutOfFrame {
		t.Error("edge placement should not be fully out of frame")
	}
}

func TestApplyTopWithPositionOverride(t *testing.T) {
	assPath, reportPath, rep := writeMergedFixture(t)
	w := rep.Windows[0]
	var top *Suggestion
	for _, g := range w.ByEvent {
		if s := suggestionOfKind(w, g.Event, "top"); s != nil {
			top = s
			break
		}
	}
	if top == nil {
		t.Fatal("no top candidate")
	}
	p := Point{X: 200, Y: 250}
	out := filepath.Join(t.TempDir(), "out.ass")
	res, err := ApplyFixes(assPath, reportPath, out, []Fix{{
		WindowID: w.ID, SuggestionIndex: top.ID,
		Override: &FixOverride{At: &p},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Applied) != 1 || res.Applied[0].Kind != "top" {
		t.Fatalf("applied = %+v", res.Applied)
	}
	data, _ := os.ReadFile(out)
	lines := dialogueLinesForTest(string(data))
	if top.Event >= len(lines) {
		t.Fatalf("event %d outside output", top.Event)
	}
	layer := strings.SplitN(strings.TrimPrefix(lines[top.Event], "Dialogue: "), ",", 2)[0]
	if layer != "1" {
		t.Errorf("layer = %q, want 1", layer)
	}
	if !strings.Contains(lines[top.Event], `\pos(200,250)`) {
		t.Errorf("top+position did not write pos:\n%s", lines[top.Event])
	}
}
