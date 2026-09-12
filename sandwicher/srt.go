package sandwicher

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/asticode/go-astisub"
)

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
