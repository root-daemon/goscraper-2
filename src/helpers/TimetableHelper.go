package helpers

import (
	"fmt"
	"goscraper/src/globals"
	"goscraper/src/types"
	"goscraper/src/utils"
	"log"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/valyala/fasthttp"
)

const (
	unifiedTimetableBase = "https://academia.srmist.edu.in/srm_university/academia-academic-services/page/"
	expectedPeriodCount  = 10
)

// Hardcoded fallbacks used when the Unified Time Table page cannot be scraped.
var batch1 = types.Batch{
	Batch: "1",
	Slots: []types.Slot{
		{Day: 1, DayOrder: "Day 1", Slots: []string{"A", "A", "F", "F", "G", "P6", "P7", "P8", "P9", "P10"}},
		{Day: 2, DayOrder: "Day 2", Slots: []string{"P11", "P12", "P13", "P14", "P15", "B", "B", "G", "G", "A"}},
		{Day: 3, DayOrder: "Day 3", Slots: []string{"C", "C", "A", "D", "B", "P26", "P27", "P28", "P29", "P30"}},
		{Day: 4, DayOrder: "Day 4", Slots: []string{"P31", "P32", "P33", "P34", "P35", "D", "D", "B", "E", "C"}},
		{Day: 5, DayOrder: "Day 5", Slots: []string{"E", "E", "C", "F", "D", "P46", "P47", "P48", "P49", "P50"}},
	},
}

var batch2 = types.Batch{
	Batch: "2",
	Slots: []types.Slot{
		{Day: 1, DayOrder: "Day 1", Slots: []string{"P1", "P2", "P3", "P4", "P5", "A", "A", "F", "F", "G"}},
		{Day: 2, DayOrder: "Day 2", Slots: []string{"B", "B", "G", "G", "A", "P16", "P17", "P18", "P19", "P20"}},
		{Day: 3, DayOrder: "Day 3", Slots: []string{"P21", "P22", "P23", "P24", "P25", "C", "C", "A", "D", "B"}},
		{Day: 4, DayOrder: "Day 4", Slots: []string{"D", "D", "B", "E", "C", "P36", "P37", "P38", "P39", "P40"}},
		{Day: 5, DayOrder: "Day 5", Slots: []string{"P41", "P42", "P43", "P44", "P45", "E", "E", "C", "F", "D"}},
	},
}

var (
	dayOrderPattern = regexp.MustCompile(`(?i)Day\s*([1-5])`)
	slotCodePattern = regexp.MustCompile(`(?i)^(?:[A-G]|P\d{1,2}|L\d{1,2})$`)
)

type Timetable struct {
	cookie string
}

func NewTimetable(cookie string) *Timetable {
	return &Timetable{cookie: cookie}
}

func (t *Timetable) GetTimetable(batchNumber int) (*types.TimetableResult, error) {
	// Courses + student details come from My_Time_Table_2023_24.
	coursePage := NewCoursePage(t.cookie)
	courseList, err := coursePage.GetCourses()
	if err != nil {
		log.Printf("TimetableHelper.GetTimetable: failed to get courses - %v", err)
		return &types.TimetableResult{
			RegNumber: "",
			Batch:     fmt.Sprintf("%d", batchNumber),
			Schedule:  []types.DaySchedule{},
		}, nil
	}

	if batchNumber != 1 && batchNumber != 2 {
		log.Printf("TimetableHelper.GetTimetable: invalid batch number %d", batchNumber)
		return &types.TimetableResult{
			RegNumber: courseList.RegNumber,
			Batch:     fmt.Sprintf("%d", batchNumber),
			Schedule:  []types.DaySchedule{},
		}, nil
	}

	// Slot grid comes from Unified_Time_Table_2025_Batch_1 / _batch_2.
	selectedBatch := t.resolveBatch(batchNumber)
	mappedSchedule := t.mapSlotsToSubjects(selectedBatch, courseList.Courses)

	return &types.TimetableResult{
		RegNumber: courseList.RegNumber,
		Batch:     selectedBatch.Batch,
		Schedule:  mappedSchedule,
	}, nil
}

