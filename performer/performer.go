package performer

import (
	"crypto/sha1"
	"encoding/base32"
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

func (e Event) LockKey() string {
	hs := sha1.Sum([]byte(e.String()))
	return base32.StdEncoding.EncodeToString(hs[:])
}

func (e Event) Time() time.Time {
	return time.Unix(int64(e.Timestamp), 0)
}

// A Site in a WP install.
type Site struct {
	URL string `json:"url"`
}

// Sites is a map of siteurl => Site
type Sites = map[string]Site

func (s *Site) LockKey() string {
	hs := sha1.Sum([]byte(s.URL))
	return base32.StdEncoding.EncodeToString(hs[:])
}
