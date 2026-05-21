package diagnostics

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/cubence/cube-agent-sdk/internal/core"
)

// New returns provider diagnostics limited to metadata that is safe to surface.
func New(provider string, endpoint string) core.ProviderDiagnostics {
	return core.ProviderDiagnostics{
		Provider:     provider,
		EndpointHost: endpointHost(endpoint),
	}
}

// FromResponse extends endpoint diagnostics with non-sensitive response
// metadata available on provider HTTP failures.
func FromResponse(provider string, endpoint string, response *http.Response) core.ProviderDiagnostics {
	diagnostics := New(provider, endpoint)
	if response == nil {
		return diagnostics
	}
	diagnostics.HTTPStatus = response.StatusCode
	diagnostics.RequestID = providerRequestID(response.Header)
	return diagnostics
}

func endpointHost(endpoint string) string {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return ""
	}
	return parsed.Host
}

func providerRequestID(header http.Header) string {
	for _, name := range []string{"X-Request-Id", "Request-Id"} {
		if value := strings.TrimSpace(header.Get(name)); value != "" {
			return value
		}
	}
	return ""
}
