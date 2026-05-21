# 示例

本地示例位于仓库的 `examples/` 目录。除非特别说明，它们不需要真实凭证或外部
服务。

## 本地示例

```bash
go run ./examples/openai_compatible
go run ./examples/model_factory
go run ./examples/tool_schema
go run ./examples/streaming
go run ./examples/mcp_stdio
go run ./examples/session_state
go run ./examples/approval_observer
```

测试套件会编译这些示例：

```bash
go test ./...
```

## Live API 示例

Live API 示例面向真实 provider endpoint，并从环境变量读取凭证。

```bash
MODEL_API_TYPE=anthropic-messages \
MODEL_BASE_URL=https://api.anthropic.com \
MODEL_API_KEY="<your-api-key>" \
MODEL_NAME=claude-sonnet-4-6 \
go run ./examples/live_api
```

也可以使用 `MODEL_API_TYPE=openai-compatible` 搭配 OpenAI-compatible base URL，
或使用 `MODEL_API_TYPE=openai-responses` 搭配
`MODEL_BASE_URL=https://api.openai.com`。

## 可选 Live API 测试

当 `MODEL_API_TYPE`、`MODEL_BASE_URL`、`MODEL_API_KEY` 和 `MODEL_NAME` 在进程
环境或仓库根 `.env` 中完整存在时，live test 会自动运行。缺少任何必需变量时，
测试会被跳过。

```bash
go test -v -run '^TestLiveAPIModelRun$' .
```

不要提交真实凭证。把本地 `.env` 文件排除在版本控制之外。
