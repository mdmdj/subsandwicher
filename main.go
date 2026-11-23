package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mdmdj/subsandwicher/sandwicher"
)

const usage = `subsandwicher: merge two subtitle tracks of a video into a single .ass file

Usage:
  subsandwicher probe --video <file>
  subsandwicher merge --video <file> -primary-index N -secondary-index M [-out <path>] [-report <path>]
  subsandwicher report --ass <merged.ass> [-out <report.json>] [--secondary-suffix=-eng]
  subsandwicher apply --ass <merged.ass> --report <report.json> --out <out.ass> [--fix=<window>:<candidate> ...] [--fix-json=<fix object> ...]
  subsandwicher <video-file> <primary-lang> <secondary-lang>

All arguments are named. "probe" prints subtitle streams as JSON, including
external subtitle files detected beside the video. "merge"
writes the merged file, a per-event decision log, and prints result statistics
plus dialogue-line events as JSON. "report" scans an existing merged .ass and
writes only the decision log. "apply" writes a copy of the merged file with
selected review candidates applied; --fix-json carries manual overrides and
comments. The last form is the classic language-based CLI, writing output next
to the video.
`

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func commandName() string { return filepath.Base(os.Args[0]) }

func run() error {
	// Windows: put our whole process tree into a kill-on-close Job Object so
	// the GUI's terminating closes every child too.
	if err := sandwicher.AttachToOwnedJob(); err != nil {
		fmt.Fprintln(os.Stderr, "job object warn:", err)
	}
	if len(os.Args) < 2 {
		return cmdLangMerge(os.Args[1:], true)
	}
	switch os.Args[1] {
	case "probe":
		return cmdProbe(os.Args[2:])
	case "merge":
		return cmdMerge(os.Args[2:])
	case "report":
		return cmdReport(os.Args[2:])
	case "apply":
		return cmdApply(os.Args[2:])
	default:
		return cmdLangMerge(os.Args[1:], false)
	}
}

