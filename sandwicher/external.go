package sandwicher

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"golang.org/x/text/language"
)

// External subtitle files get synthetic stream indexes far above any real
// ffprobe index so the UI can pass one integer to the merge command.
const externalStreamIndexBase = 1_000_000

// Formats we can parse locally. Detection ignores other sidecar files (for
// example .sub/MicroDVD) rather than offering something the merge cannot read.
var externalSubtitleExts = map[string]bool{
	".srt": true,
	".ass": true,
	".ssa": true,
	".vtt": true,
}

// External files are commonly kept in a subtitle folder next to the video.
var externalSubtitleDirs = []string{
	"subs", "subtitles", "sub", "subtitle",
	"Subs", "Subtitles", "Sub", "Subtitle",
}

// Subtitle qualifiers that look like language codes to x/text (notably "sdh"
// parses as Southern Kurdish) or would otherwise steal the language slot.
var externalNonLanguageTokens = map[string]bool{
	"cc": true, "hcc": true, "sdh": true, "forced": true,
	"sign": true, "signs": true, "song": true, "songs": true, "lyrics": true,
	"karaoke": true, "dub": true, "dubbed": true, "commentary": true,
	"full": true, "complete": true, "main": true, "default": true,
	"hearing": true, "impaired": true, "closed": true, "caption": true,
	"captions": true, "retail": true, "web": true, "webrip": true,
	"bluray": true, "bd": true, "dvd": true, "remux": true, "proper": true,
	"repack": true, "extended": true, "directors": true, "cut": true,
	"theatrical": true,
}

// externalLanguageAliases maps full language names to codes. Short BCP 47 tags
// are handled by language.Parse below.
var externalLanguageAliases = map[string]string{
	"afrikaans": "af", "albanian": "sq", "amharic": "am", "arabic": "ar",
	"armenian": "hy", "azerbaijani": "az", "basque": "eu", "belarusian": "be",
	"bengali": "bn", "bosnian": "bs", "bulgarian": "bg", "burmese": "my",
	"catalan": "ca", "chinese": "zh", "croatian": "hr", "czech": "cs",
	"danish": "da", "dutch": "nl", "english": "en", "esperanto": "eo",
	"estonian": "et", "farsi": "fa", "filipino": "fil", "finnish": "fi",
	"french": "fr", "galician": "gl", "georgian": "ka", "german": "de",
	"greek": "el", "gujarati": "gu", "hebrew": "he", "hindi": "hi",
	"hungarian": "hu", "icelandic": "is", "indonesian": "id", "irish": "ga",
	"italian": "it", "japanese": "ja", "kannada": "kn", "kazakh": "kk",
	"khmer": "km", "korean": "ko", "lao": "lo", "latin": "la",
	"latvian": "lv", "lithuanian": "lt", "macedonian": "mk", "malay": "ms",
	"malayalam": "ml", "mandarin": "zh", "marathi": "mr", "mongolian": "mn",
	"montenegrin": "cnr", "nepali": "ne", "norwegian": "no", "persian": "fa",
	"polish": "pl", "portuguese": "pt", "punjabi": "pa", "romanian": "ro",
	"russian": "ru", "serbian": "sr", "sinhala": "si", "slovak": "sk",
	"slovenian": "sl", "spanish": "es", "swahili": "sw", "swedish": "sv",
	"tagalog": "tl", "tamil": "ta", "telugu": "te", "thai": "th",
	"turkish": "tr", "ukrainian": "uk", "urdu": "ur", "uzbek": "uz",
	"vietnamese": "vi", "welsh": "cy", "cantonese": "yue",
}

// knownExternalLanguageCodes limits three-letter guesses to languages this app
// already understands. Without it x/text accepts obscure ISO 639-3 codes and
// filename words such as "the" or "and" would masquerade as languages.
var knownExternalLanguageCodes = func() map[string]bool {
	m := map[string]bool{}
	add := func(code string) {
		tag, err := language.Parse(code)
		if err != nil {
			return
		}
		base, conf := tag.Base()
		if conf == language.No {
			return
		}
		iso3 := base.ISO3()
		if iso3 == "" || iso3 == "und" {
			return
		}
		m[iso3] = true
		if b, ok := isoTtoB[iso3]; ok {
			m[b] = true
		}
	}
	for _, code := range externalLanguageAliases {
		add(code)
	}
	for b, t := range isoBtoT {
		m[b], m[t] = true, true
	}
	return m
}()

