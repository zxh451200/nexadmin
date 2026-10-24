package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReleaseMockEnv(t *testing.T) {
	for _, tt := range []struct {
		name, prod, local, process string
		bad                        bool
	}{
		{"comment", "# VITE_MOCK=false", "", "", true}, {"duplicate", "VITE_MOCK=false\nVITE_MOCK=true", "", "", true},
		{"quoted", "VITE_MOCK='false' # ok", "", "", false}, {"missing", "OTHER=1", "", "", true},
		{"local", "VITE_MOCK=false", "VITE_MOCK=true", "", true}, {"process", "VITE_MOCK=false", "", "true", true},
		{"disabled", "VITE_MOCK=false", "", "false", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			w := filepath.Join(root, "web")
			if err := os.Mkdir(w, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(w, ".env.production"), []byte(tt.prod), 0644); err != nil {
				t.Fatal(err)
			}
			if tt.local != "" {
				if err := os.WriteFile(filepath.Join(w, ".env.production.local"), []byte(tt.local), 0644); err != nil {
					t.Fatal(err)
				}
			}
			if tt.process != "" {
				t.Setenv("VITE_MOCK", tt.process)
			} else {
				old, had := os.LookupEnv("VITE_MOCK")
				os.Unsetenv("VITE_MOCK")
				t.Cleanup(func() {
					if had {
						os.Setenv("VITE_MOCK", old)
					}
				})
			}
			if err := checkReleaseMockEnv(root); (err != nil) != tt.bad {
				t.Fatalf("error=%v", err)
			}
		})
	}
}
