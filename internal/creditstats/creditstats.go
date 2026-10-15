// Package creditstats aggregates upstream credit consumption reported in
// OpenAI-compatible usage objects (the CodeBuddy-specific "credit" field).
// Statistics are process-wide, persisted to a JSON file, and exposed via
// an HTTP endpoint plus Codex-compatible response headers.
package creditstats

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// envStatsFile overrides the on-disk statistics file location.
	envStatsFile = "CLIPROXY_CREDIT_STATS_FILE"
	// maxDays bounds the per-day history kept in memory and on disk.
	maxDays = 90
)

// LastRecord describes the most recent recorded request.
type LastRecord struct {
	Model  string    `json:"model"`
	Credit float64   `json:"credit"`
	Tokens int64     `json:"tokens"`
	At     time.Time `json:"at"`
}

// ModelStat aggregates consumption for a single model.
type ModelStat struct {
	Model    string  `json:"model"`
	Credit   float64 `json:"credit"`
	Requests int64   `json:"requests"`
	Tokens   int64   `json:"tokens"`
}

// DayStat aggregates consumption for a single local calendar day.
type DayStat struct {
	Day      string  `json:"day"`
	Credit   float64 `json:"credit"`
	Requests int64   `json:"requests"`
	Tokens   int64   `json:"tokens"`
}

// Snapshot is the external, read-only view of the accumulated statistics.
type Snapshot struct {
	Enabled       bool        `json:"enabled"`
	TotalCredit   float64     `json:"total_credit"`
	Requests      int64       `json:"requests"`
	TotalTokens   int64       `json:"total_tokens"`
	Today         string      `json:"today"`
	TodayCredit   float64     `json:"today_credit"`
	TodayTokens   int64       `json:"today_tokens"`
	TodayRequests int64       `json:"today_requests"`
	Since         time.Time   `json:"since"`
	UpdatedAt     time.Time   `json:"updated_at"`
	Last          *LastRecord `json:"last,omitempty"`
	Models        []ModelStat `json:"models"`
	Days          []DayStat   `json:"days"`
}

// diskState is the on-disk representation; maps keep diffs stable.
type diskState struct {
	Since       time.Time             `json:"since"`
	TotalCredit float64               `json:"total_credit"`
	Requests    int64                 `json:"requests"`
	TotalTokens int64                 `json:"total_tokens"`
	Models      map[string]*ModelStat `json:"models"`
	Days        map[string]*DayStat   `json:"days"`
}

type stats struct {
	mu          sync.Mutex
	file        string
	since       time.Time
	totalCredit float64
	requests    int64
	totalTokens int64
	last        *LastRecord
	models      map[string]*ModelStat
	days        map[string]*DayStat
	loaded      bool
}

var global = &stats{}

// statsFile resolves the persistence path, honoring the environment override.
func statsFile() string {
	if p := strings.TrimSpace(os.Getenv(envStatsFile)); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "credit-usage.json")
	}
	return filepath.Join(home, ".cli-proxy-api-stats", "credit-usage.json")
}

func localDay(t time.Time) string { return t.Local().Format("2006-01-02") }

// round6 removes floating point noise so JSON output stays human readable.
func round6(v float64) float64 { return math.Round(v*1e6) / 1e6 }

// Record accumulates one finished request. A non-positive credit is ignored
// (non-CodeBuddy upstreams never send the field and must not be counted).
func Record(model string, credit float64, tokens int64) {
	if credit <= 0 {
		return
	}
	global.mu.Lock()
	defer global.mu.Unlock()
	global.loadLocked()

	now := time.Now()
	day := localDay(now)

	global.totalCredit = round6(global.totalCredit + credit)
	global.requests++
	global.totalTokens += tokens
	global.last = &LastRecord{Model: model, Credit: round6(credit), Tokens: tokens, At: now}

	ms := global.models[model]
	if ms == nil {
		ms = &ModelStat{Model: model}
		global.models[model] = ms
	}
	ms.Credit = round6(ms.Credit + credit)
	ms.Requests++
	ms.Tokens += tokens

	ds := global.days[day]
	if ds == nil {
		ds = &DayStat{Day: day}
		global.days[day] = ds
	}
	ds.Credit = round6(ds.Credit + credit)
	ds.Requests++
	ds.Tokens += tokens

	global.pruneLocked()
	global.saveLocked()
}

