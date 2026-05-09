package handlers

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

const spMarksURL = "https://sp.srmist.edu.in/srmiststudentportal/students/report/studentInternalMarkDetails.jsp"

type SPMark struct {
	Code        string  `json:"code"`
	Description string  `json:"description"`
	Obtained    float64 `json:"obtained"`
	Max         float64 `json:"max"`
	SubjectID   string  `json:"subjectId,omitempty"`
}

type SPMarksResponse struct {
	Marks      []SPMark `json:"marks"`
	NewCookies string   `json:"-"`
	Expired    bool     `json:"-"`
}

// onclick="funViewComponentWiseMarks('42255', '21CSE313P', 'ACCELERATED DATA SCIENCE',2)"
var spComponentMarksOnclickRe = regexp.MustCompile(`funViewComponentWiseMarks\(\s*'([^']*)'\s*,\s*'([^']*)'\s*,\s*'([^']*)'\s*,\s*(\d+)\s*\)`)

func GetSPMarks(spCookies string) (*SPMarksResponse, error) {
	body, newCookies, expired, err := spPostForm(
		spMarksURL,
		"iden=13&filter=&hdnFormDetails=1&csrfPreventionSalt=",
		spCookies,
	)
	if err != nil {
		return nil, err
	}
	if expired {
		return &SPMarksResponse{Expired: true, NewCookies: newCookies}, nil
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("sp marks: parse html: %w", err)
	}

	marks := make([]SPMark, 0)
	doc.Find("table tbody tr").Each(func(_ int, row *goquery.Selection) {
		cells := row.Find("td")
		if cells.Length() < 3 {
			return
		}
		code := strings.TrimSpace(cells.Eq(0).Text())
		desc := strings.TrimSpace(cells.Eq(1).Text())
		markCell := strings.TrimSpace(cells.Eq(2).Text())

		if code == "" {
			return
		}

		obtained, max := splitMarkCell(markCell)

		mark := SPMark{
			Code:        code,
			Description: desc,
			Obtained:    obtained,
			Max:         max,
		}

		if cells.Length() >= 4 {
			onclick, _ := cells.Eq(3).Find("button").Attr("onclick")
			if m := spComponentMarksOnclickRe.FindStringSubmatch(onclick); m != nil {
				mark.SubjectID = m[1]
			}
		}

		marks = append(marks, mark)
	})

	return &SPMarksResponse{Marks: marks, NewCookies: newCookies}, nil
}

// splitMarkCell parses "53.20 / 60.00" into (53.20, 60.00). Bad input → zeros.
func splitMarkCell(s string) (float64, float64) {
	parts := strings.SplitN(s, "/", 2)
	obtained, _ := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	var max float64
	if len(parts) == 2 {
		max, _ = strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	}
	return obtained, max
}
