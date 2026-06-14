package detectors

// Direction arah impulse / bias.
type Direction int

const (
	Bullish Direction = iota // impulse naik: swing low -> swing high
	Bearish                  // impulse turun: swing high -> swing low
)

func (d Direction) String() string {
	if d == Bearish {
		return "bearish"
	}
	return "bullish"
}

// Impulse = "last impulsive move" (Section A.1): leg dari Start ke End tanpa
// sub-swing counter-trend valid di tengah. Start = anchor_start, End = anchor_end.
type Impulse struct {
	Start Swing
	End   Swing
	Dir   Direction
}

// FindLastImpulse mengembalikan impulse ter-validasi paling baru searah dir.
// "Ter-validasi" = anchor_end (ekstrem) sudah dikonfirmasi swing counter-trend
// SESUDAHNYA (Section A.1: tanpa retracement, ekstrem belum jadi swing valid →
// anchor tetap pakai swing sebelumnya). Input z = hasil Zigzag.
func FindLastImpulse(z []Swing, dir Direction) (Impulse, bool) {
	// j = index End; butuh z[j+1] ada (konfirmasi counter-trend) → j <= len-2.
	for j := len(z) - 2; j >= 1; j-- {
		switch dir {
		case Bullish:
			if z[j].Kind == SwingHigh && z[j-1].Kind == SwingLow {
				return Impulse{Start: z[j-1], End: z[j], Dir: Bullish}, true
			}
		case Bearish:
			if z[j].Kind == SwingLow && z[j-1].Kind == SwingHigh {
				return Impulse{Start: z[j-1], End: z[j], Dir: Bearish}, true
			}
		}
	}
	return Impulse{}, false
}

// Zone klasifikasi premium/discount relatif equilibrium (level 0.5).
type Zone int

const (
	ZoneDiscount    Zone = iota // < 0.5 — untuk buy
	ZoneEquilibrium             // == 0.5
	ZonePremium                 // > 0.5 — untuk sell
)

func (z Zone) String() string {
	switch z {
	case ZoneDiscount:
		return "discount"
	case ZonePremium:
		return "premium"
	default:
		return "equilibrium"
	}
}

// Fib = level Fibonacci dari sebuah impulse (Low..High ekstrem leg).
type Fib struct {
	Low  float64
	High float64
}

// NewFib membentuk Fib dari impulse (Low/High = ekstrem leg, lepas dari arah).
func NewFib(imp Impulse) Fib {
	if imp.Dir == Bullish {
		return Fib{Low: imp.Start.Price, High: imp.End.Price}
	}
	return Fib{Low: imp.End.Price, High: imp.Start.Price}
}

// Level mengembalikan harga pada rasio r (0 = Low, 1 = High).
func (f Fib) Level(r float64) float64 { return f.Low + r*(f.High-f.Low) }

// Equilibrium = level 0.5 (garis tengah premium/discount).
func (f Fib) Equilibrium() float64 { return f.Level(0.5) }

// Defined true kalau Fib punya leg nyata (Low != High). Fib zero-value
// (Fib{}) dianggap "tak ada zona" → dipakai SelectPOI* untuk melewati filter
// premium/discount saat gate Fib di-OFF (FibZoneGate=false).
func (f Fib) Defined() bool { return f.Low != f.High }

// Zone klasifikasi harga terhadap equilibrium (Section F.2/F.3).
func (f Fib) Zone(price float64) Zone {
	eq := f.Equilibrium()
	switch {
	case price < eq:
		return ZoneDiscount
	case price > eq:
		return ZonePremium
	default:
		return ZoneEquilibrium
	}
}
