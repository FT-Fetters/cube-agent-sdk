package agent

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// parseDotEnvForLiveTests supports the small .env subset needed by live tests.
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

// findRepoRootForLiveTests walks upward so live tests can load root-level files from any package.
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

// loadRootDotEnvForLiveTests treats a missing root .env as an empty live-test configuration.
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

// requiredLiveModelEnvForTests lists the credentials needed to run live API tests.
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

// requireLiveModelConfigForTest skips live tests unless a complete root or environment config exists.
func requireLiveModelConfigForTest(t *testing.T) ModelConfig {
	t.Helper()

	workingDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root, err := findRepoRootForLiveTests(workingDir)
	if err != nil {
		t.Fatal(err)
	}
	dotEnv, err := loadRootDotEnvForLiveTests(root)
	if err != nil {
		t.Fatal(err)
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

// formatLiveAPIErrorForTest keeps live failures useful without exposing credentials.
func formatLiveAPIErrorForTest(err error) string {
	var agentErr *AgentError
	if errors.As(err, &agentErr) {
		return fmt.Sprintf(
			"%v category=%s operation=%s request=%s",
			err,
			agentErr.Category,
			agentErr.Operation,
			agentErr.RequestID,
		)
	}
	return err.Error()
}

// logLiveAPIObservationsForTest emits only the sanitized telemetry fields safe for verbose test logs.
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
		"MODEL_API_TYPE": "openai-compatible",
		"MODEL_BASE_URL": "https://api.openai.com",
		"MODEL_API_KEY":  "secret-key",
		"MODEL_NAME":     "gpt-test",
		"IGNORED_SPACES": "value with spaces",
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

func TestLoadLiveModelConfigForTestsUsesEnvironmentBeforeDotEnv(t *testing.T) {
	t.Setenv("MODEL_API_TYPE", string(ModelAPIOpenAICompatible))
	t.Setenv("MODEL_BASE_URL", "https://env.example.test")
	t.Setenv("MODEL_API_KEY", "env-key")
	t.Setenv("MODEL_NAME", "env-model")
	t.Setenv("ANTHROPIC_VERSION", "2025-01-01")

	config, skip, err := liveModelConfigForTests(map[string]string{
		"MODEL_API_TYPE":    string(ModelAPIAnthropicMessages),
		"MODEL_BASE_URL":    "https://dotenv.example.test",
		"MODEL_API_KEY":     "dotenv-key",
		"MODEL_NAME":        "dotenv-model",
		"ANTHROPIC_VERSION": "2023-06-01",
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
