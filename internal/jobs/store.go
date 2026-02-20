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
// Each job is stored as a markdown file at ~/.teanode/jobs/<jobId>.md
// with YAML frontmatter for metadata and the message as the body.
type Store struct {
	directory string
	mutex     sync.Mutex
}

// NewStore creates a Store that persists to ~/.teanode/jobs/.
func NewStore() (*Store, error) {
	directory, err := configs.JobsDirectory()
	if err != nil {
		return nil, err
	}
	return &Store{directory: directory}, nil
}

// Load reads all jobs from the jobs directory.
// Returns empty slice if the directory doesn't exist or is empty.
func (self *Store) Load() ([]Job, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return self.loadLocked()
}

func (self *Store) loadLocked() ([]Job, error) {
	entries, err := os.ReadDir(self.directory)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading jobs directory: %w", err)
	}

	var jobs []Job
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".md")
		job, err := self.readJobFile(id)
		if err != nil {
			log.Errorf("reading job file %s: %v", entry.Name(), err)
			continue
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}

// Save writes the full job list to disk, replacing all existing files.
func (self *Store) Save(jobs []Job) error {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return self.saveLocked(jobs)
}

func (self *Store) saveLocked(jobs []Job) error {
	trashDirectory, err := configs.TrashDirectory()
	if err != nil {
		return err
	}

	// Remove all existing .md files.
	entries, err := os.ReadDir(self.directory)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading jobs directory: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
			if err := trash.Move(filepath.Join(self.directory, entry.Name()), trashDirectory); err != nil {
				return fmt.Errorf("moving old job file to trash: %w", err)
			}
		}
	}

	// Write each job as a separate file.
	for _, job := range jobs {
		if err := self.writeJobFile(job); err != nil {
			return err
		}
	}
	return nil
}

// Create writes a new job file.
func (self *Store) Create(job Job) error {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return self.writeJobFile(job)
}

// Update replaces a job file by ID.
func (self *Store) Update(job Job) error {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	path := filepath.Join(self.directory, job.ID+".md")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("job not found: %s", job.ID)
	}
	return self.writeJobFile(job)
}

// Delete removes a job file by ID.
func (self *Store) Delete(id string) error {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	path := filepath.Join(self.directory, id+".md")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("job not found: %s", id)
	}
	trashDirectory, err := configs.TrashDirectory()
	if err != nil {
		return err
	}
	return trash.Move(path, trashDirectory)
}

// readJobFile parses a single job markdown file.
func (self *Store) readJobFile(id string) (Job, error) {
	data, err := os.ReadFile(filepath.Join(self.directory, id+".md"))
	if err != nil {
		return Job{}, err
	}
	return parseJobMarkdown(id, data)
}

// writeJobFile writes a single job as a markdown file with YAML frontmatter.
func (self *Store) writeJobFile(job Job) error {
	data := formatJobMarkdown(job)
	return atomicfile.WriteFile(filepath.Join(self.directory, job.ID+".md"), data)
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
