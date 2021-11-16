package performer

import (
	"fmt"
	"time"
)

// Performer is responsible for the real interaction w/ sites.
// It does the event fetching and running, be that through wp cli, php-fpm, or rest apis.
type Performer interface {
	IsReady() bool
	GetSites(time.Duration) (Sites, error)
	GetEvents(site Site) ([]Event, error)
	RunEvent(event Event) error
}

// The Event recieved and run from the WP site.
type Event struct {
	URL       string
	Timestamp int    `json:"timestamp"`
	Action    string `json:"action"`
	Instance  string `json:"instance"`
}

func (e Event) String() string {
	return fmt.Sprintf("event(url=%q, ts=%d, action=%q, instance=%q)", e.URL, e.Timestamp, e.Action, e.Instance)
}

// A Site in a WP install.
type Site struct {
	URL string `json:"url"`
}

// Sites is a map of siteurl => Site
type Sites = map[string]Site
