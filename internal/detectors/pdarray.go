package detectors

import (
	"sort"
	"time"

	"xau-ict-engine/internal/data"
)

// TFKind = timeframe asal sebuah POI (POI fractal multi-TF). Urutan = prioritas:
// W > D > H4 > H1 ("agenda besar" di TF tinggi didahulukan).
type TFKind int

const (
	TFH1 TFKind = iota
	TFH4
	TFD
	TFW
)

func (t TFKind) String() string {
	switch t {
	case TFW:
		return "W"
	case TFD:
		return "D"
	case TFH4:
		return "H4"
	default:
		return "H1"
	}
}

// PDRKind = jenis komponen PD Array (Section F.1).
type PDRKind int

const (
	KindFVG      PDRKind = iota // Tier 4
	KindVI                      // Tier 1 (Volume Imbalance, Kunci #4)
	KindOB                      // Tier 5 (Order Block)
	KindBB                      // Tier 3 (Breaker Block — wajib ber-FVG)
	KindBPR                     // Tier 3 (Balanced Price Range = BISI+SIBI overlap)
	KindIFVG                    // Tier 4 (Inversion FVG — FVG gagal, flip arah lawan)
	KindFVGBreak                // Tier 2 (Kunci #1 — FVG adjacent swing-high/low yg baru di-break)
)

func (k PDRKind) String() string {
	switch k {
	case KindFVG:
		return "FVG"
	case KindVI:
		return "VI"
	case KindOB:
		return "OB"
	case KindBB:
		return "BB"
	case KindBPR:
		return "BPR"
	case KindIFVG:
		return "IFVG"
	default:
		return "FVGBreak"
	}
}

func tierOf(k PDRKind) int {
	switch k {
	case KindVI:
		return 1
	case KindFVGBreak:
		return 2 // Kunci #1: FVG @ swing-break
	case KindBB, KindBPR:
		return 3
	case KindFVG, KindIFVG:
		return 4
	default: // OB
		return 5
	}
}

// lessPDR = TOTAL order deterministik untuk mengurutkan pool PDR. Sebelumnya sort
// hanya pakai key Bottom (sort.Slice non-stable) → urutan elemen ber-tie bergantung
// urutan input. Karena pool DISORTIR saat MASIH berisi OB lalu OB dibuang BELAKANGAN
// (dropOB), perubahan himpunan OB (mis. proxy↔strict) menggeser tie non-OB → cluster
// BuildPOIs ikut bergeser (drift). Total order (Bottom→Top→Kind→Dir→Index) bikin
// urutan non-OB independen dari himpunan OB → deterministik & parity bersih.
func lessPDR(a, b PDR) bool {
	if a.Bottom != b.Bottom {
		return a.Bottom < b.Bottom
	}
	if a.Top != b.Top {
		return a.Top < b.Top
	}
	if a.Kind != b.Kind {
		return a.Kind < b.Kind
	}
	if a.Dir != b.Dir {
		return a.Dir < b.Dir
	}
	return a.Index < b.Index
}

// PDR = satu zona price-delivery (range harga + arah).
type PDR struct {
	Kind   PDRKind
	Dir    Direction
	Top    float64
	Bottom float64
	Index  int       // candle acuan
	Time   time.Time // waktu candle pembentuk (diisi DetectPDRs; berguna lintas-TF)
}

