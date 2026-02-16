// Package watcher provides debounced filesystem change notifications.
package watcher

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/op/go-logging"
	"github.com/teanode/teanode/internal/util/deferutil"
)

var log = logging.MustGetLogger("watcher")

const debounceInterval = 500 * time.Millisecond

// Watcher monitors the teanode data directory for file changes and triggers reload callbacks.
type Watcher struct {
	directory   string
	stopChannel chan struct{}

	OnConfigReload func() // called when config.yaml changes
	OnSkillsReload func() // called when skills/*.yaml changes
	OnJobsReload   func() // called when jobs/*.md changes
	OnAgentsReload func() // called when agents/*/config.yaml changes
}

// New creates a new Watcher for the given data directory.
func New(directory string) *Watcher {
	return &Watcher{
		directory:   directory,
		stopChannel: make(chan struct{}),
	}
}

// Start begins watching for file changes. Non-blocking.
func (self *Watcher) Start() error {
	notifier, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	// Watch the root directory (for config.yaml).
	if err := notifier.Add(self.directory); err != nil {
		notifier.Close()
		return err
	}

	// Watch the skills directory if it exists.
	skillsDirectory := filepath.Join(self.directory, "skills")
	if info, err := os.Stat(skillsDirectory); err == nil && info.IsDir() {
		if err := notifier.Add(skillsDirectory); err != nil {
			log.Warningf("cannot watch skills dir: %v", err)
		}
	}

	// Watch the jobs directory if it exists.
	jobsDirectory := filepath.Join(self.directory, "jobs")
	if info, err := os.Stat(jobsDirectory); err == nil && info.IsDir() {
		if err := notifier.Add(jobsDirectory); err != nil {
			log.Warningf("cannot watch jobs dir: %v", err)
		}
	}

	// Watch the agents directory and each agent subdirectory.
	agentsDirectory := filepath.Join(self.directory, "agents")
	if info, err := os.Stat(agentsDirectory); err == nil && info.IsDir() {
		if err := notifier.Add(agentsDirectory); err != nil {
			log.Warningf("cannot watch agents dir: %v", err)
		}
		entries, err := os.ReadDir(agentsDirectory)
		if err == nil {
			for _, entry := range entries {
				if entry.IsDir() {
					subdir := filepath.Join(agentsDirectory, entry.Name())
					if err := notifier.Add(subdir); err != nil {
						log.Warningf("cannot watch agent subdir %s: %v", entry.Name(), err)
					}
				}
			}
		}
	}

	go self.run(notifier)
	log.Infof("file watcher started on %s", self.directory)
	return nil
}

// Stop halts the watcher.
func (self *Watcher) Stop() {
	close(self.stopChannel)
}

func (self *Watcher) run(notifier *fsnotify.Watcher) {
	defer deferutil.Recover()
	defer notifier.Close()

	var mutex sync.Mutex
	timers := make(map[string]*time.Timer) // category → debounce timer

	debounce := func(category string, callback func()) {
		if callback == nil {
			return
		}
		mutex.Lock()
		defer mutex.Unlock()
		if timer, ok := timers[category]; ok {
			timer.Reset(debounceInterval)
			return
		}
		timers[category] = time.AfterFunc(debounceInterval, func() {
			defer deferutil.Recover()
			mutex.Lock()
			delete(timers, category)
			mutex.Unlock()
			callback()
		})
	}

	for {
		select {
		case <-self.stopChannel:
			return
		case event, ok := <-notifier.Events:
			if !ok {
				return
			}
			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename|fsnotify.Remove) == 0 {
				continue
			}

			name := filepath.Base(event.Name)
			eventDirectory := filepath.Dir(event.Name)
			agentsDirectory := filepath.Join(self.directory, "agents")

			if name == "config.yaml" && eventDirectory == self.directory {
				log.Infof("config.yaml changed, scheduling reload")
				debounce("config", self.OnConfigReload)
			} else if strings.HasSuffix(name, ".md") && eventDirectory == filepath.Join(self.directory, "jobs") {
				log.Infof("job changed (%s), scheduling reload", name)
				debounce("jobs", self.OnJobsReload)
			} else if strings.HasSuffix(name, ".yaml") && eventDirectory == filepath.Join(self.directory, "skills") {
				log.Infof("skills changed (%s), scheduling reload", name)
				debounce("skills", self.OnSkillsReload)
			} else if name == "config.yaml" && strings.HasPrefix(eventDirectory, agentsDirectory+string(filepath.Separator)) {
				log.Infof("agent config changed (%s), scheduling reload", filepath.Base(eventDirectory))
				debounce("agents", self.OnAgentsReload)
			} else if eventDirectory == agentsDirectory && event.Op&fsnotify.Create != 0 {
				// New agent subdirectory created — start watching it.
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					if err := notifier.Add(event.Name); err != nil {
						log.Warningf("cannot watch new agent subdir %s: %v", name, err)
					}
				}
			} else if eventDirectory == agentsDirectory && event.Op&fsnotify.Remove != 0 {
				// Agent subdirectory removed — trigger reload.
				debounce("agents", self.OnAgentsReload)
			}

		case err, ok := <-notifier.Errors:
			if !ok {
				return
			}
			log.Errorf("watcher error: %v", err)
		}
	}
}
