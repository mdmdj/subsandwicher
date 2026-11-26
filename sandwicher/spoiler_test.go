package sandwicher

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScrambleTextIsDeterministic(t *testing.T) {
	const src = "Jim is dead\\NJohnny will live."
	a := ScrambleText(src, 42, 7)
	b := ScrambleText(src, 42, 7)
	if a != b {
		t.Fatalf("scramble is not deterministic:\n%s\n%s", a, b)
	}
	if a == src {
		t.Fatal("scramble left the text unchanged")
	}
	if c := ScrambleText(src, 42, 8); c == a {
		t.Fatal("different events should scramble differently")
	}
}

func TestScrambleTextKeepsStructureAndWidth(t *testing.T) {
	cases := []string{
		"Jim is dead",
		"Johnny will live.",
		"{\\an8}the quick brown fox jumps over the lazy dog",
		"{\\c&HFFFFFF&}A long line, with punctuation!{}\\Nand a second line.",
		"no",
		"x",
	}
	for _, src := range cases {
		got := ScrambleText(src, 99, 3)
		if strings.Contains(got, "Jim") || strings.Contains(got, "Johnny") || strings.Contains(got, "dead") {
			t.Fatalf("scrambled text leaked a word: %q", got)
		}
		origLines := splitSpoilerLines(src)
		gotLines := splitSpoilerLines(got)
		if len(origLines) != len(gotLines) {
			t.Fatalf("line count changed for %q: %d -> %d", src, len(origLines), len(gotLines))
		}
		for i := range origLines {
			if strings.Count(origLines[i], " ") != strings.Count(gotLines[i], " ") {
				t.Fatalf("space count changed for %q: %q -> %q", origLines[i], origLines[i], gotLines[i])
			}
		}
		ow := spoilerLineWidths(src)
		gw := spoilerLineWidths(got)
		for i := range ow {
			if d := spoilerWidthDelta(ow[i], gw[i]); d > 0.02 {
				t.Fatalf("line %d width drifted %.2f%% for %q -> %q (%d vs %d)",
					i, d*100, src, got, ow[i], gw[i])
			}
		}
	}
}

func TestScrambleTextPreservesTagsAndDrawings(t *testing.T) {
	src := `{\an8}{\c&HFFFFFF&}hello world{\r}`
	got := ScrambleText(src, 5, 1)
	if !strings.HasPrefix(got, `{\an8}{\c&HFFFFFF&}`) || !strings.HasSuffix(got, `{\r}`) {
		t.Fatalf("tags were not preserved: %q", got)
	}
	drawing := `{\p1}m 0 0 l 100 0 100 100{\p0}`
	if got := ScrambleText(drawing, 5, 1); got != drawing {
		t.Fatalf("drawing should be untouched, got %q", got)
	}
	mixed := `{\p1}m 0 0 l 100 0{\p0}secret text`
	got = ScrambleText(mixed, 5, 1)
	if !strings.Contains(got, `{\p1}m 0 0 l 100 0{\p0}`) {
		t.Fatalf("drawing prefix was changed: %q", got)
	}
	if strings.Contains(got, "secret") {
		t.Fatalf("text after the drawing leaked: %q", got)
	}
}

func TestWriteSpoilerASSProducesPreview(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "merged.ass")
	out := filepath.Join(dir, "merged.spoiler.ass")
	body := "[Script Info]\nScriptType: v4.00+\nPlayResX: 640\nPlayResY: 360\n" +
		"\n[V4+ Styles]\nFormat: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding\n" +
		"Style: Default,Arial,20,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,2,2,2,10,10,10,1\n" +
		"\n[Events]\nFormat: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\n" +
		"Dialogue: 0,0:00:01.00,0:00:02.00,Default,,0,0,0,,Jim is dead\n" +
		"Dialogue: 0,0:00:03.00,0:00:04.00,Default,,0,0,0,,Johnny will live\n"
	if err := os.WriteFile(src, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	display, err := WriteSpoilerASS(src, out, 123)
	if err != nil {
		t.Fatal(err)
	}
	if len(display) != 2 {
		t.Fatalf("display rows = %d", len(display))
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "Jim") || strings.Contains(string(data), "Johnny") {
		t.Fatalf("spoiler preview leaked original text:\n%s", data)
	}
	for _, d := range display {
		if strings.TrimSpace(d) == "" {
			t.Fatal("display text is empty")
		}
	}
}