// DetectPDRs mengumpulkan komponen PD Array (FVG + VI + OB + BB + BPR + IFVG +
// FVG@swing-break) dari deret candle.
//
// minGapPips = fvg_min_gap_pips_gold (threshold FVG & BB-FVG & BPR & IFVG).
// viMinGapPips = threshold KHUSUS Volume Imbalance (Bug#5: di-set lebih besar dari
// FVG supaya VI tidak murah & tidak banjir). viMinGapPips<=0 → pakai minGapPips
// (perilaku lama).
//
// fvgSwingBreakAdj = jarak adjacency (candle) FVG ke swing yg baru di-break untuk
// promosi ke Kunci #1 / Tier-2 (Config.FVGSwingBreakAdjacency, default 3).
// bprMaxDist = jarak maksimum (candle) antara BISI & SIBI untuk membentuk BPR
// (Config.BPRMaxDistanceCandles, default 5). Keduanya <=0 → pakai default internal.
//
// CATATAN tier: FVG yg lolos Kunci #1 di-EMIT sebagai KindFVGBreak (Tier-2) DAN
// tetap di-emit sebagai KindFVG (Tier-4) supaya: (a) cluster yg mengandungnya
// terangkat ke Tier-2 lewat clusterTier; (b) FVG biasa di lokasi sama tetap punya
// representasi Tier-4 kalau tak ber-cluster dengan komponen lain (tier lain tak rusak).
func DetectPDRs(candles []data.Candle, minGapPips, viMinGapPips, fvgSwingBreakAdj, bprMaxDist float64, ifvgRequireNoSameDirFVG, obStrict, bprDirectional, bbRequireDisplacement, fvgBreakGeometric bool) []PDR {
	if viMinGapPips <= 0 {
		viMinGapPips = minGapPips
	}
	fvgs := DetectFVGs(candles, minGapPips)
	var out []PDR
	for _, f := range fvgs {
		out = append(out, PDR{Kind: KindFVG, Dir: f.Dir, Top: f.Top, Bottom: f.Bottom, Index: f.Index})
	}
	for _, v := range DetectVolumeImbalances(candles, viMinGapPips) {
		out = append(out, PDR{Kind: KindVI, Dir: v.Dir, Top: v.Top, Bottom: v.Bottom, Index: v.Index})
	}
	if obStrict {
		out = append(out, detectOBsStrict(candles, minGapPips)...)
	} else {
		out = append(out, detectOBs(candles)...)
	}
	out = append(out, detectBBs(candles, minGapPips, bbRequireDisplacement)...)
	// (A) Kunci #1 — FVG @ swing-break → Tier-2.
	out = append(out, detectFVGSwingBreaks(candles, fvgs, fvgSwingBreakAdj, fvgBreakGeometric)...)
	// (B) BPR (Tier-3) — BISI + SIBI overlap dalam <= bprMaxDist candle.
	out = append(out, detectBPRs(candles, fvgs, bprMaxDist, bprDirectional)...)
	// (C) IFVG (Tier-4) — FVG ter-mitigasi flip jadi PD array arah lawan.
	out = append(out, detectIFVGs(candles, fvgs, ifvgRequireNoSameDirFVG)...)
	// Isi waktu candle pembentuk (berguna untuk POI fractal lintas-TF & narasi).
	for k := range out {
		if out[k].Index >= 0 && out[k].Index < len(candles) {
			out[k].Time = candles[out[k].Index].Time
		}
	}
	sort.Slice(out, func(i, j int) bool { return lessPDR(out[i], out[j]) })
	return out
}

// FilterLivePDRs membuang PDR yang sudah ter-mitigasi / di-break harga setelah
// terbentuk (Section F: POI yang sudah ditembus = INVALID). Break dihitung dari
// WICK, bukan cuma body close (keputusan user 2026-06-01): PDR bullish (support)
// invalid kalau ADA candle SETELAH pembentukannya yang Low menembus di bawah
// Bottom; PDR bearish (resistance) invalid kalau High menembus di atas Top.
// KECUALI KindIFVG — IFVG justru LAHIR dari break FVG, jadi tak ikut aturan ini.
func FilterLivePDRs(candles []data.Candle, pdrs []PDR, wickBreak bool) []PDR {
	out := make([]PDR, 0, len(pdrs))
	for _, p := range pdrs {
		if p.Kind == KindIFVG || !pdrBroken(candles, p, wickBreak) {
			out = append(out, p)
		}
	}
	return out
}

// pdrBroken true kalau ada candle setelah PDR.Index yang menembus zona ke arah
// pembalik. wickBreak=true → pakai Low/High (wick cukup, lebih ketat); false →
// pakai Close (body). Bullish: break ke bawah Bottom; Bearish: break ke atas Top.
func pdrBroken(candles []data.Candle, p PDR, wickBreak bool) bool {
	for i := p.Index + 1; i < len(candles); i++ {
		c := candles[i]
		if p.Dir == Bullish {
			lvl := c.Close
			if wickBreak {
				lvl = c.Low
			}
			if lvl < p.Bottom {
				return true
			}
		} else {
			lvl := c.Close
			if wickBreak {
				lvl = c.High
			}
			if lvl > p.Top {
				return true
			}
		}
	}
	return false
}

