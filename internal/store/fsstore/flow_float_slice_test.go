package fsstore

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestFlowFloatSliceMarshalYAML(t *testing.T) {
	type wrapper struct {
		Values flowFloatSlice `yaml:"values,omitempty"`
	}
	data := wrapper{Values: flowFloatSlice{0.1, 0.2, 0.3}}
	encoded, err := yaml.Marshal(&data)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	output := string(encoded)
	if !strings.Contains(output, "[0.1, 0.2, 0.3]") {
		t.Errorf("expected flow-style [0.1, 0.2, 0.3], got:\n%s", output)
	}
}

func TestFlowFloatSliceUnmarshalFlowStyle(t *testing.T) {
	input := "values: [0.1, 0.2, 0.3]\n"
	type wrapper struct {
		Values flowFloatSlice `yaml:"values,omitempty"`
	}
	var result wrapper
	if err := yaml.Unmarshal([]byte(input), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(result.Values) != 3 || result.Values[0] != 0.1 || result.Values[1] != 0.2 || result.Values[2] != 0.3 {
		t.Errorf("expected [0.1 0.2 0.3], got %v", result.Values)
	}
}

func TestFlowFloatSliceUnmarshalBlockStyle(t *testing.T) {
	input := "values:\n- 0.1\n- 0.2\n- 0.3\n"
	type wrapper struct {
		Values flowFloatSlice `yaml:"values,omitempty"`
	}
	var result wrapper
	if err := yaml.Unmarshal([]byte(input), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(result.Values) != 3 || result.Values[0] != 0.1 || result.Values[1] != 0.2 || result.Values[2] != 0.3 {
		t.Errorf("expected [0.1 0.2 0.3], got %v", result.Values)
	}
}

func TestFlowFloatSliceEmptyOmitted(t *testing.T) {
	type wrapper struct {
		Name   string         `yaml:"name"`
		Values flowFloatSlice `yaml:"values,omitempty"`
	}
	data := wrapper{Name: "test"}
	encoded, err := yaml.Marshal(&data)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(encoded), "values") {
		t.Errorf("expected empty flowFloatSlice to be omitted, got:\n%s", encoded)
	}
}

func TestFlowFloatSliceRoundtrip(t *testing.T) {
	type wrapper struct {
		Values flowFloatSlice `yaml:"values,omitempty"`
	}
	original := wrapper{Values: flowFloatSlice{1.5, -0.003, 42, 0}}
	encoded, err := yaml.Marshal(&original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded wrapper
	if err := yaml.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(decoded.Values) != len(original.Values) {
		t.Fatalf("length mismatch: %d vs %d", len(decoded.Values), len(original.Values))
	}
	for index, value := range original.Values {
		if decoded.Values[index] != value {
			t.Errorf("index %d: got %f, want %f", index, decoded.Values[index], value)
		}
	}
}
