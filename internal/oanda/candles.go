package oanda

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"xau-ict-engine/internal/data"
)

// maxCount adalah batas candle per request OANDA.
const maxCount = 5000

// alignmentTimezone untuk dailyAlignment — sesuai anchor sesi 18:00 NY.
const alignmentTimezone = "America/New_York"

// FetchCandles menarik seluruh candle complete untuk instrument+granularity
// sejak `from` sampai `to`, dengan paginasi otomatis (max 5000 per request).
//
// dailyAlign (0-23) menetapkan jam anchor harian dalam alignmentTimezone;
// untuk anchor 18:00 NY pakai dailyAlign=18. Param ini berpengaruh ke
// granularity yang punya daily alignment (D dan intraday seperti H1/H4).
//
// Hanya candle dengan Complete==true yang dikembalikan (candle berjalan
// dibuang). Harga yang diambil adalah midpoint (price=M).
func (c *Client) FetchCandles(instrument, granularity string, from, to time.Time, dailyAlign int) ([]data.Candle, error) {
	var out []data.Candle
	cursor := from.UTC()

	for {
		q := url.Values{}
		q.Set("granularity", granularity)
		q.Set("price", "M")
		q.Set("count", strconv.Itoa(maxCount))
		q.Set("from", cursor.Format(time.RFC3339))
		q.Set("dailyAlignment", strconv.Itoa(dailyAlign))
		q.Set("alignmentTimezone", alignmentTimezone)

		path := "/v3/instruments/" + instrument + "/candles"
		body, err := c.get(path, q)
		if err != nil {
			return nil, err
		}

		var r candlesResponse
		if err := json.Unmarshal(body, &r); err != nil {
			return nil, err
		}
		if len(r.Candles) == 0 {
			break
		}

		var lastTime time.Time
		for _, rc := range r.Candles {
			t, err := time.Parse(time.RFC3339, rc.Time)
			if err != nil {
				return nil, fmt.Errorf("parse waktu candle %q: %w", rc.Time, err)
			}
			lastTime = t
			if !rc.Complete {
				continue // buang candle yang masih berjalan
			}
			if t.Before(cursor) {
				// OANDA mengikutkan candle yang MENGANDUNG `from`; saat paginasi
				// (cursor = lastTime+1s) itu = candle terakhir halaman sebelumnya
				// → skip supaya tidak dobel di batas 5000-candle.
				continue
			}
			if t.After(to) {
				continue // di luar rentang yang diminta
			}
			cdl, err := toCandle(t, rc)
			if err != nil {
				return nil, err
			}
			out = append(out, cdl)
		}

		// Stop kalau respons belum penuh (sudah mentok ujung data) atau
		// candle terakhir sudah melewati target `to`.
		if len(r.Candles) < maxCount || !lastTime.Before(to) {
			break
		}
		// Maju: mulai 1 detik setelah candle terakhir agar tidak dobel.
		cursor = lastTime.Add(time.Second)
	}

	return out, nil
}

// toCandle mengubah rawCandle (harga string, midpoint) jadi domain Candle.
func toCandle(t time.Time, rc rawCandle) (data.Candle, error) {
	o, err := strconv.ParseFloat(rc.Mid.O, 64)
	if err != nil {
		return data.Candle{}, fmt.Errorf("parse open %q: %w", rc.Mid.O, err)
	}
	h, err := strconv.ParseFloat(rc.Mid.H, 64)
	if err != nil {
		return data.Candle{}, fmt.Errorf("parse high %q: %w", rc.Mid.H, err)
	}
	l, err := strconv.ParseFloat(rc.Mid.L, 64)
	if err != nil {
		return data.Candle{}, fmt.Errorf("parse low %q: %w", rc.Mid.L, err)
	}
	cl, err := strconv.ParseFloat(rc.Mid.C, 64)
	if err != nil {
		return data.Candle{}, fmt.Errorf("parse close %q: %w", rc.Mid.C, err)
	}
	return data.Candle{
		Time:   t.UTC(),
		Open:   o,
		High:   h,
		Low:    l,
		Close:  cl,
		Volume: rc.Volume,
	}, nil
}