func (t *Timetable) resolveBatch(batchNumber int) types.Batch {
	fallback := batch1
	if batchNumber == 2 {
		fallback = batch2
	}

	scraped, err := t.fetchUnifiedBatch(batchNumber)
	if err != nil {
		log.Printf("TimetableHelper.resolveBatch: using hardcoded batch %d grid (%v)", batchNumber, err)
		return fallback
	}
	if scraped == nil || len(scraped.Slots) < 5 {
		log.Printf("TimetableHelper.resolveBatch: incomplete scraped grid for batch %d, using fallback", batchNumber)
		return fallback
	}

	log.Printf("TimetableHelper.resolveBatch: using scraped Unified Time Table grid for batch %d", batchNumber)
	return *scraped
}

func unifiedTimetableURLs(batchNumber int) []string {
	if batchNumber == 1 {
		return []string{
			unifiedTimetableBase + "Unified_Time_Table_2025_Batch_1",
			unifiedTimetableBase + "Unified_Time_Table_2025_batch_1",
			unifiedTimetableBase + "Unified_Time_Table_2025_Batch1",
		}
	}
	return []string{
		unifiedTimetableBase + "Unified_Time_Table_2025_batch_2",
		unifiedTimetableBase + "Unified_Time_Table_2025_Batch_2",
		unifiedTimetableBase + "Unified_Time_Table_2025_batch2",
	}
}

func (t *Timetable) fetchUnifiedBatch(batchNumber int) (*types.Batch, error) {
	var lastErr error
	for _, url := range unifiedTimetableURLs(batchNumber) {
		html, err := t.fetchAcademiaPage(url)
		if err != nil {
			lastErr = err
			continue
		}
		batch, err := parseUnifiedBatchHTML(html, batchNumber)
		if err != nil {
			lastErr = err
			continue
		}
		return batch, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no unified timetable URL succeeded for batch %d", batchNumber)
	}
	return nil, lastErr
}

func (t *Timetable) fetchAcademiaPage(url string) (string, error) {
	req := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(req)

	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(resp)

	req.SetRequestURI(url)
	req.Header.SetMethod("GET")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	req.Header.Set("cookie", t.cookie)
	req.Header.Set("Referer", "https://academia.srmist.edu.in/")
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("dnt", "1")
	req.Header.Set("sec-ch-ua", `"Not)A;Brand";v="8", "Chromium";v="138", "Google Chrome";v="138"`)
	req.Header.Set("sec-ch-ua-mobile", "?0")
	req.Header.Set("sec-ch-ua-platform", `"macOS"`)
	req.Header.Set("sec-gpc", "1")

	if err := globals.HttpClient.Do(req, resp); err != nil {
		return "", fmt.Errorf("failed to fetch %s: %v", url, err)
	}
	if resp.StatusCode() != fasthttp.StatusOK {
		return "", fmt.Errorf("server returned status %d for %s", resp.StatusCode(), url)
	}

	data := string(resp.Body())
	parts := strings.Split(data, ".sanitize('")
	if len(parts) < 2 {
		return "", fmt.Errorf("invalid response format for %s", url)
	}
	htmlHex := strings.Split(parts[1], "')")[0]
	return utils.ConvertHexToHTML(htmlHex), nil
}

