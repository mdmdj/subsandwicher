package sandwicher

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Decision levels and surface policies. Levels describe how much a decision
// could change what the viewer sees; surface says whether the UI should show
// it, and whether it can be applied without asking.
const (
	LevelInfo = iota
	LevelNotice
	LevelWarning
	LevelCritical
)

const (
	SurfaceNone     = "none"     // log only
	SurfaceAuto     = "auto"     // applied, safe to hide behind a summary
	SurfaceReview   = "review"   // show in the conflict review list
	SurfaceRequired = "required" // must be acknowledged before export
)

// Change kinds, used for filtering. Keep these stable.
const (
	KindRole      = "role"
	KindAlignment = "alignment"
	KindScale     = "scale"
	KindOrder     = "order"
	KindConflict  = "conflict"
	KindEdge      = "edge"
	KindNone      = "none"
)

func levelName(l int) string {
	switch l {
	case LevelCritical:
		return "critical"
	case LevelWarning:
		return "warning"
	case LevelNotice:
		return "notice"
	default:
		return "info"
	}
}

func surfaceRank(s string) int {
	switch s {
	case SurfaceRequired:
		return 3
	case SurfaceReview:
		return 2
	case SurfaceAuto:
		return 1
	default:
		return 0
	}
}

func maxSurface(a, b string) string {
	if surfaceRank(b) > surfaceRank(a) {
		return b
	}
	return a
}

// Decision is one recorded choice about one event. Every event carries at
// least one, so a UI can list "what this job did" exhaustively.
type Decision struct {
	Rule       string  `json:"rule"`
	Kind       string  `json:"kind"`
	Level      int     `json:"level"`
	LevelName  string  `json:"levelName"`
	Action     string  `json:"action"`
	Before     string  `json:"before,omitempty"`
	After      string  `json:"after,omitempty"`
	Reason     string  `json:"reason"`
	Confidence float64 `json:"confidence"`
	Surface    string  `json:"surface"`
}

// EventDecision is the per-event log entry.
type EventDecision struct {
	ID         int        `json:"id"`
	Track      string     `json:"track"`
	Role       string     `json:"role"`
	Style      string     `json:"style"`
	Start      float64    `json:"start"`
	End        float64    `json:"end"`
	Text       string     `json:"text"`
	Layer      int        `json:"layer"`
	Align      int        `json:"align"`
	Pos        *Point     `json:"pos,omitempty"`
	Moving     bool       `json:"moving"`
	BBox       Box        `json:"bbox"`
	BBoxExact  bool       `json:"bboxExact"`
	ExactBBox  *Box       `json:"exactBBox,omitempty"`
	ExactSeek  float64    `json:"exactSeek,omitempty"`
	Importance float64    `json:"importance"`
	Level      int        `json:"level"`
	LevelName  string     `json:"levelName"`
	Surface    string     `json:"surface"`
	Decisions  []Decision `json:"decisions"`
}

// Conflict is one overlapping pair (or one off-safe-area event).
type Conflict struct {
	ID             int     `json:"id"`
	Kind           string  `json:"kind"`
	Cause          string  `json:"cause"`
	Start          float64 `json:"start"`
	End            float64 `json:"end"`
	Peak           float64 `json:"peak"`
	Events         []int   `json:"events"`
	OverlapRatio   float64 `json:"overlapRatio"`
	Severity       int     `json:"severity"`
	Level          int     `json:"level"`
	LevelName      string  `json:"levelName"`
	Surface        string  `json:"surface"`
	Suppressed     bool    `json:"suppressed"`
	SuppressReason string  `json:"suppressReason,omitempty"`
	Box            Box     `json:"box"`
	Reason         string  `json:"reason"`
}

