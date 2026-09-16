package api

import (
	"encoding/json"
	"fmt"
	"io"
)

func writeSSE(w io.Writer, event string, data any) error {
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
	if err != nil {
		return err
	}
	return nil
}
