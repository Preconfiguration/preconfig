// Command events renders verify's JSON Lines events as the command prints them,
// for the demo page: it reads events on stdin and writes a JSON list of
// {"t": seconds, "type": event type, "line": text} on stdout.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"

	"preconfiguration.com/preconfig/internal/verify"
)

type line struct {
	T     float64        `json:"t"`
	Type  string         `json:"type"`
	Step  string         `json:"step,omitempty"`
	Line  string         `json:"line"`
	Field map[string]any `json:"fields,omitempty"`
}

func main() {
	var out []line
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var e verify.Event
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			fmt.Fprintln(os.Stderr, "events:", err)
			os.Exit(1)
		}
		out = append(out, line{T: e.T, Type: e.Type, Step: e.Step, Line: verify.Text(e), Field: e.Fields})
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(out); err != nil {
		fmt.Fprintln(os.Stderr, "events:", err)
		os.Exit(1)
	}
}
