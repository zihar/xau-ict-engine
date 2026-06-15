// Package engine menjalankan loop backtest end-to-end (pipeline Section N.2):
// iterasi candle 1H kronologis, refresh state multi-TF (W/D/H4/H1/M5,
// complete-only), evaluasi gate berantai (bias→daily align→QT/session/day type
// →POI→trigger entry 5m), emit Signal (signal layer selalu fire), lalu
// simulasikan manajemen posisi (Section K) → Result.
//
// Entry-point: Run(TFData, Config) (Result, error). Lihat engine.go (pipeline),
// sim.go (simulasi trade + sizing/weekly re-baseline I.1). Aproksimasi POC
// didokumentasikan di komentar engine.go.
package engine
