package operator

import (
	"fmt"
	"testing"
	"time"
)

func TestParseKey(t *testing.T) {
	b, ok := parseKey("backups/20261104T030000Z-scheduled.tar.age")
	if !ok || b.Kind != "scheduled" || b.CreatedAt != time.Date(2026, 11, 4, 3, 0, 0, 0, time.UTC) {
		t.Fatalf("regular key: %+v %v", b, ok)
	}
	b, ok = parseKey("pre-drop/stash/20261104T120000Z.tar.age")
	if !ok || b.Kind != "pre-drop" || b.Database != "stash" {
		t.Fatalf("pre-drop key: %+v %v", b, ok)
	}
	for _, k := range []string{"state/status.json", "backups/garbage.tar.age", "../etc/passwd"} {
		if _, ok := parseKey(k); ok {
			t.Errorf("%q should not parse", k)
		}
	}
}

func TestRetained(t *testing.T) {
	// Two backups a day for 400 days, newest first.
	start := time.Date(2026, 11, 4, 3, 0, 0, 0, time.UTC)
	var all []BackupObject
	for d := 0; d < 400; d++ {
		for _, h := range []int{0, 9} {
			ts := start.AddDate(0, 0, -d).Add(time.Duration(h) * time.Hour)
			all = append(all, BackupObject{Key: fmt.Sprintf("k%d-%d", d, h), CreatedAt: ts})
		}
	}
	keep := retained(all, Settings{KeepDaily: 7, KeepWeekly: 4, KeepMonthly: 6}, time.UTC)

	if !keep["k0-9"] {
		t.Error("newest backup must be kept")
	}
	if keep["k0-0"] {
		t.Error("only the newest backup of a day is kept")
	}
	for d := 0; d < 7; d++ {
		if !keep[fmt.Sprintf("k%d-9", d)] {
			t.Errorf("day %d should be kept", d)
		}
	}
	// 7 daily + up to 4 weekly + up to 6 monthly, with overlaps.
	if n := len(keep); n < 7 || n > 17 {
		t.Errorf("kept %d backups", n)
	}
	for k := range keep {
		var d, h int
		fmt.Sscanf(k, "k%d-%d", &d, &h)
		if d > 200 {
			t.Errorf("%s is older than 6 months", k)
		}
	}
}

func TestRetainedManual(t *testing.T) {
	now := time.Now()
	all := []BackupObject{
		{Key: "sched-today", Kind: "scheduled", CreatedAt: now.Add(-2 * time.Hour)},
		{Key: "manual-today", Kind: "manual", CreatedAt: now.Add(-time.Hour)},
		{Key: "manual-old", Kind: "manual", CreatedAt: now.AddDate(0, 0, -10)},
	}
	keep := retained(all, Settings{KeepDaily: 7}, time.UTC)
	if !keep["sched-today"] {
		t.Error("a manual backup must not push out the day's scheduled backup")
	}
	if !keep["manual-today"] || keep["manual-old"] {
		t.Errorf("manual backups are kept for KeepDaily days: %v", keep)
	}
}
