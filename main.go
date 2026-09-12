package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mdmdj/subsandwicher/sandwicher"
)

const usage = `subsandwicher: merge two subtitle tracks of a video into a single .ass file

Usage:
  subsandwicher probe --video <file>
  subsandwicher merge --video <file> -primary-index N -secondary-index M [-out <path>]
  subsandwicher <video-file> <primary-lang> <secondary-lang>

All arguments are named. "probe" prints subtitle streams as JSON. "merge"
writes the merged file and prints result statistics plus dialogue-line
events as JSON. The third form is the classic language-based CLI, writing
output next to the video.
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
	stats, err := sandwicher.MergeTracks(video, *primaryIdx, *secondaryIdx, outPath,
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
		strings.TrimSuffix(filepath.Base(video), filepath.Ext(video))+"-"+lang1+"_"+lang2+".ass")
	stats, err := sandwicher.MergeTracks(video, prim.Index, sec.Index, outPath, nil)
	if err != nil {
		return err
	}
	fmt.Printf("wrote %s (%d styles, %d lines)\n", stats.OutPath, stats.Styles, stats.Lines)
	return nil
}