// Suggestion is a cheap, deterministic fix option. Nothing is applied here.
type Suggestion struct {
	ID              int     `json:"id"`
	Event           int     `json:"event"`
	Kind            string  `json:"kind"` // nudge | shrink | hide | raise | top
	Dx              float64 `json:"dx,omitempty"`
	Dy              float64 `json:"dy,omitempty"`
	At              *Point  `json:"at,omitempty"`
	FontScale       float64 `json:"fontScale,omitempty"`
	Cost            float64 `json:"cost"`
	OriginalCost    float64 `json:"originalCost,omitempty"`
	Improvement     float64 `json:"improvement,omitempty"`
	AutoApply       bool    `json:"autoApply"`
	ManualReview    bool    `json:"manualReview"`
	Recommended     bool    `json:"recommended"`
	DialogueOverlap float64 `json:"dialogueOverlap,omitempty"`
	// SharedWith lists related signs that offer the same move. A candidate
	// shared by every related sign keeps a cluster in its original layout.
	SharedWith []int   `json:"sharedWith,omitempty"`
	Seek       float64 `json:"seek,omitempty"`
	Reason     string  `json:"reason"`
}

// EventSuggestions groups the candidates for one event inside a window, so a
// window with several conflicting events can be resolved one event at a time.
type EventSuggestions struct {
	Event         int     `json:"event"`
	SuggestionIDs []int   `json:"suggestionIds"`
	Seek          float64 `json:"seek,omitempty"`
	Note          string  `json:"note,omitempty"`
	// Related lists sibling signs whose position moves with this event, such as
	// a pair of labels animated by matching \move tags. The UI uses it to offer
	// "same move as #N" without inventing a fix the apply step cannot replay.
	Related []int `json:"related,omitempty"`
}

// ConflictWindow is the review unit: a merged interval plus its events and
// candidate fixes.
type ConflictWindow struct {
	ID                int                `json:"id"`
	Start             float64            `json:"start"`
	End               float64            `json:"end"`
	Peak              float64            `json:"peak"`
	Kind              string             `json:"kind"`
	Cause             string             `json:"cause"`
	Causes            []string           `json:"causes"`
	Severity          int                `json:"severity"`
	Level             int                `json:"level"`
	LevelName         string             `json:"levelName"`
	Surface           string             `json:"surface"`
	Events            []int              `json:"events"`
	Conflicts         []int              `json:"conflicts"`
	Suggestions       []Suggestion       `json:"suggestions"`
	ByEvent           []EventSuggestions `json:"byEvent"`
	AutoApplyCount    int                `json:"autoApplyCount"`
	ManualReviewCount int                `json:"manualReviewCount"`
	Reason            string             `json:"reason"`
}

// JobInfo identifies one processing run.
type JobInfo struct {
	ID              string  `json:"id"`
	CreatedAt       string  `json:"createdAt"`
	Video           string  `json:"video,omitempty"`
	Out             string  `json:"out,omitempty"`
	PrimaryLang     string  `json:"primaryLang,omitempty"`
	SecondaryLang   string  `json:"secondaryLang,omitempty"`
	PrimaryIndex    int     `json:"primaryIndex"`
	SecondaryIndex  int     `json:"secondaryIndex"`
	PlayResX        float64 `json:"playResX"`
	PlayResY        float64 `json:"playResY"`
	Scale           float64 `json:"scale"`
	Styles          int     `json:"styles"`
	Lines           int     `json:"lines"`
	TrackSource     string  `json:"trackSource,omitempty"`
	TrackConfidence float64 `json:"trackConfidence,omitempty"`
	TrackNote       string  `json:"trackNote,omitempty"`
	Exact           bool    `json:"exact"`
	ExactEvents     int     `json:"exactEvents"`
	ExactNote       string  `json:"exactNote,omitempty"`
	Generator       string  `json:"generator"`
}

// Summary aggregates filters.
type Summary struct {
	Events         int            `json:"events"`
	Conflicts      int            `json:"conflicts"`
	Windows        int            `json:"windows"`
	Decisions      int            `json:"decisions"`
	ReviewEvents   int            `json:"reviewEvents"`
	RequiredEvents int            `json:"requiredEvents"`
	ByLevel        map[string]int `json:"byLevel"`
	BySurface      map[string]int `json:"bySurface"`
	ByKind         map[string]int `json:"byKind"`
	ByRule         map[string]int `json:"byRule"`
	ByCause        map[string]int `json:"byCause"`
}

