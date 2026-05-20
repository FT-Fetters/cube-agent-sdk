package agent

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const liveAPIDebugSummaryLimitForTest = 260

var (
	liveAPIDebugStatusPatternForTest = regexp.MustCompile(`(?i)\bstatus\s+([0-9]{3})\b`)
	liveAPIDebugURLPatternForTest    = regexp.MustCompile(`https?://[^\s"']+`)
	liveAPIDebugSecretPatternForTest = regexp.MustCompile(`(?i)(api[_-]?key|token|authorization|x-api-key|key)(["']?\s*[:=]\s*["']?)[^"',}\s]+`)
	liveAPIDebugBearerPatternForTest = regexp.MustCompile(`(?i)(bearer\s+)[^\s"',}]+`)
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

	return (&url.URL{
		Scheme:  parsed.Scheme,
		Host:    parsed.Host,
		Path:    parsed.Path,
		RawPath: parsed.RawPath,
	}).String()
}

// formatLiveAPIErrorForTest keeps live failures useful without exposing credentials.
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

// formatLiveAPIModelConstructionErrorForTest avoids printing raw provider configuration errors.
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

func liveAPIDebugEnabledForTest() bool {
	value := strings.TrimSpace(os.Getenv("LIVE_API_DEBUG"))
	return value == "1" || strings.EqualFold(value, "true")
}

func logLiveAPIDebugForTest(t *testing.T, err error, config ModelConfig) {
	t.Helper()

	if !liveAPIDebugEnabledForTest() {
		return
	}
	for _, line := range liveAPIDebugLinesForTest(err, config) {
		t.Log(line)
	}
}

func liveAPIDebugLinesForTest(err error, config ModelConfig) []string {
	fields := []string{"live api debug"}
	if err == nil {
		return []string{strings.Join(append(fields, "error_type=<nil>"), " ")}
	}

	sourceErr := err
	var agentErr *AgentError
	if errors.As(err, &agentErr) {
		if agentErr.Category != "" {
			fields = append(fields, fmt.Sprintf("category=%s", agentErr.Category))
		}
		if agentErr.Operation != "" {
			fields = append(fields, fmt.Sprintf("operation=%s", agentErr.Operation))
		}
		if agentErr.RequestID != "" {
			fields = append(fields, fmt.Sprintf("request=%s", agentErr.RequestID))
		}
		if agentErr.Cause != nil {
			sourceErr = agentErr.Cause
		}
	} else {
		fields = append(fields, fmt.Sprintf("error_type=%T", err))
	}

	summary := sanitizeLiveAPIDebugSummaryForTest(sourceErr.Error(), config)
	if status := liveAPIHTTPStatusForTest(summary); status != "" {
		fields = append(fields, "status="+status)
	}
	if summary != "" {
		fields = append(fields, fmt.Sprintf("summary=%q", summary))
	}
	return []string{strings.Join(fields, " ")}
}

func liveAPIHTTPStatusForTest(summary string) string {
	matches := liveAPIDebugStatusPatternForTest.FindStringSubmatch(summary)
	if len(matches) != 2 {
		return ""
	}
	return matches[1]
}

func sanitizeLiveAPIDebugSummaryForTest(summary string, config ModelConfig) string {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return ""
	}
	if strings.TrimSpace(config.APIKey) != "" {
		summary = strings.ReplaceAll(summary, strings.TrimSpace(config.APIKey), "<redacted>")
	}
	summary = liveAPIDebugURLPatternForTest.ReplaceAllStringFunc(summary, safeBaseURLForLiveTest)
	summary = liveAPIDebugSecretPatternForTest.ReplaceAllString(summary, "$1$2<redacted>")
	summary = liveAPIDebugBearerPatternForTest.ReplaceAllString(summary, "$1<redacted>")
	summary = strings.Join(strings.Fields(summary), " ")
	if len(summary) > liveAPIDebugSummaryLimitForTest {
		summary = summary[:liveAPIDebugSummaryLimitForTest] + "..."
	}
	return summary
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

func TestSafeBaseURLForLiveTestStripsSensitiveURLParts(t *testing.T) {
	got := safeBaseURLForLiveTest("https://user:password@api.example.test/v1/models?api_key=secret#token")
	want := "https://api.example.test/v1/models"
	if got != want {
		t.Fatalf("safe base URL = %q, want %q", got, want)
	}
}

func TestSafeBaseURLForLiveTestRejectsInvalidURL(t *testing.T) {
	got := safeBaseURLForLiveTest("not-a-url")
	if got != "<invalid>" {
		t.Fatalf("safe base URL = %q, want <invalid>", got)
	}
}

func TestFormatLiveAPIErrorForTestOmitsRawAgentErrorText(t *testing.T) {
	err := &AgentError{
		Category:  ErrorCategoryModel,
		Operation: "model.generate",
		RequestID: "req-1",
		Cause:     errors.New("provider failed with api_key=secret"),
	}

	got := formatLiveAPIErrorForTest(err)
	for _, unsafe := range []string{"provider failed", "api_key=secret", "secret"} {
		if strings.Contains(got, unsafe) {
			t.Fatalf("formatted error = %q, want no raw provider text containing %q", got, unsafe)
		}
	}
	for _, want := range []string{"category=model", "operation=model.generate", "request=req-1"} {
		if !strings.Contains(got, want) {
			t.Fatalf("formatted error = %q, want %q", got, want)
		}
	}
}

