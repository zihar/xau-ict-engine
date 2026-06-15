package xlsx

import (
	"archive/zip"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

func TestColName(t *testing.T) {
	cases := map[int]string{
		0:   "A",
		25:  "Z",
		26:  "AA",
		27:  "AB",
		51:  "AZ",
		52:  "BA",
		701: "ZZ",
		702: "AAA",
	}
	for in, want := range cases {
		if got := colName(in); got != want {
			t.Errorf("colName(%d) = %q, mau %q", in, got, want)
		}
	}
}

// readEntry membuka arsip xlsx & mengembalikan isi entry bernama name.
func readEntry(t *testing.T, path, name string) string {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("buka zip: %v", err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				t.Fatalf("buka entry %s: %v", name, err)
			}
			defer rc.Close()
			b, err := io.ReadAll(rc)
			if err != nil {
				t.Fatalf("baca entry %s: %v", name, err)
			}
			return string(b)
		}
	}
	t.Fatalf("entry %q tidak ditemukan di %s", name, path)
	return ""
}

func TestWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.xlsx")

	header := []string{"name", "price", "note"}
	rows := [][]string{
		{"emas", "1926.77", "buy"},
		{"perak", "23.5", "teks biasa"},
	}
	if err := Write(path, "trades", header, rows); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Entry wajib ada.
	for _, name := range []string{
		"[Content_Types].xml",
		"_rels/.rels",
		"xl/workbook.xml",
		"xl/_rels/workbook.xml.rels",
		"xl/worksheets/sheet1.xml",
	} {
		_ = readEntry(t, path, name) // akan Fatal kalau hilang
	}

	// Nama sheet muncul di workbook.
	wb := readEntry(t, path, "xl/workbook.xml")
	if !strings.Contains(wb, `name="trades"`) {
		t.Errorf("workbook.xml tidak memuat nama sheet: %s", wb)
	}

	sheet := readEntry(t, path, "xl/worksheets/sheet1.xml")

	// Teks ditulis sebagai inline string.
	if !strings.Contains(sheet, "emas") || !strings.Contains(sheet, "teks biasa") {
		t.Errorf("sheet tidak memuat nilai teks: %s", sheet)
	}
	// Angka ditulis sebagai <v>..</v> (bukan inlineStr).
	if !strings.Contains(sheet, "<v>1926.77</v>") {
		t.Errorf("sheet tidak memuat sel angka 1926.77: %s", sheet)
	}
	// Header juga sel teks.
	if !strings.Contains(sheet, "price") {
		t.Errorf("sheet tidak memuat header: %s", sheet)
	}
}

func TestWriteEscape(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "esc.xlsx")

	if err := Write(path, "s", []string{"col"}, [][]string{{`<a>&"b"`}}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	sheet := readEntry(t, path, "xl/worksheets/sheet1.xml")

	// Karakter spesial harus ter-encode.
	if !strings.Contains(sheet, "&lt;a&gt;&amp;&quot;b&quot;") {
		t.Errorf("escape XML salah, sheet: %s", sheet)
	}
	// Tidak boleh ada '<a>' mentah di luar tag XML.
	if strings.Contains(sheet, "<a>") {
		t.Errorf("konten mentah '<a>' bocor ke XML: %s", sheet)
	}
}