// Report is the full decision log for one job.
type Report struct {
	Version   int              `json:"version"`
	Job       JobInfo          `json:"job"`
	Tracks    *TrackInference  `json:"tracks,omitempty"`
	Summary   Summary          `json:"summary"`
	Events    []EventDecision  `json:"events"`
	Conflicts []Conflict       `json:"conflicts"`
	Windows   []ConflictWindow `json:"windows"`
}

// plannedEvent is the merge pipeline's per-event working record.
type plannedEvent struct {
	d          *Dialogue
	track      Track
	role       Role
	styleName  string
	origAlign  int
	finalAlign int
	geom       *eventGeom
	bbox       Box
	moving     bool
	speed      float64 // script-space points per second for \move events
	importance float64
	textColor  RGB
	durSec     float64
	startSec   float64
	endSec     float64
	chars      int
	decisions  []Decision
	order      int
	origOrder  int
	exactBox   Box
	exactSeek  float64
	exactValid bool
}

func (p *plannedEvent) add(d Decision) {
	d.LevelName = levelName(d.Level)
	if d.Confidence == 0 {
		d.Confidence = 1
	}
	if d.Surface == "" {
		d.Surface = SurfaceNone
	}
	p.decisions = append(p.decisions, d)
}

func (p *plannedEvent) overall() (int, string) {
	lvl := LevelInfo
	surf := SurfaceNone
	for _, d := range p.decisions {
		if d.Level > lvl {
			lvl = d.Level
		}
		surf = maxSurface(surf, d.Surface)
	}
	return lvl, surf
}

func importanceOf(role Role, durSec float64, chars int, fontSize, baseFont float64, moving bool) float64 {
	fontRatio := 1.0
	if baseFont > 0 {
		fontRatio = math.Min(fontSize/baseFont, 1.5) / 1.5
	}
	sign := 0.0
	if role == RoleSign {
		sign = 1
	}
	still := 1.0
	if moving {
		still = 0
	}
	v := 0.35*math.Min(durSec/3.0, 1) +
		0.25*math.Min(float64(chars)/20.0, 1) +
		0.15*fontRatio +
		0.15*sign +
		0.10*still
	return math.Round(v*1000) / 1000
}

// NewJobInfo builds the stable header for a job log.
func NewJobInfo(video, out string) JobInfo {
	return JobInfo{
		ID:        fmt.Sprintf("%d-%d", time.Now().Unix(), os.Getpid()),
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		Video:     video,
		Out:       out,
		Generator: "subsandwicher",
	}
}

func measurePlan(events []*plannedEvent, styles map[string]*ASSStyle, playW, playH float64, baseFont map[Track]float64) {
	for _, p := range events {
		st := styles[p.styleName]
		p.geom = measureEvent(p.d, st, playW, playH)
		if n, ok := inlineAlign(p.d.Text); ok {
			p.finalAlign = n
		} else if st != nil {
			p.finalAlign = styleInt(st, "alignment")
		}
		if p.finalAlign < 1 || p.finalAlign > 9 {
			p.finalAlign = 2
		}
		p.startSec = float64(p.d.startCS) / 100
		p.endSec = float64(parseTimeCS(p.d.End)) / 100
		p.durSec = math.Max(0, p.endSec-p.startSec)
		p.moving = p.geom.move != nil
		p.chars = p.geom.charCount
		p.bbox = p.geom.lifetimeBox(p.startSec, p.endSec, playW, playH)
		p.importance = importanceOf(p.role, p.durSec, p.chars, p.geom.fontSize, baseFont[p.track], p.moving)
		p.textColor = eventTextColor(p.d, st)
	}
}

func kindOf(a, b *plannedEvent) string {
	ad := a.role == RoleDialogue
	bd := b.role == RoleDialogue
	switch {
	case ad && bd:
		return "dialogue-dialogue"
	case ad || bd:
		return "sign-dialogue"
	default:
		return "sign-sign"
	}
}

// causeOf groups conflicts by the layout problem behind them, so a UI can
// batch fixes by cause rather than by individual window.
func causeOf(a, b *plannedEvent) string {
	ad := a.role == RoleDialogue
	bd := b.role == RoleDialogue
	sameTrack := a.track == b.track
	switch {
	case ad && bd:
		if sameTrack {
			return "same-track-dialogue"
		}
		return "cross-track-dialogue"
	case ad || bd:
		sign := a
		if ad {
			sign = b
		}
		if sameTrack {
			return sign.track.String() + "-sign-vs-dialogue"
		}
		if sign.track == TrackSecondary {
			return "secondary-sign-vs-primary-dialogue"
		}
		return "primary-sign-vs-secondary-dialogue"
	default:
		if sameTrack {
			return a.track.String() + "-signs"
		}
		return "cross-track-signs"
	}
}

