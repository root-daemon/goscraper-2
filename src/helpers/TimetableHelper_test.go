package helpers

import (
	"strings"
	"testing"
)

func TestExtractSlotCode(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"A/-", "A"},
		{"-/P6", "P6"},
		{"A/P1", "A"},
		{"P1", "P1"},
		{"A\n-", "A"},
		{"-\nP16", "P16"},
		{"  G  ", "G"},
		{"-", ""},
		{"", ""},
		{"Lunch", ""},
	}

	for _, tc := range cases {
		got := extractSlotCode(tc.in)
		if got != tc.want {
			t.Fatalf("extractSlotCode(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestParseUnifiedBatchHTMLBatch2(t *testing.T) {
	html := `
	<html><body>
	<table>
		<tr>
			<td>Hours</td>
			<td>08:00-08:50</td><td>08:50-09:40</td><td>09:45-10:35</td><td>10:40-11:30</td><td>11:35-12:25</td>
			<td>12:30-13:20</td><td>13:25-14:15</td><td>14:20-15:10</td><td>15:10-16:00</td><td>16:00-16:50</td>
		</tr>
		<tr>
			<td>Day 1</td>
			<td>P1/-</td><td>P2/-</td><td>P3/-</td><td>P4/-</td><td>P5/-</td>
			<td>A/-</td><td>A/-</td><td>F/-</td><td>F/-</td><td>G/-</td>
		</tr>
		<tr>
			<td>Day 2</td>
			<td>B/-</td><td>B/-</td><td>G/-</td><td>G/-</td><td>A/-</td>
			<td>P16/-</td><td>P17/-</td><td>P18/-</td><td>P19/-</td><td>P20/-</td>
		</tr>
		<tr>
			<td>Day 3</td>
			<td>P21/-</td><td>P22/-</td><td>P23/-</td><td>P24/-</td><td>P25/-</td>
			<td>C/-</td><td>C/-</td><td>A/-</td><td>D/-</td><td>B/-</td>
		</tr>
		<tr>
			<td>Day 4</td>
			<td>D/-</td><td>D/-</td><td>B/-</td><td>E/-</td><td>C/-</td>
			<td>P36/-</td><td>P37/-</td><td>P38/-</td><td>P39/-</td><td>P40/-</td>
		</tr>
		<tr>
			<td>Day 5</td>
			<td>P41/-</td><td>P42/-</td><td>P43/-</td><td>P44/-</td><td>P45/-</td>
			<td>E/-</td><td>E/-</td><td>C/-</td><td>F/-</td><td>D/-</td>
		</tr>
	</table>
	</body></html>`

	batch, err := parseUnifiedBatchHTML(html, 2)
	if err != nil {
		t.Fatalf("parseUnifiedBatchHTML failed: %v", err)
	}
	if batch.Batch != "2" {
		t.Fatalf("batch = %q, want 2", batch.Batch)
	}
	if len(batch.Slots) != 5 {
		t.Fatalf("days = %d, want 5", len(batch.Slots))
	}

	wantDay1 := []string{"P1", "P2", "P3", "P4", "P5", "A", "A", "F", "F", "G"}
	gotDay1 := batch.Slots[0].Slots
	if len(gotDay1) != len(wantDay1) {
		t.Fatalf("day1 len = %d, want %d", len(gotDay1), len(wantDay1))
	}
	for i := range wantDay1 {
		if gotDay1[i] != wantDay1[i] {
			t.Fatalf("day1[%d] = %q, want %q (full=%v)", i, gotDay1[i], wantDay1[i], gotDay1)
		}
	}
}

func TestParseUnifiedBatchHTMLBatch1(t *testing.T) {
	html := `
	<table align="center" border="5" cellpadding="18" cellspacing="2" width="400">
		<tr>
			<td>TO</td>
			<td>08:00 - 08:50</td><td>08:50 - 09:40</td><td>09:45 - 10:35</td><td>10:40 - 11:30</td><td>11:35 - 12:25</td>
			<td>12:30 - 13:20</td><td>13:25 - 14:15</td><td>14:20 - 15:10</td><td>15:10 - 16:00</td><td>16:00 - 16:50</td>
		</tr>
		<tr><td>Day 1</td><td>A/-</td><td>A/-</td><td>F/-</td><td>F/-</td><td>G/-</td><td>P6/-</td><td>P7/-</td><td>P8/-</td><td>P9/-</td><td>P10/-</td></tr>
		<tr><td>Day 2</td><td>P11/-</td><td>P12/-</td><td>P13/-</td><td>P14/-</td><td>P15/-</td><td>B/-</td><td>B/-</td><td>G/-</td><td>G/-</td><td>A/-</td></tr>
		<tr><td>Day 3</td><td>C/-</td><td>C/-</td><td>A/-</td><td>D/-</td><td>B/-</td><td>P26/-</td><td>P27/-</td><td>P28/-</td><td>P29/-</td><td>P30/-</td></tr>
		<tr><td>Day 4</td><td>P31/-</td><td>P32/-</td><td>P33/-</td><td>P34/-</td><td>P35/-</td><td>D/-</td><td>D/-</td><td>B/-</td><td>E/-</td><td>C/-</td></tr>
		<tr><td>Day 5</td><td>E/-</td><td>E/-</td><td>C/-</td><td>F/-</td><td>D/-</td><td>P46/-</td><td>P47/-</td><td>P48/-</td><td>P49/-</td><td>P50/-</td></tr>
	</table>`

	batch, err := parseUnifiedBatchHTML(html, 1)
	if err != nil {
		t.Fatalf("parseUnifiedBatchHTML failed: %v", err)
	}
	if !strings.EqualFold(batch.Slots[0].Slots[0], "A") || batch.Slots[0].Slots[5] != "P6" {
		t.Fatalf("unexpected day1 slots: %v", batch.Slots[0].Slots)
	}
}

func TestUnifiedTimetableURLs(t *testing.T) {
	urls := unifiedTimetableURLs(2)
	if len(urls) == 0 || !strings.Contains(urls[0], "Unified_Time_Table_2025_batch_2") {
		t.Fatalf("batch2 url missing: %v", urls)
	}
	urls = unifiedTimetableURLs(1)
	if len(urls) == 0 || !strings.Contains(urls[0], "Unified_Time_Table_2025_Batch_1") {
		t.Fatalf("batch1 url missing: %v", urls)
	}
}
