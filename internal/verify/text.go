package verify

import "fmt"

// Text renders an event as the command prints it, or "" for an event the
// command doesn't print.
func Text(e Event) string {
	f := e.Fields
	at := fmt.Sprintf("verify %6.1fs", e.T)
	switch e.Type {
	case "verify.start":
		return fmt.Sprintf("%s  start  %s: %s, on a clean %s", at, f["name"], f["spec"], f["image"])
	case "step.start":
		return fmt.Sprintf("%s  step   %s", at, e.Step)
	case "step.end":
		if f["ok"] == true {
			return fmt.Sprintf("%s  ok     %s  (%.1f s)", at, e.Step, f["seconds"])
		}
	case "step.fail":
		if c, ok := f["exit_code"]; ok {
			return fmt.Sprintf("%s  FAIL   %s  (exit code %v)", at, e.Step, c)
		}
		return fmt.Sprintf("%s  FAIL   %s: %v", at, e.Step, f["reason"])
	case "machine.ready":
		return fmt.Sprintf("%s  have   %s", at, f["versions"])
	case "warning":
		return fmt.Sprintf("%s  warn   %s", at, f["message"])
	case "note":
		return fmt.Sprintf("%s  note   %s", at, f["message"])
	case "verify.end":
		code := toInt(f["code"])
		switch code {
		case Ready:
			steps := "steps"
			if toInt(f["steps"]) == 1 {
				steps = "step"
			}
			return fmt.Sprintf("%s  READY  %v; %v %s", at, f["message"], f["steps"], steps)
		case CouldNotRun:
			return fmt.Sprintf("%s  ERROR  %v", at, f["message"])
		default:
			return fmt.Sprintf("%s  NOT READY  %v (exit %d)", at, f["message"], code)
		}
	}
	return ""
}

// toInt reads a number that may have come back from JSON as a float64.
func toInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	}
	return 0
}
