// Package state berisi state machine order flow & regime (Section B & E):
// Weekly OF / LTL-LTH (B.1), Daily bias (B.2), Kunci #2 gate + regime (E).
// EARLY vs DEFINITIF (B.3) di-handle engine via TF hierarchy (weekly=definitif,
// daily/1H=early) memakai detectors.DetectIntermediates.
package state
