package codegen

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"nexadmin/core/migrate"
)

// ValidationIssue 是可由界面按 code 翻译的结构化诊断；Detail 保留编译器的原始定位。
type ValidationIssue struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Path     string `json:"path,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

type ValidationCheck struct {
	Code   string `json:"code"`
	Status string `json:"status"`
}

type ValidationReport struct {
	OK        bool              `json:"ok"`
	CheckedAt time.Time         `json:"checkedAt"`
	Checks    []ValidationCheck `json:"checks"`
	Issues    []ValidationIssue `json:"issues"`
}

func newValidationReport() *ValidationReport {
	return &ValidationReport{OK: true, CheckedAt: time.Now().UTC(), Checks: []ValidationCheck{}, Issues: []ValidationIssue{}}
}

func (r *ValidationReport) check(code, status, path, detail string) {
	r.Checks = append(r.Checks, ValidationCheck{Code: code, Status: status})
	if status == "failed" {
		r.OK = false
	}
	if status == "passed" {
		return
	}
	r.Issues = append(r.Issues, ValidationIssue{Code: code, Severity: status, Path: path, Detail: truncateDiagnostic(detail)})
}

func truncateDiagnostic(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= 12000 {
		return s
	}
	return s[:12000] + "\n…"
}

func (s *Service) validateOutput(ctx context.Context, spec *Spec, out *Output, root string) *ValidationReport {
	report := newValidationReport()
	out.Validation = report
	report.check("go_build", "passed", "", "")
	if err := validateGoOutput(ctx, spec, out.Files, root); err != nil {
		code, status := "go_build_failed", "failed"
		if errorsIsUnavailable(err) {
			code, status = "go_build_skipped", "warning"
		}
		report.Checks[len(report.Checks)-1].Status = status
		report.Issues = append(report.Issues, ValidationIssue{Code: code, Severity: status, Detail: truncateDiagnostic(err.Error())})
	}
	if err := validateTypeScriptOutput(ctx, out.Files, root); err != nil {
		status, code := "failed", "typescript_failed"
		if errorsIsUnavailable(err) {
			status, code = "warning", "typescript_skipped"
		}
		report.check(code, status, "", err.Error())
	} else {
		report.check("typescript", "passed", "", "")
	}
	report.check("sql", "passed", "", "")
	sqlCheck := len(report.Checks) - 1
	for _, f := range out.Files {
		if f.Lang != "sql" {
			continue
		}
		engine := ""
		parts := strings.Split(filepath.ToSlash(f.Path), "/")
		if len(parts) >= 4 {
			engine = parts[2]
		}
		stmts, err := migrate.SplitForEngine(engine, f.Content)
		if err != nil || len(stmts) == 0 {
			report.check("sql_failed", "failed", f.Path, fmt.Sprint(err))
			report.Checks[sqlCheck].Status = "failed"
		}
	}
	report.check("migrations", "passed", "", "")
	if issues := s.migrationIssues(out.Files); len(issues) > 0 {
		report.Checks[len(report.Checks)-1].Status = "failed"
		for _, issue := range issues {
			report.check(issue.Code, "failed", issue.Path, issue.Detail)
		}
	}
	return report
}

func errorsIsUnavailable(err error) bool {
	return err != nil && strings.HasPrefix(err.Error(), "unavailable:")
}

func validateGoOutput(ctx context.Context, spec *Spec, files []File, root string) error {
	if root == "" {
		return fmt.Errorf("unavailable: project root with go.mod is not available")
	}
	actual, ok := goModuleLine(filepath.Join(root, "go.mod"))
	if !ok {
		return fmt.Errorf("unavailable: project go.mod has no module directive")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		return fmt.Errorf("unavailable: Go toolchain not found in PATH")
	}
	tmp, err := os.MkdirTemp("", "nexadmin-codegen-go-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	mod := actual + "/_codegen_validate"
	goMod := fmt.Sprintf("module %s\n\ngo 1.26.0\n\nrequire %s v0.0.0\n\nreplace %s => %s\n", mod, actual, actual, filepath.ToSlash(root))
	if _, err := os.Stat(filepath.Join(root, "flowlite", "go.mod")); err == nil {
		goMod += fmt.Sprintf("\nreplace github.com/nexadmin/flowlite => %s\n", filepath.ToSlash(filepath.Join(root, "flowlite")))
	}
	if err := os.WriteFile(filepath.Join(tmp, "go.mod"), []byte(goMod), 0o600); err != nil {
		return err
	}
	for _, f := range files {
		if f.Lang != "go" {
			continue
		}
		content := strings.ReplaceAll(f.Content, spec.GoModule+"/", actual+"/")
		full := filepath.Join(tmp, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			return err
		}
	}
	cmdCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, goBin, "build", "-mod=mod", "-trimpath", "./internal/modules/"+spec.Module)
	cmd.Dir = tmp
	cmd.Env = append(os.Environ(), "GOWORK=off")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		if cmdCtx.Err() != nil {
			detail = "validation timed out: " + detail
		}
		return fmt.Errorf("%w\n%s", err, strings.ReplaceAll(detail, filepath.ToSlash(tmp), "<validation>"))
	}
	return nil
}

func validateTypeScriptOutput(ctx context.Context, files []File, root string) error {
	if root == "" {
		return fmt.Errorf("unavailable: project root is not available")
	}
	web := filepath.Join(root, "web")
	tsc := filepath.Join(web, "node_modules", "typescript", "bin", "tsc")
	if _, err := os.Stat(tsc); err != nil {
		return fmt.Errorf("unavailable: web/node_modules/typescript is not installed")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		return fmt.Errorf("unavailable: Node.js not found in PATH")
	}
	cache := filepath.Join(web, "node_modules", ".cache")
	if err := os.MkdirAll(cache, 0o700); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(cache, "nexadmin-codegen-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	for _, f := range files {
		if f.Lang != "ts" && f.Lang != "tsx" {
			continue
		}
		full := filepath.Join(tmp, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(full, []byte(f.Content), 0o600); err != nil {
			return err
		}
	}
	config := map[string]any{
		"extends": filepath.ToSlash(filepath.Join(web, "tsconfig.json")),
		"compilerOptions": map[string]any{
			"paths":     map[string]any{"@/*": []string{filepath.ToSlash(filepath.Join(web, "src", "*"))}},
			"typeRoots": []string{filepath.ToSlash(filepath.Join(web, "node_modules", "@types"))},
			"types":     []string{"node", "react", "react-dom"},
		},
		"include": []string{
			"web/src/**/*.ts", "web/src/**/*.tsx",
			filepath.ToSlash(filepath.Join(web, "node_modules", "vite", "client.d.ts")),
		},
	}
	raw, _ := json.Marshal(config)
	if err := os.WriteFile(filepath.Join(tmp, "tsconfig.json"), raw, 0o600); err != nil {
		return err
	}
	cmdCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, node, tsc, "--project", filepath.Join(tmp, "tsconfig.json"), "--pretty", "false")
	cmd.Dir = tmp
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		detail = strings.ReplaceAll(detail, filepath.ToSlash(tmp)+"/", "")
		if cmdCtx.Err() != nil {
			detail = "validation timed out: " + detail
		}
		return fmt.Errorf("%w\n%s", err, detail)
	}
	return nil
}

func (s *Service) migrationIssues(files []File) []ValidationIssue {
	var out []ValidationIssue
	applied := map[string]bool{}
	if s.db != nil {
		store, err := migrate.NewGormStore(s.db)
		if err != nil {
			out = append(out, ValidationIssue{Code: "migration_check_failed", Severity: "failed", Detail: err.Error()})
		} else if exists, err := store.HasTable(migrate.Table); err != nil {
			out = append(out, ValidationIssue{Code: "migration_check_failed", Severity: "failed", Detail: err.Error()})
		} else if exists {
			if rows, err := store.Applied(); err != nil {
				out = append(out, ValidationIssue{Code: "migration_check_failed", Severity: "failed", Detail: err.Error()})
			} else {
				for _, rec := range rows {
					if rec.Scope == migrate.ScopeBusiness {
						applied[rec.Version] = true
					}
				}
			}
		}
	}
	root, _ := projectRoot()
	for _, f := range files {
		if f.Lang != "sql" {
			continue
		}
		name := filepath.Base(filepath.FromSlash(f.Path))
		if applied[name] {
			out = append(out, ValidationIssue{Code: "migration_applied", Path: f.Path, Detail: name})
			continue
		}
		if root == "" {
			continue
		}
		full := filepath.Join(root, filepath.FromSlash(f.Path))
		old, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		if migrate.Checksum(string(old)) != migrate.Checksum(f.Content) {
			out = append(out, ValidationIssue{Code: "migration_exists", Path: f.Path, Detail: name})
		}
	}
	return out
}

func contentHash(content string) string {
	sum := sha256.Sum256([]byte(strings.ReplaceAll(content, "\r\n", "\n")))
	return hex.EncodeToString(sum[:])
}