// detectOBs (Tier 5): order block = candle berlawanan arah TERAKHIR sebelum
// impulse (pakai pergerakan riil, bukan candle ujung versi YouTube — F.1 warning).
// Bullish OB = candle bearish yg langsung diikuti candle yg close > high-nya.
func detectOBs(candles []data.Candle) []PDR {
	var out []PDR
	for i := 0; i < len(candles)-1; i++ {
		c, n := candles[i], candles[i+1]
		bearish := c.Close < c.Open
		bullish := c.Close > c.Open
		if bearish && n.Close > c.High { // displacement ke atas → bullish OB
			out = append(out, PDR{Kind: KindOB, Dir: Bullish, Top: c.High, Bottom: c.Low, Index: i})
		}
		if bullish && n.Close < c.Low { // displacement ke bawah → bearish OB
			out = append(out, PDR{Kind: KindOB, Dir: Bearish, Top: c.High, Bottom: c.Low, Index: i})
		}
	}
	return out
}

// detectOBsStrict (pertemuan 4 — Config.OBStrict): OB = komponen REVERSAL asli,
// BUKAN proxy "1 candle close menembus high" seperti detectOBs lama. Tiga syarat
// inti materi:
//  1. Displacement impulsif ber-FVG: kita iterasi tiap FVG (bukti move impulsif),
//     bukan sekadar candle berikutnya close lewat high.
//  2. Candle lawan-arah TERAKHIR tepat sebelum kaki FVG (skeleton detectBBs) —
//     menjawab warning "jangan pakai candle paling ujung / wick ekstrem".
//  3. Liquidity sweep sebelum reversal (pembeda Stop Hunt vs OB): old swing low
//     (bullish) / swing high (bearish) sebelum OB harus DI-SWEEP. Tanpa sweep =
//     stop hunt / jebakan, BUKAN OB.
//
// Zona = full candle (Top=High, Bottom=Low) — konsisten detectBBs & invalidasi
// pdrBroken (warning "wick ekstrem" itu soal SELEKSI candle, sudah dijawab #2).
func detectOBsStrict(candles []data.Candle, minGapPips float64) []PDR {
	const adjacency = 3 // sama dgn detectBBs: candle lawan-arah dalam <=3 candle sebelum kaki FVG
	swings := DetectSwings(candles)
	var out []PDR
	for _, f := range DetectFVGs(candles, minGapPips) {
		// (2) candle lawan-arah TERAKHIR sebelum kaki FVG (kaki kiri = f.Index-1).
		obIdx := -1
		for k := f.Index - 1; k >= 0 && k >= f.Index-1-adjacency; k-- {
			c := candles[k]
			if f.Dir == Bullish && c.Close < c.Open { // bearish terakhir sblm impulse naik
				obIdx = k
				break
			}
			if f.Dir == Bearish && c.Close > c.Open {
				obIdx = k
				break
			}
		}
		if obIdx < 0 {
			continue
		}
		// (3) liquidity sweep SEBELUM reversal.
		if !obSweptLiquidity(candles, swings, f.Dir, obIdx, f.Index+1) {
			continue
		}
		out = append(out, PDR{Kind: KindOB, Dir: f.Dir, Top: candles[obIdx].High, Bottom: candles[obIdx].Low, Index: obIdx})
	}
	return out
}

// obSweptLiquidity: bullish OB → ambil swing-LOW terakhir SEBELUM obIdx; reversal
// sah hanya kalau low itu DI-SWEEP (ada candle dgn Low < swing.Price) dalam window
// [0..endIdx]. Bearish mirror (swing-HIGH, High > level). Wick dihitung (sweep
// stop-hunt khas lewat wick). False kalau tak ada swing acuan.
func obSweptLiquidity(candles []data.Candle, swings []Swing, dir Direction, obIdx, endIdx int) bool {
	want := SwingLow
	if dir == Bearish {
		want = SwingHigh
	}
	var lvl float64
	ok := false
	for _, s := range swings {
		if s.Kind != want || s.Index >= obIdx {
			continue
		}
		lvl, ok = s.Price, true // terus update → swing sejenis terdekat ke obIdx
	}
	if !ok {
		return false
	}
	for i := 0; i <= endIdx && i < len(candles); i++ {
		if dir == Bullish && candles[i].Low < lvl {
			return true
		}
		if dir == Bearish && candles[i].High > lvl {
			return true
		}
	}
	return false
}

