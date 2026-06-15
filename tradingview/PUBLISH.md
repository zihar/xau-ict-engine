# Panduan Publish — Luna Trade Indicators

> Catatan: ini publikasi **open-source / public (gratis)**, dan ini **publish PERTAMA**
> (belum pernah terbit) → pakai alur **publikasi baru**, BUKAN "Update existing publication".
> Opsi "Update existing" baru relevan untuk rilis versi berikutnya (agar like/komentar tak hilang).

## Langkah (publish pertama)
1. Buka script di Pine Editor → **Save** (`⌘S`), pastikan tanpa error (warning `PINE_VERSION_OUTDATED` boleh diabaikan).
2. Klik **Publish script** (tombol di **kanan-atas Pine Editor**, bukan di dropdown nama script).
3. Dialog publikasi **baru** muncul → set visibility = **Open source (Public)**.
4. **Title** & **Description** di bawah → copy-paste.
5. Kategori/Tags saran: `Trend Analysis`, `Market Structure`, `Support and Resistance` → **Publish**.

---

## Title

```
Luna Trade Indicators
```

## Description

```
Luna Trade Indicators — deteksi struktur pasar otomatis: ITH/ITL (intermediate)
di timeframe intraday dan LTH/LTL (long-term Order Flow anchor) di Weekly.

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
APA YANG DITAMPILKAN

• Timeframe intraday/intermediate → ITH (Intermediate Term High) & ITL
  (Intermediate Term Low). Titik balik struktur yang sudah terkonfirmasi penuh.

• Timeframe Weekly → LTH (Long Term High) & LTL (Long Term Low). Anchor macro
  Order Flow: LTL = invalidation/support struktur bullish, LTH = target/resistance
  (peran membalik saat bearish).

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
LOGIKA — ITH / ITL (intraday)

Dari pola 5-titik zigzag (A-B-C-E-D) dengan 3 syarat konfirmasi:
• ITH : peak C lebih tinggi dari swing high kiri (A) DAN kanan (D); konfirmasi =
  harga break swing low terbaru ke bawah. Batal jika harga menembus peak C.
• ITL : trough C lebih rendah dari swing low kiri (A) DAN kanan (D); konfirmasi =
  harga break swing high terbaru ke atas. Batal jika harga menembus trough C.

Syarat kanan (D) di-cek saat pembentukan: kalau harga sudah menembus C lebih dulu,
kandidat gugur — jadi hanya struktur "standard" yang lolos (tanpa sinyal dini palsu).

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
LOGIKA — LTH / LTL (Weekly)

Anchor Order Flow macro (unified formulation):
• LTL aktif = swing low terbaru yang masih DI BAWAH harga sekarang.
• LTH aktif = swing high terbaru yang masih DI ATAS harga sekarang.

Anchor bergeser otomatis mengikuti struktur; saat harga menembusnya (flip),
anchor berikutnya yang valid otomatis terpilih. Hanya 1 LTH & 1 LTL aktif yang
ditampilkan agar chart bersih.

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
QT FRACTAL (Quarterly Theory)

Kotak tipis tiap quarter siklus Quarterly Theory — tinggi kotak mengikuti range
harga (high–low) candle di quarter itu (ngepas ke chart). LEVELNYA dipilih otomatis
dari timeframe chart — tiap siklus dibagi 4 quarter (Q1 Akumulasi, Q2 Manipulasi,
Q3 Distribusi, Q4 Kelanjutan):
• 5m  → Session  : 1 sesi (6 jam) → 4× quarter 90 menit.
• 15m → Daily    : 1 hari (18:00→18:00 NY) → 4 sesi (Asia/London/NY-AM/PM).
• 1H  → Mingguan : 1 minggu → hari (Senin–Kamis Q1–Q4, Jumat blok special).
• 4H  → Bulanan  : 1 bulan → 4 minggu trading penuh (minggu ke-5 = special).
• >4H → off (Daily ke atas tidak menampilkan QT).

Label berlatar warna di pojok kanan-bawah tiap kotak: nama periode + quarter + FASE AMD (A/M/D/X).
Skenario AMDX/XAMD ditentukan tiap siklus dari Q1 (true-move |close−open| ≥ ambang×ATR
→ X, selainnya A) — tanpa syarat FVG. Anchor jam New York (DST-aware otomatis).
Display-only — bukan sinyal entry. Bisa dipaksa ke level tertentu lewat input "Level".

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
TAMPILAN

• ITH/ITL: menyisakan 1 level aktif (warna penuh) + 1 level "Old" terakhir
  (dipudarkan), sisanya disembunyikan otomatis.
• Garis level memanjang ke kanan sebagai ray horizontal.
• QT Fractal: kotak tipis tiap quarter (ngepas range harga) + label, hanya N siklus terakhir.
• Alert tersedia untuk konfirmasi ITL/LTL & ITH/LTH.

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
INPUT

• Pivot kiri/kanan (bar) — sensitivitas swing 3-bar.
• Filter swing minimal (× ATR) — 0 = tanpa filter (default, granular).
• Periode ATR — untuk filter swing.
• Konfirmasi break pakai BODY (close) — default wick.
• Tampilkan garis zigzag swing — default off.
• Tampilkan garis level — default on.
• Warna ITL/LTL, ITH/LTH, dan "Old" (dipudarkan).
• QT Fractal: Level (Auto/Session/Daily/Mingguan/Bulanan), jumlah siklus terakhir,
  isi kotak (fill on/off), teks watermark, kelipatan ATR AMD, transparansi, warna Q1–Q4.

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
CARA PAKAI

1. Pasang di chart. Di TF rendah/menengah (mis. 1H/4H) baca ITH/ITL untuk
   struktur intermediate; di Weekly baca LTH/LTL untuk konteks macro.
2. Selaraskan arah: Weekly (LTH/LTL) = arah fokus, struktur intraday (ITH/ITL)
   = trigger di TF lebih kecil.
3. Pasang Alert pada kondisi "ITH/LTH terkonfirmasi" atau "ITL/LTL terkonfirmasi".

Catatan: indikator ini alat bantu analisis struktur, bukan sinyal beli/jual.
Selalu kelola risiko sendiri. Open source — silakan pelajari & modifikasi.
```
