package sandwicher

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDetectExternalSubtitles(t *testing.T) {
	dir := t.TempDir()
	video := filepath.Join(dir, "Film.2024.mkv")
	writeTestFile(t, video, "not a real video")

	writeTestFile(t, filepath.Join(dir, "Film.2024.en.srt"), "1\n00:00:01,000 --> 00:00:02,000\nhello\n")
	writeTestFile(t, filepath.Join(dir, "Film.2024.English.forced.srt"), "1\n00:00:01,000 --> 00:00:02,000\nhello\n")
	writeTestFile(t, filepath.Join(dir, "Film.2024.jpn.ass"), "[Events]\n")
	writeTestFile(t, filepath.Join(dir, "Film.2024.vtt"), "WEBVTT\n")
	writeTestFile(t, filepath.Join(dir, "Film.2024.srt"), "1\n00:00:01,000 --> 00:00:02,000\nhello\n")
	writeTestFile(t, filepath.Join(dir, "subs", "Film.2024.de.srt"), "1\n00:00:01,000 --> 00:00:02,000\nhallo\n")

	// Unrelated or unsupported files must be ignored.
	writeTestFile(t, filepath.Join(dir, "Other.en.srt"), "1\n00:00:01,000 --> 00:00:02,000\nno\n")
	writeTestFile(t, filepath.Join(dir, "Film.20240.en.srt"), "1\n00:00:01,000 --> 00:00:02,000\nno\n")
	writeTestFile(t, filepath.Join(dir, "Film.2024.en.txt"), "no\n")
	writeTestFile(t, filepath.Join(dir, "Film.2024.en.sub"), "no\n")

	streams := detectExternalSubtitles(video)
	if len(streams) != 6 {
		t.Fatalf("got %d external streams, want 6: %+v", len(streams), streams)
	}
	if streams[0].Index != externalStreamIndexBase {
		t.Errorf("first external index = %d, want %d", streams[0].Index, externalStreamIndexBase)
	}
	byName := map[string]Stream{}
	for _, s := range streams {
		if !s.External {
			t.Errorf("%s: External = false", s.FileName)
		}
		if s.CodecType != "subtitle" {
			t.Errorf("%s: CodecType = %q", s.FileName, s.CodecType)
		}
		if s.Path == "" {
			t.Errorf("%s: empty Path", s.FileName)
		}
		byName[s.FileName] = s
	}
	if got := byName["Film.2024.en.srt"]; got.Tags["language"] != "en" || got.Tags["title"] != "" {
		t.Errorf("en sidecar tags = %v, want language en and no title", got.Tags)
	}
	if got := byName["Film.2024.English.forced.srt"]; got.Tags["language"] != "en" || got.Tags["title"] != "forced" {
		t.Errorf("English.forced sidecar tags = %v", got.Tags)
	}
	if got := byName["Film.2024.jpn.ass"]; got.Tags["language"] != "ja" {
		t.Errorf("jpn sidecar language = %q, want ja", got.Tags["language"])
	}
	if got := byName["Film.2024.de.srt"]; got.Tags["language"] != "de" {
		t.Errorf("subs/de language = %q, want de", got.Tags["language"])
	}
}

func TestExternalSubtitleName(t *testing.T) {
	cases := []struct {
		fileStem string
		lang     string
		title    string
	}{
		{"Movie.en", "en", ""},
		{"Movie.en-US", "en", ""},
		{"Movie.eng", "en", ""},
		{"Movie.English", "en", ""},
		{"Movie.English.SDH", "en", "SDH"},
		{"Movie.pt_BR.forced", "pt", "forced"},
		{"Movie.ger", "de", ""},
		{"Movie.zh-Hans", "zh", ""},
		{"Movie.hi", "hi", ""},
		{"Movie.SDH", "", "SDH"},
		{"Movie.the", "", "the"},
		{"Movie.2024", "", "2024"},
		{"Movie - English", "en", ""},
		{"Movie", "", ""},
	}
	for _, tc := range cases {
		lang, title := externalSubtitleName("Movie", tc.fileStem)
		if lang != tc.lang || title != tc.title {
			t.Errorf("externalSubtitleName(%q) = (%q, %q), want (%q, %q)",
				tc.fileStem, lang, title, tc.lang, tc.title)
		}
	}
}

func TestParseExternalSubtitleFormats(t *testing.T) {
	dir := t.TempDir()
	srt := filepath.Join(dir, "Movie.en.srt")
	writeTestFile(t, srt, "1\n00:00:01,000 --> 00:00:02,500\nhello\n")
	vtt := filepath.Join(dir, "Movie.en.vtt")
	writeTestFile(t, vtt, "WEBVTT\n\n00:00:01.000 --> 00:00:02.500\nhello\n")
	ass := filepath.Join(dir, "Movie.en.ass")
	writeTestFile(t, ass, "[Script Info]\nPlayResY: 288\n\n[Events]\nFormat: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\nDialogue: 0,0:00:01.00,0:00:02.50,Default,,0,0,0,,hello\n")

	for _, path := range []string{srt, vtt, ass} {
		doc, err := parseExternalSubtitle(path)
		if err != nil {
			t.Fatalf("parseExternalSubtitle(%s): %v", filepath.Base(path), err)
		}
		if len(doc.Dialogues) != 1 {
			t.Fatalf("%s: got %d dialogues, want 1", filepath.Base(path), len(doc.Dialogues))
		}
		if doc.Dialogues[0].Text != "hello" {
			t.Errorf("%s: dialogue text = %q, want hello", filepath.Base(path), doc.Dialogues[0].Text)
		}
	}

	stream := &Stream{External: true, Path: srt, CodecName: "srt"}
	doc, err := extractSubtitle("", stream, dir, "primary", 0, nil)
	if err != nil {
		t.Fatalf("extractSubtitle external: %v", err)
	}
	if len(doc.Dialogues) != 1 {
		t.Errorf("extractSubtitle external: got %d dialogues", len(doc.Dialogues))
	}
}
