package index

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/diamondburned/mass-shootings-count/cmd/mass-shootings-count/frontend"
	"github.com/diamondburned/mass-shootings-count/gva"
	"github.com/diamondburned/mass-shootings-count/internal/watcher"
	"github.com/diamondburned/tmplutil"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/timewasted/go-accept-headers"
)

var index = frontend.Template.Register("index", "index/index.html")

type handler struct {
	renderedData *watcher.Watcher[RenderData]
}

const watchFlags = watcher.WatchAllowStale

const Day = 24 * time.Hour

type cachedScraper struct {
	scraper *gva.Scraper
	records [][]gva.MassShootingRecord
}

func cacheScraper(scraper *gva.Scraper) *cachedScraper {
	return &cachedScraper{
		scraper: scraper,
	}
}

func (s *cachedScraper) MassShootings(ctx context.Context, i int) ([]gva.MassShootingRecord, error) {
	if len(s.records) < i {
		return s.records[i], nil
	}

	records, err := s.scraper.MassShootings(ctx, i)
	if err != nil {
		return nil, err
	}

	if i == len(s.records) {
		s.records = append(s.records, records)
	}

	return records, nil
}

func Mount(scraper *gva.Scraper) http.Handler {
	renderedData := watcher.Watch(2*time.Minute, watchFlags, func() (RenderData, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		scraper := cacheScraper(scraper)

		records, err := scraper.MassShootings(ctx, 0)
		if err != nil {
			return RenderData{}, err
		}

		if len(records) == 0 {
			return RenderData{}, errors.New("no records found")
		}

		recordsToday, err := gva.MassShootingsOnDate(ctx, scraper, gva.Today())
		if err != nil {
			return RenderData{}, err
		}

		now := time.Now()
		latestIncident := records[0].IncidentDate.AsTime(now)

		// Require a full day without any incidents in order for it to be
		// counted.
		days := int(now.Sub(latestIncident)/Day) - 1
		if days < 0 {
			days = 0
		}

		return RenderData{
			Days:        days,
			Records:     recordsToday,
			LastUpdated: time.Now(),
		}, nil
	})

	h := handler{
		renderedData: renderedData,
	}

	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(tmplutil.AlwaysFlush)
		r.Use(middleware.NoCache)
		r.Get("/", h.index)
	})

	// Preload.
	h.renderedData.Get()

	return r
}

type RenderData struct {
	Days        int
	Records     []gva.MassShootingRecord
	LastUpdated time.Time
}

func (h handler) index(w http.ResponseWriter, r *http.Request) {
	accepting, _ := accept.Negotiate(r.Header.Get("accept"),
		"text/html",
		"application/json",
	)

	switch accepting {
	case "application/json":
		w.Header().Set("Content-Type", "application/json")
		h.indexJSON(w, r)
	case "text/html":
		fallthrough
	default:
		w.Header().Set("Content-Type", "text/html")
		h.indexHTML(w, r)
	}
}

func (h handler) indexHTML(w http.ResponseWriter, r *http.Request) {
	var responseData struct {
		RenderData
		TotalInjured int
		TotalKilled  int
		Refresh      int // seconds
	}

	var err error

	responseData.RenderData, err = h.renderedData.Get()
	if err != nil {
		frontend.ErrorHTML(w, 503, err)
		return
	}

	q := r.URL.Query()
	if refresh, err := strconv.Atoi(q.Get("refresh")); err == nil {
		responseData.Refresh = refresh
	}

	for _, rec := range responseData.Records {
		responseData.TotalKilled += rec.NoKilled
		responseData.TotalInjured += rec.NoInjured
	}

	index.Execute(w, responseData)
}

func (h handler) indexJSON(w http.ResponseWriter, r *http.Request) {
	renderData, err := h.renderedData.Get()
	if err != nil {
		errorJSON(w, 503, err)
		return
	}

	if err := json.NewEncoder(w).Encode(renderData); err != nil {
		errorJSON(w, 500, err)
	}
}

func errorJSON(w http.ResponseWriter, code int, err error) {
	var resp struct {
		Error string
	}

	resp.Error = err.Error()

	w.WriteHeader(code)
	json.NewEncoder(w).Encode(resp)
}
