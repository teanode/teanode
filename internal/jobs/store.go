package jobs

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/util/atomicfile"
	"github.com/teanode/teanode/internal/util/trash"
	"gopkg.in/yaml.v3"
)

// Store provides thread-safe persistence for scheduled jobs.
// Each job is stored as a markdown file at ~/.teanode/users/<userId>/jobs/<jobId>.md
// with YAML frontmatter for metadata and the message as the body.
type Store struct {
	usersDirectory string
	mutex          sync.Mutex
}

// NewStore creates a Store that persists under ~/.teanode/users/*/jobs/.
func NewStore() (*Store, error) {
	usersDirectory := configs.UsersDirectory()
	return &Store{usersDirectory: usersDirectory}, nil
}

// Load reads all jobs from the jobs directory.
// Returns empty slice if the directory doesn't exist or is empty.
func (self *Store) Load() ([]Job, error) {
	ownedJobs, err := self.LoadOwned()
	if err != nil {
		return nil, err
	}
	jobs := make([]Job, 0, len(ownedJobs))
	for _, ownedJob := range ownedJobs {
		jobs = append(jobs, ownedJob.Job)
	}
	return jobs, nil
}

// LoadOwned reads all jobs and keeps owner information from users/*/jobs/.
func (self *Store) LoadOwned() ([]OwnedJob, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return self.loadOwnedLocked()
}

func (self *Store) loadOwnedLocked() ([]OwnedJob, error) {
	users, err := os.ReadDir(self.usersDirectory)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading users directory: %w", err)
	}

	var jobs []OwnedJob
	for _, userEntry := range users {
		if !userEntry.IsDir() {
			continue
		}
		userId := userEntry.Name()
		userJobs, err := self.loadUserJobsLocked(userId)
		if err != nil {
			log.Errorf("reading jobs for user %s: %v", userId, err)
			continue
		}
		jobs = append(jobs, userJobs...)
	}
	return jobs, nil
}

func (self *Store) loadUserJobsLocked(userId string) ([]OwnedJob, error) {
	directory, err := self.userJobsDirectory(userId)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(directory)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading jobs directory for user %s: %w", userId, err)
	}

	var jobs []Job
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".md")
		job, err := self.readJobFile(directory, id)
		if err != nil {
			log.Errorf("reading job file %s for user %s: %v", entry.Name(), userId, err)
			continue
		}
		jobs = append(jobs, job)
	}
	ownedJobs := make([]OwnedJob, 0, len(jobs))
	for _, job := range jobs {
		ownedJobs = append(ownedJobs, OwnedJob{UserID: userId, Job: job})
	}
	return ownedJobs, nil
}

// Save writes the full job list to disk, replacing all existing files.
func (self *Store) Save(jobs []OwnedJob) error {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return self.saveLocked(jobs)
}

func (self *Store) saveLocked(jobs []OwnedJob) error {
	trashDirectory := configs.TrashDirectory()

	// Remove all existing job markdown files across users/*/jobs/.
	existingJobFiles, err := self.listAllJobFilesLocked()
	if err != nil {
		return err
	}
	for _, path := range existingJobFiles {
		if err := trash.Move(path, trashDirectory); err != nil {
			return fmt.Errorf("moving old job file to trash: %w", err)
		}
	}

	// Write each job as a separate file.
	for _, ownedJob := range jobs {
		if err := self.writeJobFile(ownedJob.UserID, ownedJob.Job); err != nil {
			return err
		}
	}
	return nil
}

// Create writes a new job file.
func (self *Store) Create(userId string, job Job) error {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if strings.TrimSpace(userId) == "" {
		return fmt.Errorf("userId is required")
	}
	return self.writeJobFile(userId, job)
}

