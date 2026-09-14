package api

import (
	"encoding/json"
	"io"
	"fmt"
)

func writeSSE(w io.Writer, event string, data any) error {
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err := fmt.Fprintf("event: %s\ndata: %s\n\n", event, b)
	if err != nil {
		return err
	}
	return nil
}
