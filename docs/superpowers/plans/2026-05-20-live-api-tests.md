# Live API Tests Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add live provider tests that automatically run when root `.env` contains a complete real API configuration and skip when it does not.

**Architecture:** Keep deterministic unit tests unchanged. Add a root-package test helper that loads root `.env`, parses minimal `KEY=value` entries, builds `ModelConfig`, and skips incomplete live runs. Add one live integration test that uses the public SDK surface and logs safe output through Go's verbose test mode.

**Tech Stack:** Go 1.22, standard library only, `go test`, existing SDK public APIs.

---

## File Structure

- Create `live_api_test_helper_test.go`: test-only `.env` parser, repository root finder, live model configuration loader, and safe error/log helpers.
- Create `live_api_test.go`: live SDK test that runs only when live configuration is complete.
- Modify `CONTRIBUTING.md`: document `.env` keys and targeted verbose test commands.
- Modify `README.md`: add a short developer note for optional live API tests.

---

### Task 1: Test the `.env` Parser

**Files:**
- Create: `live_api_test_helper_test.go`

- [ ] **Step 1: Write the failing parser tests**

Create `live_api_test_helper_test.go` with these tests and minimal imports only:

```go
package agent

import (
	"strings"
	"testing"
)

func TestParseDotEnvForLiveTestsParsesPracticalCredentialFile(t *testing.T) {
	values, err := parseDotEnvForLiveTests(strings.NewReader(`
# local live settings
MODEL_API_TYPE=openai-compatible
MODEL_BASE_URL="https://api.openai.com"
MODEL_API_KEY='secret-key'
MODEL_NAME=gpt-test

IGNORED_SPACES = value with spaces
`))
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]string{
		"MODEL_API_TYPE":  "openai-compatible",
		"MODEL_BASE_URL":  "https://api.openai.com",
		"MODEL_API_KEY":   "secret-key",
		"MODEL_NAME":      "gpt-test",
		"IGNORED_SPACES":  "value with spaces",
	}
	for key, wantValue := range want {
		if values[key] != wantValue {
			t.Fatalf("%s = %q, want %q", key, values[key], wantValue)
		}
	}
}

func TestParseDotEnvForLiveTestsRejectsMalformedLine(t *testing.T) {
	_, err := parseDotEnvForLiveTests(strings.NewReader("MODEL_API_KEY\n"))
	if err == nil {
		t.Fatal("parseDotEnvForLiveTests returned nil error, want malformed line error")
	}
	if !strings.Contains(err.Error(), "line 1") {
		t.Fatalf("err = %v, want line number", err)
	}
}

func TestParseDotEnvForLiveTestsRejectsEmptyKey(t *testing.T) {
	_, err := parseDotEnvForLiveTests(strings.NewReader("=value\n"))
	if err == nil {
		t.Fatal("parseDotEnvForLiveTests returned nil error, want empty key error")
	}
	if !strings.Contains(err.Error(), "empty key") {
		t.Fatalf("err = %v, want empty key message", err)
	}
}
```

- [ ] **Step 2: Run parser tests to verify RED**

Run:

```bash
go test -run 'TestParseDotEnvForLiveTests' .
```

Expected: FAIL because `parseDotEnvForLiveTests` is undefined.

- [ ] **Step 3: Implement the minimal parser**

Update `live_api_test_helper_test.go` to include the parser and required imports:

```go
package agent

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"testing"
)

func parseDotEnvForLiveTests(r io.Reader) (map[string]string, error) {
	values := make(map[string]string)
	scanner := bufio.NewScanner(r)
	lineNumber := 0

	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("line %d: malformed .env entry", lineNumber)
		}

		key = strings.TrimSpace(key)
		if key == "" {
			return nil, fmt.Errorf("line %d: empty key", lineNumber)
		}

		values[key] = trimDotEnvValueForLiveTests(strings.TrimSpace(value))
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return values, nil
}

func trimDotEnvValueForLiveTests(value string) string {
	if len(value) < 2 {
		return value
	}
	if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
		return value[1 : len(value)-1]
	}
	return value
}
```