// Update replaces a job file by ID.
func (self *Store) Update(userId string, job Job) error {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if strings.TrimSpace(userId) == "" {
		return fmt.Errorf("userId is required")
	}

	directory, err := self.userJobsDirectory(userId)
	if err != nil {
		return err
	}
	path := filepath.Join(directory, job.ID+".md")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("job not found: %s", job.ID)
	}
	return self.writeJobFile(userId, job)
}

// Delete removes a job file by ID.
func (self *Store) Delete(userId, id string) error {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if strings.TrimSpace(userId) == "" {
		return fmt.Errorf("userId is required")
	}

	directory, err := self.userJobsDirectory(userId)
	if err != nil {
		return err
	}
	path := filepath.Join(directory, id+".md")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("job not found: %s", id)
	}
	trashDirectory := configs.TrashDirectory()
	return trash.Move(path, trashDirectory)
}

// readJobFile parses a single job markdown file.
func (self *Store) readJobFile(directory, id string) (Job, error) {
	data, err := os.ReadFile(filepath.Join(directory, id+".md"))
	if err != nil {
		return Job{}, err
	}
	return parseJobMarkdown(id, data)
}

// writeJobFile writes a single job as a markdown file with YAML frontmatter.
func (self *Store) writeJobFile(userId string, job Job) error {
	directory, err := self.userJobsDirectory(userId)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(directory, 0755); err != nil {
		return err
	}
	data := formatJobMarkdown(job)
	return atomicfile.WriteFile(filepath.Join(directory, job.ID+".md"), data)
}

func (self *Store) userJobsDirectory(userId string) (string, error) {
	return configs.UserJobsDirectory(userId), nil
}

func (self *Store) listAllJobFilesLocked() ([]string, error) {
	users, err := os.ReadDir(self.usersDirectory)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading users directory: %w", err)
	}

	var paths []string
	for _, userEntry := range users {
		if !userEntry.IsDir() {
			continue
		}
		directory, err := self.userJobsDirectory(userEntry.Name())
		if err != nil {
			return nil, err
		}
		entries, err := os.ReadDir(directory)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("reading jobs directory for user %s: %w", userEntry.Name(), err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}
			paths = append(paths, filepath.Join(directory, entry.Name()))
		}
	}
	return paths, nil
}

// parseJobMarkdown parses a markdown file with YAML frontmatter into a Job.
func parseJobMarkdown(id string, data []byte) (Job, error) {
	content := string(data)

	// Expect: ---\n<yaml>\n---\n<message>
	if !strings.HasPrefix(content, "---\n") {
		return Job{}, fmt.Errorf("missing frontmatter delimiter")
	}

	// Find the closing ---
	rest := content[4:] // skip opening "---\n"
	closingIndex := strings.Index(rest, "\n---\n")
	if closingIndex < 0 {
		// Try trailing --- at end of file (no message body).
		if strings.HasSuffix(rest, "\n---") {
			closingIndex = len(rest) - 4
		} else {
			return Job{}, fmt.Errorf("missing closing frontmatter delimiter")
		}
	}

	frontmatterYAML := rest[:closingIndex]
	message := ""
	bodyStart := closingIndex + 5 // len("\n---\n")
	if bodyStart <= len(rest) {
		message = strings.TrimSpace(rest[bodyStart:])
	}

	var frontmatter jobFrontmatter
	if err := yaml.Unmarshal([]byte(frontmatterYAML), &frontmatter); err != nil {
		return Job{}, fmt.Errorf("parsing frontmatter: %w", err)
	}

	return frontmatter.toJob(id, message), nil
}

// formatJobMarkdown formats a Job as a markdown file with YAML frontmatter.
func formatJobMarkdown(job Job) []byte {
	frontmatter := toFrontmatter(job)
	yamlData, _ := yaml.Marshal(frontmatter)

	var buffer bytes.Buffer
	buffer.WriteString("---\n")
	buffer.Write(yamlData)
	buffer.WriteString("---\n\n")
	buffer.WriteString(job.Message)
	buffer.WriteString("\n")
	return buffer.Bytes()
}