func parseUnifiedBatchHTML(html string, batchNumber int) (*types.Batch, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, fmt.Errorf("failed to parse unified timetable HTML: %v", err)
	}

	var gridTable *goquery.Selection
	doc.Find("table").EachWithBreak(func(_ int, table *goquery.Selection) bool {
		text := strings.ToLower(table.Text())
		if strings.Contains(text, "day 1") && strings.Contains(text, "08:00") {
			gridTable = table
			return false
		}
		return true
	})
	if gridTable == nil {
		return nil, fmt.Errorf("unified timetable grid table not found")
	}

	rows := gridTable.Find("tr")
	if rows.Length() == 0 {
		return nil, fmt.Errorf("unified timetable grid has no rows")
	}

	// Determine which columns are period columns from the header row.
	headerCells := rows.First().Find("td, th")
	periodIndexes := make([]int, 0, expectedPeriodCount)
	headerCells.Each(func(i int, cell *goquery.Selection) {
		text := normalizeCellText(cell.Text())
		if strings.Contains(text, ":") && !strings.Contains(strings.ToLower(text), "day") {
			periodIndexes = append(periodIndexes, i)
		}
	})
	if len(periodIndexes) == 0 {
		// Fallback: every column after the day label.
		for i := 1; i < headerCells.Length(); i++ {
			periodIndexes = append(periodIndexes, i)
		}
	}
	if len(periodIndexes) > expectedPeriodCount {
		periodIndexes = periodIndexes[:expectedPeriodCount]
	}
	if len(periodIndexes) < expectedPeriodCount {
		return nil, fmt.Errorf("expected %d period columns, found %d", expectedPeriodCount, len(periodIndexes))
	}

	daySlots := make([]types.Slot, 0, 5)
	rows.Each(func(_ int, row *goquery.Selection) {
		cells := row.Find("td")
		if cells.Length() == 0 {
			return
		}
		dayText := normalizeCellText(cells.Eq(0).Text())
		match := dayOrderPattern.FindStringSubmatch(dayText)
		if match == nil {
			return
		}

		dayNum, err := strconv.Atoi(match[1])
		if err != nil || dayNum < 1 || dayNum > 5 {
			return
		}

		slots := make([]string, 0, expectedPeriodCount)
		for _, idx := range periodIndexes {
			if idx >= cells.Length() {
				slots = append(slots, "")
				continue
			}
			slots = append(slots, extractSlotCode(cells.Eq(idx).Text()))
		}

		validSlots := 0
		for _, slot := range slots {
			if slot != "" {
				validSlots++
			}
		}
		if validSlots == 0 {
			return
		}

		daySlots = append(daySlots, types.Slot{
			Day:      dayNum,
			DayOrder: fmt.Sprintf("Day %d", dayNum),
			Slots:    slots,
		})
	})

	if len(daySlots) < 5 {
		return nil, fmt.Errorf("expected 5 day rows, found %d", len(daySlots))
	}

	// Keep days 1-5 in order.
	ordered := make([]types.Slot, 5)
	found := 0
	for _, day := range daySlots {
		if day.Day >= 1 && day.Day <= 5 && ordered[day.Day-1].Day == 0 {
			ordered[day.Day-1] = day
			found++
		}
	}
	if found < 5 {
		return nil, fmt.Errorf("missing day rows after ordering (found %d)", found)
	}

	return &types.Batch{
		Batch: fmt.Sprintf("%d", batchNumber),
		Slots: ordered,
	}, nil
}

func normalizeCellText(text string) string {
	text = strings.ReplaceAll(text, "\u00a0", " ")
	text = strings.TrimSpace(text)
	return strings.Join(strings.Fields(text), " ")
}

// extractSlotCode pulls the active slot token from a Unified TT cell.
// Cells often look like "A/-", "-/P6", "A/P1", or multiline "A\n-".
func extractSlotCode(raw string) string {
	raw = strings.ReplaceAll(raw, "\u00a0", " ")
	raw = strings.ReplaceAll(raw, "\r", "")
	raw = strings.ReplaceAll(raw, "\n", "/")
	raw = strings.ReplaceAll(raw, "|", "/")

	parts := strings.Split(raw, "/")
	candidates := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || part == "-" {
			continue
		}
		fields := strings.Fields(part)
		if len(fields) == 0 {
			continue
		}
		token := strings.TrimSpace(fields[0])
		token = strings.Trim(token, ".-")
		if token == "" {
			continue
		}
		if slotCodePattern.MatchString(token) {
			candidates = append(candidates, strings.ToUpper(token))
		}
	}

	if len(candidates) == 0 {
		return ""
	}
	return candidates[0]
}

func (t *Timetable) getSlotsFromRange(slotRange string) []string {
	parts := strings.Split(slotRange, "-")
	slots := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			slots = append(slots, part)
		}
	}
	return slots
}