Keep the tests from Step 1 below these helpers in the same file.

- [ ] **Step 4: Run parser tests to verify GREEN**

Run:

```bash
go test -run 'TestParseDotEnvForLiveTests' .
```

Expected: PASS.

- [ ] **Step 5: Commit parser helper**

Run:

```bash
git add live_api_test_helper_test.go
git commit -m "test: add live api env parser"
```

---

### Task 2: Test Live Config Loading and Skip Behavior

**Files:**
- Modify: `live_api_test_helper_test.go`

- [ ] **Step 1: Add failing tests for config loading**

Append these tests to `live_api_test_helper_test.go`:

```go
func TestLoadLiveModelConfigForTestsUsesEnvironmentBeforeDotEnv(t *testing.T) {
	t.Setenv("MODEL_API_TYPE", string(ModelAPIOpenAICompatible))
	t.Setenv("MODEL_BASE_URL", "https://env.example.test")
	t.Setenv("MODEL_API_KEY", "env-key")
	t.Setenv("MODEL_NAME", "env-model")
	t.Setenv("ANTHROPIC_VERSION", "2025-01-01")

	config, skip, err := liveModelConfigForTests(map[string]string{
		"MODEL_API_TYPE":     string(ModelAPIAnthropicMessages),
		"MODEL_BASE_URL":     "https://dotenv.example.test",
		"MODEL_API_KEY":      "dotenv-key",
		"MODEL_NAME":         "dotenv-model",
		"ANTHROPIC_VERSION":  "2023-06-01",
	})
	if err != nil {
		t.Fatal(err)
	}
	if skip != "" {
		t.Fatalf("skip = %q, want empty", skip)
	}
	if config.APIType != ModelAPIOpenAICompatible {
		t.Fatalf("APIType = %q, want %q", config.APIType, ModelAPIOpenAICompatible)
	}
	if config.BaseURL != "https://env.example.test" || config.APIKey != "env-key" || config.Model != "env-model" {
		t.Fatalf("config = %#v, want environment values", config)
	}
	if config.AnthropicVersion != "2025-01-01" {
		t.Fatalf("AnthropicVersion = %q, want environment value", config.AnthropicVersion)
	}
}

func TestLoadLiveModelConfigForTestsUsesDotEnvWhenEnvironmentMissing(t *testing.T) {
	for _, key := range []string{"MODEL_API_TYPE", "MODEL_BASE_URL", "MODEL_API_KEY", "MODEL_NAME", "ANTHROPIC_VERSION"} {
		t.Setenv(key, "")
	}

	config, skip, err := liveModelConfigForTests(map[string]string{
		"MODEL_API_TYPE": string(ModelAPIAnthropicMessages),
		"MODEL_BASE_URL": "https://dotenv.example.test",
		"MODEL_API_KEY":  "dotenv-key",
		"MODEL_NAME":     "dotenv-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	if skip != "" {
		t.Fatalf("skip = %q, want empty", skip)
	}
	if config.APIType != ModelAPIAnthropicMessages {
		t.Fatalf("APIType = %q, want %q", config.APIType, ModelAPIAnthropicMessages)
	}
	if config.BaseURL != "https://dotenv.example.test" || config.APIKey != "dotenv-key" || config.Model != "dotenv-model" {
		t.Fatalf("config = %#v, want .env values", config)
	}
}

func TestLoadLiveModelConfigForTestsReportsMissingRequiredVariables(t *testing.T) {
	for _, key := range []string{"MODEL_API_TYPE", "MODEL_BASE_URL", "MODEL_API_KEY", "MODEL_NAME", "ANTHROPIC_VERSION"} {
		t.Setenv(key, "")
	}

	config, skip, err := liveModelConfigForTests(map[string]string{
		"MODEL_API_TYPE": string(ModelAPIOpenAICompatible),
	})
	if err != nil {
		t.Fatal(err)
	}
	if config != (ModelConfig{}) {
		t.Fatalf("config = %#v, want zero config", config)
	}
	for _, name := range []string{"MODEL_BASE_URL", "MODEL_API_KEY", "MODEL_NAME"} {
		if !strings.Contains(skip, name) {
			t.Fatalf("skip = %q, want missing %s", skip, name)
		}
	}
}
```

