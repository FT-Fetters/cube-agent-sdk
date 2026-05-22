package sse

import (
	"bufio"
	"context"
	"io"
	"strings"
)

const maxScannerBuffer = 4 * 1024 * 1024

// Event is one Server-Sent Events frame. Data joins multiple data lines with a
// newline, matching the SSE framing rules providers use for streaming JSON.
type Event struct {
	Name string
	Data string
}

// Read consumes Server-Sent Events frames from r and calls handle for each
// complete frame. It ignores comments and preserves only event/data fields.
func Read(ctx context.Context, r io.Reader, handle func(Event) error) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024), maxScannerBuffer)

	var name string
	var data []string
	dispatch := func() error {
		if name == "" && len(data) == 0 {
			return nil
		}
		event := Event{Name: name, Data: strings.Join(data, "\n")}
		name = ""
		data = nil
		return handle(event)
	}

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		line := scanner.Text()
		if line == "" {
			if err := dispatch(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, ok := strings.Cut(line, ":")
		if !ok {
			field = line
			value = ""
		} else if strings.HasPrefix(value, " ") {
			value = strings.TrimPrefix(value, " ")
		}
		switch field {
		case "event":
			name = value
		case "data":
			data = append(data, value)
		}
	}
	if err := scanner.Err(); err != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return err
		}
	}
	return dispatch()
}
