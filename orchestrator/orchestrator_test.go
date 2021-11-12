package orchestrator

import (
	"reflect"
	"testing"
)

func TestCheckForSiteDiffs(t *testing.T) {
	sitesToWatch, sitesToClose := checkForSiteDiffs(fakeSiteWatchers(), fakeRetrieveSites())

	expectedSitesToWatch := make(sites)
	sitesExpected := []string{"example4.com", "example5.com", "example6.com"}
	for _, url := range sitesExpected {
		expectedSitesToWatch[url] = site{URL: url}
	}

	if len(sitesToWatch) != len(sitesExpected) {
		t.Fatalf("checkForSiteDiffs() did not return an expected numbers of sites to watch")
	}
	for url, site := range sitesToWatch {
		if !reflect.DeepEqual(site, expectedSitesToWatch[url]) {
			t.Fatalf("checkForSiteDiffs() did not return an expected site to watch %s", site)
		}
	}

	expectedSitesToClose := []string{"example1.com"}
	if len(sitesToClose) != len(expectedSitesToClose) {
		t.Fatalf("checkForSiteDiffs() did not return an expected numbers of sites to close")
	}
	for _, url := range sitesToClose {
		if !stringInSlice(url, expectedSitesToClose) {
			t.Fatalf("checkForSiteDiffs() did not return an expected site to close %s", url)
		}
	}
}

func fakeRetrieveSites() sites {
	fakeSites := make(sites)

	sitesArr := []site{{URL: "example2.com"}, {URL: "example3.com"}, {URL: "example4.com"}, {URL: "example5.com"}, {URL: "example6.com"}}
	for _, site := range sitesArr {
		fakeSites[site.URL] = site
	}

	return fakeSites
}

func fakeSiteWatchers() threadTracker {
	watchedSites := make(threadTracker)

	watchedSites["example1.com"] = make(chan struct{})
	watchedSites["example2.com"] = make(chan struct{})
	watchedSites["example3.com"] = make(chan struct{})

	return watchedSites
}

func stringInSlice(a string, list []string) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}
