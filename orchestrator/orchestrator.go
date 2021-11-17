package orchestrator

import (
	"fmt"
	"github.com/Automattic/cron-control-runner/locker"
	"github.com/Automattic/cron-control-runner/logger"
	"github.com/Automattic/cron-control-runner/metrics"
	"github.com/Automattic/cron-control-runner/performer"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"gopkg.in/tomb.v2"
)

// Orchestrator is responsible for managing the event queue & related runners.
// It adds events, runs them, and gracefully closes down processes when needed.
type Orchestrator struct {
	events           chan performer.Event
	config           Config
	performer        performer.Performer
	metrics          metrics.Manager
	logger           logger.Logger
	locker           locker.Locker
	tomb             tomb.Tomb
	semGetEvents     chan bool
	busyEventWorkers int32
}

// Config helps callers set up the orchestrator.
type Config struct {
	GetSitesInterval     time.Duration
	GetEventsInterval    time.Duration
	GetEventsParallelism int
	NumRunWorkers        int
	EventBacklog         int
}

// Easier internal access
type sites = performer.Sites
type site = performer.Site
type event = performer.Event

type threadTracker map[string]chan struct{}

// New sets up a new orchestrator based on the passed configurations
func New(perf performer.Performer, metrics metrics.Manager, logger logger.Logger, locker locker.Locker, config Config) *Orchestrator {
	config = sanitizeConfig(config)
	orchestrator := &Orchestrator{
		events:       make(chan event, config.EventBacklog),
		config:       config,
		performer:    perf,
		metrics:      metrics,
		logger:       logger,
		locker:       locker,
		semGetEvents: make(chan bool, config.GetEventsParallelism),
	}

	// Kick things off!
	orchestrator.tomb.Go(orchestrator.setupWatchers)
	orchestrator.tomb.Go(orchestrator.setupRunners)

	return orchestrator
}

// Close the orchestrator processes.
func (orch *Orchestrator) Close() error {
	orch.logger.Infof("orchestrator is beginning shutdown")
	orch.tomb.Kill(nil)
	orch.tomb.Wait()
	orch.logger.Infof("orchestrator has completed shutdown")
	return nil
}

/*
 * The watching is comprised of two main steps:
 *   1) Every "getSites" interval, request sites we need to track.
 *   2) For every site we are responsible for tracking, spin up a site watcher.
 */
func (orch *Orchestrator) setupWatchers() error {
	for !orch.performer.IsReady() {
		select {
		case <-orch.tomb.Dying():
			orch.logger.Infof("orchestrator is shutting down, performer was never ready")
			return nil
		case <-time.After(1 * time.Second):
			orch.logger.Debugf("testing performer readiness")
		}
	}

	watchedSites := make(threadTracker)
	defer closeTrackedThreads(watchedSites)

	// Kick off the process right away the first time around.
	watchedSites = orch.manageSiteWatchers(watchedSites)

	// Setup interval where we'll check for site changes.
	ticker := time.NewTicker(orch.config.GetSitesInterval)
	defer ticker.Stop()

	for {
		select {
		case <-orch.tomb.Dying():
			orch.logger.Infof("orchestrator is shutting down, stopping watchers")
			// return from the outer function, triggers defer(s) to cleanup.
			return nil
		case <-ticker.C:
			watchedSites = orch.manageSiteWatchers(watchedSites)
		}
	}
}

// Starts up watchers for new sites, removes watchers for unlisted sites.
func (orch *Orchestrator) manageSiteWatchers(watchedSites threadTracker) threadTracker {
	t0 := time.Now()
	fetchedSites, err := orch.performer.GetSites(orch.config.GetSitesInterval)
	duration := time.Since(t0)

	if err != nil {
		// Record the error but continue on no matter what, as empty sites will propogate soft shutdowns automatically.
		// Watchers will be closed below, and the event queue will empty out itself.
		orch.logger.Errorf("getSites: could not get sites list; err: %v", err)
		orch.metrics.RecordGetSites(false, duration)
	} else {
		orch.metrics.RecordGetSites(true, duration)
	}

	sitesToWatch, sitesToClose := checkForSiteDiffs(watchedSites, fetchedSites)

	// If the site no longer exists, send the close signal and stop tracking.
	for _, siteURL := range sitesToClose {
		orch.logger.Debugf("closing thread %q", siteURL)
		close(watchedSites[siteURL])
		delete(watchedSites, siteURL)
	}

	// Start up individual goroutines per each site we need to be watching.
	for _, site := range sitesToWatch {
		orch.logger.Debugf("starting thread %q", site.URL)
		closeChan := make(chan struct{})
		watchedSites[site.URL] = closeChan
		go orch.startSiteWatcher(site, closeChan)
	}

	return watchedSites
}

/*
 * Site watchers poll for new events every "getEvents" interval, and push them into the events queue.
 *
 * These are gated by a semaphore as a means of rate limiting. This helps ensure that:
 * A) we don't spam the DB too hard all at once
 * B) the events that were fetched won't grow super stale while waiting to be pushed into the events queue
 */
func (orch *Orchestrator) startSiteWatcher(site site, close chan struct{}) {
	initialDelay := time.Duration(rand.Int63()) % orch.config.GetEventsInterval
	orch.logger.Infof("starting watcher for site %+v, initial delay: %v", site, initialDelay)
	select {
	case <-close:
		return // closed while waiting for initial delay
	case <-time.After(initialDelay):
		orch.logger.Debugf("initial delay has elapsed for %+v, starting to run events", site)
	}

	ticker := time.NewTicker(orch.config.GetEventsInterval)
	defer ticker.Stop()

	// initial fetch, does not wait for ticker's first firing.
	select {
	case <-close:
		return // closed while waiting for semaphore
	case orch.semGetEvents <- true:
		// we have acquired the semaphore
		orch.fetchSiteEvents(site, close)
	}

	for {
		select {
		case <-close:
			return // closed while waiting for ticker
		case <-ticker.C:
			select {
			case <-close:
				return // closed while waiting for semaphore
			case orch.semGetEvents <- true:
				// we have acquired the semaphore
				orch.fetchSiteEvents(site, close)
			}
		}
	}
}

