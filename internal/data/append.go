package data

import (
	"os"
	"time"
)

// LastCandleTime mengembalikan waktu candle TERAKHIR di cache CSV untuk
// (instrument, granularity). ok=false kalau file tidak ada / kosong (belum ada
// cache → caller harus bootstrap dari -from).
func LastCandleTime(baseDir, instrument, granularity string) (time.Time, bool, error) {
	path := CSVPath(baseDir, instrument, granularity)
	candles, err := ReadCSV(path)
	if err != nil {
		if os.IsNotExist(err) {
			return time.Time{}, false, nil
		}
		return time.Time{}, false, err
	}
	if len(candles) == 0 {
		return time.Time{}, false, nil
	}
	return candles[len(candles)-1].Time, true, nil
}

// AppendNew menambahkan candle `fresh` ke cache CSV (anti-duplikat). Kalau cache
// sudah ada, elemen fresh dengan Time <= candle terakhir existing dibuang lalu
// sisanya digabung dan ditulis ulang (WriteCSV). Kalau belum ada file →
// WriteCSV(fresh) langsung.
func AppendNew(baseDir, instrument, granularity string, fresh []Candle) error {
	path := CSVPath(baseDir, instrument, granularity)
	existing, err := ReadCSV(path)
	if err != nil {
		if os.IsNotExist(err) {
			return WriteCSV(path, fresh)
		}
		return err
	}
	if len(existing) == 0 {
		return WriteCSV(path, fresh)
	}

	last := existing[len(existing)-1].Time
	merged := existing
	for _, c := range fresh {
		if c.Time.After(last) {
			merged = append(merged, c)
		}
	}
	return WriteCSV(path, merged)
}
