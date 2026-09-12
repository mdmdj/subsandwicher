package sandwicher

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/text/language"
)

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

// Stream is one subtitle stream as reported by ffprobe.
type Stream struct {
	Index     int               `json:"index"`
	CodecName string            `json:"codec_name,omitempty"`
	CodecType string            `json:"codec_type,omitempty"`
	Tags      map[string]string `json:"tags,omitempty"`
}

// Progress is the per-step extraction progress emitted by MergeTracks.
type Progress struct {
	Role      string  `json:"role"`    // "primary" or "secondary"
	Overall   float64 `json:"overall"` // 0..1 across both extraction steps
	Step      float64 `json:"step"`    // 0..1 within the current extraction
	Seconds   float64 `json:"seconds"` // processed so far in current step
	TotalSecs float64 `json:"total_seconds"`
}

// IsBitmap reports whether the stream is an image-based subtitle format.
func (s *Stream) IsBitmap() bool { return bitmapSubCodecs[s.CodecName] }

// Lang returns the stream language tag (lowercased, "" if unset).
func (s *Stream) Lang() string {
	for _, k := range []string{"language", "LANGUAGE", "Language"} {
		if v, ok := s.Tags[k]; ok && strings.TrimSpace(v) != "" {
			return strings.ToLower(strings.TrimSpace(v))
		}
	}
	return ""
}

// Title returns the stream title tag, "" if unset.
func (s *Stream) Title() string {
	for _, k := range []string{"title", "TITLE", "Title"} {
		if v, ok := s.Tags[k]; ok {
			return v
		}
	}
	return ""
}

// ProbeResult is the JSON payload the CLI `probe` subcommand outputs.
type ProbeResult struct {
	Video   string   `json:"video"`
	Streams []Stream `json:"streams"`
}

// resolveBin prefers the vendored binary next to our exe (process-manager
// clarity and no dependency on the user's PATH), falling back to bare names.
// The resolution result is emitted once so the report log shows which
// concrete binary ran.
var resolvedOnce sync.Once

func resolveBin(name string) string {
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		base := filepath.Base(name) // tolerate "path/to/ffmpeg" inputs
		cands := []string{
			filepath.Join(exeDir, "third_party", "bin", runtime.GOOS, base+extOf()),
			filepath.Join(exeDir, "third_party", "bin", base+extOf()),
			filepath.Join(exeDir, base+extOf()),
		}
		for _, c := range cands {
			if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
				logBinResolved(c)
				return c
			}
		}
	}
	logBinResolved(name)
	return name
}

func logBinResolved(path string) {
	resolvedOnce.Do(func() {
		if b, err := json.Marshal(map[string]string{"event": "bin", "path": path}); err == nil {
			fmt.Fprintln(os.Stderr, string(b))
		}
	})
}

func extOf() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// ProbeSubtitleStreams runs ffprobe on the video and returns its text+bitmap
// subtitle streams.
func ProbeSubtitleStreams(video string) ([]Stream, error) {
	out, err := exec.Command(resolveBin("ffprobe"), "-v", "error", "-print_format", "json",
		"-show_streams", "-select_streams", "s", video).Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe failed: %w", err)
	}
	var p struct {
		Streams []Stream `json:"streams"`
	}
	if err := json.Unmarshal(out, &p); err != nil {
		return nil, fmt.Errorf("parsing ffprobe output failed: %w", err)
	}
	var subs []Stream
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

func PickTrack(streams []Stream, code string, overrideIdx int, role string) (*Stream, error) {
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

	var matches []Stream
	for _, s := range streams {
		if bitmapSubCodecs[s.CodecName] {
			continue
		}
		if languageCandidates(code)[s.Lang()] {
			matches = append(matches, s)
		}
	}
	if len(matches) == 0 {
		for _, s := range streams {
			if bitmapSubCodecs[s.CodecName] && languageCandidates(code)[s.Lang()] {
				return nil, fmt.Errorf("%s: only image-based subtitle track(s) (%s) found for language %q, which is out of scope",
					role, s.CodecName, code)
			}
		}
		var have []string
		for _, s := range streams {
			have = append(have, fmt.Sprintf("#%d %s lang=%s title=%q", s.Index, s.CodecName, s.Lang(), s.Title()))
		}
		return nil, fmt.Errorf("%s: no text subtitle track for language %q (available: %s)", role, code, strings.Join(have, "; "))
	}

	// Prefer tracks whose title does not indicate SDH, then lowest index.
	sort.SliceStable(matches, func(i, j int) bool {
		sdh := func(s Stream) bool { return strings.Contains(strings.ToLower(s.Title()), "sdh") }
		if sdh(matches[i]) != sdh(matches[j]) {
			return !sdh(matches[i])
		}
		return matches[i].Index < matches[j].Index
	})
	chosen := matches[0]
	fmt.Fprintf(os.Stderr, "%s: stream #%d (%s, lang=%s, title=%q)\n", role, chosen.Index, chosen.CodecName, chosen.Lang(), chosen.Title())
	return &chosen, nil
}

// ProbeDuration returns the container duration in seconds via ffprobe.
func ProbeDuration(video string) float64 {
	out, err := exec.Command(resolveBin("ffprobe"), "-v", "error", "-show_entries", "format=duration",
		"-of", "default=nw=1:nk=1", video).Output()
	if err != nil {
		return 0
	}
	var d float64
	if _, err := fmt.Sscanf(strings.TrimSpace(string(out)), "%f", &d); err != nil {
		return 0
	}
	return d
}

