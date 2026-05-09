package handlers

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

const spAttendanceURL = "https://sp.srmist.edu.in/srmiststudentportal/students/report/studentAttendanceDetails.jsp"

type SPAttendanceCourse struct {
	Code        string  `json:"code"`
	Description string  `json:"description"`
	MaxHours    int     `json:"maxHours"`
	AttHours    int     `json:"attHours"`
	AbsentHours int     `json:"absentHours"`
	Percentage  float64 `json:"percentage"`
}

type SPAttendanceMonthly struct {
	Period  string `json:"period"`
	Present int    `json:"present"`
	Absent  int    `json:"absent"`
}

type SPAttendanceResponse struct {
	Period       string                `json:"period"`
	Courses      []SPAttendanceCourse  `json:"courses"`
	Monthly      []SPAttendanceMonthly `json:"monthly"`
	HoursPresent int                   `json:"hoursPresent"`
	HoursAbsent  int                   `json:"hoursAbsent"`
	NewCookies   string                `json:"-"`
	Expired      bool                  `json:"-"`
}

var spAttendancePeriodRe = regexp.MustCompile(`<b>([^<]+)</b>\s*To\s*<b>([^<]+)</b>`)

func GetSPAttendance(spCookies string) (*SPAttendanceResponse, error) {
	body, newCookies, expired, err := spPostForm(
		spAttendanceURL,
		"iden=9&filter=&hdnFormDetails=1&csrfPreventionSalt=",
		spCookies,
	)
	if err != nil {
		return nil, err
	}
	if expired {
		return &SPAttendanceResponse{Expired: true, NewCookies: newCookies}, nil
	}

	html := string(body)
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, fmt.Errorf("sp attendance: parse html: %w", err)
	}

	res := &SPAttendanceResponse{
		Courses: []SPAttendanceCourse{},
		Monthly: []SPAttendanceMonthly{},
	}

	if m := spAttendancePeriodRe.FindStringSubmatch(html); m != nil {
		res.Period = strings.TrimSpace(m[1]) + " to " + strings.TrimSpace(m[2])
	}

	doc.Find("table").Each(func(_ int, table *goquery.Selection) {
		firstHeader := strings.TrimSpace(table.Find("thead th").First().Text())
		switch firstHeader {
		case "Code":
			parseAttendanceCourses(table, res)
		case "Month / Year":
			parseAttendanceMonthly(table, res)
		}
	})

	for _, c := range res.Courses {
		res.HoursPresent += c.AttHours
		res.HoursAbsent += c.AbsentHours
	}

	res.NewCookies = newCookies
	return res, nil
}

func parseAttendanceCourses(table *goquery.Selection, res *SPAttendanceResponse) {
	table.Find("tbody tr").Each(func(_ int, tr *goquery.Selection) {
		cells := tr.Find("td")
		if cells.Length() < 6 {
			return
		}
		max, _ := strconv.Atoi(strings.TrimSpace(cells.Eq(2).Text()))
		att, _ := strconv.Atoi(strings.TrimSpace(cells.Eq(3).Text()))
		abs, _ := strconv.Atoi(strings.TrimSpace(cells.Eq(4).Text()))
		pct, _ := strconv.ParseFloat(strings.TrimSpace(cells.Eq(5).Text()), 64)
		res.Courses = append(res.Courses, SPAttendanceCourse{
			Code:        strings.TrimSpace(cells.Eq(0).Text()),
			Description: strings.TrimSpace(cells.Eq(1).Text()),
			MaxHours:    max,
			AttHours:    att,
			AbsentHours: abs,
			Percentage:  pct,
		})
	})
}

func parseAttendanceMonthly(table *goquery.Selection, res *SPAttendanceResponse) {
	table.Find("tbody tr").Each(func(_ int, tr *goquery.Selection) {
		cells := tr.Find("td")
		if cells.Length() < 3 {
			return
		}
		present, _ := strconv.Atoi(strings.TrimSpace(cells.Eq(1).Text()))
		absent, _ := strconv.Atoi(strings.TrimSpace(cells.Eq(2).Text()))
		res.Monthly = append(res.Monthly, SPAttendanceMonthly{
			Period:  strings.TrimSpace(cells.Eq(0).Text()),
			Present: present,
			Absent:  absent,
		})
	})
}