- [ ] **Step 2: Run config tests to verify RED**

Run:

```bash
go test -run 'TestLoadLiveModelConfigForTests' .
```

Expected: FAIL because `liveModelConfigForTests` is undefined.

- [ ] **Step 3: Implement config loading helper**

Add these helpers above the tests in `live_api_test_helper_test.go`:

```go
var requiredLiveModelEnvForTests = []string{
	"MODEL_API_TYPE",
	"MODEL_BASE_URL",
	"MODEL_API_KEY",
	"MODEL_NAME",
}

func liveModelConfigForTests(dotEnv map[string]string) (ModelConfig, string, error) {
	value := func(key string) string {
		if envValue := strings.TrimSpace(os.Getenv(key)); envValue != "" {
			return envValue
		}
		return strings.TrimSpace(dotEnv[key])
	}

	var missing []string
	for _, key := range requiredLiveModelEnvForTests {
		if value(key) == "" {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		return ModelConfig{}, "missing live API environment variables: " + strings.Join(missing, ", "), nil
	}

	return ModelConfig{
		APIType:          ModelAPIType(value("MODEL_API_TYPE")),
		BaseURL:          value("MODEL_BASE_URL"),
		APIKey:           value("MODEL_API_KEY"),
		Model:            value("MODEL_NAME"),
		AnthropicVersion: value("ANTHROPIC_VERSION"),
	}, "", nil
}
```

Update the import list to include `os`.

- [ ] **Step 4: Run config tests to verify GREEN**

Run:

```bash
go test -run 'TestLoadLiveModelConfigForTests' .
```

Expected: PASS.

- [ ] **Step 5: Commit config helper**

Run:

```bash
git add live_api_test_helper_test.go
git commit -m "test: add live api config helper"
```

---

### Task 3: Test Root `.env` Discovery

**Files:**
- Modify: `live_api_test_helper_test.go`

- [ ] **Step 1: Add failing tests for repository root loading**

Append these tests to `live_api_test_helper_test.go`:

```go
func TestFindRepoRootForLiveTestsFindsGoModuleFromNestedDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}

	got, err := findRepoRootForLiveTests(nested)
	if err != nil {
		t.Fatal(err)
	}
	if got != root {
		t.Fatalf("root = %q, want %q", got, root)
	}
}

func TestLoadRootDotEnvForLiveTestsReturnsEmptyMapWhenFileMissing(t *testing.T) {
	values, err := loadRootDotEnvForLiveTests(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 0 {
		t.Fatalf("values = %#v, want empty map", values)
	}
}

func TestLoadRootDotEnvForLiveTestsParsesRootEnvFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("MODEL_NAME=dotenv-model\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	values, err := loadRootDotEnvForLiveTests(root)
	if err != nil {
		t.Fatal(err)
	}
	if values["MODEL_NAME"] != "dotenv-model" {
		t.Fatalf("MODEL_NAME = %q, want dotenv-model", values["MODEL_NAME"])
	}
}
```

- [ ] **Step 2: Run root discovery tests to verify RED**

Run:

```bash
go test -run 'TestFindRepoRootForLiveTests|TestLoadRootDotEnvForLiveTests' .
```

Expected: FAIL because `findRepoRootForLiveTests` and `loadRootDotEnvForLiveTests` are undefined.

