package cbz

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"regexp"
	"time"
)

// Field is one ComicInfo.xml element to set. An empty Value removes it.
type Field struct {
	Name, Value string
}

// SetComicInfo sets fields in a chapter's ComicInfo.xml (creating the file if
// there is none) and reports whether anything changed. The XML is edited as
// text, so fields Tomo doesn't know about survive. Pages are copied as-is,
// the archive is only rewritten when a field actually changes, and the
// rewrite goes through a temp file so a failure never damages the original.
func SetComicInfo(path string, fields ...Field) (bool, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return false, err
	}
	defer zr.Close()

	var info []byte
	for _, f := range zr.File {
		if f.Name == "ComicInfo.xml" {
			if info, err = readEntry(f); err != nil {
				return false, err
			}
		}
	}
	updated := info
	if len(updated) == 0 {
		updated = []byte(xml.Header + "<ComicInfo>\n</ComicInfo>")
	}
	for _, f := range fields {
		updated = setElement(updated, f.Name, f.Value)
	}
	// Nothing to do when no field changed, or when there's no ComicInfo.xml
	// and every field is being cleared.
	if bytes.Equal(updated, info) || (len(info) == 0 && allEmpty(fields)) {
		return false, nil
	}
	return true, rewrite(path, zr, func(zw *zip.Writer) error {
		for _, f := range zr.File {
			if f.Name == "ComicInfo.xml" {
				continue
			}
			if err := zw.Copy(f); err != nil {
				return err
			}
		}
		w, err := zw.CreateHeader(&zip.FileHeader{Name: "ComicInfo.xml", Method: zip.Deflate, Modified: time.Now()})
		if err != nil {
			return err
		}
		_, err = w.Write(updated)
		return err
	})
}

func allEmpty(fields []Field) bool {
	for _, f := range fields {
		if f.Value != "" {
			return false
		}
	}
	return true
}

// setElement sets <name>value</name> in a ComicInfo document: replacing the
// element's text if present, inserting it before </ComicInfo> if not, and
// removing it (with its line) when value is empty.
func setElement(x []byte, name, value string) []byte {
	re := regexp.MustCompile(`(?s)\n?[ \t]*<` + regexp.QuoteMeta(name) + `>.*?</` + regexp.QuoteMeta(name) + `>`)
	var esc bytes.Buffer
	xml.EscapeText(&esc, []byte(value))
	elem := "  <" + name + ">" + esc.String() + "</" + name + ">"
	if loc := re.FindIndex(x); loc != nil {
		if value == "" {
			return append(append([]byte{}, x[:loc[0]]...), x[loc[1]:]...)
		}
		return append(append(append([]byte{}, x[:loc[0]]...), "\n"+elem...), x[loc[1]:]...)
	}
	if value == "" {
		return x
	}
	if i := bytes.LastIndex(x, []byte("</ComicInfo>")); i >= 0 {
		return insertAt(x, i, elem+"\n")
	}
	return x
}
