package agent

import (
	"bufio"
	"fmt"
	"io"
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

func TestParseDotEnvForLiveTestsParsesPracticalCredentialFile(t *testing.T) {
	values, err := parseDotEnvForLiveTests(strings.NewReader(`
# local live settings
MODEL_API_TYPE=openai-responses
MODEL_BASE_URL="https://api.openai.com"
MODEL_API_KEY='secret-key'
MODEL_NAME=gpt-test

IGNORED_SPACES = value with spaces
`))
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]string{
		"MODEL_API_TYPE": "openai-responses",
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
