package skill

import (
	"testing"
	"time"
)

func TestParseDurations_Defaults(t *testing.T) {
	config := &SkillConfig{}
	if err := config.ParseDurations(); err != nil {
		t.Fatalf("ParseDurations failed: %v", err)
	}

	if config.ParsedTimeout != 60*time.Second {
		t.Errorf("expected default timeout 60s, got %v", config.ParsedTimeout)
	}
	if config.ParsedMemory != 512*1024*1024 {
		t.Errorf("expected default memory 512MB, got %d", config.ParsedMemory)
	}
}

func TestParseDurations_CustomTimeout(t *testing.T) {
	config := &SkillConfig{Timeout: "300s"}
	if err := config.ParseDurations(); err != nil {
		t.Fatalf("ParseDurations failed: %v", err)
	}

	if config.ParsedTimeout != 300*time.Second {
		t.Errorf("expected 300s timeout, got %v", config.ParsedTimeout)
	}
}

func TestParseDurations_CustomMemory(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
	}{
		{"512M", 512 * 1024 * 1024},
		{"1G", 1024 * 1024 * 1024},
		{"256K", 256 * 1024},
	}

	for _, tt := range tests {
		config := &SkillConfig{MaxMemory: tt.input}
		if err := config.ParseDurations(); err != nil {
			t.Fatalf("ParseDurations(%s) failed: %v", tt.input, err)
		}
		if config.ParsedMemory != tt.expected {
			t.Errorf("ParseDurations(%s): expected %d, got %d", tt.input, tt.expected, config.ParsedMemory)
		}
	}
}

func TestParseDurations_InvalidTimeout(t *testing.T) {
	config := &SkillConfig{Timeout: "not-a-duration"}
	err := config.ParseDurations()
	if err == nil {
		t.Error("expected error for invalid timeout")
	}
}

func TestParseMemorySize(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
	}{
		{"512M", 512 * 1024 * 1024},
		{"1G", 1024 * 1024 * 1024},
		{"256K", 256 * 1024},
		{"1T", 1024 * 1024 * 1024 * 1024},
		{"", 0},
	}

	for _, tt := range tests {
		result := parseMemorySize(tt.input)
		if result != tt.expected {
			t.Errorf("parseMemorySize(%q): expected %d, got %d", tt.input, tt.expected, result)
		}
	}
}