func severityOf(ratio, importance, durSec float64) int {
	r := math.Min(math.Max(ratio*4, 0), 1)
	imp := math.Min(math.Max(importance, 0), 1)
	d := math.Min(math.Max(durSec/1.0, 0), 1)
	return int(math.Round(100 * (0.55*r + 0.25*imp + 0.20*d)))
}

func levelForSeverity(sev int) int {
	switch {
	case sev >= 65:
		return LevelCritical
	case sev >= 40:
		return LevelWarning
	case sev >= 20:
		return LevelNotice
	default:
		return LevelInfo
	}
}

func surfaceFor(kind string, sev int, suppressed, blocking bool) string {
	if suppressed {
		return SurfaceNone
	}
	if blocking {
		return SurfaceRequired
	}
	switch kind {
	case "dialogue-dialogue":
		if sev >= 20 {
			return SurfaceReview
		}
	case "sign-dialogue":
		if sev >= 30 {
			return SurfaceReview
		}
	case "sign-sign":
		if sev >= 40 {
			return SurfaceReview
		}
	case "edge":
		if sev >= 70 {
			return SurfaceReview
		}
	}
	return SurfaceNone
}

// lowImportance reports whether every non-dialogue participant is too short or
// too unimportant to be worth surfacing.
func lowImportance(events []*plannedEvent) (bool, string) {
	seen := false
	for _, e := range events {
		if e.role == RoleDialogue {
			continue
		}
		seen = true
		if e.importance >= 0.25 && e.durSec >= 0.5 {
			return false, ""
		}
	}
	if !seen {
		return false, ""
	}
	return true, "every non-dialogue event is shorter than 0.5s or below importance 0.25"
}

// mustSee decides whether a conflict should block export. Only two dialogue
// lanes colliding qualifies; a sign over dialogue is review, not a blocker.
func mustSee(kind string, sev int) bool {
	return kind == "dialogue-dialogue" && sev >= 65
}

// hiddenSignCoverage is the share of a sign's box that must be painted over
// before a behind-dialogue conflict is escalated to required review.
const hiddenSignCoverage = 0.90

// drawsBehind reports whether a is painted below b. Higher layers win; within
// one layer libass draws later events on top.
func drawsBehind(a, b *plannedEvent) bool {
	al := atoiDefault(a.d.Layer, 0)
	bl := atoiDefault(b.d.Layer, 0)
	if al != bl {
		return al < bl
	}
	return a.order < b.order
}

func signCoverageAt(sign, dialogue *plannedEvent, t, playW, playH float64) float64 {
	sb := sign.geom.boxAt(t, sign.startSec, sign.endSec, playW, playH)
	db := dialogue.geom.boxAt(t, dialogue.startSec, dialogue.endSec, playW, playH)
	if sb.Area() <= 0 {
		return 0
	}
	return sb.Intersect(db).Area() / sb.Area()
}

