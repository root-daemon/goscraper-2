package helpers

import (
	"strings"
	"testing"
	"time"
)

func TestCalendarPageURLsPreferAcademicReports(t *testing.T) {
	urls := calendarPageURLs(time.Date(2026, time.July, 26, 0, 0, 0, 0, time.UTC))
	if len(urls) == 0 {
		t.Fatal("expected calendar page URLs")
	}
	if !strings.Contains(urls[0], "Academic_Reports") || strings.Contains(urls[0], "Unified") {
		t.Fatalf("first calendar URL = %q, want Academic_Reports", urls[0])
	}
}

func TestCalendarPageURLsIncludeCurrentOddPlannerFallback(t *testing.T) {
	urls := calendarPageURLs(time.Date(2026, time.July, 26, 0, 0, 0, 0, time.UTC))
	found := false
	for _, url := range urls {
		if strings.Contains(url, "Academic_Planner_2026_27_ODD") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected Academic_Planner_2026_27_ODD fallback in %v", urls)
	}
}

func TestDiscoverPlannerURLs(t *testing.T) {
	raw := `PAGELINKNAME":"Academic_Planner_2026_27_ODD" and also Academic_Planner_2025_26_EVEN`
	urls := discoverPlannerURLs(raw)
	if len(urls) < 2 {
		t.Fatalf("expected discovered planners, got %v", urls)
	}
	if !strings.Contains(urls[0], "Academic_Planner_2026_27_ODD") {
		t.Fatalf("unexpected first discovery %q", urls[0])
	}
}
