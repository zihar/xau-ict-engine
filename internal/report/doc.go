// Package report menghitung metrik Section L dari engine.Result: ringkasan
// per-run (win rate, total R, profit factor, max drawdown R/%, max consecutive
// loss, avg R, Sharpe-like, net PnL, equity akhir), breakdown per dimensi
// (exit reason, ITH/ITL type, QT scenario, phase, regime, RR, day type, dll),
// perbandingan signal-layer vs executed (J.2), dan tulis per-trade record ke CSV.
//
// Entry-point: report.Print(w, res) dan report.WriteCSV(path, res).
package report