// detectBBs (Tier 3, pendekatan): breaker = zona retracement (candle lawan OF)
// yg langsung diikuti impulse ber-FVG searah OF dalam <=3 candle
// (bb_validity_requires_impulse_fvg, bb_fvg_adjacency_max_candles: 3). Di sini
// di-couple ke FVG: zona BB = candle lawan-arah tepat sebelum kaki FVG.
//
// requireDisplacement (BBRequireDisplacement, uji user 2026-06-09): kalau true, FVG
// penyah harus DISPLACEMENT yang KELUAR dari range candle BB — bullish: f.Top > c.High;
// bearish: f.Bottom < c.Low. Gap yg masih nyangkut total di dalam range BB = bukan
// impulsive → retracement itu bukan BB valid.
func detectBBs(candles []data.Candle, minGapPips float64, requireDisplacement bool) []PDR {
	const adjacency = 3
	var out []PDR
	for _, f := range DetectFVGs(candles, minGapPips) {
		// kaki FVG mulai di f.Index-1; cari candle retracement (lawan arah) di <=3 candle sebelumnya.
		for k := f.Index - 1; k >= 0 && k >= f.Index-1-adjacency; k-- {
			c := candles[k]
			if f.Dir == Bullish && c.Close < c.Open { // retrace bearish sebelum impulse bullish
				if requireDisplacement && f.Top <= c.High { // gap tak keluar di atas range BB → bukan displacement
					break
				}
				out = append(out, PDR{Kind: KindBB, Dir: Bullish, Top: c.High, Bottom: c.Low, Index: k})
				break
			}
			if f.Dir == Bearish && c.Close > c.Open {
				if requireDisplacement && f.Bottom >= c.Low { // gap tak keluar di bawah range BB → bukan displacement
					break
				}
				out = append(out, PDR{Kind: KindBB, Dir: Bearish, Top: c.High, Bottom: c.Low, Index: k})
				break
			}
		}
	}
	return out
}

// POI = cluster PDR yang range-nya beririsan (strict overlap) — minimal
// confluence komponen (Section F.4). Zone = irisan (overlap region).
type POI struct {
	Dir        Direction
	TF         TFKind  // timeframe asal POI (fractal multi-TF); zero value = TFH1
	Top        float64 // batas atas irisan
	Bottom     float64 // batas bawah irisan
	Tier       int     // tier tertinggi (angka terkecil) di cluster
	Components []PDR
}

func (p POI) Confluence() int         { return len(p.Components) }
func (p POI) Mid() float64            { return (p.Top + p.Bottom) / 2 }
func (p POI) Contains(x float64) bool { return x >= p.Bottom && x <= p.Top }

// BuildPOIs mengelompokkan PDR searah yang beririsan STRICT (F-Q4: tanpa
// toleransi gap) jadi cluster, lalu simpan yang punya >= confluenceMin komponen.
// Tier cluster: VI→1; BB+FVG→2 (bread-and-butter); selain itu = tier tertinggi.
//
// maxWidthPrice = CAP lebar irisan cluster (Bug#5: cluster chain transitif bisa
// serap belasan PDR berjauhan → POI band terlalu lebar & tier inflasi). JANGAN
// gabung PDR baru kalau lebar irisan jadi > maxWidthPrice. <=0 → tanpa cap
// (perilaku lama). Engine kirim fraksi ATR_H1 (mis. 0.5×ATR).
func BuildPOIs(pdrs []PDR, confluenceMin int, maxWidthPrice float64) []POI {
	var out []POI
	for _, dir := range []Direction{Bullish, Bearish} {
		var ds []PDR
		for _, p := range pdrs {
			// IFVG tidak boleh jadi confluence di cluster searah bias (user
			// 2026-06-01): IFVG cuma transform untuk arah LAWAN FVG aslinya,
			// bukan padding cluster. Tetap di-detect (penanda), tapi tak meng-cluster.
			if p.Kind == KindIFVG {
				continue
			}
			if p.Dir == dir {
				ds = append(ds, p)
			}
		}
		sort.Slice(ds, func(i, j int) bool { return lessPDR(ds[i], ds[j]) })

		i := 0
		for i < len(ds) {
			lo, hi := ds[i].Bottom, ds[i].Top
			cluster := []PDR{ds[i]}
			j := i + 1
			for j < len(ds) {
				nlo, nhi := maxf(lo, ds[j].Bottom), minf(hi, ds[j].Top)
				if nlo > nhi { // tidak lagi beririsan
					break
				}
				// Cap lebar: tolak penggabungan kalau irisan cluster jadi terlalu lebar.
				if maxWidthPrice > 0 && (nhi-nlo) > maxWidthPrice {
					break
				}
				lo, hi = nlo, nhi
				cluster = append(cluster, ds[j])
				j++
			}
			if len(cluster) >= confluenceMin {
				out = append(out, POI{Dir: dir, Top: hi, Bottom: lo, Tier: clusterTier(cluster), Components: cluster})
			}
			if j == i+1 {
				i++
			} else {
				i = j
			}
		}
	}
	return out
}

