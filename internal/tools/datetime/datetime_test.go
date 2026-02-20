package datetime

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestDatetimeToolExecute(t *testing.T) {
	tool := &datetimeTool{}
	result, err := tool.Execute(context.Background(), "{}")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// Result should contain today's date.
	today := time.Now().Format("2006-01-02")
	if !strings.Contains(result, today) {
		t.Errorf("result %q should contain today's date %s", result, today)
	}

	// Result should contain timezone.
	tz := time.Now().Format("MST")
	if !strings.Contains(result, tz) {
		t.Errorf("result %q should contain timezone %s", result, tz)
	}
}

func TestDatetimeToolDefinition(t *testing.T) {
	tool := &datetimeTool{}
	def := tool.Definition()
	if def.Function.Name != "datetime" {
		t.Errorf("name = %q, want datetime", def.Function.Name)
	}
}
