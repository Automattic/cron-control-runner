package performer

import (
	"cron-control-runner/logger"
	"cron-control-runner/metrics"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"time"

	"github.com/yookoala/gofast"
)

// CLI uses the CLI interface for site interactions.
// Don't initialize directly, use NewCLI()
type CLI struct {
	wpCLIPath string
	wpPath    string
	metrics   metrics.Manager
	logger    logger.Logger
	useFPM    bool
	fpm       gofast.ClientFactory
}

type siteInfo struct {
	Multisite int    `json:"multisite"`
	SiteURL   string `json:"siteurl"`
	Disabled  int    `json:"disabled"`
}

// NewCLI sets up the CLI Performer w/ special initializations.
func NewCLI(wpCLIPath string, wpPath string, fpmURL string, metrics metrics.Manager, logger logger.Logger) *CLI {
	rand.Seed(time.Now().UnixNano())

	performer := &CLI{
		wpCLIPath: wpCLIPath,
		wpPath:    wpPath,
		metrics:   metrics,
		logger:    logger,
	}

	if fpmURL != "" {
		var err error
		parsedURL, err := url.Parse(fpmURL)
		if err != nil || parsedURL == nil || parsedURL.Scheme != "unix" || parsedURL.Path == "" {
			logger.Errorf("problem parsing FPM url %q: %v", fpmURL, err)
			panic(err)
		}
		logger.Infof("Using FPM runtime at %q", parsedURL)
		performer.fpm = gofast.SimpleClientFactory(gofast.SimpleConnFactory(parsedURL.Scheme, parsedURL.Path))
		performer.useFPM = true
	}

	return performer
}

// GetSites fetches a list of sites this instance is responsible for tracking via CLI.
// Returns an empty instance of Sites on error.
func (perf CLI) GetSites(hbInterval time.Duration) (Sites, error) {
	sites := make(Sites)

	siteInfo, err := perf.getSiteInfo()
	if err != nil {
		// Can't reach the site or get valid info, so abort w/ empty list.
		return sites, err
	}

	// 0 = enabled, 1 = enabled, otherwise timestamp when execution will resume.
	if siteInfo.Disabled != 0 {
		// Runner is disabled on the WP-side, no sites to track currently.
		perf.logger.Debugf("runner is disabled")
		return sites, nil
	}

	if siteInfo.Multisite == 1 {
		// For multisites, we need to perform the heartbeat to make the site aware which batch hosts exist.
		// This allows WP to split up large numbers of sites between different hosts.
		// TODO: Just do this within the "sites list" CLI instead?
		hbInSeconds := hbInterval / time.Second
		perf.runWpCmd([]string{"cron-control", "orchestrate", "sites", "heartbeat", fmt.Sprintf("--heartbeat-interval=%d", hbInSeconds)})

		return perf.getMultisiteSites()
	}

	// Mock for single site
	sites[siteInfo.SiteURL] = Site{URL: siteInfo.SiteURL}
	return sites, nil
}

func (perf CLI) getMultisiteSites() (Sites, error) {
	sites := make(Sites)

	raw, err := perf.runWpCmd([]string{"cron-control", "orchestrate", "sites", "list"})
	if err != nil {
		return sites, err
	}

	jsonRes := make([]Site, 0)
	if err = json.Unmarshal([]byte(raw), &jsonRes); err != nil {
		return sites, err
	}

	// Shuffle the order, helping keep GetEvents(site) calls spread out differently in multiple-container setups.
	rand.Shuffle(len(jsonRes), func(i, j int) { jsonRes[i], jsonRes[j] = jsonRes[j], jsonRes[i] })

	for _, site := range jsonRes {
		sites[site.URL] = Site{URL: site.URL}
	}

	return sites, nil
}

func (perf CLI) getSiteInfo() (siteInfo, error) {
	raw, err := perf.runWpCmd([]string{"cron-control", "orchestrate", "runner-only", "get-info", "--format=json"})
	if err != nil {
		return siteInfo{}, err
	}

	jsonRes := make([]siteInfo, 0)
	if err = json.Unmarshal([]byte(raw), &jsonRes); err != nil {
		return siteInfo{}, err
	}

	return jsonRes[0], nil
}

// GetEvents returns a list of events for a particular site via CLI.
func (perf CLI) GetEvents(site Site) ([]Event, error) {
	var emptyEvents []Event

	raw, err := perf.runWpCmd([]string{"cron-control", "orchestrate", "runner-only", "list-due-batch", fmt.Sprintf("--url=%s", site.URL), "--queue-window=0", "--format=json"})
	if err != nil {
		return emptyEvents, err
	}

	siteEvents := make([]Event, 0)
	if err = json.Unmarshal([]byte(raw), &siteEvents); err != nil {
		return emptyEvents, err
	}

	for idx := range siteEvents {
		siteEvents[idx].URL = site.URL
	}

	return siteEvents, nil
}

