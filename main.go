package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/asticode/go-astisub"
	"golang.org/x/text/language"
)

const usage = `subsandwicher: merge two subtitle tracks of a video into a single .ass file

Usage:
  subsandwicher [flags] <video-file> <primary-lang> <secondary-lang>

The merged file is written next to the video as
  <video base name>-<primary-lang>_<secondary-lang>.ass

Flags:
  -primary-index N      use subtitle stream N (absolute ffprobe index) as primary
  -secondary-index N    use subtitle stream N (absolute ffprobe index) as secondary
`

// ISO 639-2/B -> /T codes where the two differ (Matroska tags use B codes).
var isoBtoT = map[string]string{
	"alb": "sqi", "arm": "hye", "baq": "eus", "bur": "mya", "chi": "zho",
	"cze": "ces", "dut": "nld", "fre": "fra", "ger": "deu", "geo": "kat",
	"gre": "ell", "ice": "isl", "mac": "mkd", "may": "msa", "per": "fas",
	"rum": "ron", "slo": "slk", "tib": "bod", "wel": "cym",
}

var isoTtoB = func() map[string]string {
	m := make(map[string]string, len(isoBtoT))
	for b, t := range isoBtoT {
		m[t] = b
	}
	return m
}()

var bitmapSubCodecs = map[string]bool{
	"hdmv_pgs_subtitle": true,
	"dvd_subtitle":      true,
	"dvb_subtitle":      true,
	"arib_caption":      true,
}