func TestFormatLiveAPIErrorForTestOmitsRawNonAgentErrorText(t *testing.T) {
	got := formatLiveAPIErrorForTest(errors.New("transport failed with token=secret"))
	if strings.Contains(got, "transport failed") || strings.Contains(got, "token=secret") || strings.Contains(got, "secret") {
		t.Fatalf("formatted error = %q, want no raw error text", got)
	}
	if !strings.Contains(got, "error_type=") {
		t.Fatalf("formatted error = %q, want type metadata", got)
	}
}

func TestFormatLiveAPIModelConstructionErrorForTestOmitsRawConfigText(t *testing.T) {
	_, err := NewModel(ModelConfig{
		APIType: ModelAPIOpenAICompatible,
		BaseURL: "https://api.example.test/%zz?api_key=secret",
		APIKey:  "secret-api-key",
		Model:   "live-model",
	})
	if err == nil {
		t.Fatal("NewModel returned nil error, want malformed base URL error")
	}
	if !strings.Contains(err.Error(), "api_key=secret") {
		t.Fatalf("test setup error = %q, want raw error to contain query secret", err.Error())
	}

	got := formatLiveAPIModelConstructionErrorForTest(err)
	for _, unsafe := range []string{"api.example.test", "%zz", "api_key=secret", "secret"} {
		if strings.Contains(got, unsafe) {
			t.Fatalf("formatted construction error = %q, want no raw config text containing %q", got, unsafe)
		}
	}
	if !strings.Contains(got, "error_type=") {
		t.Fatalf("formatted construction error = %q, want type metadata", got)
	}
}

func TestLiveAPIDebugEnabledForTestReadsExplicitOptIn(t *testing.T) {
	t.Setenv("LIVE_API_DEBUG", "")
	if liveAPIDebugEnabledForTest() {
		t.Fatal("debug enabled with empty LIVE_API_DEBUG, want disabled")
	}

	t.Setenv("LIVE_API_DEBUG", "1")
	if !liveAPIDebugEnabledForTest() {
		t.Fatal("debug disabled with LIVE_API_DEBUG=1, want enabled")
	}
}

func TestLiveAPIDebugLinesForTestIncludeStatusAndRedactSecrets(t *testing.T) {
	err := &AgentError{
		Category:  ErrorCategoryModel,
		Operation: "model.generate",
		RequestID: "req-1",
		Cause:     errors.New(`agent: anthropic messages returned status 401: {"error":"bad key secret-key","url":"https://user:pass@example.test/v1/messages?api_key=secret-key","token":"secret-token"}`),
	}
	config := ModelConfig{
		BaseURL: "https://api.example.test/v1?api_key=secret-key",
		APIKey:  "secret-key",
	}

	lines := liveAPIDebugLinesForTest(err, config)
	if len(lines) != 1 {
		t.Fatalf("debug lines = %#v, want one line", lines)
	}
	line := lines[0]
	for _, want := range []string{"category=model", "operation=model.generate", "request=req-1", "status=401"} {
		if !strings.Contains(line, want) {
			t.Fatalf("debug line = %q, want %q", line, want)
		}
	}
	for _, unsafe := range []string{"secret-key", "secret-token", "user:pass", "api_key=secret-key", `"token":"secret-token"`} {
		if strings.Contains(line, unsafe) {
			t.Fatalf("debug line = %q, want no unsafe text %q", line, unsafe)
		}
	}
}

func TestLiveAPIDebugLinesForTestBoundLongSummary(t *testing.T) {
	err := errors.New("agent: provider returned status 500: " + strings.Repeat("x", 600))

	lines := liveAPIDebugLinesForTest(err, ModelConfig{})
	if len(lines) != 1 {
		t.Fatalf("debug lines = %#v, want one line", lines)
	}
	if len(lines[0]) > 420 {
		t.Fatalf("debug line length = %d, want bounded line", len(lines[0]))
	}
}

func TestRequireLiveModelConfigForTestUsesEnvironmentWithoutDotEnv(t *testing.T) {
	t.Setenv("MODEL_API_TYPE", string(ModelAPIOpenAICompatible))
	t.Setenv("MODEL_BASE_URL", "https://env.example.test")
	t.Setenv("MODEL_API_KEY", "env-key")
	t.Setenv("MODEL_NAME", "env-model")

	loaderCalled := false
	config := requireLiveModelConfigFromSourcesForTest(t, func() (map[string]string, error) {
		loaderCalled = true
		return nil, errors.New("malformed .env should not be parsed when env config is complete")
	})

	if loaderCalled {
		t.Fatal("loaded .env despite complete process environment config")
	}
	if config.APIType != ModelAPIOpenAICompatible || config.BaseURL != "https://env.example.test" || config.APIKey != "env-key" || config.Model != "env-model" {
		t.Fatalf("config = %#v, want environment values", config)
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
