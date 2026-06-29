package diagnostics

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/cubence/cube-agent-sdk/internal/core"
)

const (
	maxProviderErrorBodyBytes  = 16 * 1024
	maxProviderErrorFieldRunes = 512
	maxProviderSensitiveDepth  = 8
)

var (
	providerErrorURLPattern        = regexp.MustCompile(`https?://[^\s"'<>]+`)
	providerErrorBearerPattern     = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/\-=]+`)
	providerErrorCredentialPattern = regexp.MustCompile(`(?i)\b(api[_-]?key|access[_-]?token|token|password|secret|authorization|cookie)\b\s*[:=]\s*["']?[^"',\s)]+`)
)

type providerErrorDetails struct {
	message string
	typ     string
	code    string
	param   string
}

// ErrorSummaryFromResponse extracts a bounded, sanitized provider error summary
// from known JSON error response shapes. Non-JSON bodies are intentionally
// ignored because provider plaintext errors often echo sensitive request data.
func ErrorSummaryFromResponse(response *http.Response, sensitiveValues ...string) string {
	if response == nil || response.Body == nil {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxProviderErrorBodyBytes+1))
	if err != nil || len(body) == 0 {
		return ""
	}
	if len(body) > maxProviderErrorBodyBytes {
		body = body[:maxProviderErrorBodyBytes]
	}
	details, ok := parseProviderErrorDetails(body)
	if !ok {
		return ""
	}

	redactor := newProviderErrorRedactor(sensitiveValues...)
	details.message = redactor.sanitize(details.message)
	details.typ = redactor.sanitize(details.typ)
	details.code = redactor.sanitize(details.code)
	details.param = redactor.sanitize(details.param)
	return details.summary()
}

// SensitiveValuesFromModelRequest returns high-risk strings from the model
// request that should not be echoed through provider error messages.
func SensitiveValuesFromModelRequest(request core.ModelRequest, values ...string) []string {
	collector := providerErrorSensitiveCollector{values: append([]string(nil), values...)}
	collector.addString(request.SystemPrompt)
	for _, message := range request.Messages {
		collector.addString(message.Content)
		collector.addAny(message.Metadata)
		for _, call := range message.ToolCalls {
			collector.addAny(call.Arguments)
		}
	}
	for _, tool := range request.Tools {
		collector.addString(tool.Description)
		if tool.Parameters != nil {
			collector.addAny(tool.Parameters.JSONSchema())
		}
	}
	for _, server := range request.MCPServers {
		for _, value := range server.Env {
			collector.addString(value)
		}
		collector.addString(server.URL)
	}
	for _, skill := range request.ActiveSkills {
		collector.addString(skill.Description)
		collector.addString(skill.Instructions)
		for _, phrase := range skill.TriggerPhrases {
			collector.addString(phrase)
		}
		collector.addString(skill.Source)
	}
	return collector.values
}

func parseProviderErrorDetails(body []byte) (providerErrorDetails, bool) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return providerErrorDetails{}, false
	}

	root, ok := decoded.(map[string]any)
	if !ok {
		return providerErrorDetails{}, false
	}

	var details providerErrorDetails
	errorObject, ok := root["error"].(map[string]any)
	if !ok {
		return providerErrorDetails{}, false
	}
	details.message = providerErrorScalar(errorObject["message"])
	details.typ = providerErrorScalar(errorObject["type"])
	details.code = providerErrorScalar(errorObject["code"])
	details.param = providerErrorScalar(errorObject["param"])
	return details, details.message != "" || details.typ != "" || details.code != "" || details.param != ""
}

func providerErrorScalar(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(typed)
	case nil:
		return ""
	default:
		return ""
	}
}

func (d providerErrorDetails) summary() string {
	var metadata []string
	if d.typ != "" {
		metadata = append(metadata, "type="+d.typ)
	}
	if d.code != "" {
		metadata = append(metadata, "code="+d.code)
	}
	if d.param != "" {
		metadata = append(metadata, "param="+d.param)
	}
	switch {
	case d.message != "" && len(metadata) > 0:
		return d.message + " (" + strings.Join(metadata, ", ") + ")"
	case d.message != "":
		return d.message
	case len(metadata) > 0:
		return strings.Join(metadata, ", ")
	default:
		return ""
	}
}

