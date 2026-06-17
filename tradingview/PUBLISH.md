# Publishing Guide — Luna Trade Indicators

> Note: this is an **open-source / public (free)** publication, and it is the **FIRST publish**
> (never published before) → use the **new publication** flow, NOT "Update existing publication".
> The "Update existing" option only becomes relevant for subsequent version releases (so likes/comments aren't lost).

## Steps (first publish)
1. Open the script in the Pine Editor → **Save** (`⌘S`), make sure there are no errors (the `PINE_VERSION_OUTDATED` warning can be ignored).
2. Click **Publish script** (the button in the **top right of the Pine Editor**, not in the script-name dropdown).
3. The **new** publication dialog appears → set visibility = **Open source (Public)**.
4. **Title** & **Description** below → copy-paste.
5. Suggested Category/Tags: `Trend Analysis`, `Market Structure`, `Support and Resistance` → **Publish**.

---

## Title

```
Luna Trade Indicators
```

## Description

```
Luna Trade Indicators — automatic market structure detection: ITH/ITL (intermediate)
on intraday timeframes and LTH/LTL (long-term Order Flow anchors) on Weekly.

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
WHAT IT SHOWS

• Intraday/intermediate timeframes → ITH (Intermediate Term High) & ITL
  (Intermediate Term Low). Fully confirmed structural turning points.

• Weekly timeframe → LTH (Long Term High) & LTL (Long Term Low). Macro Order Flow
  anchors: LTL = invalidation/support for bullish structure, LTH = target/resistance
  (roles flip when bearish).

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
LOGIC — ITH / ITL (intraday)

From a 5-point zigzag pattern (A-B-C-E-D) with 3 confirmation conditions:
• ITH : peak C higher than the left (A) AND right (D) swing high; confirmation =
  price breaks the latest swing low downward. Invalidated if price breaks through peak C.
• ITL : trough C lower than the left (A) AND right (D) swing low; confirmation =
  price breaks the latest swing high upward. Invalidated if price breaks through trough C.

The right-side (D) condition is checked at formation: if price has already broken through C first,
the candidate is dropped — so only "standard" structures pass (no false early signals).

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
LOGIC — LTH / LTL (Weekly)

Macro Order Flow anchors (unified formulation):
• Active LTL = the latest swing low still BELOW the current price.
• Active LTH = the latest swing high still ABOVE the current price.

The anchor shifts automatically following structure; when price breaks through it (flip),
the next valid anchor is selected automatically. Only 1 active LTH & 1 active LTL are
shown to keep the chart clean.

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
QT FRACTAL (Quarterly Theory)

A thin box for each quarter of the Quarterly Theory cycle — the box height follows the
candle's price range (high–low) in that quarter (fitting the chart). The LEVEL is chosen automatically
from the chart timeframe — each cycle is divided into 4 quarters (Q1 Accumulation, Q2 Manipulation,
Q3 Distribution, Q4 Continuation):
• 5m  → Session  : 1 session (6 hours) → 4× 90-minute quarter.
• 15m → Daily    : 1 day (18:00→18:00 NY) → 4 sessions (Asia/London/NY-AM/PM).
• 1H  → Weekly   : 1 week → days (Monday–Thursday Q1–Q4, Friday special block).
• 4H  → Monthly  : 1 month → 4 full trading weeks (the 5th week = special).
• >4H → off (Daily and above do not show QT).

A color-backed label in the bottom-right corner of each box: period name + quarter + AMD PHASE (A/M/D/X).
The AMDX/XAMD scenario is determined each cycle from Q1 (true-move |close−open| ≥ threshold×ATR
→ X, otherwise A) — with no FVG condition. New York time anchor (DST-aware automatically).
Display-only — not an entry signal. Can be forced to a specific level via the "Level" input.

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
DISPLAY

• ITH/ITL: keeps 1 active level (full color) + 1 most recent "Old" level
  (faded), the rest hidden automatically. The active level becomes "Old"/faded when
  its pivot is BROKEN by price (ITH upward / ITL downward) or when replaced by a new level.
• Level lines extend to the right as a horizontal ray.
• QT Fractal: a thin box for each quarter (fitting the price range) + label, only the last N cycles.
• Alerts available for ITL/LTL & ITH/LTH confirmation.

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
INPUTS

• Left/right pivot (bars) — 3-bar swing sensitivity.
• Minimum swing filter (× ATR) — 0 = no filter (default, granular).
• ATR period — for the swing filter.
• Confirm break using BODY (close) — default wick.
• Show swing zigzag lines — default off.
• Show level lines — default on.
• Colors for ITL/LTL, ITH/LTH, and "Old" (faded).
• QT Fractal: Level (Auto/Session/Daily/Weekly/Monthly), number of recent cycles,
  box fill (on/off), watermark text, AMD ATR multiple, transparency, Q1–Q4 colors.

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
HOW TO USE

1. Add it to the chart. On low/medium TFs (e.g. 1H/4H) read ITH/ITL for
   intermediate structure; on Weekly read LTH/LTL for macro context.
2. Align direction: Weekly (LTH/LTL) = focus direction, intraday structure (ITH/ITL)
   = trigger on a smaller TF.
3. Set an Alert on the "ITH/LTH confirmed" or "ITL/LTL confirmed" condition.

Note: this indicator is a structure-analysis aid, not a buy/sell signal.
Always manage your own risk. Open source — feel free to study & modify.
```
