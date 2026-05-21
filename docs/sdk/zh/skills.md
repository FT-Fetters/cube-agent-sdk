# Skills

Skills 是可复用指令包，可以为 agent run 显式或隐式激活。

## 定义 Skill

```go
reviewSkill := agent.Skill{
	Name:           "review",
	Description:    "Review code changes",
	Instructions:   "Inspect changes for bugs, regressions, and missing tests.",
	TriggerPhrases: []string{"review", "code review"},
}

bot, err := agent.New(cfg, model, agent.WithSkills(reviewSkill))
```

## 加载 Skill 目录

`LoadSkills` 读取包含 `SKILL.md` 文件的标准目录。

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

## 激活路径

- `ActivateSkill` 持久激活 skill。
- `DeactivateSkill` 移除持久激活。
- `WithRunSkills` 为单次 run 激活 skills。
- `+review` 或 `+skill:review` 等内联标记会激活已注册 skills。
- `TriggerPhrases` 和自定义 `Match` 函数支持隐式激活。

Active skill 指令会在本次 run 中加入 system prompt。
