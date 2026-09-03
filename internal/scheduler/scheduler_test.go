package scheduler

import (
	"testing"
	"time"
)

func TestFridaySchedule(t *testing.T) {
	schedule, err := Parse("0 18 * * 5")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 9, 3, 18, 1, 0, 0, time.FixedZone("JST", 9*3600))
	next, err := schedule.Next(start)
	if err != nil {
		t.Fatal(err)
	}
	if next.Weekday() != time.Friday || next.Hour() != 18 || next.Minute() != 0 {
		t.Fatalf("unexpected next: %s", next)
	}
}

func TestSundayMayBeSeven(t *testing.T) {
	schedule, err := Parse("0 0 * * 7")
	if err != nil {
		t.Fatal(err)
	}
	if !schedule.Matches(time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)) {
		t.Fatal("Sunday did not match day 7")
	}
}