- [ ] **Step 3: Implement root discovery and `.env` file loading**

Add these helpers above the tests in `live_api_test_helper_test.go`:

```go
func findRepoRootForLiveTests(start string) (string, error) {
	dir := start
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not find go.mod from %s", start)
		}
		dir = parent
	}
}

func loadRootDotEnvForLiveTests(root string) (map[string]string, error) {
	file, err := os.Open(filepath.Join(root, ".env"))
	if errors.Is(err, os.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()

	return parseDotEnvForLiveTests(file)
}
```

Update the import list to include `errors` and `path/filepath`.

- [ ] **Step 4: Run root discovery tests to verify GREEN**

Run:

```bash
go test -run 'TestFindRepoRootForLiveTests|TestLoadRootDotEnvForLiveTests' .
```

Expected: PASS.

- [ ] **Step 5: Commit root `.env` loading**

Run:

```bash
git add live_api_test_helper_test.go
git commit -m "test: load root env for live api tests"
```

---

### Task 4: Add the Live API Test

**Files:**
- Create: `live_api_test.go`
- Modify: `live_api_test_helper_test.go`

- [ ] **Step 1: Add the failing live test shell**

Create `live_api_test.go`:

```go
package agent

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestLiveAPIModelRun(t *testing.T) {
	config := requireLiveModelConfigForTest(t)
	t.Logf("live api type=%s model=%s base_url=%s", config.APIType, config.Model, safeBaseURLForLiveTest(config.BaseURL))

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	model, err := NewModel(config)
	if err != nil {
		t.Fatal(formatLiveAPIModelConstructionErrorForTest(err))
	}

	var observations []Observation
	bot, err := New(
		Config{SystemPrompt: "You are verifying an SDK integration. Answer in one short sentence."},
		model,
		WithObserver(ObserverFunc(func(ctx context.Context, observation Observation) {
			observations = append(observations, observation)
		})),
	)
	if err != nil {
		t.Fatal(err)
	}

	reply, err := bot.Run(ctx, "Reply with the exact phrase: live api check ok")
	if err != nil {
		t.Fatal(formatLiveAPIErrorForTest(err))
	}
	if strings.TrimSpace(reply.Content) == "" {
		t.Fatal("reply content is empty")
	}

	t.Logf("assistant: %s", strings.TrimSpace(reply.Content))
	logLiveAPIObservationsForTest(t, observations)
}
```

- [ ] **Step 2: Run live test to verify RED**

Run:

```bash
go test -run '^TestLiveAPIModelRun$' -v .
```

Expected: FAIL because `requireLiveModelConfigForTest`,
`safeBaseURLForLiveTest`, `formatLiveAPIModelConstructionErrorForTest`,
`formatLiveAPIErrorForTest`, and `logLiveAPIObservationsForTest` are undefined.

- [ ] **Step 3: Implement live test helpers**

Add these helpers above the tests in `live_api_test_helper_test.go`:

