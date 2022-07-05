package gva

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"strconv"

	"github.com/PuerkitoBio/goquery"
	"github.com/pkg/errors"
)

// MassShootingRecord is a single record within a mass shootings record table.
type MassShootingRecord struct {
	IncidentID   int
	IncidentDate Date
	State        string
	CityCounty   string
	Address      string
	NoKilled     int
	NoInjured    int
	IncidentURL  string
	SourceURL    string
}

// MassShootings scrapes for the list of mass shootings.
func (s *Scraper) MassShootings(ctx context.Context, page int) ([]MassShootingRecord, error) {
	doc, err := s.getHTML(ctx, "/reports/mass-shooting", url.Values{
		"page": {strconv.Itoa(page)},
	})
	if err != nil {
		return nil, err
	}

	records := make([]MassShootingRecord, 0, 25)

	table := doc.Find(`#block-system-main table`)
	if table == nil {
		return nil, errors.New("cannot find HTML table")
	}

	trs := table.Find("tbody > tr")
	trs.Each(func(_ int, tr *goquery.Selection) {
		td := tr.Find("td")
		tdData := selectTexts(td)
		if len(tdData) < 8 {
			err = fmt.Errorf("table got %d column, expected 8", len(tdData))
			return
		}

		operations := td.Find("li > a")
		operationsHref := selectHrefs(operations)
		if len(operationsHref) < 2 {
			err = fmt.Errorf("table's operations cell got %d links, expected 2", len(operationsHref))
			return
		}

		var (
			incidentID, _   = strconv.Atoi(tdData[0])
			incidentDate, _ = ParseDate(tdData[1], TZ)
			state           = tdData[2]
			cityCounty      = tdData[3]
			address         = tdData[4]
			noKilled, _     = strconv.Atoi(tdData[5])
			noInjured, _    = strconv.Atoi(tdData[6])
			incidentURL     = s.BaseURL.String() + operationsHref[0]
			sourceURL       = operationsHref[1]
		)

		records = append(records, MassShootingRecord{
			IncidentID:   incidentID,
			IncidentDate: incidentDate,
			State:        state,
			CityCounty:   cityCounty,
			Address:      address,
			NoKilled:     noKilled,
			NoInjured:    noInjured,
			IncidentURL:  incidentURL,
			SourceURL:    sourceURL,
		})

	})

	return records, err
}

// MassShootingsToday returns a list of mass shooting records for today.
func (s *Scraper) MassShootingsToday(ctx context.Context) ([]MassShootingRecord, error) {
	return s.MassShootingsOnDate(ctx, Today())
}

// MassShootingsOnDate returns a list of mass shooting records for the given
// date.
func (s *Scraper) MassShootingsOnDate(ctx context.Context, date Date) ([]MassShootingRecord, error) {
	return MassShootingsOnDate(func(i int) ([]MassShootingRecord, error) {
		return s.MassShootings(ctx, i)
	}, date)
}

func MassShootingsOnDate(f func(i int) ([]MassShootingRecord, error), date Date) ([]MassShootingRecord, error) {
	records := make([]MassShootingRecord, 0, 25)
	log.Println("today =", date)

	for i := 0; true; i++ {
		page, err := f(i)
		if err != nil {
			return nil, errors.Wrapf(err, "cannot get page %d", i)
		}

		if len(page) == 0 {
			// No records in the page. Exit.
			break
		}

		for _, record := range page {
			if record.IncidentDate.Eq(date) {
				records = append(records, record)
			}
		}

		if i == 0 && page[0].IncidentDate.Before(date) {
			// The latest incident happened before today, so there's nothing
			// today.
			break
		}

		if len(records) > 0 && !page[len(page)-1].IncidentDate.Eq(date) {
			// The last record does not match the date and we already have
			// records, meaning that the date only gets older after this.
			// Break out of the loop; there's nothing else.
			break
		}
	}

	return records, nil
}
