package news

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

// State = dedup persisten antar-run: per "release key" (indikator@waktu) catat
// apakah alert PRA dan PASCA sudah dikirim, supaya scheduler yang jalan tiap
// beberapa menit tidak mengirim ganda. Mirip notify.State tapi khusus news.
type State struct {
	// Sent[key] = penanda kirim untuk satu rilis. key = Release.Key().
	Sent map[string]Sent `json:"sent"`
}

// Sent = penanda waktu kirim alert pra/pasca satu rilis (zero = belum). Digest
// dipakai untuk reminder mingguan (key = WeekKey, bukan release key) — disimpan
// di map yang sama supaya satu file state cukup.
type Sent struct {
	Pre    time.Time `json:"pre,omitempty"`
	Post   time.Time `json:"post,omitempty"`
	Digest time.Time `json:"digest,omitempty"`
}

// Key = identitas stabil satu rilis untuk dedup ("CPI@2026-06-10T12:30:00Z").
func (r Release) Key() string {
	return fmt.Sprintf("%s@%s", r.Name, r.Time.UTC().Format(time.RFC3339))
}

// WeekKey = identitas stabil satu minggu ISO untuk dedup digest mingguan
// ("DIGEST@2026-W24"). ISO week → reminder Senin terkirim sekali per minggu
// walau timer/-once dijalankan beberapa kali.
func WeekKey(t time.Time) string {
	y, w := t.UTC().ISOWeek()
	return fmt.Sprintf("DIGEST@%04d-W%02d", y, w)
}

// LoadState membaca state dari path. File tak ada → State kosong + nil error.
func LoadState(path string) (State, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return State{Sent: map[string]Sent{}}, nil
		}
		return State{Sent: map[string]Sent{}}, err
	}
	var s State
	if err := json.Unmarshal(b, &s); err != nil {
		return State{Sent: map[string]Sent{}}, err
	}
	if s.Sent == nil {
		s.Sent = map[string]Sent{}
	}
	return s, nil
}

// Save menulis state JSON indented (0644).
func (s State) Save(path string) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// PreSent / PostSent = sudah pernah kirim alert pra/pasca untuk rilis ini?
func (s State) PreSent(key string) bool  { return !s.Sent[key].Pre.IsZero() }
func (s State) PostSent(key string) bool { return !s.Sent[key].Post.IsZero() }

// DigestSent = sudah kirim reminder mingguan untuk minggu ini (key = WeekKey)?
func (s State) DigestSent(key string) bool { return !s.Sent[key].Digest.IsZero() }

// MarkDigest menandai digest mingguan terkirim (now disuntik agar deterministik).
func (s *State) MarkDigest(key string, now time.Time) {
	e := s.Sent[key]
	e.Digest = now.UTC()
	s.Sent[key] = e
}

// MarkPre / MarkPost menandai alert terkirim (now disuntik agar deterministik di test).
func (s *State) MarkPre(key string, now time.Time) {
	e := s.Sent[key]
	e.Pre = now.UTC()
	s.Sent[key] = e
}

func (s *State) MarkPost(key string, now time.Time) {
	e := s.Sent[key]
	e.Post = now.UTC()
	s.Sent[key] = e
}
