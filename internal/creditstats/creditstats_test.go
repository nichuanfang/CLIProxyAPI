package creditstats

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// resetGlobal swaps the singleton for a hermetic test instance.
func resetGlobal(t *testing.T, file string) {
	t.Helper()
	global = &stats{file: file}
	t.Cleanup(func() { global = &stats{} })
}

func TestRecordAggregatesAndPersists(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "stats.json")
	resetGlobal(t, file)

	Record("kimi-k3-2", 4.37, 55637)
	Record("glm-5.3", 1.5, 100)
	Record("kimi-k3-2", 0.63, 200)
	// Non-positive credits must be ignored.
	Record("kimi-k3-2", 0, 999)
	Record("kimi-k3-2", -1, 999)

	snap := Current()
	if !snap.Enabled {
		t.Fatal("snapshot should be enabled")
	}
	if snap.TotalCredit != 6.5 {
		t.Fatalf("TotalCredit = %v, want 6.5", snap.TotalCredit)
	}
	if snap.Requests != 3 {
		t.Fatalf("Requests = %d, want 3", snap.Requests)
	}
	if snap.TotalTokens != 55937 {
		t.Fatalf("TotalTokens = %d, want 55937", snap.TotalTokens)
	}
	if snap.TodayCredit != 6.5 || snap.TodayRequests != 3 || snap.TodayTokens != 55937 {
		t.Fatalf("today stats = %+v, want credit 6.5 requests 3 tokens 55937", snap)
	}
	if snap.Last == nil || snap.Last.Model != "kimi-k3-2" || snap.Last.Credit != 0.63 {
		t.Fatalf("Last = %+v, want kimi-k3-2/0.63", snap.Last)
	}
	if len(snap.Models) != 2 {
		t.Fatalf("Models len = %d, want 2", len(snap.Models))
	}
	// Sorted by model name.
	if snap.Models[0].Model != "glm-5.3" || snap.Models[1].Model != "kimi-k3-2" {
		t.Fatalf("Models order = %+v", snap.Models)
	}
	kimi := snap.Models[1]
	if kimi.Credit != 5 || kimi.Requests != 2 || kimi.Tokens != 55837 {
		t.Fatalf("kimi stat = %+v, want credit 5 requests 2 tokens 55837", kimi)
	}
	if len(snap.Days) != 1 {
		t.Fatalf("Days len = %d, want 1", len(snap.Days))
	}

	// Persistence round-trip: a fresh instance must reload the same totals.

	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("stats file missing: %v", err)
	}
	var disk diskState
	if err := json.Unmarshal(raw, &disk); err != nil {
		t.Fatalf("stats file invalid JSON: %v", err)
	}
	if disk.TotalCredit != 6.5 || disk.Requests != 3 {
		t.Fatalf("disk totals = %+v", disk)
	}

	global = &stats{file: file}
	snap2 := Current()
	if snap2.TotalCredit != 6.5 || snap2.Requests != 3 || snap2.TotalTokens != 55937 {
		t.Fatalf("reloaded snapshot = %+v", snap2)
	}
}

func TestRecordConcurrent(t *testing.T) {
	dir := t.TempDir()
	resetGlobal(t, filepath.Join(dir, "stats.json"))

	const workers = 16
	const perWorker = 50
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perWorker; j++ {
				Record("m", 0.01, 10)
			}
		}()
	}
	wg.Wait()

	snap := Current()
	wantRequests := int64(workers * perWorker)
	if snap.Requests != wantRequests {
		t.Fatalf("Requests = %d, want %d", snap.Requests, wantRequests)
	}
	wantCredit := round6(float64(workers*perWorker) * 0.01)
	if snap.TotalCredit != wantCredit {
		t.Fatalf("TotalCredit = %v, want %v", snap.TotalCredit, wantCredit)
	}
}

func TestPruneDays(t *testing.T) {
	dir := t.TempDir()
	resetGlobal(t, filepath.Join(dir, "stats.json"))

	global.mu.Lock()
	global.loadLocked()
	for i := 1; i <= maxDays+10; i++ {
		month := 1 + (i / 28)
		day := fmt.Sprintf("2026-%02d-%02d", month, 1+(i%28))
		global.days[day] = &DayStat{Day: day, Credit: 1, Requests: 1, Tokens: 1}
	}
	global.pruneLocked()
	global.mu.Unlock()

	snap := Current()
	if len(snap.Days) != maxDays {
		t.Fatalf("Days len = %d, want %d", len(snap.Days), maxDays)
	}
	// Sorted ascending.
	for i := 1; i < len(snap.Days); i++ {
		if snap.Days[i-1].Day >= snap.Days[i].Day {
			t.Fatalf("Days not sorted: %+v", snap.Days)
		}
	}
}

func TestStatsFileEnvOverride(t *testing.T) {
	t.Setenv(envStatsFile, `C:\custom\path\stats.json`)
	if got := statsFile(); got != `C:\custom\path\stats.json` {
		t.Fatalf("statsFile() = %q", got)
	}
}