// Current returns a consistent snapshot of the accumulated statistics.
func Current() Snapshot {
	global.mu.Lock()
	defer global.mu.Unlock()
	global.loadLocked()
	return global.snapshotLocked()
}

// loadLocked lazily restores previously persisted state from disk.
func (s *stats) loadLocked() {
	if s.loaded {
		return
	}
	s.loaded = true
	if s.file == "" {
		s.file = statsFile()
	}
	s.models = make(map[string]*ModelStat)
	s.days = make(map[string]*DayStat)
	s.since = time.Now()

	raw, err := os.ReadFile(s.file)
	if err != nil {
		return
	}
	var disk diskState
	if err := json.Unmarshal(raw, &disk); err != nil {
		return
	}
	s.since = disk.Since
	s.totalCredit = disk.TotalCredit
	s.requests = disk.Requests
	s.totalTokens = disk.TotalTokens
	for k, v := range disk.Models {
		if v != nil {
			s.models[k] = v
		}
	}
	for k, v := range disk.Days {
		if v != nil {
			s.days[k] = v
		}
	}
	s.pruneLocked()
}

// pruneLocked drops day entries beyond the retention window.
func (s *stats) pruneLocked() {
	if len(s.days) <= maxDays {
		return
	}
	keys := make([]string, 0, len(s.days))
	for k := range s.days {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys[:len(keys)-maxDays] {
		delete(s.days, k)
	}
}

// saveLocked atomically persists the current state. Errors are intentionally
// ignored: losing statistics must never break proxying.
func (s *stats) saveLocked() {
	disk := diskState{
		Since:       s.since,
		TotalCredit: round6(s.totalCredit),
		Requests:    s.requests,
		TotalTokens: s.totalTokens,
		Models:      s.models,
		Days:        s.days,
	}
	raw, err := json.MarshalIndent(disk, "", "  ")
	if err != nil {
		return
	}
	dir := filepath.Dir(s.file)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	tmp, err := os.CreateTemp(dir, ".credit-usage-*.tmp")
	if err != nil {
		return
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return
	}
	if err := os.Rename(tmpName, s.file); err != nil {
		_ = os.Remove(tmpName)
	}
}

// snapshotLocked builds the external view with sorted slices.
func (s *stats) snapshotLocked() Snapshot {
	today := localDay(time.Now())
	todayStat := s.days[today]

	snap := Snapshot{
		Enabled:     true,
		TotalCredit: round6(s.totalCredit),
		Requests:    s.requests,
		TotalTokens: s.totalTokens,
		Today:       today,
		Since:       s.since,
		UpdatedAt:   time.Now(),
		Last:        s.last,
	}
	if todayStat != nil {
		snap.TodayCredit = round6(todayStat.Credit)
		snap.TodayTokens = todayStat.Tokens
		snap.TodayRequests = todayStat.Requests
	}

	models := make([]ModelStat, 0, len(s.models))
	for _, m := range s.models {
		models = append(models, ModelStat{
			Model:    m.Model,
			Credit:   round6(m.Credit),
			Requests: m.Requests,
			Tokens:   m.Tokens,
		})
	}
	sort.Slice(models, func(i, j int) bool { return models[i].Model < models[j].Model })
	snap.Models = models

	days := make([]DayStat, 0, len(s.days))
	for _, d := range s.days {
		days = append(days, DayStat{
			Day:      d.Day,
			Credit:   round6(d.Credit),
			Requests: d.Requests,
			Tokens:   d.Tokens,
		})
	}
	sort.Slice(days, func(i, j int) bool { return days[i].Day < days[j].Day })
	snap.Days = days

	return snap
}

// String aids debugging in logs.
func (s Snapshot) String() string {
	return fmt.Sprintf("credit total=%.6g today=%.6g requests=%d", s.TotalCredit, s.TodayCredit, s.Requests)
}