func detectConflicts(events []*plannedEvent, playW, playH float64) []Conflict {
	var out []Conflict
	minAbs := 0.0005 * playW * playH
	for i := 0; i < len(events); i++ {
		for j := i + 1; j < len(events); j++ {
			a, b := events[i], events[j]
			ovStart := math.Max(a.startSec, b.startSec)
			ovEnd := math.Min(a.endSec, b.endSec)
			if ovEnd-ovStart < 0.02 {
				continue
			}
			minRatio := 0.02
			if a.role != RoleDialogue && b.role != RoleDialogue {
				minRatio = 0.05
			}
			var best Box
			var bestRatio, bestArea float64
			bestT := ovStart
			samples := []float64{ovStart, (ovStart + ovEnd) / 2, ovEnd}
			for _, t := range samples {
				ba := a.geom.boxAt(t, a.startSec, a.endSec, playW, playH)
				bb := b.geom.boxAt(t, b.startSec, b.endSec, playW, playH)
				inter := ba.Intersect(bb)
				area := inter.Area()
				if area <= 0 {
					continue
				}
				den := math.Min(ba.Area(), bb.Area())
				if den <= 0 {
					continue
				}
				ratio := area / den
				if ratio > bestRatio || (ratio == bestRatio && area > bestArea) {
					bestRatio, bestArea, best, bestT = ratio, area, inter, t
				}
			}
			if bestArea < minAbs || bestRatio < minRatio {
				continue
			}
			kind := kindOf(a, b)
			imp := math.Max(a.importance, b.importance)
			sev := severityOf(bestRatio, imp, ovEnd-ovStart)
			suppressed, why := lowImportance([]*plannedEvent{a, b})
			surface := surfaceFor(kind, sev, suppressed, mustSee(kind, sev))
			reason := fmt.Sprintf("%s overlap %.1f%% of the smaller box for %.2fs", kind, bestRatio*100, ovEnd-ovStart)
			// A sign that draws beneath the dialogue and is almost completely
			// painted over is invisible, not "low importance". Surface it as
			// required instead of hiding or suppressing it.
			if kind == "sign-dialogue" {
				sign, dialogue := a, b
				if a.role == RoleDialogue {
					sign, dialogue = b, a
				}
				if drawsBehind(sign, dialogue) {
					if cov := signCoverageAt(sign, dialogue, bestT, playW, playH); cov >= hiddenSignCoverage {
						suppressed = false
						why = ""
						surface = SurfaceRequired
						reason += fmt.Sprintf("; sign is drawn beneath the dialogue and %.0f%% covered", cov*100)
					}
				}
			}
			lvl := levelForSeverity(sev)
			out = append(out, Conflict{
				Kind:           kind,
				Cause:          causeOf(a, b),
				Start:          round2(ovStart),
				End:            round2(ovEnd),
				Peak:           round2(bestT),
				Events:         []int{a.order, b.order},
				OverlapRatio:   round4(bestRatio),
				Severity:       sev,
				Level:          lvl,
				LevelName:      levelName(lvl),
				Surface:        surface,
				Suppressed:     suppressed,
				SuppressReason: why,
				Box:            roundBox(best),
				Reason:         reason,
			})
		}
	}

	// A sign whose box leaves the safe area is a conflict even with no partner.
	insetX, insetY := playW*0.05, playH*0.05
	safe := Box{X0: insetX, Y0: insetY, X1: playW - insetX, Y1: playH - insetY}
	for _, e := range events {
		if e.role == RoleDialogue || !e.bbox.Valid() {
			continue
		}
		inter := e.bbox.Intersect(safe)
		ratio := 0.0
		if a := e.bbox.Area(); a > 0 {
			ratio = (a - inter.Area()) / a
		}
		if ratio <= 0.02 {
			continue
		}
		sev := severityOf(ratio, e.importance, e.durSec)
		suppressed := e.importance < 0.25 || e.durSec < 0.5
		why := ""
		if suppressed {
			why = "short or low-importance sign"
		}
		lvl := levelForSeverity(sev)
		out = append(out, Conflict{
			Kind:           "edge",
			Cause:          "sign-outside-safe-area",
			Start:          round2(e.startSec),
			End:            round2(e.endSec),
			Peak:           round2((e.startSec + e.endSec) / 2),
			Events:         []int{e.order},
			OverlapRatio:   round4(ratio),
			Severity:       sev,
			Level:          lvl,
			LevelName:      levelName(lvl),
			Surface:        surfaceFor("edge", sev, suppressed, false),
			Suppressed:     suppressed,
			SuppressReason: why,
			Box:            roundBox(e.bbox),
			Reason:         fmt.Sprintf("%.1f%% of the sign box lies outside the 5%% safe area", ratio*100),
		})
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Start != out[j].Start {
			return out[i].Start < out[j].Start
		}
		return out[i].Severity > out[j].Severity
	})
	for i := range out {
		out[i].ID = i + 1
	}
	return out
}

