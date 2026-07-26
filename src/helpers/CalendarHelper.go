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
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/valyala/fasthttp"
)

const academiaPageBase = "https://academia.srmist.edu.in/srm_university/academia-academic-services/page/"

type CalendarFetcher struct {
	cookie string
	date   time.Time
}

func NewCalendarFetcher(date time.Time, cookie string) *CalendarFetcher {
	return &CalendarFetcher{
		cookie: cookie,
		date:   date,
	}
}

func (c *CalendarFetcher) GetCalendar() (*types.CalendarResponse, error) {
	var lastErr string
	urls := calendarPageURLs(c.date)

	for i := 0; i < len(urls); i++ {
		url := urls[i]
		body, status, err := c.fetchPlannerPage(url)
		if err != nil {
			lastErr = err.Error()
			log.Printf("CalendarHelper.GetCalendar: %s -> %v", url, err)
			continue
		}
		if status != fasthttp.StatusOK {
			lastErr = fmt.Sprintf("HTTP %d for %s", status, url)
			log.Printf("CalendarHelper.GetCalendar: %s", lastErr)
			continue
		}

		// Academic_Reports may embed the planner table or link to Academic_Planner_* pages.
		for _, discovered := range discoverPlannerURLs(body) {
			urls = appendUniqueURL(urls, discovered)
		}

		calendar, err := c.parseCalendar(body)
		if err != nil {
			lastErr = err.Error()
			log.Printf("CalendarHelper.GetCalendar: parse failed for %s - %v", url, err)
			continue
		}
		if len(calendar.Calendar) == 0 {
			lastErr = fmt.Sprintf("empty calendar from %s", url)
			log.Printf("CalendarHelper.GetCalendar: %s", lastErr)
			continue
		}

		log.Printf("CalendarHelper.GetCalendar: using page %s", url)
		calendar.Status = status
		return calendar, nil
	}

	if lastErr == "" {
		lastErr = "no academic calendar URL succeeded"
	}
	return &types.CalendarResponse{
		Error:    true,
		Message:  lastErr,
		Status:   500,
		Calendar: []types.CalendarMonth{},
	}, nil
}

// calendarPageURLs prefers the stable Academic_Reports page (browser hash
// #Academic_Reports), then related report pages, then term-named planners.
func calendarPageURLs(now time.Time) []string {
	seen := make(map[string]bool)
	var urls []string
	add := func(list ...string) {
		for _, u := range list {
			if !seen[u] {
				seen[u] = true
				urls = append(urls, u)
			}
		}
	}

	add(
		academiaPageBase+"Academic_Reports",
		academiaPageBase+"Academic_Reports_Unified",
		academiaPageBase+"Academic_Calendar",
		academiaPageBase+"Day_Order",
	)

	year := now.Year()
	month := int(now.Month())
	isOdd := month >= 6
	academicYear := year
	if !isOdd {
		academicYear = year - 1
	}

	build := func(y int, term string) []string {
		shortNext := fmt.Sprintf("%02d", (y+1)%100)
		return []string{
			academiaPageBase + fmt.Sprintf("Academic_Planner_%d_%s_%s", y, shortNext, term),
			academiaPageBase + fmt.Sprintf("Academic_Planner_%d_%d_%s", y, y+1, term),
		}
	}

	primary := "ODD"
	secondary := "EVEN"
	if !isOdd {
		primary, secondary = "EVEN", "ODD"
	}

	add(build(academicYear, primary)...)
	add(build(academicYear, secondary)...)
	add(build(academicYear+1, "ODD")...)
	add(build(academicYear+1, "EVEN")...)
	add(build(academicYear-1, "EVEN")...)
	add(build(academicYear-1, "ODD")...)
	add(academiaPageBase + "Academic_Planner")
	return urls
}

var plannerLinkPattern = regexp.MustCompile(`Academic_Planner_[A-Za-z0-9_]+`)

func discoverPlannerURLs(raw string) []string {
	decoded := utils.ConvertHexToHTML(raw)
	matches := plannerLinkPattern.FindAllString(decoded, -1)
	if len(matches) == 0 {
		matches = plannerLinkPattern.FindAllString(raw, -1)
	}
	seen := make(map[string]bool)
	var urls []string
	for _, name := range matches {
		if seen[name] {
			continue
		}
		seen[name] = true
		urls = append(urls, academiaPageBase+name)
	}
	return urls
}

func appendUniqueURL(urls []string, url string) []string {
	for _, existing := range urls {
		if existing == url {
			return urls
		}
	}
	return append(urls, url)
}

func (c *CalendarFetcher) fetchPlannerPage(url string) (string, int, error) {
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
	// Use the full session cookie (same as timetable/courses). ExtractCookies-only
	// requests often get 403 on planner pages.
	req.Header.Set("cookie", c.cookie)
	req.Header.Set("Referer", "https://academia.srmist.edu.in/")
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Cache-Control", "public, max-age=3600, stale-while-revalidate=7200")

	if err := globals.HttpClient.Do(req, resp); err != nil {
		return "", 0, err
	}
	return string(resp.Body()), resp.StatusCode(), nil
}

