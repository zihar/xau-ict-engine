package detectors

import "testing"

func TestZigzagMinFiltersNoise(t *testing.T) {
	// Swing berselang: Low100, High101 (leg 1, kecil), Low99 (leg 2, kecil),
	// High130 (leg 31, besar), Low90 (leg 40, besar).
	sw := []Swing{
		{Price: 100, Kind: SwingLow},
		{Price: 101, Kind: SwingHigh},
		{Price: 99, Kind: SwingLow},
		{Price: 130, Kind: SwingHigh},
		{Price: 90, Kind: SwingLow},
	}
	// minLeg=0 → identik Zigzag (semua turning point kebawa).
	if got := len(ZigzagMin(sw, 0)); got != len(Zigzag(sw)) {
		t.Errorf("ZigzagMin(0) len=%d != Zigzag len=%d", got, len(Zigzag(sw)))
	}
	// minLeg=5 → High101 (leg 1) ditolak; Low99 meng-update pivot low ke ekstrem
	// terendah (99 < 100). Sisa: 99(low) → 130(high) → 90(low).
	z := ZigzagMin(sw, 5)
	if len(z) != 3 {
		t.Fatalf("ZigzagMin(5) len=%d, mau 3 (%+v)", len(z), z)
	}
	if z[0].Price != 99 || z[1].Price != 130 || z[2].Price != 90 {
		t.Errorf("pivot tersaring salah: %+v (mau 99,130,90)", z)
	}
}

func TestZigzagMinKeepsExtreme(t *testing.T) {
	// Dua high berurutan setelah filter: pivot high harus update ke yang tertinggi.
	sw := []Swing{
		{Price: 100, Kind: SwingLow},
		{Price: 120, Kind: SwingHigh},
		{Price: 125, Kind: SwingHigh}, // sejenis, lebih ekstrem → update
		{Price: 90, Kind: SwingLow},
	}
	z := ZigzagMin(sw, 5)
	if len(z) != 3 || z[1].Price != 125 {
		t.Errorf("pivot high harus 125 (ekstrem), dapat %+v", z)
	}
}