func buildWindows(conflicts []Conflict, events []*plannedEvent, playW, playH float64, exact *exactContext, video *videoContext) []ConflictWindow {
	// Windows are the review units. Log-only and edge conflicts stay in the
	// conflict list but do not create windows.
	var review []Conflict
	for _, c := range conflicts {
		if c.Kind == "edge" || surfaceRank(c.Surface) < surfaceRank(SurfaceReview) {
			continue
		}
		review = append(review, c)
	}
	if len(review) == 0 {
		return nil
	}
	var windows []ConflictWindow
	cur := ConflictWindow{Start: review[0].Start, End: review[0].End}
	addC := func(c Conflict) {
		cur.Conflicts = append(cur.Conflicts, c.ID)
		cur.Events = appendUnique(cur.Events, c.Events...)
		if c.Severity > cur.Severity {
			cur.Severity = c.Severity
			cur.Kind = c.Kind
			cur.Cause = c.Cause
			cur.Peak = c.Peak
			cur.Reason = c.Reason
		}
		if !containsStr(cur.Causes, c.Cause) {
			cur.Causes = append(cur.Causes, c.Cause)
		}
		cur.End = math.Max(cur.End, c.End)
		cur.Surface = maxSurface(cur.Surface, c.Surface)
		if c.Level > cur.Level {
			cur.Level = c.Level
		}
	}
	for _, c := range review {
		if c.Start <= cur.End+0.1 {
			addC(c)
			continue
		}
		windows = append(windows, cur)
		cur = ConflictWindow{Start: c.Start, End: c.End}
		addC(c)
	}
	windows = append(windows, cur)

	byID := map[int]*plannedEvent{}
	for _, e := range events {
		byID[e.order] = e
	}
	cmap := map[int]Conflict{}
	for _, c := range conflicts {
		cmap[c.ID] = c
	}
	for i := range windows {
		w := &windows[i]
		w.ID = i + 1
		w.LevelName = levelName(w.Level)
		if w.Surface == "" {
			w.Surface = SurfaceNone
		}
		w.Suggestions, w.ByEvent, w.AutoApplyCount, w.ManualReviewCount = suggestForWindow(*w, byID, cmap, playW, playH, exact, video)
	}
	return windows
}

func containsInt(xs []int, n int) bool {
	for _, x := range xs {
		if x == n {
			return true
		}
	}
	return false
}