func TestApplyNoSpoilersAndRevealWindow(t *testing.T) {
	dir := t.TempDir()
	assPath := filepath.Join(dir, "merged.ass")
	body := "[Script Info]\nScriptType: v4.00+\nPlayResX: 640\nPlayResY: 360\n" +
		"\n[V4+ Styles]\nFormat: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding\n" +
		"Style: Default,Arial,20,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,2,2,2,10,10,10,1\n" +
		"\n[Events]\nFormat: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\n" +
		"Dialogue: 0,0:00:01.00,0:00:02.00,Default,,0,0,0,,Jim is dead\n" +
		"Dialogue: 0,0:00:03.00,0:00:04.00,Default,,0,0,0,,Johnny will live\n"
	if err := os.WriteFile(assPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	rep := &Report{
		Version: 1,
		Job:     JobInfo{PlayResX: 640, PlayResY: 360},
		Spoiler: &SpoilerInfo{Enabled: true, Seed: 55},
		Events: []EventDecision{
			{ID: 0, Text: "Jim is dead"},
			{ID: 1, Text: "Johnny will live"},
		},
		Windows: []ConflictWindow{{ID: 1, Events: []int{0}}},
	}
	reportPath := filepath.Join(dir, "report.json")
	if err := WriteReport(reportPath, rep); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(dir, "preview.ass")
	if _, err := ApplyFixesWithOptions(assPath, reportPath, out, nil, ApplyOptions{NoSpoilers: true}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "Jim is dead") || strings.Contains(string(data), "Johnny will live") {
		t.Fatalf("no-spoilers preview leaked original text:\n%s", data)
	}

	revealed := filepath.Join(dir, "revealed.ass")
	if _, err := ApplyFixesWithOptions(assPath, reportPath, revealed, nil, ApplyOptions{NoSpoilers: true, RevealWindow: 1}); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(revealed)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Jim is dead") {
		t.Fatalf("revealed window should keep its original text:\n%s", data)
	}
	if strings.Contains(string(data), "Johnny will live") {
		t.Fatalf("other windows should stay scrambled:\n%s", data)
	}

	// A fix adds position tags; the visible scramble must stay the same and the
	// tags must survive the rewrite.
	fixed := filepath.Join(dir, "fixed.ass")
	rep.Windows = []ConflictWindow{{
		ID: 1, Events: []int{0},
		Suggestions: []Suggestion{{
			ID: 0, Event: 0, Kind: "nudge", At: &Point{X: 100, Y: 200}, Reason: "test",
		}},
	}}
	if err := WriteReport(reportPath, rep); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyFixesWithOptions(assPath, reportPath, fixed, []Fix{{WindowID: 1, SuggestionIndex: 0}},
		ApplyOptions{NoSpoilers: true}); err != nil {
		t.Fatal(err)
	}
	fixedData, err := os.ReadFile(fixed)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(fixedData), ScrambleText("Jim is dead", 55, 0)) {
		t.Fatalf("fixed preview did not use the stable scramble:\n%s", fixedData)
	}
	if strings.Contains(string(fixedData), "Jim is dead") {
		t.Fatalf("fixed preview leaked the original text:\n%s", fixedData)
	}
	if !strings.Contains(string(fixedData), `\pos(100,200)`) {
		t.Fatalf("fix position tag was lost:\n%s", fixedData)
	}

	// The save path never enables NoSpoilers, so it must stay byte-identical.
	saved := filepath.Join(dir, "saved.ass")
	if _, err := ApplyFixes(assPath, reportPath, saved, nil); err != nil {
		t.Fatal(err)
	}
	orig, _ := os.ReadFile(assPath)
	got, _ := os.ReadFile(saved)
	if string(orig) != string(got) {
		t.Fatal("normal apply must keep the original text")
	}
}