func (c *CalendarFetcher) parseCalendar(html string) (*types.CalendarResponse, error) {
	var htmlText string
	if strings.Contains(html, "<table bgcolor=") {
		htmlText = html
	} else if parts := strings.Split(html, ".sanitize('"); len(parts) >= 2 {
		htmlHex := strings.Split(parts[1], "')")[0]
		htmlText = utils.ConvertHexToHTML(htmlHex)
	} else {
		parts := strings.Split(html, "zmlvalue=\"")
		if len(parts) < 2 {
			log.Printf("CalendarHelper.parseCalendar: invalid HTML format")
			return &types.CalendarResponse{
				Error:    true,
				Message:  "invalid HTML format",
				Status:   500,
				Calendar: []types.CalendarMonth{},
			}, nil
		}
		decodedHTML := utils.ConvertHexToHTML(strings.Split(parts[1], "\" > </div> </div>")[0])
		htmlText = utils.DecodeHTMLEntities(decodedHTML)
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlText))
	if err != nil {
		log.Printf("CalendarHelper.parseCalendar: failed to parse HTML - %v", err)
		return &types.CalendarResponse{
			Error:    true,
			Message:  err.Error(),
			Status:   500,
			Calendar: []types.CalendarMonth{},
		}, nil
	}

	var monthHeaders []string
	doc.Find("th").Each(func(_ int, s *goquery.Selection) {
		month := strings.TrimSpace(s.Text())
		if strings.Contains(month, "'2") {
			monthHeaders = append(monthHeaders, month)
		}
	})

	data := make([]types.CalendarMonth, len(monthHeaders))
	for i := range monthHeaders {
		data[i].Month = monthHeaders[i]
		data[i].Days = make([]types.Day, 0)
	}

	doc.Find("table tr").Each(func(_ int, row *goquery.Selection) {
		tds := row.Find("td")
		for i := range monthHeaders {
			pad := 0
			if i > 0 {
				pad = i * 5
			}

			date := strings.TrimSpace(tds.Eq(pad).Text())
			day := strings.TrimSpace(tds.Eq(pad + 1).Text())
			event := strings.TrimSpace(tds.Eq(pad + 2).Text())
			dayOrder := strings.TrimSpace(tds.Eq(pad + 3).Text())

			if date != "" && dayOrder != "" {
				data[i].Days = append(data[i].Days, types.Day{
					Date:     date,
					Day:      day,
					Event:    event,
					DayOrder: dayOrder,
				})
			}
		}
	})

	sortedData := SortCalendarData(data)

	monthNames := []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
	currentMonthName := monthNames[c.date.Month()-1]

	var monthEntry types.CalendarMonth
	var monthIndex int
	for i, entry := range sortedData {
		if strings.Contains(entry.Month, currentMonthName) {
			monthEntry = entry
			monthIndex = i
			break
		}
	}

	if monthEntry.Month == "" && len(sortedData) > 0 {
		monthEntry = sortedData[0]
		monthIndex = 0
	}

	var today, tomorrow *types.Day
	if len(monthEntry.Days) > 0 {
		todayIndex := c.date.Day() - 1
		if todayIndex >= 0 && todayIndex < len(monthEntry.Days) {
			today = &monthEntry.Days[todayIndex]

			tomorrowIndex := todayIndex + 1
			if tomorrowIndex < len(monthEntry.Days) {
				tomorrow = &monthEntry.Days[tomorrowIndex]
			} else if monthIndex+1 < len(sortedData) && len(sortedData[monthIndex+1].Days) > 0 {
				tomorrow = &sortedData[monthIndex+1].Days[0]
			}
		}
	}

	return &types.CalendarResponse{
		Today:    today,
		Tomorrow: tomorrow,
		Index:    monthIndex,
		Calendar: sortedData,
	}, nil
}

func SortCalendarData(data []types.CalendarMonth) []types.CalendarMonth {
	monthNames := []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}

	monthIndices := make(map[string]int)
	for i, month := range monthNames {
		monthIndices[month] = i
	}

	for i := 0; i < len(data)-1; i++ {
		for j := 0; j < len(data)-i-1; j++ {
			month1 := strings.Split(data[j].Month, "'")[0][:3]
			month2 := strings.Split(data[j+1].Month, "'")[0][:3]

			if monthIndices[month1] > monthIndices[month2] {
				data[j], data[j+1] = data[j+1], data[j]
			}
		}
	}

	for i := range data {
		for j := 0; j < len(data[i].Days)-1; j++ {
			for k := 0; k < len(data[i].Days)-j-1; k++ {
				date1, _ := strconv.Atoi(data[i].Days[k].Date)
				date2, _ := strconv.Atoi(data[i].Days[k+1].Date)
				if date1 > date2 {
					data[i].Days[k], data[i].Days[k+1] = data[i].Days[k+1], data[i].Days[k]
				}
			}
		}
	}

	return data
}
