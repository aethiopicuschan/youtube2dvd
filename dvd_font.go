package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/aethiopicuschan/video2dvd"
	"golang.org/x/image/font/sfnt"
)

// The DVD menu and subtitles are drawn with a Japanese font of the operating
// system when there is one. video2dvd's built-in fonts show ASCII letters in
// upper case and have no accented Latin letters, which subtitles in other
// languages need. Characters the font lacks are still drawn with the
// built-in fonts.

// systemFontCandidate is a font file to try, and the font to take from it.
type systemFontCandidate struct {
	// name is the file name. On Linux it is looked for in the font
	// directories; elsewhere it is a full path.
	name string
	// fullName starts the full name of the font to take from a collection,
	// such as "Noto Sans CJK JP"; empty takes the first font.
	fullName string
}

// systemFontCandidates lists the fonts to try on goos, best first. env reads
// an environment variable.
func systemFontCandidates(goos string, env func(string) string) []systemFontCandidate {
	switch goos {
	case "darwin":
		// Hiragino Sans, in a medium weight that stays legible on a
		// television.
		var candidates []systemFontCandidate
		for _, weight := range []string{"W5", "W4", "W6", "W3"} {
			candidates = append(candidates, systemFontCandidate{
				name:     "/System/Library/Fonts/ヒラギノ角ゴシック " + weight + ".ttc",
				fullName: "Hiragino Sans " + weight,
			})
		}
		return candidates
	case "windows":
		windir := env("WINDIR")
		if windir == "" {
			windir = `C:\Windows`
		}
		dir := windir + `\Fonts\`
		return []systemFontCandidate{
			{name: dir + "YuGothM.ttc", fullName: "Yu Gothic Medium"},
			{name: dir + "BIZ-UDGothicR.ttc", fullName: "BIZ UDGothic"},
			{name: dir + "meiryo.ttc", fullName: "Meiryo"},
			{name: dir + "msgothic.ttc", fullName: "MS Gothic"},
		}
	default:
		return []systemFontCandidate{
			{name: "NotoSansCJK-Medium.ttc", fullName: "Noto Sans CJK JP"},
			{name: "NotoSansCJK-Regular.ttc", fullName: "Noto Sans CJK JP"},
			{name: "NotoSansCJKjp-Medium.otf"},
			{name: "NotoSansCJKjp-Regular.otf"},
			{name: "NotoSansJP-Medium.otf"},
			{name: "NotoSansJP-Regular.otf"},
			{name: "ipaexg.ttf"},
			{name: "ipag.ttf"},
			{name: "TakaoGothic.ttf"},
			{name: "VL-Gothic-Regular.ttf"},
		}
	}
}

// linuxFontDirectories are the directories fontconfig reads by default.
func linuxFontDirectories(env func(string) string) []string {
	dirs := []string{"/usr/share/fonts", "/usr/local/share/fonts"}
	if dataHome := env("XDG_DATA_HOME"); dataHome != "" {
		dirs = append(dirs, filepath.Join(dataHome, "fonts"))
	}
	if home := env("HOME"); home != "" {
		dirs = append(dirs, filepath.Join(home, ".local", "share", "fonts"), filepath.Join(home, ".fonts"))
	}
	return dirs
}

// findFontFiles maps the file names of candidates to the paths where they
// are found below dirs; the first found wins.
func findFontFiles(dirs []string, candidates []systemFontCandidate) map[string]string {
	wanted := make(map[string]bool, len(candidates))
	for _, candidate := range candidates {
		wanted[candidate.name] = true
	}
	found := make(map[string]string)
	for _, dir := range dirs {
		_ = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				// An unreadable directory is skipped, not the whole walk.
				return nil
			}
			name := entry.Name()
			if !entry.IsDir() && wanted[name] && found[name] == "" {
				found[name] = path
			}
			return nil
		})
	}
	return found
}

// loadSystemMenuFont returns the first candidate font that can be read and
// has Japanese, and the path it came from. It returns nil when there is
// none, which leaves the text to the built-in fonts.
func loadSystemMenuFont(candidates []systemFontCandidate, locate func(name string) string) (*video2dvd.MenuFont, string) {
	for _, candidate := range candidates {
		path := locate(candidate.name)
		if path == "" {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		index, ok := japaneseFontIndex(data, candidate.fullName)
		if !ok {
			continue
		}
		font, err := video2dvd.ParseMenuFontIndex(data, index)
		if err != nil {
			continue
		}
		return font, path
	}
	return nil, ""
}

// japaneseFontIndex finds the font of a font file or collection whose full
// name starts with fullName (any, when empty) and that has kana, kanji and
// Latin letters.
func japaneseFontIndex(data []byte, fullName string) (index int, ok bool) {
	defer func() {
		// The font parser can index past the end of a damaged table.
		if recover() != nil {
			index, ok = 0, false
		}
	}()
	collection, err := sfnt.ParseCollection(data)
	if err != nil {
		return 0, false
	}
	var buffer sfnt.Buffer
	for i := range collection.NumFonts() {
		font, err := collection.Font(i)
		if err != nil {
			continue
		}
		if fullName != "" {
			name, err := font.Name(&buffer, sfnt.NameIDFull)
			if err != nil || !strings.HasPrefix(name, fullName) {
				continue
			}
		}
		covered := true
		for _, r := range "aあア漢" {
			if glyph, err := font.GlyphIndex(&buffer, r); err != nil || glyph == 0 {
				covered = false
				break
			}
		}
		if covered {
			return i, true
		}
	}
	return 0, false
}

// systemMenuFont finds the font of the operating system goos to draw the
// DVD menu and subtitles with, or nil.
func systemMenuFont(goos string, env func(string) string) *video2dvd.MenuFont {
	candidates := systemFontCandidates(goos, env)
	locate := func(name string) string { return name }
	if goos != "darwin" && goos != "windows" {
		found := findFontFiles(linuxFontDirectories(env), candidates)
		locate = func(name string) string { return found[name] }
	}
	font, _ := loadSystemMenuFont(candidates, locate)
	return font
}
