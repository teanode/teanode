package cron

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/teanode/teanode/internal/config"
	"github.com/teanode/teanode/internal/util/atomicfile"
)

// Store provides thread-safe persistence for cron jobs.
type Store struct {
	path  string
	mutex sync.Mutex
}

// NewStore creates a Store that persists to ~/.teanode/crons.json.
func NewStore() (*Store, error) {
	path, err := config.CronsFile()
	if err != nil {
		return nil, err
	}
	return &Store{path: path}, nil
}

// Load reads all cron jobs from the store file.
// Returns empty slice if the file doesn't exist.
func (self *Store) Load() ([]CronJob, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return self.loadLocked()
}

func (self *Store) loadLocked() ([]CronJob, error) {
	data, err := os.ReadFile(self.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading crons file: %w", err)
	}

	var cronData cronFile
	if err := json.Unmarshal(data, &cronData); err != nil {
		return nil, fmt.Errorf("parsing crons file: %w", err)
	}
	return cronData.Jobs, nil
}

// Save writes the full job list to disk atomically.
func (self *Store) Save(jobs []CronJob) error {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return self.saveLocked(jobs)
}

func (self *Store) saveLocked(jobs []CronJob) error {
	cronData := cronFile{Version: 1, Jobs: jobs}
	data, err := json.MarshalIndent(cronData, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling crons: %w", err)
	}
	return atomicfile.WriteFile(self.path, data)
}

// Create appends a new job and saves.
func (self *Store) Create(job CronJob) error {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	jobs, err := self.loadLocked()
	if err != nil {
		return err
	}
	jobs = append(jobs, job)
	return self.saveLocked(jobs)
}

// Update replaces a job by ID and saves.
func (self *Store) Update(job CronJob) error {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	jobs, err := self.loadLocked()
	if err != nil {
		return err
	}
	found := false
	for i, existing := range jobs {
		if existing.ID == job.ID {
			jobs[i] = job
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("job not found: %s", job.ID)
	}
	return self.saveLocked(jobs)
}

// Delete removes a job by ID and saves.
func (self *Store) Delete(id string) error {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	jobs, err := self.loadLocked()
	if err != nil {
		return err
	}
	filtered := jobs[:0]
	found := false
	for _, existing := range jobs {
		if existing.ID == id {
			found = true
			continue
		}
		filtered = append(filtered, existing)
	}
	if !found {
		return fmt.Errorf("job not found: %s", id)
	}
	return self.saveLocked(filtered)
}
