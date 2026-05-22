# Eval 与 Replay

Eval helpers 是公开、无额外依赖的 Go 测试构件。它们可以脚本化模型行为、捕获
稳定 transcript、回放已保存 artifact，并在没有 live provider 的情况下断言
agent 行为。

## Scripted Models

使用 `ScriptedModel` 测试确定性的 `Run` 行为。

```go
recorder := agent.NewEvalRecorder()
model := agent.NewScriptedModel(
	agent.ScriptedResponse(agent.ModelResponse{
		ToolCalls: []agent.ToolCall{{
			ID:        "call-1",
			Name:      "lookup_account",
			Arguments: map[string]any{"account_id": "acct_123"},
		}},
	}),
	agent.ScriptedResponse(agent.ModelResponse{
		Message: agent.Message{Role: agent.RoleAssistant, Content: "active"},
	}),
).RecordWith(recorder)

bot, err := agent.New(agent.Config{ID: "eval-agent"}, model,
	agent.WithHook(recorder.Hook()),
	agent.WithObserver(recorder),
	agent.WithTools(lookupTool),
)
if err != nil {
	t.Fatal(err)
}

reply, runErr := bot.Run(ctx, "check account")
transcript := recorder.RecordRun("check account", reply, runErr)

agent.AssertToolCall(t, transcript, agent.ToolCallExpectation{
	Name:      "lookup_account",
	Arguments: map[string]any{"account_id": "acct_123"},
})
agent.AssertFinalMessage(t, transcript, "active")
```

`ScriptedModel.Requests()` 返回模型请求的隔离副本。脚本耗尽时，模型会返回确定性
错误，不会静默生成临时响应。

使用 `ScriptedError` 覆盖模型失败路径：

```go
model := agent.NewScriptedModel(
	agent.ScriptedError(errors.New("fixture model unavailable")),
).RecordWith(recorder)

reply, runErr := bot.Run(ctx, "hello")
transcript := recorder.RecordRun("hello", reply, runErr)

agent.AssertFinalError(t, transcript, "fixture model unavailable")
agent.AssertObservation(t, transcript, agent.ObservationExpectation{
	Type:          agent.EventAfterModel,
	Failed:        true,
	ErrorCategory: agent.ErrorCategoryModel,
})
```

## 流式失败

使用 `ScriptedStreamModel` 测试 `RunStream`。流式脚本可以发出 delta、done、
stream error，或启动阶段错误。

```go
model := agent.NewScriptedStreamModel(agent.ScriptedStreamEvents(
	agent.StreamEvent{Type: agent.StreamEventDelta, Delta: "partial"},
	agent.StreamEvent{Type: agent.StreamEventError, Error: errors.New("stream failed")},
)).RecordWith(recorder)

events, err := bot.RunStream(ctx, "stream", agent.WithStreamObservations())
if err != nil {
	t.Fatal(err)
}

var streamErr error
for event := range events {
	if event.Type == agent.StreamEventError {
		streamErr = event.Error
	}
}
transcript := recorder.RecordRun("stream", agent.Message{}, streamErr)

agent.AssertFinalError(t, transcript, "stream failed")
agent.AssertObservation(t, transcript, agent.ObservationExpectation{
	Type:          agent.EventStreamError,
	Failed:        true,
	ErrorCategory: agent.ErrorCategoryModel,
})
```

## Golden Transcripts

`EvalRecorder` 会记录 lifecycle hooks、脱敏 observations、来自 scripted model 的
model exchanges、stream exchanges、inputs、tool calls/results 和最终结果。

```go
payload, err := transcript.StableJSON()
if err != nil {
	t.Fatal(err)
}

replayed, err := agent.ReplayRunTranscript(payload)
if err != nil {
	t.Fatal(err)
}
agent.AssertEventOrder(t, replayed, agent.EventBeforeModel, agent.EventAfterModel)
```

`StableJSON` 会从 transcript 视图中排除不稳定的 timing 字段，使输出适合 CI 和
golden-file 比对。

## 典型 Eval 用例

工具选择：

```go
agent.AssertToolCalled(t, transcript, "lookup_account")
agent.AssertToolCall(t, transcript, agent.ToolCallExpectation{
	Name:          "lookup_account",
	ResultContent: "active",
})
```

审批拒绝：

```go
bot, err := agent.New(cfg, model,
	agent.WithApprovalPolicy(agent.DenyAllApproval{}),
	agent.WithTools(deleteTool),
	agent.WithHook(recorder.Hook()),
	agent.WithObserver(recorder),
)

reply, runErr := bot.Run(ctx, "delete account")
transcript := recorder.RecordRun("delete account", reply, runErr)

agent.AssertApprovalDenied(t, transcript, "delete_account")
agent.AssertFinalError(t, transcript, "approval denied")
```

上下文压缩：

```go
agent.AssertObservation(t, transcript, agent.ObservationExpectation{
	Type: agent.EventBeforeCompact,
})
agent.AssertObservation(t, transcript, agent.ObservationExpectation{
	Type: agent.EventAfterCompact,
})
```

模型错误和流式失败应同时断言最终错误和失败 observation category，这样测试能覆盖
用户可见行为和遥测行为。

## Session Event Replay

使用 `ReplaySessionEvents` 将已保存的 append-only `SessionEvent` logs 载入
transcript 并断言顺序。

```go
events, err := store.ListSessionEvents(ctx, "session-123", 0)
if err != nil {
	t.Fatal(err)
}

transcript, err := agent.ReplaySessionEvents(events)
if err != nil {
	t.Fatal(err)
}
agent.AssertSessionEventOrder(t, transcript,
	agent.SessionEventRunStarted,
	agent.SessionEventRunCompleted,
)
```

Replay 会校验 event schema metadata 和单调顺序。Session event metadata 会表示为
排序后的 key/value pairs，以获得稳定 JSON。

## 存储和脱敏

Eval transcripts 会按测试脚本记录 inputs、model messages、tool arguments、tool
results 和 error strings。请把它们当作可能包含用户内容的测试 fixtures 处理。
应用负责 redaction、加密、访问控制和保留策略。

Transcript 中的 model request 视图会省略 MCP command paths、arguments、
environment variables、URLs 和 provider credentials，只保留 MCP server name 和
transport 等安全请求元数据。
