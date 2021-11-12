package performer

import (
	"log"
	"time"
)

// Mock is a fake performer, gives example data back. Useful for testing orchestrator changes.
type Mock struct {
	UseSleeps   bool
	LogCommands bool
	RotateSites bool
}

var rotation = 0

// GetSites fetches a mocked list of sites.
func (perf Mock) GetSites(_ time.Duration) (Sites, error) {
	if perf.UseSleeps {
		// mock remote calltime
		time.Sleep(1 * time.Second)
	}

	if perf.LogCommands {
		log.Printf("wp cron-control orchestrate sites list")
	}

	var slice []string
	siteURLs := []string{
		"example1.com", "example2.com", "example3.com", "example4.com", "example5.com",
		"example6.com", "example7.com", "example8.com", "example9.com", "example10.com",
	}

	if perf.RotateSites {
		// Simulate sites changing. Though uncommon, good to test w/ it.
		rotation = rotation + 1
		if rotation > 2 {
			rotation = 0
		}
	}

	if rotation == 0 {
		slice = siteURLs[0:4]
	} else if rotation == 1 {
		slice = siteURLs[3:7]
	} else if rotation == 2 {
		slice = siteURLs[5:9]
	}

	sitesToReturn := make(Sites)
	for _, url := range slice {
		sitesToReturn[url] = Site{URL: url}
	}

	return sitesToReturn, nil
}

// GetEvents returns a mocked list of events.
func (perf Mock) GetEvents(site Site) ([]Event, error) {
	if perf.UseSleeps {
		time.Sleep(2 * time.Second)
	}

	if perf.LogCommands {
		log.Printf("wp cron-control orchestrate runner-only list-due-batch --url=%s", site.URL)
	}

	return []Event{
		{Action: "action1", URL: site.URL},
		{Action: "action2", URL: site.URL},
		{Action: "action3", URL: site.URL},
		{Action: "action4", URL: site.URL},
		{Action: "action5", URL: site.URL},
	}, nil
}

// RunEvent mocks the running of an event.
func (perf Mock) RunEvent(event Event) error {
	if perf.UseSleeps {
		time.Sleep(5 * time.Second)
	}

	if perf.LogCommands {
		log.Printf("wp cron-control orchestrate runner-only run %s --url=%s", event.Action, event.URL)
	}

	return nil
}
