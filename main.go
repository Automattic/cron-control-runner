package main

import (
	"flag"
	"github.com/Automattic/cron-control-runner/logger"
	"github.com/Automattic/cron-control-runner/metrics"
	"github.com/Automattic/cron-control-runner/orchestrator"
	"github.com/Automattic/cron-control-runner/performer"
	"github.com/Automattic/cron-control-runner/remote"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

type options struct {
	metricsAddress     string
	useMockData        bool
	debug              bool
	wpCLIPath          string
	wpPath             string
	fpmURL             string
	orchestratorConfig orchestrator.Config
	remoteToken        string
	useWebsockets      bool
}

func main() {
	rand.Seed(time.Now().UnixNano())

	// Listen for termination signals.
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	defer signal.Stop(sigChan)

	options := getCliOptions()
	logger := logger.Logger{Logger: log.Default(), DebugMode: options.debug}

	// Setup metrics.
	var metricsManager metrics.Manager
	if options.metricsAddress == "" {
		logger.Infof("Using Mock Metrics")
		metricsManager = metrics.Mock{Log: false}
	} else {
		metricsManager = metrics.NewProm()
		go metrics.StartPromServer(options.metricsAddress)
	}

	// Setup the performer.
	var perf performer.Performer
	if options.useMockData {
		logger.Infof("Using Mock Performer")
		perf = &performer.Mock{UseSleeps: true, LogCommands: false, RotateSites: true}
	} else {
		perf = performer.NewCLI(options.wpCLIPath, options.wpPath, options.fpmURL, metricsManager, logger)
	}

	// Launch the orchestrator.
	orch := orchestrator.New(perf, metricsManager, logger, options.orchestratorConfig)
	defer orch.Close()

	// Setup the remote CLI module if enabled.
	if 0 < len(options.remoteToken) {
		// TODO: This module could definitely use some general refactoring, but namely a graceful shutdown would be good.
		remote.Setup(options.remoteToken, options.useWebsockets, options.wpCLIPath, options.wpPath)
		go remote.ListenForConnections()
	}

	logger.Infof("cron runner has started all processes, running with debug mode set to %t", options.debug)

	// Wait for cancel signal
	sig := <-sigChan
	logger.Infof("caught termination signal %v, starting shutdown", sig)
}

func getCliOptions() options {
	// Set defaults
	options := options{
		metricsAddress: "",
		useMockData:    false,
		debug:          false,
		wpCLIPath:      "/usr/local/bin/wp",
		wpPath:         "/var/www/html",
		fpmURL:         "",
		orchestratorConfig: orchestrator.Config{
			GetSitesInterval:     60 * time.Second,
			GetEventsInterval:    30 * time.Second,
			GetEventsParallelism: 4,
			NumRunWorkers:        8,
			EventBacklog:         0,
		},
		remoteToken:   "",
		useWebsockets: false,
	}

	// General purpose
	flag.StringVar(&(options.metricsAddress), "prom-metrics-address", options.metricsAddress, "Listen address for prometheus metrics (e.g. :4444); if set, can scrape http://:4444/metrics.")
	flag.BoolVar(&(options.useMockData), "use-mock-data", options.useMockData, "use the mock performer for testing")
	flag.BoolVar(&(options.debug), "debug", options.debug, "enables debug mode (extra logging)")

	// Used for the Performer
	flag.StringVar(&(options.wpCLIPath), "wp-cli-path", options.wpCLIPath, "path to WP-CLI binary")
	flag.StringVar(&(options.wpPath), "wp-path", options.wpPath, "path to the WordPress installation")
	flag.StringVar(&(options.fpmURL), "fpm-url", options.fpmURL, "URL for the php-fpm server or socket (e.g. unix:///var/run/fastcgi.sock)")

	// Used for the Orchestrator
	flag.DurationVar(&(options.orchestratorConfig.GetSitesInterval), "get-sites-interval", options.orchestratorConfig.GetSitesInterval, "get-sites interval")
	flag.DurationVar(&(options.orchestratorConfig.GetEventsInterval), "get-events-interval", options.orchestratorConfig.GetEventsInterval, "get-events interval")
	flag.IntVar(&(options.orchestratorConfig.GetEventsParallelism), "get-events-parallelism", options.orchestratorConfig.GetEventsParallelism, "number of get-events calls (across sites) that may run concurrently")
	flag.IntVar(&(options.orchestratorConfig.NumRunWorkers), "num-run-workers", options.orchestratorConfig.NumRunWorkers, "number of run-events workers to run")
	flag.IntVar(&(options.orchestratorConfig.EventBacklog), "event-backlog", options.orchestratorConfig.EventBacklog, "depth of events channel, 0 is unbuffered")

	// Used for remote CLI
	flag.StringVar(&(options.remoteToken), "token", options.remoteToken, "Token to authenticate remote WP CLI requests")
	flag.BoolVar(&(options.useWebsockets), "use-websockets", options.useWebsockets, "Use the websocket listener instead of raw tcp for remote WP CLI requests")

	// NOTE: this will exit if options are invalid or if help is requested, etc.
	flag.Parse()

	if !options.useMockData {
		// Need to do these regardless of fpm because remote.go will still invoke wp-cli directly.
		validatePath(&(options.wpCLIPath), "WP-CLI path")
		validatePath(&(options.wpPath), "WordPress path")
	}

	return options
}

func validatePath(path *string, label string) {
	if len(*path) < 1 {
		log.Printf("Empty path provided for %s\n", label)
		flag.Usage()
		os.Exit(3)
	}

	var err error
	*path, err = filepath.Abs(*path)
	if err != nil {
		log.Printf("Error for %s: %s\n", label, err.Error())
		os.Exit(3)
	}

	if _, err = os.Stat(*path); os.IsNotExist(err) {
		log.Printf("Error for %s: '%s' does not exist\n", label, *path)
		flag.Usage()
		os.Exit(3)
	}
}
