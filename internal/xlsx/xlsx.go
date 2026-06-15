// Package xlsx adalah writer .xlsx minimal berbasis std-lib (archive/zip +
// encoding/xml + strconv). Sebuah file .xlsx pada dasarnya adalah arsip ZIP
// berisi beberapa berkas XML (OOXML / SpreadsheetML). Writer ini sengaja
// dibuat sesederhana mungkin: satu worksheet, sel teks memakai inline string
// (t="inlineStr") dan sel angka memakai <c><v>..</v></c>, tanpa sharedStrings.
// Tujuannya cukup untuk dibuka oleh Excel, LibreOffice, maupun Google Sheets.
package xlsx

import (
	"archive/zip"
	"os"
	"strconv"
	"strings"
)

// colName mengubah indeks kolom berbasis-0 menjadi nama kolom Excel.
// 0 -> "A", 25 -> "Z", 26 -> "AA", dst (bijective base-26).
func colName(i int) string {
	var sb strings.Builder
	// Bangun dari digit paling kanan, lalu balik.
	digits := make([]byte, 0, 4)
	for {
		rem := i % 26
		digits = append(digits, byte('A'+rem))
		i = i/26 - 1
		if i < 0 {
			break
		}
	}
	for j := len(digits) - 1; j >= 0; j-- {
		sb.WriteByte(digits[j])
	}
	return sb.String()
}

// escapeXML meng-escape karakter yang punya arti khusus di XML.
// Wajib untuk konten inline string agar dokumen tetap well-formed.
func escapeXML(s string) string {
	// Urutan penting: '&' harus diganti lebih dulu.
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	return s
}

// cellXML merender satu sel pada koordinat (col, row 1-based).
// Auto-detect: kalau value bisa di-parse sebagai float (dan tidak kosong),
// tulis sebagai sel ANGKA; selain itu sebagai inline string.
func cellXML(sb *strings.Builder, col, row int, value string) {
	ref := colName(col) + strconv.Itoa(row)
	if value != "" {
		if _, err := strconv.ParseFloat(value, 64); err == nil {
			// Sel numerik: nilai apa adanya di dalam <v>.
			sb.WriteString(`<c r="`)
			sb.WriteString(ref)
			sb.WriteString(`"><v>`)
			sb.WriteString(value)
			sb.WriteString(`</v></c>`)
			return
		}
	}
	// Sel teks (inline string).
	sb.WriteString(`<c r="`)
	sb.WriteString(ref)
	sb.WriteString(`" t="inlineStr"><is><t xml:space="preserve">`)
	sb.WriteString(escapeXML(value))
	sb.WriteString(`</t></is></c>`)
}

// rowXML merender satu <row r="..."> berisi sel-selnya.
func rowXML(sb *strings.Builder, rowNum int, cells []string) {
	sb.WriteString(`<row r="`)
	sb.WriteString(strconv.Itoa(rowNum))
	sb.WriteString(`">`)
	for c, v := range cells {
		cellXML(sb, c, rowNum, v)
	}
	sb.WriteString(`</row>`)
}

// sheetXML membangun isi xl/worksheets/sheet1.xml dari header + rows.
func sheetXML(header []string, rows [][]string) string {
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	sb.WriteString(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">`)
	sb.WriteString(`<sheetData>`)
	rowNum := 1
	if len(header) > 0 {
		rowXML(&sb, rowNum, header)
		rowNum++
	}
	for _, r := range rows {
		rowXML(&sb, rowNum, r)
		rowNum++
	}
	sb.WriteString(`</sheetData>`)
	sb.WriteString(`</worksheet>`)
	return sb.String()
}

// Berkas-berkas pendukung berukuran tetap (tidak tergantung data).

const contentTypesXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
	`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
	`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
	`<Default Extension="xml" ContentType="application/xml"/>` +
	`<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>` +
	`<Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>` +
	`</Types>`

const rootRelsXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
	`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
	`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>` +
	`</Relationships>`

const workbookRelsXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
	`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
	`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>` +
	`</Relationships>`

// workbookXML membangun xl/workbook.xml dengan satu sheet bernama sheetName.
func workbookXML(sheetName string) string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" ` +
		`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">` +
		`<sheets>` +
		`<sheet name="` + escapeXML(sheetName) + `" sheetId="1" r:id="rId1"/>` +
		`</sheets>` +
		`</workbook>`
}

// Write menulis satu sheet ke file .xlsx pada path.
// header = baris pertama; rows = data. Tiap sel di-AUTO-DETECT: jika string-nya
// dapat di-parse ke float (strconv.ParseFloat) maka ditulis sebagai sel ANGKA
// (bisa di-sort/sum di Excel), selain itu sebagai sel teks (inline string).
func Write(path, sheetName string, header []string, rows [][]string) error {
	if sheetName == "" {
		sheetName = "Sheet1"
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	// Pastikan file ditutup; utamakan error dari penulisan ZIP.
	defer f.Close()

	zw := zip.NewWriter(f)

	// Daftar entry (nama -> isi). Urutan penulisan tidak kritikal, tapi
	// kita tulis berurut agar deterministik.
	entries := []struct {
		name string
		data string
	}{
		{"[Content_Types].xml", contentTypesXML},
		{"_rels/.rels", rootRelsXML},
		{"xl/workbook.xml", workbookXML(sheetName)},
		{"xl/_rels/workbook.xml.rels", workbookRelsXML},
		{"xl/worksheets/sheet1.xml", sheetXML(header, rows)},
	}

	for _, e := range entries {
		w, err := zw.Create(e.name)
		if err != nil {
			_ = zw.Close()
			return err
		}
		if _, err := w.Write([]byte(e.data)); err != nil {
			_ = zw.Close()
			return err
		}
	}

	return zw.Close()
}
