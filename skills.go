package agent

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SkillMatcher allows custom implicit skill activation logic.
type SkillMatcher func(input string) bool

// Skill contains a reusable instruction bundle that can be explicitly or implicitly activated.
type Skill struct {
	Name           string
	Description    string
	Instructions   string
	TriggerPhrases []string
	Match          SkillMatcher
	Source         string
}

// LoadSkills loads standard skill directories containing SKILL.md files.
func LoadSkills(root string) ([]Skill, error) {
	var skills []Skill
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Name() != "SKILL.md" {
			return nil
		}
		skill, err := loadSkillFile(path)
		if err != nil {
			return err
		}
		skills = append(skills, skill)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(skills, func(i, j int) bool {
		if skills[i].Name == skills[j].Name {
			return skills[i].Source < skills[j].Source
		}
		return skills[i].Name < skills[j].Name
	})
	return skills, nil
}

func loadSkillFile(path string) (Skill, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return Skill{}, err
	}
	metadata, body := parseSkillMarkdown(string(content))
	name := metadata["name"]
	if name == "" {
		name = filepath.Base(filepath.Dir(path))
	}
	return Skill{
		Name:           name,
		Description:    metadata["description"],
		Instructions:   strings.TrimSpace(body),
		TriggerPhrases: splitMetadataList(metadata["triggers"], metadata["trigger_phrases"]),
		Source:         path,
	}, nil
}

func parseSkillMarkdown(content string) (map[string]string, string) {
	metadata := make(map[string]string)
	trimmed := strings.TrimPrefix(content, "\ufeff")
	if !strings.HasPrefix(trimmed, "---") {
		return metadata, trimmed
	}
	lines := strings.Split(trimmed, "\n")
	if strings.TrimSpace(lines[0]) != "---" {
		return metadata, trimmed
	}

	currentListKey := ""
	for i := 1; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "---" {
			return metadata, strings.Join(lines[i+1:], "\n")
		}
		trimmedLine := strings.TrimSpace(line)
		if trimmedLine == "" {
			continue
		}
		if strings.HasPrefix(trimmedLine, "- ") && currentListKey != "" {
			value := strings.TrimSpace(strings.TrimPrefix(trimmedLine, "- "))
			metadata[currentListKey] = appendCSV(metadata[currentListKey], trimQuotes(value))
			continue
		}
		key, value, ok := strings.Cut(trimmedLine, ":")
		if !ok {
			currentListKey = ""
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		if value == "" {
			currentListKey = key
			continue
		}
		currentListKey = ""
		metadata[key] = trimQuotes(value)
	}
	return metadata, trimmed
}

func appendCSV(existing, value string) string {
	if existing == "" {
		return value
	}
	return existing + "," + value
}

func splitMetadataList(values ...string) []string {
	var result []string
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(trimQuotes(part))
			if part != "" {
				result = append(result, part)
			}
		}
	}
	return result
}

func trimQuotes(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "[")
	value = strings.TrimSuffix(value, "]")
	value = strings.Trim(value, `"'`)
	return value
}

func (s Skill) clone() Skill {
	s.TriggerPhrases = append([]string(nil), s.TriggerPhrases...)
	return s
}

func (s Skill) matches(input string) bool {
	if s.Match != nil && s.Match(input) {
		return true
	}
	lowerInput := strings.ToLower(input)
	for _, trigger := range s.TriggerPhrases {
		trigger = strings.ToLower(strings.TrimSpace(trigger))
		if trigger != "" && strings.Contains(lowerInput, trigger) {
			return true
		}
	}
	return false
}