func cmdProbe(args []string) error {
	fs := flag.NewFlagSet("probe", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	video := ""
	fs.StringVar(&video, "video", "", "input video file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if video == "" {
		fs.Usage()
		return errors.New("probe requires -video")
	}
	if _, err := os.Stat(video); err != nil {
		return fmt.Errorf("input video not accessible: %w", err)
	}
	streams, err := sandwicher.ProbeSubtitleStreams(video)
	if err != nil {
		return err
	}
	enc, err := json.Marshal(sandwicher.ProbeResult{Video: video, Streams: streams})
	if err != nil {
		return err
	}
	fmt.Println(string(enc))
	return nil
}

func cmdMerge(args []string) error {
	fs := flag.NewFlagSet("merge", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	video := ""
	primaryIdx := fs.Int("primary-index", -1, "primary subtitle stream (absolute ffprobe index)")
	secondaryIdx := fs.Int("secondary-index", -1, "secondary subtitle stream (absolute ffprobe index)")
	out := fs.String("out", "", "output .ass path (defaults next to the video)")
	report := fs.String("report", "", "decision log path (defaults to <out>.report.json)")
	noExact := fs.Bool("no-exact", false, "skip libass ink masks; use estimated boxes only")
	fs.StringVar(&video, "video", "", "input video file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if video == "" || *primaryIdx < 0 || *secondaryIdx < 0 {
		fs.Usage()
		if video == "" {
			return errors.New("merge requires -video")
		}
		if *primaryIdx < 0 {
			return errors.New("merge requires -primary-index")
		}
		return errors.New("merge requires -secondary-index")
	}
	if _, err := os.Stat(video); err != nil {
		return fmt.Errorf("input video not accessible: %w", err)
	}
	outPath := *out
	if outPath == "" {
		outPath = filepath.Join(filepath.Dir(video), strings.TrimSuffix(filepath.Base(video), filepath.Ext(video))+".merged.ass")
	}
	stats, err := sandwicher.MergeTracks(video, *primaryIdx, *secondaryIdx, outPath, *report, !*noExact,
		func(p sandwicher.Progress) {
			// progress lines go to stderr (stdout is the JSON result contract)
			if b, err := json.Marshal(p); err == nil {
				fmt.Fprintf(os.Stderr, "%s\n", b)
			}
		})
	if err != nil {
		return err
	}
	enc, err := json.Marshal(stats)
	if err != nil {
		return err
	}
	fmt.Println(string(enc))
	return nil
}

// cmdLangMerge preserves the original language-based CLI; CLI subcommands are
// internal, so the first arg is either a file path or a subcommand keyword.
func cmdLangMerge(args []string, defaultMode bool) error {
	var primaryIdx, secondaryIdx int
	fs := flag.NewFlagSet(commandName(), flag.ContinueOnError)
	fs.IntVar(&primaryIdx, "primary-index", -1, "override primary subtitle track (absolute ffprobe stream index)")
	fs.IntVar(&secondaryIdx, "secondary-index", -1, "override secondary subtitle track (absolute ffprobe stream index)")
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	if !defaultMode {
		fs.Parse(args)
	} else {
		// directly invoked with no subcommand: parse as flags implicitly
		fs.Parse(args)
	}
	if fs.NArg() != 3 {
		fmt.Fprint(os.Stderr, usage)
		return errors.New("expected exactly 3 arguments")
	}
	video, lang1, lang2 := fs.Arg(0), fs.Arg(1), fs.Arg(2)
	if _, err := os.Stat(video); err != nil {
		return fmt.Errorf("input video not accessible: %w", err)
	}
	if strings.EqualFold(strings.TrimSpace(lang1), strings.TrimSpace(lang2)) {
		return errors.New("primary and secondary language codes must differ")
	}

	streams, err := sandwicher.ProbeSubtitleStreams(video)
	if err != nil {
		return err
	}
	prim, err := sandwicher.PickTrack(streams, lang1, primaryIdx, "primary")
	if err != nil {
		return err
	}
	sec, err := sandwicher.PickTrack(streams, lang2, secondaryIdx, "secondary")
	if err != nil {
		return err
	}
	if prim.Index == sec.Index {
		return fmt.Errorf("primary and secondary resolved to the same subtitle stream (%d)", prim.Index)
	}
	outPath := filepath.Join(filepath.Dir(video),
		strings.TrimSuffix(filepath.Base(video), filepath.Ext(video))+"."+lang1+"-"+lang2+".ass")
	stats, err := sandwicher.MergeTracks(video, prim.Index, sec.Index, outPath, "", true, nil)
	if err != nil {
		return err
	}
	fmt.Printf("wrote %s (%d styles, %d lines, %d conflicts)\n", stats.OutPath, stats.Styles, stats.Lines, stats.Conflicts)
	return nil
}

// cmdReport scans an existing merged .ass and writes the decision log without
// rewriting the subtitles.
func cmdReport(args []string) error {
	fs := flag.NewFlagSet("report", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	ass := fs.String("ass", "", "merged .ass to scan")
	out := fs.String("out", "", "report path (defaults to <ass>.report.json)")
	suffix := fs.String("secondary-suffix", "-eng", "style suffix marking the secondary track (empty = run the track classifier)")
	noExact := fs.Bool("no-exact", false, "skip libass ink masks; use estimated boxes only")
	video := fs.String("video", "", "optional source video for the job header")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *ass == "" {
		fs.Usage()
		return errors.New("report requires -ass")
	}
	path := *out
	if path == "" {
		path = *ass + ".report.json"
	}
	rep, err := sandwicher.ScanASS(*ass, sandwicher.ScanOptions{
		Video:           *video,
		SecondarySuffix: *suffix,
		Exact:           !*noExact,
	})
	if err != nil {
		return err
	}
	if err := sandwicher.WriteReport(path, rep); err != nil {
		return err
	}
	enc, err := json.Marshal(map[string]any{
		"report":          path,
		"events":          rep.Summary.Events,
		"conflicts":       rep.Summary.Conflicts,
		"windows":         rep.Summary.Windows,
		"review_events":   rep.Summary.ReviewEvents,
		"required_events": rep.Summary.RequiredEvents,
		"by_surface":      rep.Summary.BySurface,
		"by_level":        rep.Summary.ByLevel,
		"exact":           rep.Job.Exact,
		"exact_events":    rep.Job.ExactEvents,
	})
	if err != nil {
		return err
	}
	fmt.Println(string(enc))
	return nil
}

// fixList collects repeatable --fix=window:candidate flags.
type fixList []sandwicher.Fix

func (f *fixList) String() string { return "" }

// fixJSONList collects --fix-json=<object> flags for fixes that carry manual
// overrides or comments, which do not fit the compact window:candidate form.
type fixJSONList []sandwicher.Fix

func (f *fixJSONList) String() string { return "" }

func (f *fixJSONList) Set(v string) error {
	var fix sandwicher.Fix
	if err := json.Unmarshal([]byte(v), &fix); err != nil {
		return fmt.Errorf("fix-json: %w", err)
	}
	*f = append(*f, fix)
	return nil
}

func (f *fixList) Set(v string) error {
	parts := strings.SplitN(strings.TrimSpace(v), ":", 2)
	if len(parts) != 2 {
		return fmt.Errorf("fix must be window:candidate, got %q", v)
	}
	w, err := strconv.Atoi(parts[0])
	if err != nil {
		return fmt.Errorf("fix window %q: %w", parts[0], err)
	}
	c, err := strconv.Atoi(parts[1])
	if err != nil {
		return fmt.Errorf("fix candidate %q: %w", parts[1], err)
	}
	*f = append(*f, sandwicher.Fix{WindowID: w, SuggestionIndex: c})
	return nil
}

// cmdApply writes the merged file with the chosen review candidates applied.
func cmdApply(args []string) error {
	fs := flag.NewFlagSet("apply", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	ass := fs.String("ass", "", "merged .ass to modify")
	report := fs.String("report", "", "decision log with the candidates")
	out := fs.String("out", "", "output .ass")
	var fixes fixList
	var fixJSON fixJSONList
	fs.Var(&fixes, "fix", "window:candidate (repeatable; candidate -1 keeps the original)")
	fs.Var(&fixJSON, "fix-json", "fix object as JSON, including manual overrides/comments (repeatable)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *ass == "" || *out == "" {
		fs.Usage()
		return errors.New("apply requires -ass and -out")
	}
	allFixes := append([]sandwicher.Fix(fixes), []sandwicher.Fix(fixJSON)...)
	res, err := sandwicher.ApplyFixes(*ass, *report, *out, allFixes)
	if err != nil {
		return err
	}
	enc, err := json.Marshal(res)
	if err != nil {
		return err
	}
	fmt.Println(string(enc))
	return nil
}
