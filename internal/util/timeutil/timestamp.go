package timeutil

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const localTimestampLayout = "2006-01-02T15:04:05.000-07:00"

// Timestamp stores a time value serialized as ISO datetime in local timezone.
// It accepts legacy unix timestamps (seconds or milliseconds) when decoding.
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
	number, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid timestamp: %s", trimmed)
	}
	self.Time = fromUnix(number)
	return nil
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
	switch node.Tag {
	case "!!int":
		number, err := strconv.ParseInt(strings.TrimSpace(node.Value), 10, 64)
		if err != nil {
			return fmt.Errorf("invalid timestamp integer: %w", err)
		}
		self.Time = fromUnix(number)
		return nil
	default:
		parsed, err := Parse(node.Value)
		if err != nil {
			return err
		}
		self.Time = parsed.Time
		return nil
	}
}

func Parse(value string) (Timestamp, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return Timestamp{}, nil
	}
	if number, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
		return Timestamp{Time: fromUnix(number)}, nil
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

func fromUnix(value int64) time.Time {
	abs := value
	if abs < 0 {
		abs = -abs
	}
	if abs >= 1_000_000_000_000 {
		return time.UnixMilli(value).In(time.Local)
	}
	return time.Unix(value, 0).In(time.Local)
}