```go
func requireLiveModelConfigForTest(t *testing.T) ModelConfig {
	t.Helper()

	return requireLiveModelConfigFromSourcesForTest(t, func() (map[string]string, error) {
		workingDir, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		root, err := findRepoRootForLiveTests(workingDir)
		if err != nil {
			return nil, err
		}
		return loadRootDotEnvForLiveTests(root)
	})
}

func requireLiveModelConfigFromSourcesForTest(t *testing.T, loadDotEnv func() (map[string]string, error)) ModelConfig {
	t.Helper()

	dotEnv := map[string]string{}
	if !hasRequiredLiveModelEnvForTests() {
		var err error
		dotEnv, err = loadDotEnv()
		if err != nil {
			t.Fatal(err)
		}
	}

	config, skip, err := liveModelConfigForTests(dotEnv)
	if err != nil {
		t.Fatal(err)
	}
	if skip != "" {
		t.Skip(skip)
	}
	return config
}

func hasRequiredLiveModelEnvForTests() bool {
	for _, key := range requiredLiveModelEnvForTests {
		if strings.TrimSpace(os.Getenv(key)) == "" {
			return false
		}
	}
	return true
}

func safeBaseURLForLiveTest(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "<invalid>"
	}
	return (&url.URL{Scheme: parsed.Scheme, Host: parsed.Host, Path: parsed.Path, RawPath: parsed.RawPath}).String()
}

func formatLiveAPIErrorForTest(err error) string {
	if err == nil {
		return "live api error_type=<nil>"
	}
	var agentErr *AgentError
	if errors.As(err, &agentErr) {
		return fmt.Sprintf(
			"live api error category=%s operation=%s request=%s",
			agentErr.Category,
			agentErr.Operation,
			agentErr.RequestID,
		)
	}
	return fmt.Sprintf("live api error_type=%T", err)
}

func formatLiveAPIModelConstructionErrorForTest(err error) string {
	if err == nil {
		return "live api model construction error_type=<nil>"
	}
	var agentErr *AgentError
	if errors.As(err, &agentErr) {
		return fmt.Sprintf(
			"live api model construction error category=%s operation=%s request=%s",
			agentErr.Category,
			agentErr.Operation,
			agentErr.RequestID,
		)
	}
	return fmt.Sprintf("live api model construction error_type=%T", err)
}

func logLiveAPIObservationsForTest(t *testing.T, observations []Observation) {
	t.Helper()

	for i, observation := range observations {
		t.Logf(
			"observation=%d event=%s failed=%t round=%d duration=%s estimated_tokens=%d request=%s error_category=%s",
			i+1,
			observation.Type,
			observation.Failed,
			observation.Round,
			observation.Duration,
			observation.EstimatedTokens,
			observation.RequestID,
			observation.ErrorCategory,
		)
	}
}
```

Update the helper file import list to include `net/url` along with the existing
standard-library imports used by these helpers.

- [ ] **Step 4: Run live test to verify GREEN or SKIP**

Run:

```bash
go test -run '^TestLiveAPIModelRun$' -v .
```

Expected without complete live variables: SKIP with missing variable names.

Expected with complete root `.env`: PASS and verbose logs containing API type, model, assistant response, and observation lines.

- [ ] **Step 5: Run deterministic package tests**

Run:

```bash
go test .
```

Expected: PASS, or PASS with live test skipped when `.env` is incomplete.

- [ ] **Step 6: Commit live test**

Run:

```bash
git add live_api_test.go live_api_test_helper_test.go
git commit -m "test: add optional live api coverage"
```

---

### Task 5: Document Live API Test Usage

**Files:**
- Modify: `examples/examples_test.go`
- Modify: `CONTRIBUTING.md`
- Modify: `README.md`

- [ ] **Step 1: Add failing documentation coverage test**

Create or update documentation coverage in `examples/examples_test.go` by adding these required README substrings to the existing `sections` list:

```go
"## Optional Live API Tests",
"go test -v -run '^TestLiveAPIModelRun$' .",
```

Also add a new assertion for `CONTRIBUTING.md`:

```go
contributing, err := os.ReadFile(filepath.Join("..", "CONTRIBUTING.md"))
if err != nil {
	t.Fatal(err)
}
contributingText := string(contributing)
for _, phrase := range []string{
	"Optional live API tests",
	"MODEL_API_TYPE",
	"go test -v -run '^TestLiveAPIModelRun$' .",
} {
	if !strings.Contains(contributingText, phrase) {
		t.Fatalf("CONTRIBUTING must contain phrase %q", phrase)
	}
}
```

- [ ] **Step 2: Run documentation coverage test to verify RED**

Run:

```bash
go test ./examples -run '^TestTask9ExamplesAndReadmeCoverage$'
```

Expected: FAIL because README and CONTRIBUTING do not yet contain the new live-test text.

- [ ] **Step 3: Update README**