type providerErrorRedactor struct {
	sensitiveValues []string
}

type providerErrorSensitiveCollector struct {
	values []string
}

func (c *providerErrorSensitiveCollector) addString(value string) {
	c.values = append(c.values, value)
}

func (c *providerErrorSensitiveCollector) addAny(value any) {
	c.addValue(reflect.ValueOf(value), 0)
}

func (c *providerErrorSensitiveCollector) addValue(value reflect.Value, depth int) {
	if !value.IsValid() || depth > maxProviderSensitiveDepth {
		return
	}
	for value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return
		}
		value = value.Elem()
	}

	switch value.Kind() {
	case reflect.String:
		c.addString(value.String())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		c.addString(strconv.FormatInt(value.Int(), 10))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		c.addString(strconv.FormatUint(value.Uint(), 10))
	case reflect.Float32, reflect.Float64:
		c.addString(strconv.FormatFloat(value.Float(), 'f', -1, value.Type().Bits()))
	case reflect.Map:
		for _, key := range value.MapKeys() {
			c.addValue(value.MapIndex(key), depth+1)
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < value.Len(); i++ {
			c.addValue(value.Index(i), depth+1)
		}
	}
}

func newProviderErrorRedactor(values ...string) providerErrorRedactor {
	seen := make(map[string]struct{})
	for _, value := range values {
		addProviderErrorSensitiveValue(seen, value)
		if parsed, err := url.Parse(strings.TrimSpace(value)); err == nil && parsed.Host != "" {
			addProviderErrorSensitiveValue(seen, parsed.User.Username())
			if password, ok := parsed.User.Password(); ok {
				addProviderErrorSensitiveValue(seen, password)
			}
			for _, queryValues := range parsed.Query() {
				for _, queryValue := range queryValues {
					addProviderErrorSensitiveValue(seen, queryValue)
				}
			}
			addProviderErrorSensitiveValue(seen, parsed.Fragment)
		}
	}
	sensitiveValues := make([]string, 0, len(seen))
	for value := range seen {
		sensitiveValues = append(sensitiveValues, value)
	}
	sort.Slice(sensitiveValues, func(i, j int) bool {
		return len(sensitiveValues[i]) > len(sensitiveValues[j])
	})
	return providerErrorRedactor{sensitiveValues: sensitiveValues}
}

func addProviderErrorSensitiveValue(seen map[string]struct{}, value string) {
	value = strings.TrimSpace(value)
	if len(value) < 4 {
		return
	}
	seen[value] = struct{}{}
}

func (r providerErrorRedactor) sanitize(value string) string {
	value = collapseProviderErrorWhitespace(value)
	if value == "" {
		return ""
	}
	value = providerErrorURLPattern.ReplaceAllStringFunc(value, sanitizeProviderErrorURL)
	value = providerErrorBearerPattern.ReplaceAllString(value, "Bearer <redacted>")
	value = providerErrorCredentialPattern.ReplaceAllString(value, "$1 <redacted>")
	for _, sensitive := range r.sensitiveValues {
		value = strings.ReplaceAll(value, sensitive, "<redacted>")
	}
	return truncateProviderErrorField(strings.TrimSpace(value))
}

func sanitizeProviderErrorURL(raw string) string {
	trimmed := strings.TrimRight(raw, ".,);]}")
	suffix := strings.TrimPrefix(raw, trimmed)
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "<redacted-url>" + suffix
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String() + suffix
}

func collapseProviderErrorWhitespace(value string) string {
	return strings.Join(strings.FieldsFunc(strings.TrimSpace(value), unicode.IsSpace), " ")
}

func truncateProviderErrorField(value string) string {
	runes := 0
	for index := range value {
		if runes >= maxProviderErrorFieldRunes {
			return value[:index] + "..."
		}
		runes++
	}
	return value
}
