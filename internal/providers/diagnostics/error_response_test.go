package diagnostics

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestErrorSummaryFromResponseIgnoresGenericJSONBodies(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "root message", body: `{"message":"generic secret body"}`},
		{name: "string error", body: `{"error":"generic secret error"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := &http.Response{Body: io.NopCloser(strings.NewReader(tt.body))}
			if got := ErrorSummaryFromResponse(response); got != "" {
				t.Fatalf("ErrorSummaryFromResponse() = %q, want empty summary", got)
			}
		})
	}
}