func (orch *Orchestrator) fetchSiteEvents(site site, close chan struct{}) {
	defer (func() {
		<-orch.semGetEvents
	})()

	t0 := time.Now()
	events, err := orch.performer.GetEvents(site)
	if err != nil {
		orch.logger.Errorf("getEvents: could not get events for site %s; err=%v", site.URL, err)
	} else {
		orch.logger.Debugf("getEvents: got %d events for site %s", len(events), site.URL)
	}

	orch.metrics.RecordGetSiteEvents(err == nil, time.Since(t0), site.URL, len(events))

	for _, event := range events {
		select {
		case <-close:
			return // closed while waiting for space in event queue
		case orch.events <- event:
			continue // noop, event is sent!
		}
	}
}

/*
 * Runners are much more simple. If there is an event in the queue, run it!
 */
func (orch *Orchestrator) setupRunners() error {
	wg := &sync.WaitGroup{}
	wg.Add(orch.config.NumRunWorkers)

	activeRunners := make(threadTracker)
	for w := 1; w <= orch.config.NumRunWorkers; w++ {
		id := fmt.Sprintf("worker-%d", w)
		closeChan := make(chan struct{})
		activeRunners[id] = closeChan
		go orch.startEventRunner(id, closeChan, wg)
	}

	<-orch.tomb.Dying()
	orch.logger.Infof("orchestrator is shutting down, stopping runners")
	closeTrackedThreads(activeRunners)
	wg.Wait() // Wait for all runners to gracefully close out.
	return nil
}

func (orch *Orchestrator) startEventRunner(workerID string, close chan struct{}, wg *sync.WaitGroup) {
	defer wg.Done()
	defer orch.logger.Infof("shutdown %q gracefully", workerID)

	for {
		select {
		case <-close:
			return // closed while waiting for event
		case runnableEvent := <-orch.events:
			select {
			case <-close:
				return // higher priority close in case both channels were available
			default: // noop
			}

			var lock locker.Lock
			var err error
			if orch.locker != nil {
				if lock, err = orch.locker.Lock(runnableEvent.LockKey()); err != nil {
					// if there is an error, we continue as if it is not locked:
					orch.logger.Warningf("error locking event %v: %v", runnableEvent, err)
					orch.metrics.RecordLockEvent("error")
				} else if lock == nil {
					// already locked, move on:
					orch.metrics.RecordLockEvent("already_locked")
					continue
				} else {
					// we got a lock:
					orch.metrics.RecordLockEvent("locked")
				}
			}

			// Worker is now busy.
			orch.metrics.RecordRunWorkerStats(atomic.AddInt32(&(orch.busyEventWorkers), 1), int32(orch.config.NumRunWorkers))

			t0 := time.Now()
			err = orch.performer.RunEvent(runnableEvent)
			duration := time.Since(t0)

			if lock != nil {
				if unlockErr := lock.Unlock(); unlockErr != nil {
					orch.logger.Warningf("failed to unlock event %v: %v", runnableEvent, unlockErr)
				}
			}

			if err != nil {
				orch.logger.Errorf("runEvents: %s could not run event %v after %v; err=%v", workerID, runnableEvent, duration, err)
				orch.metrics.RecordRunEvent(false, duration, runnableEvent.URL, "error")
			} else {
				if duration > time.Second*30 {
					orch.logger.Warningf("runEvents: %s ran job %v for a long time (%v)", workerID, runnableEvent, duration)
				} else {
					orch.logger.Debugf("runEvents: %s finished job %v after %v", workerID, runnableEvent, duration)
				}
				orch.metrics.RecordRunEvent(true, duration, runnableEvent.URL, "ok")
			}

			// Worker is back to idle.
			orch.metrics.RecordRunWorkerStats(atomic.AddInt32(&(orch.busyEventWorkers), -1), int32(orch.config.NumRunWorkers))
		}
	}
}

func checkForSiteDiffs(watchedSites threadTracker, newSites sites) (sitesToAdd sites, sitesToRemove []string) {
	sitesToAdd = make(sites)

	for _, newSite := range newSites {
		if _, ok := watchedSites[newSite.URL]; !ok {
			sitesToAdd[newSite.URL] = newSite
		}
	}

	for watchedSite := range watchedSites {
		if _, ok := newSites[watchedSite]; !ok {
			sitesToRemove = append(sitesToRemove, watchedSite)
		}
	}

	return sitesToAdd, sitesToRemove
}

// Quickly send close signals to all tracked threads.
func closeTrackedThreads(activeThreads threadTracker) {
	for threadID, closeChan := range activeThreads {
		close(closeChan)
		delete(activeThreads, threadID)
	}
}

func sanitizeConfig(config Config) Config {
	if config.GetSitesInterval < (10 * time.Second) {
		config.GetSitesInterval = 10 * time.Second
	}

	if config.GetEventsInterval < (1 * time.Second) {
		config.GetEventsInterval = 1 * time.Second
	}

	if config.GetEventsParallelism < 1 {
		config.GetEventsParallelism = 1
	}

	if config.NumRunWorkers < 1 {
		config.NumRunWorkers = 1
	}

	if config.EventBacklog < 0 {
		config.EventBacklog = 0
	}

	return config
}