func (t *Timetable) mapSlotsToSubjects(batch types.Batch, subjects []types.Course) []types.DaySchedule {

	slotMapping := make(map[string][]types.TableSlot)

	for _, subject := range subjects {
		var slots []string
		if strings.Contains(subject.Slot, "-") {
			slots = t.getSlotsFromRange(subject.Slot)
		} else {
			slots = []string{subject.Slot}
		}

		isOnline := strings.Contains(strings.ToLower(subject.Room), "online")
		slotType := "Practical"
		if !isOnline {
			slotType = subject.SlotType
		}

		for _, slot := range slots {
			slot = strings.TrimSpace(slot)
			if slot == "" {
				continue
			}
			tableSlot := types.TableSlot{
				Code:       subject.Code,
				Name:       subject.Title,
				Online:     isOnline,
				CourseType: slotType,
				RoomNo:     subject.Room,
				Slot:       slot,
			}
			slotMapping[slot] = append(slotMapping[slot], tableSlot)
		}
	}

	var schedule []types.DaySchedule
	for _, day := range batch.Slots {
		var table []interface{}
		for _, slot := range day.Slots {
			if slot == "" {
				table = append(table, nil)
				continue
			}
			if slots, ok := slotMapping[slot]; ok {
				if len(slots) > 1 {
					merged := types.TableSlot{
						Code:       strings.Join(uniqueCodes(slots), "/"),
						Name:       strings.Join(uniqueNames(slots), "/"),
						Online:     slots[0].Online,
						CourseType: slots[0].CourseType,
						RoomNo:     strings.Join(uniqueRooms(slots), "/"),
						Slot:       slot,
					}
					table = append(table, merged)
				} else {
					table = append(table, slots[0])
				}
			} else {
				table = append(table, nil)
			}
		}
		schedule = append(schedule, types.DaySchedule{Day: day.Day, Table: table})
	}

	return schedule
}

func uniqueCodes(slots []types.TableSlot) []string {
	seen := make(map[string]bool)
	var result []string
	for _, slot := range slots {
		if !seen[slot.Code] {
			seen[slot.Code] = true
			result = append(result, slot.Code)
		}
	}
	return result
}

func uniqueNames(slots []types.TableSlot) []string {
	seen := make(map[string]bool)
	var result []string
	for _, slot := range slots {
		if !seen[slot.Name] {
			seen[slot.Name] = true
			result = append(result, slot.Name)
		}
	}
	return result
}

func uniqueRooms(slots []types.TableSlot) []string {
	seen := make(map[string]bool)
	var result []string
	for _, slot := range slots {
		if !seen[slot.RoomNo] {
			seen[slot.RoomNo] = true
			result = append(result, slot.RoomNo)
		}
	}
	return result
}

func (t *Timetable) mapWithFallback(subjects types.CourseResponse) *types.TimetableResult {
	batches := []types.Batch{batch1, batch2}

	for _, batch := range batches {
		hasPracticals := false
		for _, daySlot := range batch.Slots {
			for _, slot := range daySlot.Slots {
				if strings.HasPrefix(slot, "P") {
					hasPracticals = true
					break
				}
			}
			if hasPracticals {
				break
			}
		}

		hasPracticalCourses := false
		for _, course := range subjects.Courses {
			if strings.HasPrefix(course.Slot, "P") {
				hasPracticalCourses = true
				break
			}
		}
		if hasPracticalCourses && !hasPracticals {
			continue
		}

		mappedSchedule := t.mapSlotsToSubjects(batch, subjects.Courses)

		for _, course := range subjects.Courses {
			if !strings.HasPrefix(course.Slot, "P") {
				continue
			}

			var courseSlots []string
			if strings.Contains(course.Slot, "-") {
				courseSlots = t.getSlotsFromRange(course.Slot)
			} else {
				courseSlots = []string{course.Slot}
			}

			for _, courseSlot := range courseSlots {
				for _, daySlot := range batch.Slots {
					for _, slot := range daySlot.Slots {
						if courseSlot == slot {
							return &types.TimetableResult{
								RegNumber: subjects.RegNumber,
								Batch:     batch.Batch,
								Schedule:  mappedSchedule,
							}
						}
					}
				}
			}
		}
	}

	return nil
}