// extractSubtitle extracts the stream into tmpDir with parseable progress and
// returns a parsed ASS document. onProgress is called with (seconds, fraction)
// roughly as ffmpeg reports time; it may be nil.
func extractSubtitle(video string, s *Stream, tmpDir, role string, duration float64, onProgress func(seconds, frac float64)) (*ASSDoc, error) {
	raw := filepath.Join(tmpDir, role+".ass")
	var codec string
	switch s.CodecName {
	case "ass", "ssa":
		codec = "copy"
	default:
		codec = "srt"
		raw = filepath.Join(tmpDir, role+".srt")
	}
	args := []string{"-hide_banner", "-nostats", "-loglevel", "error", "-y",
		"-progress", "pipe:1",
		"-i", video, "-map", fmt.Sprintf("0:%d", s.Index), "-c:s", codec, raw}
	cmd := exec.Command(resolveBin("ffmpeg"), args...)
	progOut, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("ffmpeg progress pipe failed: %w", err)
	}
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("ffmpeg start failed: %w", err)
	}

	// UI refresh at ~10fps keeps this cheap; don't spam faster than that.
	if onProgress != nil {
		const throttle = 100 * time.Millisecond
		lastEmit := time.Time{}
		scanner := bufio.NewScanner(progOut)
		scanner.Buffer(make([]byte, 64*1024), 64*1024)
		for scanner.Scan() {
			line := scanner.Text()
			// modern ffmpeg -progress emits out_time=; older builds used time=
			var val string
			if v, ok := strings.CutPrefix(line, "out_time="); ok {
				val = v
			} else if v, ok := strings.CutPrefix(line, "time="); ok {
				val = v
			} else {
				continue
			}
			if t := parseFFmpegTime(val); t > 0 {
				if frac := progressFraction(t, duration); time.Since(lastEmit) > throttle {
					lastEmit = time.Now()
					onProgress(t, frac)
				}
			}
		}
	}
	if err := cmd.Wait(); err != nil {
		tail := strings.TrimSpace(errBuf.String())
		return nil, fmt.Errorf("ffmpeg extracting stream %d failed: %w: %s", s.Index, err, tail)
	}
	if strings.HasSuffix(raw, ".srt") {
		return srtToASSDoc(raw)
	}
	return parseASSFile(raw)
}

// parseFFmpegTime parses the "H:MM:SS.CC" timestamp ffmpeg's -progress emits.
func parseFFmpegTime(v string) float64 {
	v = strings.TrimSpace(v)
	parts := strings.Split(v, ":")
	if len(parts) != 3 {
		return 0
	}
	h, _ := strconv.Atoi(strings.TrimSpace(parts[0]))
	m, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
	sec, _ := strconv.ParseFloat(strings.TrimSpace(parts[2]), 64)
	return float64(h)*3600 + float64(m)*60 + sec
}

// progressFraction clamps time into [0,1] using the known duration (0 when
// unknown, meaning indeterminate progress).
func progressFraction(t, duration float64) float64 {
	if duration <= 0 {
		return 0
	}
	if t >= duration {
		return 1
	}
	return t / duration
}

// MergeStats is what the CLI `merge` subcommand prints to stdout.
type MergeStats struct {
	OutPath string      `json:"out"`
	Styles  int         `json:"styles"`
	Lines   int         `json:"lines"`
	Events  []LineEvent `json:"events"`
}

// MergeTracks probes, extracts, merges and writes out. onProgress may be nil;
// when set it receives (role, seconds} during extraction.
func MergeTracks(video string, primary, secondary int, outPath string, reportProgress func(p Progress)) (*MergeStats, error) {
	streams, err := ProbeSubtitleStreams(video)
	if err != nil {
		return nil, err
	}
	prim, err := PickTrack(streams, "", primary, "primary")
	if err != nil {
		return nil, err
	}
	sec, err := PickTrack(streams, "", secondary, "secondary")
	if err != nil {
		return nil, err
	}
	if prim.Index == sec.Index {
		return nil, fmt.Errorf("primary and secondary resolved to the same subtitle stream (%d)", prim.Index)
	}

	tmpDir, err := os.MkdirTemp("", "subsandwicher-*")
	if err != nil {
		return nil, fmt.Errorf("creating temp dir failed: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	duration := ProbeDuration(video)
	phaseIdx := func(role string) float64 {
		if role == "secondary" {
			return 1
		}
		return 0
	}
	tick := func(role string) func(seconds, frac float64) {
		if reportProgress == nil {
			return nil
		}
		return func(seconds, frac float64) {
			reportProgress(Progress{Role: role, Overall: (phaseIdx(role) + frac) / 2,
				Step: frac, Seconds: seconds, TotalSecs: duration})
		}
	}

	primDoc, err := extractSubtitle(video, prim, tmpDir, "primary", duration, tick("primary"))
	if err != nil {
		return nil, fmt.Errorf("primary track: %w", err)
	}
	secDoc, err := extractSubtitle(video, sec, tmpDir, "secondary", duration, tick("secondary"))
	if err != nil {
		return nil, fmt.Errorf("secondary track: %w", err)
	}

	scale := rescaleFactor(primDoc, secDoc)
	if scale != 1 {
		fmt.Fprintf(os.Stderr, "rescaling secondary track by factor %.4f (PlayResY %d -> %d)\n",
			scale, playResY(primDoc), playResY(secDoc))
	}

	// identifier used for secondary style suffixes: secondary-lang tag, if any.
	lang2 := sec.Lang()
	if lang2 == "" {
		lang2 = fmt.Sprintf("s%d", sec.Index)
	}

	stats, lines, events, err := mergeAndWrite(primDoc, secDoc, lang2, scale, outPath)
	if err != nil {
		return nil, err
	}
	return &MergeStats{OutPath: outPath, Styles: stats, Lines: lines, Events: events}, nil
}