func containsStr(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func appendUnique(dst []int, xs ...int) []int {
	for _, x := range xs {
		if !containsInt(dst, x) {
			dst = append(dst, x)
		}
	}
	return dst
}

func round2(f float64) float64 { return math.Round(f*100) / 100 }
func round4(f float64) float64 { return math.Round(f*10000) / 10000 }
func roundBox(b Box) Box {
	return Box{X0: round2(b.X0), Y0: round2(b.Y0), X1: round2(b.X1), Y1: round2(b.Y1)}
}

// buildReport assembles the event log, conflicts, windows and summary.
func buildReport(events []*plannedEvent, job JobInfo, playW, playH float64, exact *exactContext) *Report {
	conflicts := detectConflicts(events, playW, playH)
	if exact != nil {
		conflicts = exact.filterConflicts(conflicts, events)
	}

	var video *videoContext
	if job.Video != "" {
		video = newVideoContext(job.Video, playW, playH)
	}

	// Attach conflict decisions to events before serializing them.
	for _, c := range conflicts {
		for _, id := range c.Events {
			if id < 0 || id >= len(events) {
				continue
			}
			ev := events[id]
			level := c.Level
			surface := c.Surface
			if c.Suppressed {
				level = LevelNotice
				surface = SurfaceNone
			}
			others := make([]string, 0, len(c.Events)-1)
			for _, oid := range c.Events {
				if oid != id {
					others = append(others, fmt.Sprintf("#%d", oid))
				}
			}
			reason := c.Kind + ": " + c.Reason
			if len(others) > 0 {
				reason = fmt.Sprintf("%s with %s: %s", c.Kind, strings.Join(others, ", "), c.Reason)
			}
			ev.add(Decision{
				Rule:       "conflict." + c.Kind,
				Kind:       KindConflict,
				Level:      level,
				Action:     "flagged",
				Reason:     reason,
				Confidence: 0.6,
				Surface:    surface,
			})
			if c.Suppressed {
				ev.add(Decision{
					Rule:       "conflict.suppressed-low-importance",
					Kind:       KindConflict,
					Level:      LevelNotice,
					Action:     "suppressed",
					Reason:     c.SuppressReason,
					Confidence: 0.7,
					Surface:    SurfaceNone,
				})
			}
		}
	}

	windows := buildWindows(conflicts, events, playW, playH, exact, video)
	for _, w := range windows {
		for _, id := range w.Events {
			if id < 0 || id >= len(events) {
				continue
			}
			if w.Surface == SurfaceNone {
				continue
			}
			events[id].add(Decision{
				Rule:       "window.review",
				Kind:       KindConflict,
				Level:      w.Level,
				Action:     "surfaced",
				Reason:     fmt.Sprintf("conflict window %d from %.2fs to %.2fs", w.ID, w.Start, w.End),
				Confidence: 0.6,
				Surface:    w.Surface,
			})
		}
	}

	rep := &Report{
		Version:   1,
		Job:       job,
		Conflicts: conflicts,
		Windows:   windows,
	}
	rep.Summary = Summary{
		Events:    len(events),
		Conflicts: len(conflicts),
		Windows:   len(windows),
		ByLevel:   map[string]int{},
		BySurface: map[string]int{},
		ByKind:    map[string]int{},
		ByRule:    map[string]int{},
		ByCause:   map[string]int{},
	}
	for _, c := range conflicts {
		rep.Summary.ByCause[c.Cause]++
	}

	for _, ev := range events {
		if len(ev.decisions) == 0 {
			ev.add(Decision{
				Rule: "event.unchanged", Kind: KindNone, Level: LevelInfo,
				Action: "kept", Reason: "no transform or conflict applied", Surface: SurfaceNone,
			})
		}
		lvl, surf := ev.overall()
		pos := (*Point)(nil)
		if ev.geom != nil {
			pos = ev.geom.pos
		}
		var exactBox *Box
		if ev.exactValid {
			b := roundBox(ev.exactBox)
			exactBox = &b
		}
		rep.Events = append(rep.Events, EventDecision{
			ID:         ev.order,
			Track:      ev.track.String(),
			Role:       ev.role.String(),
			Style:      ev.styleName,
			Start:      round2(ev.startSec),
			End:        round2(ev.endSec),
			Text:       assTextPreview(ev.d.Text),
			Layer:      atoiDefault(ev.d.Layer, 0),
			Align:      ev.finalAlign,
			Pos:        pos,
			Moving:     ev.moving,
			BBox:       roundBox(ev.bbox),
			BBoxExact:  false,
			ExactBBox:  exactBox,
			ExactSeek:  round2(ev.exactSeek),
			Importance: ev.importance,
			Level:      lvl,
			LevelName:  levelName(lvl),
			Surface:    surf,
			Decisions:  ev.decisions,
		})
		if surfaceRank(surf) >= surfaceRank(SurfaceReview) {
			rep.Summary.ReviewEvents++
		}
		if surf == SurfaceRequired {
			rep.Summary.RequiredEvents++
		}
		for _, d := range ev.decisions {
			rep.Summary.Decisions++
			rep.Summary.ByLevel[d.LevelName]++
			rep.Summary.BySurface[d.Surface]++
			rep.Summary.ByKind[d.Kind]++
			rep.Summary.ByRule[d.Rule]++
		}
	}
	return rep
}

// WriteReport writes the JSON log.
func WriteReport(path string, rep *Report) error {
	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

// ScanOptions configures a report over an existing .ass file (no merge).
type ScanOptions struct {
	Video           string
	SecondarySuffix string // style suffix marking the secondary track; empty runs the classifier
	Exact           bool   // render libass ink masks for conflict placement
}

// ScanASS parses an existing merged .ass and produces a report without
// rewriting it. Track provenance comes from the style suffix; when the suffix
// is empty every event is treated as primary.
func ScanASS(path string, opts ScanOptions) (*Report, error) {
	doc, err := parseASSFile(path)
	if err != nil {
		return nil, err
	}
	suffix := opts.SecondarySuffix
	styles := map[string]*ASSStyle{}
	for _, s := range doc.Styles {
		styles[s.name()] = s
	}

	suffixMatch := false
	if suffix != "" {
		for _, s := range doc.Styles {
			if strings.HasSuffix(strings.ToLower(s.name()), strings.ToLower(suffix)) {
				suffixMatch = true
				break
			}
		}
	}
	var inf TrackInference
	if suffixMatch {
		inf = TrackInference{
			Method:      "suffix",
			Confidence:  1,
			Note:        fmt.Sprintf("secondary styles end with %q", suffix),
			StyleTracks: map[string]string{},
		}
		for _, s := range doc.Styles {
			if strings.HasSuffix(strings.ToLower(s.name()), strings.ToLower(suffix)) {
				inf.StyleTracks[s.name()] = "secondary"
			} else {
				inf.StyleTracks[s.name()] = "primary"
			}
		}
	} else {
		inf = InferTracks(doc)
		if suffix != "" {
			inf.Note = fmt.Sprintf("suffix %q matched no style; %s", suffix, inf.Note)
		}
	}

	playW, playH := playRes(doc)
	var events []*plannedEvent
	for _, d := range doc.Dialogues {
		styleName := d.Style
		role := classifyRole(d, styleName)
		track := TrackPrimary
		if suffixMatch {
			if strings.HasSuffix(strings.ToLower(styleName), strings.ToLower(suffix)) {
				track = TrackSecondary
			}
		} else if inf.StyleTracks[styleName] == "secondary" {
			track = TrackSecondary
		}
		align := styleInt(styles[styleName], "alignment")
		if n, ok := inlineAlign(d.Text); ok {
			align = n
		}
		p := &plannedEvent{d: d, track: track, role: role, styleName: styleName, origAlign: align}
		events = append(events, p)
	}
	for i, p := range events {
		p.order = i
		p.origOrder = i
		p.add(Decision{
			Rule: "role.classified", Kind: KindRole, Level: LevelInfo, Action: "kept",
			Reason: fmt.Sprintf("classified as %s on the %s track from position/style, not labels",
				p.role.String(), p.track.String()),
			Confidence: 0.8, Surface: SurfaceNone,
		})
		level := LevelInfo
		surface := SurfaceNone
		reason := fmt.Sprintf("track from %s (confidence %.2f): %s", inf.Method, inf.Confidence, inf.Note)
		if inf.Confidence < 0.5 {
			level = LevelWarning
			surface = SurfaceReview
			reason = fmt.Sprintf("low-confidence track inference (%.2f): %s; ask the user which track is primary",
				inf.Confidence, inf.Note)
		}
		p.add(Decision{
			Rule: "track.source", Kind: KindRole, Level: level, Action: "classified",
			Reason: reason, Confidence: inf.Confidence, Surface: surface,
		})
	}
	baseFont := map[Track]float64{}
	measurePlan(events, styles, playW, playH, baseFont)
	job := NewJobInfo(opts.Video, path)
	job.Out = path
	job.PlayResX, job.PlayResY = playW, playH
	job.Scale = 1
	job.Styles = len(doc.Styles)
	job.Lines = len(doc.Dialogues)
	job.TrackSource = inf.Method
	job.TrackConfidence = inf.Confidence
	job.TrackNote = inf.Note
	var ex *exactContext
	if opts.Exact {
		ex = newExactContext(path, playW, playH)
		defer ex.close()
	}
	rep := buildReport(events, job, playW, playH, ex)
	if ex != nil {
		ok, n, note := ex.exactSummary()
		rep.Job.Exact, rep.Job.ExactEvents, rep.Job.ExactNote = ok, n, note
	}
	rep.Tracks = &inf
	return rep, nil
}

// playRes returns the script resolution, with libass's defaults.
func playRes(doc *ASSDoc) (float64, float64) {
	x := 384.0
	y := 288.0
	if v, err := strconv.ParseFloat(strings.TrimSpace(doc.Info["playresx"]), 64); err == nil && v > 0 {
		x = v
	}
	if v, err := strconv.ParseFloat(strings.TrimSpace(doc.Info["playresy"]), 64); err == nil && v > 0 {
		y = v
	}
	return x, y
}
