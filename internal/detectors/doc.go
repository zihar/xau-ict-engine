// Package detectors berisi detektor pola per layer rules (Section A–F):
// swing/Fibonacci (A), order flow TDA/DB (B), AMS ITH/ITL (C), QT (D),
// Kunci #2 (E), PD Array (F).
//
// Status:
//
//	Layer A IMPLEMENTED (swing.go, fibonacci.go, ruleof05.go) — DetectSwings/
//	  Zigzag (3-bar), FindLastImpulse/FindValidImpulse (A.1), Fib/Zone + Rule
//	  of 0.5 (A.2).
//	Layer C IMPLEMENTED (intermediate.go) — DetectIntermediates (ITH/ITL
//	  standard + fast_early, C.2/C.3), break wick/body, LastIntermediate.
//	  STL/STH (C.1) = DetectSwings (di-reuse). C.4 fokus arah & C.5 stop hunt
//	  = engine/state (butuh konteks OF).
//	Layer B (OF state machine), D, E, F menyusul.
//
// Sumber kebenaran rules: ~/Documents/forex-lessons/BACKTEST_RULES.md.
package detectors
