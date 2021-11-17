package locker

import (
	"encoding/json"
	"fmt"
	"github.com/Automattic/cron-control-runner/logger"
	"github.com/bradfitz/gomemcache/memcache"
	"os"
	"sort"
	"time"
)

var _ Locker = &memcacheLocker{}

type memcacheLocker struct {
	Log             logger.Logger
	KeyPrefix       string
	ConfigPath      string
	ServerList      *memcache.ServerList
	Client          *memcache.Client
	LeaseInterval   time.Duration
	RefreshInterval time.Duration
	CloseChan       chan struct{}
}

func NewMemcache(log logger.Logger, keyPrefix, configPath string, leaseInterval, refreshInterval time.Duration) Locker {
	res := &memcacheLocker{
		KeyPrefix:       keyPrefix,
		Log:             log,
		ConfigPath:      configPath,
		ServerList:      &memcache.ServerList{},
		LeaseInterval:   leaseInterval,
		RefreshInterval: refreshInterval,
		CloseChan:       make(chan struct{}),
	}
	res.Client = memcache.NewFromSelector(res.ServerList)
	res.tryReloadConfig()
	go res.runConfigReloader()
	return res
}

func (m *memcacheLocker) Close() error {
	close(m.CloseChan)
	return nil
}

var myHostname = []byte((func() string {
	hostname, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return hostname
})())

func (m *memcacheLocker) Lock(id string) (Lock, error) {
	item := &memcache.Item{
		Key:        fmt.Sprintf("%s%s", m.KeyPrefix, id),
		Value:      myHostname, // we put the hostname of the lock owner as the value of the key
		Flags:      0,
		Expiration: int32(m.LeaseInterval.Seconds()),
	}
	if err := m.Client.Add(item); err == nil {
		// success!
		lock := &memcacheLock{
			Item:      item,
			Owner:     m,
			CloseChan: make(chan struct{}),
		}
		go lock.runKeepalive()
		return lock, nil
	} else if err == memcache.ErrNotStored {
		return nil, ErrAlreadyLocked
	} else {
		return nil, nil
	}
}

var _ Lock = &memcacheLock{}

type memcacheLock struct {
	Item      *memcache.Item
	Owner     *memcacheLocker
	CloseChan chan struct{}
}

func (m *memcacheLock) Unlock() {
	close(m.CloseChan) // stop keepalive thread
}

func (m *memcacheLock) runKeepalive() {
	ticker := time.NewTicker(m.Owner.LeaseInterval / 2)
	defer ticker.Stop()
	defer (func() {
		if err := m.Owner.Client.Delete(m.Item.Key); err != nil && err != memcache.ErrCacheMiss {
			m.Owner.Log.Errorf("failed to release memcache lock on %q: %v", m.Item.Key, err)
		}
	})()
	for {
		select {
		case <-m.CloseChan:
			return
		case <-ticker.C:
			// time to extend our lease!
			m.Owner.Log.Debugf("extending memcache lease on %q", m.Item.Key)
			if err := m.Owner.Client.Touch(m.Item.Key, m.Item.Expiration); err == memcache.ErrCacheMiss {
				m.Owner.Log.Errorf("failed to extend memcache lease on %q: %v", m.Item.Key, err)
				return
			}
		}
	}
}

func (m *memcacheLocker) runConfigReloader() {
	ticker := time.NewTicker(m.RefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-m.CloseChan:
			return
		case <-ticker.C:
			m.tryReloadConfig()
		}
	}
}

func (m *memcacheLocker) tryReloadConfig() {
	m.Log.Debugf("reloading memcache config")
	if err := m.reloadConfig(); err != nil {
		m.Log.Errorf("failed to reload memcache config: %v", err)
	}
}

func (m *memcacheLocker) reloadConfig() error {
	f, err := os.Open(m.ConfigPath)
	if err != nil {
		return err
	}
	defer (func() { _ = f.Close() })()
	var dc DataConfig
	if err = json.NewDecoder(f).Decode(&dc); err != nil {
		return err
	}
	servers := make([]string, len(dc.Memcache))
	for i, mcs := range dc.Memcache {
		servers[i] = fmt.Sprintf("%s:%d", mcs.Host, mcs.Port)
	}
	sort.Strings(servers)
	if err = m.ServerList.SetServers(servers...); err != nil {
		return err
	}
	return nil
}

type DataConfig struct {
	Memcache []MemcacheClientConfig `json:"memcache"`
}

type MemcacheClientConfig struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}