// clusterTier menentukan tier cluster sesuai prioritas F.1, TAPI dengan guard
// Bug#5: VI (Tier 1) TIDAK otomatis menjadikan cluster Tier-1 hanya karena ADA 1
// VI. Spec F.4: "VI count sebagai 1 component, butuh >=1 lain untuk reach
// confluence". Jadi Tier-1 hanya kalau VI + konfluen bermakna:
//   - cluster >=3 komponen, ATAU
//   - VI bersanding dengan BB atau FVG (komponen struktural)
//
// Tanpa konfluen bermakna → VI di-treat sebagai Tier-2 (tetap kuat, tapi tak
// auto-mengalahkan BB+FVG asli). Sisa: BB+FVG bersebelahan = Tier 2.
func clusterTier(cs []PDR) int {
	tier := 99
	hasVI, hasBB, hasFVG := false, false, false
	for _, c := range cs {
		// VI di-skip dari penentuan tier dasar; dinilai lewat guard di bawah.
		if c.Kind != KindVI {
			if t := tierOf(c.Kind); t < tier {
				tier = t
			}
		}
		switch c.Kind {
		case KindVI:
			hasVI = true
		case KindBB:
			hasBB = true
		case KindFVG:
			hasFVG = true
		}
	}
	if hasVI {
		// VI + konfluen bermakna → Tier 1; selain itu VI = setara Tier 2.
		if len(cs) >= 3 || hasBB || hasFVG {
			return 1
		}
		if tier > 2 {
			tier = 2
		}
	}
	if hasBB && hasFVG && tier > 2 {
		tier = 2 // BB+FVG bersebelahan = Tier 2
	}
	return tier
}

// SelectPOI memilih POI terbaik yang lagi di-tag harga `price`, di zone yang
// benar (Trapped Sellers filter F.3 + zone hierarki F.2): bullish→discount,
// bearish→premium. Prioritas: tier tertinggi → confluence terbanyak → terdekat
// ke harga (poi_selection: highest_tier_then_confluence). ofDir = arah bias.
func SelectPOI(pois []POI, price float64, fib Fib, ofDir Direction) (POI, bool) {
	var best POI
	found := false
	for _, p := range pois {
		if p.Dir != ofDir {
			continue
		}
		if !p.Contains(price) {
			continue
		}
		// Trapped Sellers + zone: buy hanya di discount, sell hanya di premium.
		// Gate dicek di HARGA yang lagi nge-tag (bukan tengah POI) — F.2/F.3.
		// Fib tak terdefinisi (gate Fib OFF) → lewati filter zona.
		if fib.Defined() {
			z := fib.Zone(price)
			if ofDir == Bullish && z == ZonePremium {
				continue
			}
			if ofDir == Bearish && z == ZoneDiscount {
				continue
			}
		}
		if !found || better(p, best, price) {
			best, found = p, true
		}
	}
	return best, found
}

