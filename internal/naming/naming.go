// Package naming produces filesystem- and Tachimanga-friendly names.
//
// Tachimanga (like Tachiyomi/Mihon local sources) imports a folder per series
// containing one archive per chapter, and recognises the chapter number from
// the file name. We therefore keep the series title as the folder and put a
// clearly-parseable, zero-padded chapter number in each CBZ name so chapters
// both sort correctly in Files.app and are recognised by the reader.
package naming

import (
	"fmt"
	"strconv"
	"strings"
)

// illegal characters for FAT/exFAT (iPad import via Files), plus a few extras.
var illegal = strings.NewReplacer(
	"/", "-", "\\", "-", ":", " -", "*", "", "?", "", "\"", "'",
	"<", "(", ">", ")", "|", "-", "\t", " ", "\n", " ", "\r", " ",
)

// reserved are Windows device names that can't be used as a file/folder name.
var reserved = map[string]bool{
	"con": true, "prn": true, "aux": true, "nul": true,
	"com1": true, "com2": true, "com3": true, "com4": true, "com5": true,
	"com6": true, "com7": true, "com8": true, "com9": true,
	"lpt1": true, "lpt2": true, "lpt3": true, "lpt4": true, "lpt5": true,
	"lpt6": true, "lpt7": true, "lpt8": true, "lpt9": true,
}

// Sanitize makes s safe to use as a single path component across macOS,
// Windows and the FAT/exFAT volumes used when importing to an iPad.
func Sanitize(s string) string {
	s = illegal.Replace(s)
	s = strings.Join(strings.Fields(s), " ") // collapse whitespace
	s = strings.Trim(s, " .")                // no leading/trailing dots/spaces
	// Truncate on a rune boundary so we never leave a half-written multibyte
	// character (common with CJK/Korean manhwa titles).
	if r := []rune(s); len(r) > 150 {
		s = strings.TrimSpace(string(r[:150]))
	}
	if s == "" {
		return "Untitled"
	}
	if reserved[strings.ToLower(s)] {
		s = "_" + s
	}
	return s
}

// PadNumber zero-pads the integer part of a chapter number to width digits,
// preserving any decimal part: "10.5" -> "0010.5", "7" -> "0007".
func PadNumber(number string, width int) string {
	number = strings.TrimSpace(number)
	if number == "" {
		return strings.Repeat("0", width)
	}
	intPart, frac := number, ""
	if i := strings.IndexByte(number, '.'); i >= 0 {
		intPart, frac = number[:i], number[i:]
	}
	if n, err := strconv.Atoi(intPart); err == nil {
		intPart = fmt.Sprintf("%0*d", width, n)
	}
	return intPart + frac
}

// ChapterFile returns the CBZ file name for a chapter. The format
// "<Series> - Chapter <padded><.title>.cbz" is parsed reliably by Tachimanga's
// chapter recogniser, which strips the series title and reads the number.
func ChapterFile(seriesTitle, number, chapterTitle string) string {
	name := fmt.Sprintf("%s - Chapter %s", Sanitize(seriesTitle), PadNumber(number, 4))
	if t := strings.TrimSpace(chapterTitle); t != "" {
		name += " - " + Sanitize(t)
	}
	return Sanitize(name) + ".cbz"
}

// PageName is the in-archive name for the i-th page (1-based), zero-padded so
// the reader displays pages in order.
func PageName(i int, ext string) string {
	if ext == "" {
		ext = ".jpg"
	}
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	return fmt.Sprintf("%04d%s", i, ext)
}
