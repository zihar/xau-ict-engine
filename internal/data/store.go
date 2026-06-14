package data

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// csvHeader adalah baris pertama setiap file cache.
var csvHeader = []string{"time", "open", "high", "low", "close", "volume"}

// CSVPath mengembalikan path file cache untuk (instrument, granularity),
// mis. <baseDir>/XAU_USD/H1.csv.
func CSVPath(baseDir, instrument, granularity string) string {
	return filepath.Join(baseDir, instrument, granularity+".csv")
}

// WriteCSV menulis (overwrite) seluruh candle ke file cache. Waktu ditulis
// dalam RFC3339 UTC; harga apa adanya (string Go default float).
//
// Penulisan ATOMIK: ke file temp di direktori sama lalu os.Rename — pembaca
// konkuren (narrate/backtest saat alertd refresh) tidak pernah melihat file
// terpotong; mereka dapat versi lama utuh atau versi baru utuh.
func WriteCSV(path string, candles []Candle) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}

	w := csv.NewWriter(f)
	writeAll := func() error {
		if err := w.Write(csvHeader); err != nil {
			return err
		}
		for _, c := range candles {
			rec := []string{
				c.Time.UTC().Format(time.RFC3339),
				strconv.FormatFloat(c.Open, 'f', -1, 64),
				strconv.FormatFloat(c.High, 'f', -1, 64),
				strconv.FormatFloat(c.Low, 'f', -1, 64),
				strconv.FormatFloat(c.Close, 'f', -1, 64),
				strconv.FormatInt(c.Volume, 10),
			}
			if err := w.Write(rec); err != nil {
				return err
			}
		}
		w.Flush()
		return w.Error()
	}

	if err := writeAll(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// ReadCSV membaca file cache kembali jadi []Candle. Berguna untuk engine nanti
// agar tidak perlu hit API tiap run.
func ReadCSV(path string) ([]Candle, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	rows, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}

	var out []Candle
	for i, row := range rows {
		if i == 0 {
			continue // skip header
		}
		if len(row) < 6 {
			return nil, fmt.Errorf("baris %d: kolom kurang (%d)", i+1, len(row))
		}
		t, err := time.Parse(time.RFC3339, row[0])
		if err != nil {
			return nil, fmt.Errorf("baris %d: parse waktu: %w", i+1, err)
		}
		open, _ := strconv.ParseFloat(row[1], 64)
		high, _ := strconv.ParseFloat(row[2], 64)
		low, _ := strconv.ParseFloat(row[3], 64)
		cl, _ := strconv.ParseFloat(row[4], 64)
		vol, _ := strconv.ParseInt(row[5], 10, 64)
		out = append(out, Candle{Time: t.UTC(), Open: open, High: high, Low: low, Close: cl, Volume: vol})
	}
	return out, nil
}
