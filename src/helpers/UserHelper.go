package helpers

import (
	"goscraper/src/types"
	"goscraper/src/utils"
	"log"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

func GetUser(rawPage string) (*types.User, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(rawPage))
	if err != nil {
		log.Printf("UserHelper.GetUser: failed to parse HTML - %v", err)
		return &types.User{}, nil
	}

	table := doc.Find(`table[border="0"][align="left"][cellpadding="1"][cellspacing="1"]`).First()
	if table.Length() == 0 {
		log.Printf("UserHelper.GetUser: user info table not found")
		return &types.User{}, nil
	}

	data := &types.User{}

	re := regexp.MustCompile(`RA2\d{12}`)
	regNumber := re.FindString(rawPage)

	if data.RegNumber == "" {
		data.RegNumber = regNumber
	}

	data.Year = getYear(data.RegNumber)

	table.Find("tr").Each(func(i int, row *goquery.Selection) {
		cells := row.Find("td")
		for i := 0; i < cells.Length(); i += 2 {
			key := cells.Eq(i).Text()
			key = strings.TrimSuffix(key, ":")
			value := cells.Eq(i + 1).Text()

			switch key {
			case "Name":
				data.Name = value
			case "Program":
				data.Program = value
			case "Combo / Batch":
				data.Batch = cells.Eq(i + 1).Find("font").Text()
			case "Mobile":
				data.Mobile = value
			case "Semester":
				data.Semester = utils.ParseInt(value)
			case "Department":
				arr := strings.Split(value, "-")
				data.Department = strings.TrimSpace(arr[0])
				if len(arr) > 1 {
					section := strings.TrimSpace(arr[1])
					section = strings.TrimPrefix(section, "(")
					section = strings.TrimSuffix(section, " Section)")
					data.Section = section
				}
			}
		}
	})

	return data, nil
}
