// Package data adalah domain candle + penyimpanan ke disk (cache CSV).
package data

import "time"

// Candle adalah satu candlestick yang sudah ternormalisasi (harga float64,
// waktu UTC). Ini bentuk yang dipakai detector/engine nanti.
type Candle struct {
	Time   time.Time
	Open   float64
	High   float64
	Low    float64
	Close  float64
	Volume int64
}
