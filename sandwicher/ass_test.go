package sandwicher

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMapAlignment(t *testing.T) {
	cases := []struct {
		n         int
		secondary bool
		want      int
	}{
		// primary: top -> bottom (horizontal kept), middle -> middle-left, bottom kept
		{1, false, 1}, {2, false, 2}, {3, false, 3},
		{4, false, 4}, {5, false, 4}, {6, false, 4},
		{7, false, 1}, {8, false, 2}, {9, false, 3},
		// secondary: bottom -> top (horizontal kept), middle -> middle-right, top kept
		{1, true, 7}, {2, true, 8}, {3, true, 9},
		{4, true, 6}, {5, true, 6}, {6, true, 6},
		{7, true, 7}, {8, true, 8}, {9, true, 9},
	}
	for _, c := range cases {
		if got := mapAlignment(c.n, c.secondary); got != c.want {
			t.Errorf("mapAlignment(%d, %v) = %d, want %d", c.n, c.secondary, got, c.want)
		}
	}
}

func TestMapLegacyAlignment(t *testing.T) {
	// \a5 is middle-center: primary -> \a5 (middle-left, hor 1 -> 1+4),
	// secondary -> \a7 (middle-right, hor 3 -> 3+4)
	if got := mapLegacyAlignment(5, false); got != 5 {
		t.Errorf("primary legacy middle-center = %d, want 5", got)
	}
	if got := mapLegacyAlignment(5, true); got != 7 {
		t.Errorf("secondary legacy middle-center = %d, want 7", got)
	}
	// \a10 is top-center: primary -> bottom-center (2), secondary stays top (10)
	if got := mapLegacyAlignment(10, false); got != 2 {
		t.Errorf("primary legacy top-center = %d, want 2", got)
	}
	if got := mapLegacyAlignment(10, true); got != 10 {
		t.Errorf("secondary legacy top-center = %d, want 10", got)
	}
	// \a1 bottom-left: primary keeps 1, secondary -> 9 (top-right)
	if got := mapLegacyAlignment(1, false); got != 1 {
		t.Errorf("primary legacy bottom-left = %d, want 1", got)
	}
	if got := mapLegacyAlignment(1, true); got != 9 {
		t.Errorf("secondary legacy bottom-left = %d, want 9", got)
	}
}

func TestLanguageCandidates(t *testing.T) {
	cases := map[string][]string{ // code -> tags that must all match
		"vi":  {"vie", "vi"},
		"en":  {"eng", "en"},
		"de":  {"ger", "deu", "de"}, // Matroska uses B codes
		"ger": {"ger", "deu"},
		"zh":  {"chi", "zho", "zh"},
		"fre": {"fre", "fra", "fr"},
	}
	for code, tags := range cases {
		set := languageCandidates(code)
		for _, tag := range tags {
			if !set[tag] {
				t.Errorf("languageCandidates(%q) missing tag %q (got %v)", code, tag, set)
			}
		}
	}
}

func TestPickTrack(t *testing.T) {
	streams := []Stream{
		{Index: 0, CodecType: "subtitle", CodecName: "subrip", Tags: map[string]string{"language": "eng", "title": "English SDH"}},
		{Index: 1, CodecType: "subtitle", CodecName: "ass", Tags: map[string]string{"language": "eng", "title": "English (main)"}},
		{Index: 2, CodecType: "subtitle", CodecName: "hdmv_pgs_subtitle", Tags: map[string]string{"language": "eng", "title": "English PGS"}},
		{Index: 3, CodecType: "subtitle", CodecName: "ass", Tags: map[string]string{"language": "jpn", "title": "Japanese (SDH)"}},
		{Index: 4, CodecType: "subtitle", CodecName: "ass", Tags: map[string]string{"language": "jpn", "title": "Japanese"}},
	}

	// SDH track skipped, bitmap track excluded
	s, err := PickTrack(streams, "en", -1, "primary")
	if err != nil || s.Index != 1 {
		t.Errorf("PickTrack(en) = %v, %v; want stream 1", s, err)
	}
	// non-SDH preferred among jpn
	if s, _ := PickTrack(streams, "jpn", -1, "x"); s.Index != 4 {
		t.Errorf("PickTrack(jpn) = stream %d, want 4", s.Index)
	}
	// override index wins even if SDH
	if s, _ := PickTrack(streams, "en", 0, "x"); s.Index != 0 {
		t.Errorf("PickTrack(en, override 0) = stream %d, want 0", s.Index)
	}
	// override to bitmap must fail
	if _, err := PickTrack(streams, "en", 2, "x"); err == nil {
		t.Error("PickTrack(en, override bitmap) should fail")
	}
	// bitmap-only language must fail with out-of-scope error
	pgs := []Stream{{Index: 9, CodecType: "subtitle", CodecName: "hdmv_pgs_subtitle", Tags: map[string]string{"language": "kor"}}}
	if _, err := PickTrack(pgs, "ko", -1, "x"); err == nil || !strings.Contains(err.Error(), "out of scope") {
		t.Errorf("PickTrack(bitmap only) err = %v, want out-of-scope error", err)
	}
}