Add this section near the existing model API documentation:

````markdown
## Optional Live API Tests

The default test suite uses local fakes and `httptest` servers. To exercise a
real provider, create a root `.env` file with a complete model configuration:

```bash
MODEL_API_TYPE=anthropic-messages
MODEL_BASE_URL=https://api.anthropic.com
MODEL_API_KEY=<your-api-key>
MODEL_NAME=claude-sonnet-4-6
```

When these variables are present in the process environment or root `.env` as a
complete configuration, the live API test runs automatically. When any required
variable is missing, it is skipped. Do not commit real credentials.

Run the live test with verbose output:

```bash
go test -v -run '^TestLiveAPIModelRun$' .
```

Run any single test with verbose output by replacing the test name:

```bash
go test -v -run '^TestName$' ./...
```
````

- [ ] **Step 4: Update CONTRIBUTING**

Add this subsection under Development Setup:

````markdown
Optional live API tests:

Create a root `.env` file to run the live provider test automatically:

```bash
MODEL_API_TYPE=anthropic-messages
MODEL_BASE_URL=https://api.anthropic.com
MODEL_API_KEY=<your-api-key>
MODEL_NAME=claude-sonnet-4-6
```

When these variables are present in the process environment or root `.env` as a
complete configuration, the live API test runs automatically. The live test
skips when any required variable is missing. Do not commit real credentials. Use
verbose mode to show the provider response and safe observer metadata:

```bash
go test -v -run '^TestLiveAPIModelRun$' .
```

Run a specific test with:

```bash
go test -v -run '^TestName$' ./...
```
````

Also change the setup sentence from "No real API keys or live model providers are required for the test suite." to:

```markdown
No real API keys or live model providers are required for the deterministic test
suite. A local root `.env` can enable optional live API tests.
```

- [ ] **Step 5: Run documentation coverage test to verify GREEN**

Run:

```bash
go test ./examples -run '^TestTask9ExamplesAndReadmeCoverage$'
```

Expected: PASS.

- [ ] **Step 6: Commit docs**

Run:

```bash
git add README.md CONTRIBUTING.md examples/examples_test.go
git commit -m "docs: document live api tests"
```

---

### Task 6: Final Verification

**Files:**
- Verify all changed files.

- [ ] **Step 1: Run all tests**

Run:

```bash
go test ./...
```

Expected: PASS. If root `.env` is incomplete or absent, `TestLiveAPIModelRun` should be skipped.

- [ ] **Step 2: Run targeted live test with output**

Run:

```bash
go test -v -run '^TestLiveAPIModelRun$' .
```

Expected without complete live variables: SKIP with missing variable names.

Expected with complete root `.env`: PASS and logs showing safe live output.

- [ ] **Step 3: Review diff for secrets and scope**

Run:

```bash
git diff --stat HEAD
git diff HEAD -- live_api_test.go live_api_test_helper_test.go CONTRIBUTING.md README.md examples/examples_test.go
```

Expected: No real API keys, no prompt or credential logging beyond safe test output, and no unrelated rewrites.

- [ ] **Step 4: Commit final adjustments if needed**

If Step 3 exposes small cleanup changes, make them, rerun Steps 1 and 2, then commit:

```bash
git add live_api_test.go live_api_test_helper_test.go CONTRIBUTING.md README.md examples/examples_test.go
git commit -m "test: polish live api test support"
```

If no cleanup changes are needed, do not create an empty commit.

---

## Self-Review

- Spec coverage: Tasks cover `.env` parsing, automatic live config detection, skip behavior, safe output, targeted test commands, documentation, and final verification.
- Placeholder scan: No deferred implementation steps remain; code snippets and commands are concrete.
- Type consistency: Helpers use existing `ModelConfig`, `ModelAPIType`, `NewModel`, `New`, `ObserverFunc`, `Observation`, and `AgentError` names from the SDK.
