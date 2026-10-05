package doctor

import "regexp"

// Secrets in logs. Doctor masks them before a line is shown or kept, so a
// diagnosis can be pasted into an issue or a chat without leaking anything.
var maskRes = []struct {
	re   *regexp.Regexp
	with string
}{
	// A password in a URL: postgres://user:password@host keeps the user.
	{regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.-]*://[^:/@\s]+:)[^@\s/]+@`), "${1}***@"},
	// Tokens with a recognizable prefix.
	{regexp.MustCompile(`\b(gh[pousr]_)[A-Za-z0-9]{20,}`), "${1}***"},
	{regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{20,}`), "github_pat_***"},
	{regexp.MustCompile(`\bglpat-[A-Za-z0-9_-]{20,}`), "glpat-***"},
	{regexp.MustCompile(`\bnpm_[A-Za-z0-9]{30,}`), "npm_***"},
	{regexp.MustCompile(`\bpypi-[A-Za-z0-9_-]{40,}`), "pypi-***"},
	{regexp.MustCompile(`\b(sk-(?:ant-|proj-)?)[A-Za-z0-9_-]{20,}`), "${1}***"},
	{regexp.MustCompile(`\bxox[abposr]-[A-Za-z0-9-]{10,}`), "xox*-***"},
	{regexp.MustCompile(`\b(AKIA|ASIA)[0-9A-Z]{16}\b`), "${1}***"},
	{regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}`), "AIza***"},
	{regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`), "***jwt***"},
	{regexp.MustCompile(`(?i)\b(Bearer|Basic|token)\s+[A-Za-z0-9._~+/=-]{16,}`), "$1 ***"},
	{regexp.MustCompile(`(?i)(authorization:\s*)\S.*`), "${1}***"},
	{regexp.MustCompile(`-----BEGIN ([A-Z ]*)PRIVATE KEY-----.*`), "-----BEGIN ${1}PRIVATE KEY----- ***"},
	// NAME=value or NAME: value, when the name says it is a secret.
	{regexp.MustCompile(`(?i)\b([A-Z0-9_]*(?:TOKEN|SECRET|PASSWORD|PASSWD|API_?KEY|PRIVATE_?KEY|ACCESS_?KEY|CREDENTIALS?)[A-Z0-9_]*)(\s*[=:]\s*["']?)([^\s"',;]{4,})`), "$1$2***"},
}

// Mask replaces anything in s that looks like a secret with ***.
func Mask(s string) string {
	for _, m := range maskRes {
		s = m.re.ReplaceAllString(s, m.with)
	}
	return s
}