// SelectPOITouched seperti SelectPOI tapi memakai WINDOW: POI valid kalau harga
// SEMPAT MENYENTUH zonanya dalam `recent` candle terakhir (bukan harus berada di
// dalam zona persis pada candle keputusan). Ini menutup "miss" saat konfirmasi
// entry 5m telat 1-2 candle dari sentuhan POI (funnel: 89.7% survivor mati di
// gate point-in-time). Zona premium/discount dicek di POI.Mid() (lokasi intrinsik
// POI). Belum-jebol: untuk buy, close terakhir tak boleh di BAWAH POI (support
// jebol); sell mirror. recent = candle H1 window (kronologis). ok=false kalau tak ada.
func SelectPOITouched(pois []POI, recent []data.Candle, fib Fib, ofDir Direction) (POI, bool) {
	if len(recent) == 0 {
		return POI{}, false
	}
	last := recent[len(recent)-1].Close
	var best POI
	found := false
	for _, p := range pois {
		if p.Dir != ofDir {
			continue
		}
		// Zona makro: POI harus di zona benar (buy=discount, sell=premium).
		// Fib tak terdefinisi (gate Fib OFF) → lewati filter zona.
		if fib.Defined() {
			z := fib.Zone(p.Mid())
			if ofDir == Bullish && z == ZonePremium {
				continue
			}
			if ofDir == Bearish && z == ZoneDiscount {
				continue
			}
		}
		// Belum-jebol: harga terakhir tak boleh menembus POI ke arah lawan.
		if ofDir == Bullish && last < p.Bottom {
			continue
		}
		if ofDir == Bearish && last > p.Top {
			continue
		}
		// Touched: ada candle di window yang [Low,High]-nya beririsan zona POI.
		touched := false
		for _, c := range recent {
			if c.Low <= p.Top && c.High >= p.Bottom {
				touched = true
				break
			}
		}
		if !touched {
			continue
		}
		if !found || better(p, best, last) {
			best, found = p, true
		}
	}
	return best, found
}

// NearestValidPOI memilih POI valid searah `ofDir` yang TERDEKAT ke `price`,
// dipakai saat TIDAK ada POI ter-tag/disentuh (narasi "tunggu harga ke zona
// mana"). Valid = arah cocok + zona benar (buy=discount, sell=premium, dicek di
// POI.Mid() seperti SelectPOITouched) + belum-jebol relatif `price`. "Terdekat"
// diukur dari jarak harga ke TEPI POI terdekat (0 kalau harga sudah di dalam).
// Pemilihan menghormati hierarki kualitas: kalau jarak SAMA, pakai better()
// (tier→confluence). ok=false kalau tak ada kandidat.
func NearestValidPOI(pois []POI, price float64, fib Fib, ofDir Direction) (POI, bool) {
	var best POI
	bestDist := 0.0
	found := false
	for _, p := range pois {
		if p.Dir != ofDir {
			continue
		}
		// Zona makro: POI harus di zona benar (buy=discount, sell=premium).
		z := fib.Zone(p.Mid())
		if ofDir == Bullish && z == ZonePremium {
			continue
		}
		if ofDir == Bearish && z == ZoneDiscount {
			continue
		}
		// Belum-jebol: harga belum menembus POI ke arah lawan setup.
		if ofDir == Bullish && price < p.Bottom {
			continue
		}
		if ofDir == Bearish && price > p.Top {
			continue
		}
		d := poiEdgeDistance(p, price)
		if !found || d < bestDist || (d == bestDist && better(p, best, price)) {
			best, bestDist, found = p, d, true
		}
	}
	return best, found
}

// poiEdgeDistance = jarak harga ke tepi POI terdekat (0 kalau di dalam zona).
func poiEdgeDistance(p POI, price float64) float64 {
	switch {
	case price > p.Top:
		return price - p.Top
	case price < p.Bottom:
		return p.Bottom - price
	default:
		return 0
	}
}

// FilterPOITier menyaring POI ke tier <= maxTier (tier 1=terbaik/VI). maxTier<=0
// → tanpa filter. Dipakai quality gate (buang POI tier rendah yang tak ber-edge).
func FilterPOITier(pois []POI, maxTier int) []POI {
	if maxTier <= 0 {
		return pois
	}
	out := pois[:0:0]
	for _, p := range pois {
		if p.Tier <= maxTier {
			out = append(out, p)
		}
	}
	return out
}

// better: p lebih bagus dari b? tier kecil > confluence besar > lebih dekat harga.
func better(p, b POI, price float64) bool {
	// Prioritas TF terbesar dulu (agenda besar W>D>H4>H1) — keputusan user 2026-06-01.
	if p.TF != b.TF {
		return p.TF > b.TF
	}
	if p.Tier != b.Tier {
		return p.Tier < b.Tier
	}
	if p.Confluence() != b.Confluence() {
		return p.Confluence() > b.Confluence()
	}
	return abs(p.Mid()-price) < abs(b.Mid()-price)
}

func maxf(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func minf(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
