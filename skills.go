package agent

import internalskills "github.com/cubence/cube-agent-sdk/internal/skills"

// LoadSkills loads standard skill directories containing SKILL.md files.
func LoadSkills(root string) ([]Skill, error) {
	return internalskills.Load(root)
}

func cloneSkill(skill Skill) Skill {
	return internalskills.Clone(skill)
}

func skillMatches(skill Skill, input string) bool {
	return internalskills.Matches(skill, input)
}