func TestParseAndMerge(t *testing.T) {
	prim := parseASS([]byte(strings.Join([]string{
		"[Script Info]",
		"; a comment",
		"PlayResX: 1920",
		"PlayResY: 1080",
		"WrapStyle: 2",
		"",
		"[V4+ Styles]",
		"Format: Name, Fontname, Fontsize, Alignment, MarginV",
		"Style: Top,Arial,20,8,40",
		"Style: Bottom,Arial,20,2,40",
		"",
		"[Events]",
		"Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text",
		"Dialogue: 0,0:00:02.00,0:00:03.00,Bottom,,0,0,40,,hello, world",
		"Dialogue: 0,0:00:01.00,0:00:02.00,Top,,0,0,40,,{\\an7}top text",
		"",
		"[Fonts]",
		"fontdata: abc",
	}, "\n") + "\n"))

	sec := parseASS([]byte(strings.Join([]string{
		"[Script Info]",
		"PlayResX: 640",
		"PlayResY: 288",
		"",
		"[V4+ Styles]",
		"Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding",
		"Style: Sub,Arial,16,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,2,2,2,11,12,10,1",
		"Style: Top-en,Arial,16,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,2,2,8,11,12,10,1", // collides with the future rename of "Top"
		"",
		"[Events]",
		"Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text",
		"Dialogue: 0,0:00:01.50,0:00:02.50,Sub,,5,6,7,,{\\an5}{\\rSub}sub text",
		"Dialogue: 0,0:00:01.00,0:00:02.00,Top-en,,0,0,0,,second line",
	}, "\n")))

	out := filepath.Join(t.TempDir(), "out.ass")
	nStyles, nLines, _, err := mergeAndWrite(prim, sec, "en", rescaleFactor(prim, sec), out)
	if err != nil {
		t.Fatal(err)
	}
	if nStyles != 4 || nLines != 4 {
		t.Fatalf("got %d styles, %d lines; want 4, 4", nStyles, nLines)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)

	// rescale factor = 1080/288 = 3.75: secondary fontsize 16 -> 60, margins 10 -> 38
	want := strings.Join([]string{
		"[Script Info]",
		"; a comment",
		"PlayResX: 1920",
		"PlayResY: 1080",
		"WrapStyle: 2",
		"",
		"[V4+ Styles]",
		"Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding",
		"Style: Top,Arial,20,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,2,2,2,10,10,40,1",
		"Style: Bottom,Arial,20,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,2,2,2,10,10,40,1",
		"Style: Sub-en,Arial,60,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,8,8,8,41,45,38,1",
		"Style: Top-en-en,Arial,60,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,8,8,8,41,45,38,1",
		"",
		"[Events]",
		"Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text",
		"Dialogue: 0,0:00:01.00,0:00:02.00,Top,,0,0,40,,{\\an1}top text",
		"Dialogue: 0,0:00:01.00,0:00:02.00,Top-en-en,,0,0,0,,second line",
		"Dialogue: 0,0:00:01.50,0:00:02.50,Sub-en,,19,23,26,,{\\an6}{\\rSub-en}sub text",
		"Dialogue: 0,0:00:02.00,0:00:03.00,Bottom,,0,0,40,,hello, world",
		"",
		"[Fonts]",
		"fontdata: abc",
	}, "\n") + "\n"
	if got != want {
		t.Errorf("merged output mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestParseTimeCS(t *testing.T) {
	cases := map[string]int{
		"0:00:01.00":  100,
		"1:02:03.99":  372399,
		"0:00:00.00":  0,
		"12:34:56.78": 4529678,
		"0:59:59.999": 360000, // centisecond rounding
	}
	for in, want := range cases {
		if got := parseTimeCS(in); got != want {
			t.Errorf("parseTimeCS(%q) = %d, want %d", in, got, want)
		}
	}
}
