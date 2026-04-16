package skill

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// LoadSkill loads the skill configuration from a skill directory
// Looks for skill.yaml in the skill directory
func LoadSkill(skillDir string) (*SkillConfig, error) {
	skillYamlPath := filepath.Join(skillDir, "skill.yaml")

	// Read the skill.yaml file
	data, err := os.ReadFile(skillYamlPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read skill.yaml: %w", err)
	}

	// Parse the YAML
	var config SkillConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse skill.yaml: %w", err)
	}

	// Validate
	if config.Name == "" {
		return nil, fmt.Errorf("skill.yaml must have a 'name' field")
	}

	// Parse durations and memory sizes
	if err := config.ParseDurations(); err != nil {
		return nil, fmt.Errorf("failed to parse durations: %w", err)
	}

	config.Dir = skillDir

	return &config, nil
}

// FindSkillDir finds the skill directory by name
// Checks: ./skills/{name}, and {name} directly
func FindSkillDir(skillName string) (string, error) {
	// Try ./skills/{name}
	skillsPath := filepath.Join("skills", skillName)
	if info, err := os.Stat(skillsPath); err == nil && info.IsDir() {
		return skillsPath, nil
	}

	// Try {name} directly
	if info, err := os.Stat(skillName); err == nil && info.IsDir() {
		return skillName, nil
	}

	return "", fmt.Errorf("skill directory '%s' not found (checked ./skills/%s and %s)", skillName, skillName, skillName)
}
