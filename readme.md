# Cron Control Runner

A Go-based runner for processing WordPress cron events, via [Cron Control](https://github.com/Automattic/Cron-Control) interfaces.

## Installation & Usage

1. Clone the repo, and cd into the repo directory.
1. Build the binary for the target machine, example: `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o bin/cron-control-runner main.go`
1. Optionally move the binary to where you want it.
1. Run it!

```
cron-control-runner -debug -wp-path="/var/www/html" -wp-cli-path="/usr/local/bin/wp"
```

If you would just like to test the runner locally, can do a simpler process and even feed it mock data:

```
cd path/to/cloned/repo/cron-control-runner
go run . -use-mock-data -debug
```

## Runner Options
- `-debug`
	- enables debug mode (extra logging)
- `-wp-cli-path` string
	- path to WP-CLI binary (default "/usr/local/bin/wp")
- `-wp-path` string
	- path to the WordPress installation (default "/var/www/html")
- `-get-sites-interval` duration
	- interval where sites are fetched (default 60s)
- `-get-events-interval` duration
	- interval where events are fetched (default 30s)
- `-get-events-parallelism` int
	- number of get-events calls (across sites) that may run concurrently (default 4)
- `-num-run-workers` int
	- number of run-events workers to run (default 8)
- `-event-backlog` int
	- depth of events channel, 0 (default) is unbuffered
- `-fpm-url` string
	- URL for the php-fpm server or socket (e.g. unix:///var/run/fastcgi.sock)
- `-prom-metrics-address` string
	- Listen address for prometheus metrics (e.g. :4444); if set, can scrape http://:4444/metrics.
- `-use-mock-data`
	- use the mock performer for testing

## Architecture

![runner diagram](https://d.pr/i/1THmhI+)

### main.go
This is the entrypoint for the CLI, it sets everything up.

### orchestrator
The Orchestrator is responsible for managing the event queue & related runners. Controls event fetchers and runners, then gracefully closes down processes when needed.

Sites are fetched at specific intervals (`get-sites-interval`). Note that the list of sites each batch host is responsible for can vary on large multisites. For each site that the runner is managing, a new goroutine (siteWatcher) is opened up that fetches events at an interval (`get-events-interval`) and pushes them into the events queue. Only X (`get-events-parallelism`) amount of sites can fetch events at once to help balance the load out.

Events are processed as soon as possible once in the queue. There are Y (`num-run-workers`) event workers able to process at once.

### performer

The Performer is an abstraction layer that does the real interaction w/ sites. The orchestrator is unaware how getEvents/runEvents is happening, it just calls on the performer to do the dirty work. This allows the event fetching and running to be done through various means such as WP CLI or REST API, and provides easy mocking. Currently there are just two implementations here: the `Mock` performer (helps feed test data into the orchestrator while testing), and the `CLI` performer (runs the real cron-control CLIs).

### metrics & logger

Helpers for logging and tracking metrics.

### remote

Functionality supporting the Remote WP CLI feature.

This went mostly untouched in the latest refactor, aside from a few globals variable tweaking and linting fixes. Perhaps could take another look later down the road into cleaning up / refactoring this piece. But for now, it's preferable to not move this stone at the same time.

## Metrics

If you enable the metrics system and endpoint by providing the `-prom-metrics-address` arg, then you will get the following metrics for performance monitoring:

```
cron_control_runner_get_sites_latency_seconds_bucket{status="success|failure",le="..."}
cron_control_runner_get_sites_latency_seconds_count{status="success|failure"}
cron_control_runner_get_sites_latency_seconds_sum{status="success|failure"}

cron_control_runner_get_site_events_events_received_total{site="https://your.site.url"}
cron_control_runner_get_site_events_latency_seconds_bucket{site="https://your.site.url",status="success|failure",le="..."}
cron_control_runner_get_site_events_latency_seconds_count{site="https://your.site.url",status="success|failure"}
cron_control_runner_get_site_events_latency_seconds_sum{site="https://your.site.url",status="success|failure"}

cron_control_runner_run_event_latency_seconds_bucket{reason="ok|error",site_url="https://your.site.url",status="success|failure",le="..."}
cron_control_runner_run_event_latency_seconds_count{reason="ok|error",site_url="https://your.site.url",status="success|failure"}
cron_control_runner_run_event_latency_seconds_sum{reason="ok|error",site_url="https://your.site.url",status="success|failure"}

cron_control_runner_run_worker_all_busy_hits
cron_control_runner_run_worker_busy_pct
cron_control_runner_run_worker_state_count{state="busy"}
cron_control_runner_run_worker_state_count{state="idle"}
cron_control_runner_run_worker_state_count{state="max"}

// TODO: insert RecordFpmTiming() metrics.
```
