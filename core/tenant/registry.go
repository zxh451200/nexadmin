package tenant

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Table identifies a tenant-owned table and the column containing its tenant ID.
type Table struct {
	Name   string
	Column string
}

// Provider optionally declares tenant-owned business or control-plane tables.
// Owners omit platform-shared data and append-only audit, usage, and history records unless they remain functionally referenced.
type Provider interface {
	TenantTables() []Table
}

var identifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// BaselineTables returns the always-protected framework tables.
func BaselineTables() []Table {
	return []Table{
		{Name: "sys_user", Column: "tenant_id"},
		{Name: "sys_role", Column: "tenant_id"},
		{Name: "sys_dept", Column: "tenant_id"},
		{Name: "sys_file", Column: "tenant_id"},
		{Name: "sys_notice", Column: "tenant_id"},
		{Name: "sys_job", Column: "tenant_id"},
	}
}

// Tables collects declarations from providers into a new slice.
func Tables(providers []Provider) []Table {
	var tables []Table
	for _, provider := range providers {
		tables = append(tables, provider.TenantTables()...)
	}
	return tables
}

// Validate returns a sorted, duplicate-free copy, rejecting unsafe identifiers and conflicting declarations.
func Validate(tables []Table) ([]Table, error) {
	out := make([]Table, 0, len(tables))
	seen := make(map[string]Table, len(tables))
	for _, table := range tables {
		if !identifier.MatchString(table.Name) {
			return nil, fmt.Errorf("tenant: invalid table name %q", table.Name)
		}
		if !identifier.MatchString(table.Column) {
			return nil, fmt.Errorf("tenant: invalid tenant column %q for table %q", table.Column, table.Name)
		}
		key := strings.ToLower(table.Name)
		if previous, ok := seen[key]; ok {
			if previous == table {
				continue
			}
			return nil, fmt.Errorf("tenant: conflicting declarations for table %q", table.Name)
		}
		seen[key] = table
		out = append(out, table)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name == out[j].Name {
			return out[i].Column < out[j].Column
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}
