package timeutil

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const localTimestampLayout = "2006-01-02T15:04:05.000-07:00"

// Timestamp stores a time value serialized as ISO datetime in local timezone.
type Timestamp struct {
	time.Time
}

func Now() Timestamp {
	return Timestamp{Time: time.Now()}
}

func (self Timestamp) String() string {
	if self.Time.IsZero() {
		return ""
	}
	return self.Time.In(time.Local).Format(localTimestampLayout)
}

func (self Timestamp) MarshalJSON() ([]byte, error) {
	if self.Time.IsZero() {
		return []byte("null"), nil
	}
	return json.Marshal(self.String())
}

func (self *Timestamp) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		self.Time = time.Time{}
		return nil
	}
	if len(trimmed) > 1 && trimmed[0] == '"' && trimmed[len(trimmed)-1] == '"' {
		var value string
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		parsed, err := Parse(value)
		if err != nil {
			return err
		}
		self.Time = parsed.Time
		return nil
	}
	return fmt.Errorf("invalid timestamp: %s", trimmed)
}

func (self Timestamp) MarshalYAML() (interface{}, error) {
	if self.Time.IsZero() {
		return nil, nil
	}
	return self.String(), nil
}

func (self *Timestamp) UnmarshalYAML(node *yaml.Node) error {
	if node == nil || node.Value == "" || node.Tag == "!!null" {
		self.Time = time.Time{}
		return nil
	}
	parsed, err := Parse(node.Value)
	if err != nil {
		return err
	}
	self.Time = parsed.Time
	return nil
}

func Parse(value string) (Timestamp, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return Timestamp{}, nil
	}
	layouts := []string{
		localTimestampLayout,
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, trimmed); err == nil {
			return Timestamp{Time: parsed.In(time.Local)}, nil
		}
	}
	return Timestamp{}, fmt.Errorf("invalid timestamp: %q", value)
}