type stream struct {
	Index     int               `json:"index"`
	CodecName string            `json:"codec_name,omitempty"`
	CodecType string            `json:"codec_type,omitempty"`
	Tags      map[string]string `json:"tags,omitempty"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	var primaryIdx, secondaryIdx int
	flag.IntVar(&primaryIdx, "primary-index", -1, "override primary subtitle track (absolute ffprobe stream index)")
	flag.IntVar(&secondaryIdx, "secondary-index", -1, "override secondary subtitle track (absolute ffprobe stream index)")
	flag.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	flag.Parse()

	if flag.NArg() != 3 {
		fmt.Fprint(os.Stderr, usage)
		return errors.New("expected exactly 3 arguments")
	}
	video, lang1, lang2 := flag.Arg(0), flag.Arg(1), flag.Arg(2)
	if _, err := os.Stat(video); err != nil {
		return fmt.Errorf("input video not accessible: %w", err)
	}
	if strings.EqualFold(strings.TrimSpace(lang1), strings.TrimSpace(lang2)) {
		return errors.New("primary and secondary language codes must differ")
	}

	streams, err := probeSubtitleStreams(video)
	if err != nil {
		return err
	}
	prim, err := pickTrack(streams, lang1, primaryIdx, "primary")
	if err != nil {
		return err
	}
	sec, err := pickTrack(streams, lang2, secondaryIdx, "secondary")
	if err != nil {
		return err
	}
	if prim.Index == sec.Index {
		return fmt.Errorf("primary and secondary resolved to the same subtitle stream (%d)", prim.Index)
	}

	tmpDir, err := os.MkdirTemp("", "subsandwicher-*")
	if err != nil {
		return fmt.Errorf("creating temp dir failed: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	primDoc, err := extractSubtitle(video, prim, tmpDir, "primary")
	if err != nil {
		return fmt.Errorf("primary track: %w", err)
	}
	secDoc, err := extractSubtitle(video, sec, tmpDir, "secondary")
	if err != nil {
		return fmt.Errorf("secondary track: %w", err)
	}

	scale := rescaleFactor(primDoc, secDoc)
	if scale != 1 {
		fmt.Fprintf(os.Stderr, "rescaling secondary track by factor %.4f (PlayResY %d -> %d)\n",
			scale, playResY(secDoc), playResY(primDoc))
	}

	outPath := filepath.Join(filepath.Dir(video),
		strings.TrimSuffix(filepath.Base(video), filepath.Ext(video))+"-"+lang1+"_"+lang2+".ass")
	nStyles, nLines, err := mergeAndWrite(primDoc, secDoc, lang2, scale, outPath)
	if err != nil {
		return err
	}
	fmt.Printf("wrote %s (%d styles, %d lines)\n", outPath, nStyles, nLines)
	return nil
}

func probeSubtitleStreams(video string) ([]stream, error) {
	out, err := exec.Command("ffprobe", "-v", "error", "-print_format", "json",
		"-show_streams", "-select_streams", "s", video).Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe failed: %w", err)
	}
	var p struct {
		Streams []stream `json:"streams"`
	}
	if err := json.Unmarshal(out, &p); err != nil {
		return nil, fmt.Errorf("parsing ffprobe output failed: %w", err)
	}
	var subs []stream
	for _, s := range p.Streams {
		if s.CodecType == "subtitle" {
			subs = append(subs, s)
		}
	}
	if len(subs) == 0 {
		return nil, errors.New("no subtitle streams found in input")
	}
	return subs, nil
}

// languageCandidates returns every tag form that should match the given code,
// covering 2-letter input, ISO 639-2 T codes and Matroska's B codes.
func languageCandidates(code string) map[string]bool {
	code = strings.ToLower(strings.TrimSpace(code))
	set := map[string]bool{code: true}
	if t, ok := isoTtoB[code]; ok {
		set[t] = true
	}
	if b, ok := isoBtoT[code]; ok {
		set[b] = true
	}
	if tag, err := language.Parse(code); err == nil {
		if base, _ := tag.Base(); base.String() != "" && base.String() != "und" {
			set[base.String()] = true
			if iso3 := base.ISO3(); iso3 != "" && iso3 != "und" {
				set[iso3] = true
				if b, ok := isoTtoB[iso3]; ok {
					set[b] = true
				}
			}
		}
	}
	return set
}

func streamLang(s stream) string {
	for _, k := range []string{"language", "LANGUAGE", "Language"} {
		if v, ok := s.Tags[k]; ok && strings.TrimSpace(v) != "" {
			return strings.ToLower(strings.TrimSpace(v))
		}
	}
	return ""
}

func streamTitle(s stream) string {
	for _, k := range []string{"title", "TITLE", "Title"} {
		if v, ok := s.Tags[k]; ok {
			return v
		}
	}
	return ""
}

func pickTrack(streams []stream, code string, overrideIdx int, role string) (*stream, error) {
	if overrideIdx >= 0 {
		for i := range streams {
			if streams[i].Index == overrideIdx {
				if bitmapSubCodecs[streams[i].CodecName] {
					return nil, fmt.Errorf("%s: stream %d is image-based (%s), which is out of scope", role, overrideIdx, streams[i].CodecName)
				}
				return &streams[i], nil
			}
		}
		return nil, fmt.Errorf("%s: no subtitle stream with index %d", role, overrideIdx)
	}

	var matches []stream
	for _, s := range streams {
		if bitmapSubCodecs[s.CodecName] {
			continue
		}
		if languageCandidates(code)[streamLang(s)] {
			matches = append(matches, s)
		}
	}
	if len(matches) == 0 {
		for _, s := range streams {
			if bitmapSubCodecs[s.CodecName] && languageCandidates(code)[streamLang(s)] {
				return nil, fmt.Errorf("%s: only image-based subtitle track(s) (%s) found for language %q, which is out of scope",
					role, s.CodecName, code)
			}
		}
		var have []string
		for _, s := range streams {
			have = append(have, fmt.Sprintf("#%d %s lang=%s title=%q", s.Index, s.CodecName, streamLang(s), streamTitle(s)))
		}
		return nil, fmt.Errorf("%s: no text subtitle track for language %q (available: %s)", role, code, strings.Join(have, "; "))
	}

	// Prefer tracks whose title does not indicate SDH, then lowest index.
	sort.SliceStable(matches, func(i, j int) bool {
		sdh := func(s stream) bool { return strings.Contains(strings.ToLower(streamTitle(s)), "sdh") }
		if sdh(matches[i]) != sdh(matches[j]) {
			return !sdh(matches[i])
		}
		return matches[i].Index < matches[j].Index
	})
	chosen := matches[0]
	fmt.Fprintf(os.Stderr, "%s: stream #%d (%s, lang=%s, title=%q)\n", role, chosen.Index, chosen.CodecName, streamLang(chosen), streamTitle(chosen))
	return &chosen, nil
}

// extractSubtitle extracts the stream into tmpDir and returns a parsed ASS document.
func extractSubtitle(video string, s *stream, tmpDir, role string) (*ASSDoc, error) {
	raw := filepath.Join(tmpDir, role+".ass")
	var codec string
	switch s.CodecName {
	case "ass":
		codec = "copy"
	case "ssa":
		codec = "ass" // transcode SSA v4 to V4+
	default:
		codec = "srt"
		raw = filepath.Join(tmpDir, role+".srt")
	}
	args := []string{"-hide_banner", "-loglevel", "error", "-y",
		"-i", video, "-map", fmt.Sprintf("0:%d", s.Index), "-c:s", codec, raw}
	if b, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("ffmpeg extracting stream %d failed: %w: %s", s.Index, err, strings.TrimSpace(string(b)))
	}
	if strings.HasSuffix(raw, ".srt") {
		return srtToASSDoc(raw)
	}
	return parseASSFile(raw)
}

// srtToASSDoc parses an SRT file with go-astisub (HTML tags, timings, override
// tags) and serializes it directly into an ASSDoc with a clean Default style.
// astisub's own SSA writer is not used because it drops SRT bold/italic/
// underline/font styling.
func srtToASSDoc(srtPath string) (*ASSDoc, error) {
	f, err := os.Open(srtPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sub, err := astisub.ReadFromSRT(f)
	if err != nil {
		return nil, err
	}

	doc := &ASSDoc{Info: map[string]string{
		"playresx": "384", "playresy": "288",
	}}
	doc.InfoRaw = []string{
		"; Converted from SRT by subsandwicher (parsing by go-astisub)",
		"ScriptType: v4.00+",
		"PlayResX: 384",
		"PlayResY: 288",
		"ScaledBorderAndShadow: yes",
	}
	doc.Styles = []*ASSStyle{{Val: map[string]string{
		"name": "Default", "fontname": "Arial", "fontsize": "16",
		"primarycolour": "&Hffffff", "secondarycolour": "&Hffffff",
		"outlinecolour": "&H0", "backcolour": "&H0",
		"bold": "0", "italic": "0", "underline": "0", "strikeout": "0",
		"scalex": "100", "scaley": "100", "spacing": "0", "angle": "0",
		"borderstyle": "1", "outline": "1", "shadow": "0",
		"alignment": "2", "marginl": "10", "marginr": "10", "marginv": "10",
		"encoding": "1",
	}}}

	for _, item := range sub.Items {
		d := &Dialogue{
			Layer:   "0",
			Start:   formatCSSTime(item.StartAt),
			End:     formatCSSTime(item.EndAt),
			Style:   "Default",
			MarginL: "0",
			MarginR: "0",
			MarginV: "0",
			startCS: int(item.StartAt / (10 * time.Millisecond)),
		}
		var lines []string
		for _, line := range item.Lines {
			var b strings.Builder
			for _, li := range line.Items {
				open, close := srtItemTags(li.InlineStyle)
				b.WriteString(open)
				b.WriteString(li.Text)
				b.WriteString(close)
			}
			// astisub keeps a trailing empty item for the blank separator line
			// of SRT entries; skip those so no stray "\N" is emitted.
			if s := strings.TrimSpace(b.String()); s != "" {
				lines = append(lines, s)
			}
		}
		d.Text = strings.Join(lines, `\N`)
		if item.InlineStyle != nil && item.InlineStyle.SRTPosition != 0 {
			d.Text = fmt.Sprintf(`{\an%d}`, item.InlineStyle.SRTPosition) + d.Text
		}
		doc.Dialogues = append(doc.Dialogues, d)
	}
	return doc, nil
}

// srtItemTags renders SRT inline styling (bold/italic/underline/color) as
// ASS override tag open/close pairs.
func srtItemTags(sa *astisub.StyleAttributes) (open, close string) {
	if sa == nil {
		return "", ""
	}
	var o, c strings.Builder
	add := func(on bool, tag string) {
		if !on {
			return
		}
		o.WriteString(`{\` + tag + `1}`)
		c.WriteString(`{\` + tag + `0}`)
	}
	add(sa.SRTBold, "b")
	add(sa.SRTItalics, "i")
	add(sa.SRTUnderline, "u")
	if sa.SRTColor != nil {
		o.WriteString(fmt.Sprintf(`{\c&H%02X%02X%02X&}`, sa.SRTColor.Blue, sa.SRTColor.Green, sa.SRTColor.Red))
		c.WriteString(`{\c}`)
	}
	return o.String(), c.String()
}

// formatCSSTime renders a duration as an ASS timestamp "H:MM:SS.CC".
func formatCSSTime(d time.Duration) string {
	cs := int(d / (10 * time.Millisecond))
	if cs < 0 {
		cs = 0
	}
	return fmt.Sprintf("%d:%02d:%02d.%02d", cs/360000, (cs%360000)/6000, (cs%6000)/100, cs%100)
}
