package watcher

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/ziyan/teanode/internal/logging"
	"github.com/ziyan/teanode/internal/util/deferutil"
)

var log = logging.Get("watcher")

const debounceInterval = 500 * time.Millisecond

// Watcher monitors the teanode data directory for file changes and triggers reload callbacks.
type Watcher struct {
	directory   string
	stopChannel chan struct{}

	OnConfigReload func() // called when config.json changes
	OnSkillsReload func() // called when skills/*.json changes
	OnCronsReload  func() // called when crons.json changes
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

	// Watch the root directory (for config.json, crons.json).
	if err := notifier.Add(self.directory); err != nil {
		notifier.Close()
		return err
	}

	// Watch the skills directory if it exists.
	skillsDir := filepath.Join(self.directory, "skills")
	if info, err := os.Stat(skillsDir); err == nil && info.IsDir() {
		if err := notifier.Add(skillsDir); err != nil {
			log.Warningf("cannot watch skills dir: %v", err)
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
			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 {
				continue
			}

			name := filepath.Base(event.Name)
			eventDirectory := filepath.Dir(event.Name)

			if name == "config.json" && eventDirectory == self.directory {
				log.Infof("config.json changed, scheduling reload")
				debounce("config", self.OnConfigReload)
			} else if name == "crons.json" && eventDirectory == self.directory {
				log.Infof("crons.json changed, scheduling reload")
				debounce("crons", self.OnCronsReload)
			} else if strings.HasSuffix(name, ".json") && eventDirectory == filepath.Join(self.directory, "skills") {
				log.Infof("skills changed (%s), scheduling reload", name)
				debounce("skills", self.OnSkillsReload)
			}

		case err, ok := <-notifier.Errors:
			if !ok {
				return
			}
			log.Errorf("watcher error: %v", err)
		}
	}
}