// detectExternalSubtitles finds sidecar subtitle files belonging to video and
// turns them into Stream values the language picker can offer. Only files
// sharing the video's base name (e.g. Film.en.srt for Film.mkv) are included.
func detectExternalSubtitles(video string) []Stream {
	video = filepath.Clean(video)
	videoStem := strings.TrimSuffix(filepath.Base(video), filepath.Ext(video))
	if videoStem == "" {
		return nil
	}

	dirs := []string{filepath.Dir(video)}
	for _, name := range externalSubtitleDirs {
		dirs = append(dirs, filepath.Join(filepath.Dir(video), name))
	}

	seen := map[string]bool{}
	var paths []string
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			ext := strings.ToLower(filepath.Ext(entry.Name()))
			if !externalSubtitleExts[ext] {
				continue
			}
			stem := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
			if !isVideoSidecar(videoStem, stem) {
				continue
			}
			p := filepath.Join(dir, entry.Name())
			if seen[p] {
				continue
			}
			seen[p] = true
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)

	streams := make([]Stream, 0, len(paths))
	for i, p := range paths {
		ext := strings.ToLower(filepath.Ext(p))
		fileName := filepath.Base(p)
		stem := strings.TrimSuffix(fileName, filepath.Ext(fileName))
		lang, title := externalSubtitleName(videoStem, stem)
		tags := map[string]string{}
		if lang != "" {
			tags["language"] = lang
		}
		if title != "" {
			tags["title"] = title
		}
		streams = append(streams, Stream{
			Index:     externalStreamIndexBase + i,
			CodecName: strings.TrimPrefix(ext, "."),
			CodecType: "subtitle",
			Tags:      tags,
			External:  true,
			Path:      p,
			FileName:  fileName,
		})
	}
	return streams
}

// isVideoSidecar reports whether fileStem is the video stem itself or starts
// with it followed by a separator (".", "-", "_", space or an opening bracket).
func isVideoSidecar(videoStem, fileStem string) bool {
	v := []rune(strings.ToLower(videoStem))
	f := []rune(strings.ToLower(fileStem))
	if len(v) == 0 || len(f) < len(v) {
		return false
	}
	if string(f[:len(v)]) != string(v) {
		return false
	}
	if len(f) == len(v) {
		return true
	}
	return strings.ContainsRune(".-_ ([{", f[len(v)])
}

// externalSubtitleName derives a language code and qualifier title from the
// part of a sidecar filename that follows the video's base name.
func externalSubtitleName(videoStem, fileStem string) (lang, title string) {
	videoRunes, fileRunes := []rune(videoStem), []rune(fileStem)
	remainder := ""
	if len(fileRunes) >= len(videoRunes) && strings.EqualFold(string(fileRunes[:len(videoRunes)]), videoStem) {
		remainder = string(fileRunes[len(videoRunes):])
	}
	var extras []string
	for _, token := range strings.FieldsFunc(remainder, func(r rune) bool {
		return r == '.' || r == '_' || r == ' ' || r == '\t' ||
			r == '(' || r == ')' || r == '[' || r == ']' || r == '{' || r == '}'
	}) {
		token = strings.Trim(token, "-+")
		if token == "" {
			continue
		}
		if lang == "" {
			if code, ok := externalLanguageCode(token); ok {
				lang = code
				continue
			}
		} else if isLocaleSuffix(token) {
			// pt_BR / en-US / zh_Hans: the base language is enough for the picker.
			continue
		}
		extras = append(extras, token)
	}
	return lang, strings.Join(extras, " ")
}

func isLocaleSuffix(token string) bool {
	runes := []rune(token)
	if len(runes) == 2 {
		return unicode.IsUpper(runes[0]) && unicode.IsUpper(runes[1])
	}
	if len(runes) == 4 {
		return unicode.IsUpper(runes[0]) && unicode.IsLower(runes[1]) &&
			unicode.IsLower(runes[2]) && unicode.IsLower(runes[3])
	}
	return false
}

// externalLanguageCode recognises full names, two/three-letter codes and
// locale-like tags such as en-US. Unknown tokens are kept as title qualifiers.
func externalLanguageCode(token string) (string, bool) {
	t := strings.ToLower(strings.TrimSpace(token))
	if t == "" || externalNonLanguageTokens[t] {
		return "", false
	}
	if code, ok := externalLanguageAliases[t]; ok {
		return code, true
	}
	if len(t) == 3 && !knownExternalLanguageCodes[t] {
		return "", false
	}
	t = strings.ReplaceAll(t, "_", "-")
	tag, err := language.Parse(t)
	if err != nil {
		return "", false
	}
	base, conf := tag.Base()
	if conf == language.No {
		return "", false
	}
	code := strings.ToLower(base.String())
	if code == "" || code == "und" || code == "mul" || code == "zxx" {
		return "", false
	}
	return code, true
}

// parseExternalSubtitle reads a sidecar file into the same ASSDoc shape used
// for extracted streams.
func parseExternalSubtitle(path string) (*ASSDoc, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".ass", ".ssa":
		return parseASSFile(path)
	case ".srt":
		return srtToASSDoc(path)
	case ".vtt":
		return vttToASSDoc(path)
	default:
		return nil, fmt.Errorf("unsupported external subtitle format %q", filepath.Ext(path))
	}
}
