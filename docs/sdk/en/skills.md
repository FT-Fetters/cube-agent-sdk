# Skills

Skills are reusable instruction bundles that can be explicitly or implicitly
activated for agent runs.

## Define a Skill

```go
reviewSkill := agent.Skill{
	Name:           "review",
	Description:    "Review code changes",
	Instructions:   "Inspect changes for bugs, regressions, and missing tests.",
	TriggerPhrases: []string{"review", "code review"},
}

bot, err := agent.New(cfg, model, agent.WithSkills(reviewSkill))
```

## Load Skill Directories

`LoadSkills` reads standard directories containing `SKILL.md` files.

```text
skills/
  review/
    SKILL.md
```

```go
loaded, err := agent.LoadSkills("skills")
if err != nil {
	return err
}
bot, err := agent.New(cfg, model, agent.WithSkills(loaded...))
```

## Activation Paths

- `ActivateSkill` persistently activates a skill.
- `DeactivateSkill` removes persistent activation.
- `WithRunSkills` activates skills for one run.
- Inline markers such as `+review` or `+skill:review` activate registered
  skills.
- `TriggerPhrases` and custom `Match` functions support implicit activation.

Active skill instructions are added to the system prompt for the run.
