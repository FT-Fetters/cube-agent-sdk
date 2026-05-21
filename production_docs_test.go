package agent

import (
	"os"
	"strings"
	"testing"
)

func TestProductionObservabilityDocsCoverDeploymentChecklist(t *testing.T) {
	// Keep the deployment docs tied to the SDK's safe observability contract.
	readme := readDocFile(t, "README.md")
	requireDocContains(t, "README", readme, []string{
		"Production observability",
		"docs/sdk/en/production.md#production-observability-checklist",
		"docs/sdk/zh/production.md#生产观测清单",
	})

	englishIndex := readDocFile(t, "docs/sdk/en/README.md")
	requireDocContains(t, "English SDK index", englishIndex, []string{
		"[Production](./production.md)",
		"production observability",
	})

	chineseIndex := readDocFile(t, "docs/sdk/zh/README.md")
	requireDocContains(t, "Chinese SDK index", chineseIndex, []string{
		"[生产集成](./production.md)",
		"生产观测清单",
	})

	english := readDocFile(t, "docs/sdk/en/production.md")
	requireDocContains(t, "English production guide", english, []string{
		"## Production Observability Checklist",
		"## Signal Wiring",
		"## Logs, Metrics, and Traces",
		"## Sampling Strategy",
		"## High-Cardinality Fields",
		"## Alerts, SLOs, and Dashboards",
		"## Provider Diagnostics",
		"## Streaming and Tool Timing",
		"## Privacy and Red Lines",
		"## Live Test and Local Verification",
		"## Troubleshooting Runbook",
		"`SlogObserver`",
		"`MetricsObserver`",
		"`NewSamplingObserver`",
		"`WithTraceContext`",
		"`WithRequestIDGenerator`",
		"`WithStreamObservations()`",
		"`ProviderDiagnosticsFromError`",
		"`ForbiddenTelemetryFieldNames()`",
		"`examples/opentelemetry`",
	})
	requireForbiddenTelemetryFieldsDocumented(t, "English production guide", english)

	chinese := readDocFile(t, "docs/sdk/zh/production.md")
	requireDocContains(t, "Chinese production guide", chinese, []string{
		"## 生产观测清单",
		"## 信号接入",
		"## 日志、指标和 Trace",
		"## 采样策略",
		"## 高基数字段",
		"## 告警、SLO 和仪表盘",
		"## Provider 诊断",
		"## 流式输出和工具耗时",
		"## 隐私和红线",
		"## Live Test 和本地验证",
		"## 故障排查 Runbook",
		"`SlogObserver`",
		"`MetricsObserver`",
		"`NewSamplingObserver`",
		"`WithTraceContext`",
		"`WithRequestIDGenerator`",
		"`WithStreamObservations()`",
		"`ProviderDiagnosticsFromError`",
		"`ForbiddenTelemetryFieldNames()`",
		"`examples/opentelemetry`",
	})
	requireForbiddenTelemetryFieldsDocumented(t, "Chinese production guide", chinese)
}

func readDocFile(t *testing.T, path string) string {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func requireDocContains(t *testing.T, name string, text string, phrases []string) {
	t.Helper()

	for _, phrase := range phrases {
		if !strings.Contains(text, phrase) {
			t.Fatalf("%s must contain %q", name, phrase)
		}
	}
}

func requireForbiddenTelemetryFieldsDocumented(t *testing.T, name string, text string) {
	t.Helper()

	for _, field := range ForbiddenTelemetryFieldNames() {
		if !strings.Contains(text, field) {
			t.Fatalf("%s must document forbidden telemetry field %q", name, field)
		}
	}
}
