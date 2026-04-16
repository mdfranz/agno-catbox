package skill

import "time"

// SkillConfig represents the configuration for a skill loaded from skill.yaml
type SkillConfig struct {
	Name             string        `yaml:"name"`
	Description      string        `yaml:"description"`
	AllowedCommands  []string      `yaml:"allowed_commands"`
	MaxMemory        string        `yaml:"max_memory"`
	Timeout          string        `yaml:"timeout"`
	MaxFileSizeBytes int64         `yaml:"max_file_size_bytes,omitempty"`
	ParsedMemory     int64         `yaml:"-"` // parsed from MaxMemory
	ParsedTimeout    time.Duration `yaml:"-"` // parsed from Timeout
	Dir              string        `yaml:"-"` // directory where skill was loaded
}

// ParseDurations parses the string durations and memory sizes into usable values
func (s *SkillConfig) ParseDurations() error {
	var err error

	// Parse timeout
	if s.Timeout != "" {
		s.ParsedTimeout, err = time.ParseDuration(s.Timeout)
		if err != nil {
			return err
		}
	} else {
		s.ParsedTimeout = 60 * time.Second // default 60s
	}

	// Parse memory (e.g., "512M", "1G")
	if s.MaxMemory != "" {
		s.ParsedMemory = parseMemorySize(s.MaxMemory)
	} else {
		s.ParsedMemory = 512 * 1024 * 1024 // default 512M
	}

	return nil
}

// parseMemorySize converts memory string like "512M", "1G" to bytes
func parseMemorySize(s string) int64 {
	multipliers := map[byte]int64{
		'K': 1024,
		'M': 1024 * 1024,
		'G': 1024 * 1024 * 1024,
		'T': 1024 * 1024 * 1024 * 1024,
	}

	if len(s) == 0 {
		return 0
	}

	lastChar := s[len(s)-1]
	if mult, ok := multipliers[lastChar]; ok {
		var num int64
		_, _ = parseFloat(s[:len(s)-1], &num)
		return num * mult
	}

	var num int64
	_, _ = parseFloat(s, &num)
	return num
}

// Simple string to int64 parser
func parseFloat(s string, result *int64) (int, error) {
	var num int64
	_, err := sscanf(s, "%d", &num)
	*result = num
	return 1, err
}

// Dummy sscanf since we're not importing fmt
func sscanf(str, format string, args ...interface{}) (int, error) {
	// Simple implementation for our use case
	var result int64
	n, err := 0, error(nil)
	if format == "%d" && len(args) > 0 {
		if ptr, ok := args[0].(*int64); ok {
			// Simple atoi equivalent
			for _, c := range str {
				if c >= '0' && c <= '9' {
					result = result*10 + int64(c-'0')
				}
			}
			*ptr = result
			n = 1
		}
	}
	return n, err
}