// RunEvent runs an event via CLI.
func (perf CLI) RunEvent(event Event) error {
	command := []string{"cron-control", "orchestrate", "runner-only", "run", fmt.Sprintf("--timestamp=%d", event.Timestamp),
		fmt.Sprintf("--action=%s", event.Action), fmt.Sprintf("--instance=%s", event.Instance), fmt.Sprintf("--url=%s", event.URL)}

	_, err := perf.runWpCmd(command)
	return err
}

func (perf CLI) runWpCmd(command []string) (string, error) {
	// `--quiet`` included to prevent WP-CLI commands from generating invalid JSON
	command = append(command, "--allow-root", "--quiet", fmt.Sprintf("--path=%s", perf.wpPath))

	if perf.useFPM {
		t0 := time.Now()
		result, err := perf.processCommandWithFPM(command)
		perf.metrics.RecordFpmTiming(err == nil, time.Since(t0))
		return result, err
	}

	// Non-FPM CLI, useful for local dev-env setups.
	return perf.processCommand(command)
}

func (perf CLI) processCommand(command []string) (string, error) {
	var stdout, stderr strings.Builder

	wpCli := exec.Command(perf.wpCLIPath, command...)
	wpCli.Stdout = &stdout
	wpCli.Stderr = &stderr

	err := wpCli.Run()
	wpOutStr := stdout.String()
	if stderr.Len() > 0 {
		if stderrStr := strings.TrimSpace(stderr.String()); len(stderrStr) > 0 {
			perf.logger.Errorf("STDERR for command[%s]: %s", strings.Join(command, " "), stderrStr)
		}
	}

	if err != nil {
		return wpOutStr, err
	}

	return wpOutStr, nil
}

type fakeHTTPResponseWriter struct {
	Dest       io.Writer
	LastStatus int
	Headers    http.Header
}

func (f *fakeHTTPResponseWriter) Header() http.Header {
	if f.Headers == nil {
		f.Headers = http.Header{}
	}
	return f.Headers
}

func (f *fakeHTTPResponseWriter) Write(bytes []byte) (int, error) {
	return f.Dest.Write(bytes)
}

func (f *fakeHTTPResponseWriter) WriteHeader(statusCode int) {
	f.LastStatus = statusCode
}

func (perf CLI) processCommandWithFPM(subcommand []string) (string, error) {
	fpmClient, err := perf.fpm()
	if err != nil {
		return "", err
	}
	defer (func() { _ = fpmClient.Close() })()

	args, err := perf.buildFpmQuery(subcommand)
	if err != nil {
		return "", err
	}

	fcgiReq := gofast.NewRequest(nil)
	fcgiReq.Params = map[string]string{
		"REQUEST_METHOD":    "GET",
		"SCRIPT_FILENAME":   "/var/wpvip/fpm-cron-runner.php",
		"GATEWAY_INTERFACE": "FastCGI/1.0",
		"QUERY_STRING":      args.Encode(),
	}

	fcgiResp, err := fpmClient.Do(fcgiReq)
	if err != nil {
		return "", err
	}
	defer fcgiResp.Close()

	stdErr := &strings.Builder{}
	stdOut := &strings.Builder{}
	hrw := &fakeHTTPResponseWriter{Dest: stdOut}

	if err = fcgiResp.WriteTo(hrw, stdErr); err != nil {
		return "", err
	}

	stdOutStr := stdOut.String()
	if hrw.LastStatus != http.StatusOK {
		return "", fmt.Errorf("fpm error: lastStatus=%d, headers=%v, stdout=%q, stderr=%q", hrw.LastStatus, hrw.Headers, stdOutStr, stdErr.String())
	}

	var res struct {
		Buf    string `json:"buf"`
		Stdout string `json:"stdout"`
		Stderr string `json:"stderr"`
	}

	err = json.Unmarshal([]byte(stdOutStr), &res)

	if err != nil {
		if idx := strings.Index(stdOutStr, `[{"url":`); idx >= 0 {
			var urls []struct {
				URL string `json:"url"`
			}
			// if a site manages to escape the output buffering, this will try to find the output anyways...
			err = json.NewDecoder(strings.NewReader(stdOutStr[idx:])).Decode(&urls)
			if err == nil {
				var buf []byte
				buf, err = json.Marshal(&urls)
				res.Buf = string(buf)
			}
		}
	}

	if err != nil {
		return "", fmt.Errorf("fpm error: could not decode json response from %q: err=%v", stdOutStr, err)
	}

	return res.Buf, err
}

func (perf CLI) buildFpmQuery(subcommand []string) (url.Values, error) {
	jsonBytes, err := json.Marshal(subcommand)
	if err != nil {
		return nil, err
	}
	return url.Values{
		"payload": []string{string(jsonBytes)},
	}, nil
}
