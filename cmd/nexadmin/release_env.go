package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func checkReleaseMockEnv(root string) error {
	value := ""
	explicit := false
	for _, name := range []string{".env", ".env.local", ".env.production", ".env.production.local"} {
		b, err := os.ReadFile(filepath.Join(root, "web", name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		seen := false
		for _, line := range strings.Split(string(b), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			line = regexp.MustCompile(`^export\s+`).ReplaceAllString(line, "")
			// dotenv also accepts colon assignments; reject unsupported forms.
			if regexp.MustCompile(`^VITE_MOCK(?:\s*:|\s*$)`).MatchString(line) {
				return fmt.Errorf("%s: ambiguous VITE_MOCK declaration", name)
			}
			if _, v, ok := strings.Cut(line, "="); ok {
				v = strings.TrimSpace(v)
				if len(v) > 0 && (v[0] == '\'' || v[0] == '"' || v[0] == '`') && !strings.ContainsRune(v[1:], rune(v[0])) {
					return fmt.Errorf("%s: multiline dotenv value cannot be determined", name)
				}
			}
			key, v, ok := strings.Cut(line, "=")
			if strings.TrimSpace(key) != "VITE_MOCK" {
				continue
			}
			if !ok || seen {
				return fmt.Errorf("%s: missing or duplicate VITE_MOCK", name)
			}
			seen = true
			v = strings.TrimSpace(v)
			if len(v) > 0 && (v[0] == '\'' || v[0] == '"') {
				q := v[0]
				end := strings.IndexByte(v[1:], q)
				if end < 0 {
					return fmt.Errorf("%s: unclosed value", name)
				}
				end++
				rest := strings.TrimSpace(v[end+1:])
				if rest != "" && !strings.HasPrefix(rest, "#") {
					return fmt.Errorf("%s: ambiguous value", name)
				}
				v = v[1:end]
			} else {
				v, _, _ = strings.Cut(v, "#")
				v = strings.TrimSpace(v)
			}
			if v != "true" && v != "false" {
				return fmt.Errorf("%s: cannot determine VITE_MOCK", name)
			}
			value = v
			if name == ".env.production" {
				explicit = true
			}
		}
	}
	if v, ok := os.LookupEnv("VITE_MOCK"); ok {
		value = v
	}
	if !explicit || value != "false" {
		return fmt.Errorf("production must explicitly disable VITE_MOCK (effective %q)", value)
	}
	return nil
}
