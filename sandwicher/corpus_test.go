package sandwicher

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCorpusTrackClassifier is an opt-in regression test over real merged
// files. It uses the "-eng" style suffix only as ground truth; InferTracks
// never sees it.
//
//	SUBS_CORPUS_DIR="/path/to/media" go test ./sandwicher -run Corpus -v
func TestCorpusTrackClassifier(t *testing.T) {
	dir := os.Getenv("SUBS_CORPUS_DIR")
	if dir == "" {
		t.Skip("set SUBS_CORPUS_DIR to run the corpus regression")
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.vie-eng.ass"))
	if err != nil || len(files) == 0 {
		t.Skipf("no *.vie-eng.ass under %s", dir)
	}
	total, ok := 0, 0
	for _, path := range files {
		doc, err := parseASSFile(path)
		if err != nil {
			t.Errorf("%s: %v", path, err)
			continue
		}
		inf := InferTracks(doc)
		if inf.Confidence < 0.5 {
			t.Errorf("%s: low confidence %.2f (%s)", filepath.Base(path), inf.Confidence, inf.Note)
		}
		used := map[string]bool{}
		for _, d := range doc.Dialogues {
			used[d.Style] = true
		}
		for _, s := range doc.Styles {
			name := s.name()
			if !used[name] || !strings.HasSuffix(strings.ToLower(name), "-eng") {
				continue
			}
			total++
			if inf.StyleTracks[name] == "secondary" {
				ok++
			} else {
				t.Errorf("%s: style %q classified %q, want secondary", filepath.Base(path), name, inf.StyleTracks[name])
			}
		}
	}
	if total > 0 && float64(ok)/float64(total) < 0.99 {
		t.Errorf("secondary style accuracy %.3f (%d/%d)", float64(ok)/float64(total), ok, total)
	}
}
