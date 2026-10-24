package main

import (
	"bytes"
	"crypto/ed25519"
	_ "embed"
	"encoding/base64"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

//go:embed tpl/release_pubkey.go.txt
var releaseKeyRuntime string

// Only the runtime builtinPublicKeysB64 declaration and its constant references
// are accepted. Dynamic expressions fail closed; unrelated strings are ignored.
func checkReleaseTargetKeys(root string) error {
	f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, "core/plugin/pubkey.go"), nil, 0)
	if err != nil {
		return err
	}
	if err := checkKeyRuntime(root, f); err != nil {
		return err
	}
	constants := map[string]ast.Expr{}
	var collection ast.Expr
	for _, decl := range f.Decls {
		g, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range g.Specs {
			v, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range v.Names {
				if g.Tok == token.CONST && i < len(v.Values) {
					constants[name.Name] = v.Values[i]
				}
				if name.Name == "builtinPublicKeysB64" {
					if collection != nil || len(v.Names) != 1 || len(v.Values) != 1 {
						return fmt.Errorf("ambiguous key definition")
					}
					collection = v.Values[0]
				}
			}
		}
	}
	var literal func(ast.Expr, map[string]bool) (string, error)
	literal = func(e ast.Expr, seen map[string]bool) (string, error) {
		switch x := e.(type) {
		case *ast.BasicLit:
			if x.Kind == token.STRING {
				return strconv.Unquote(x.Value)
			}
		case *ast.Ident:
			if !seen[x.Name] && constants[x.Name] != nil {
				seen[x.Name] = true
				defer delete(seen, x.Name)
				return literal(constants[x.Name], seen)
			}
		}
		return "", fmt.Errorf("cannot determine key literal")
	}
	list, ok := collection.(*ast.CompositeLit)
	if !ok {
		return fmt.Errorf("cannot determine key collection")
	}
	typ, ok := list.Type.(*ast.ArrayType)
	if !ok || typ.Len != nil {
		return fmt.Errorf("expected []string")
	}
	elem, ok := typ.Elt.(*ast.Ident)
	if !ok || elem.Name != "string" {
		return fmt.Errorf("expected []string")
	}
	if len(list.Elts) == 0 {
		return fmt.Errorf("empty key collection")
	}
	validate := func(value string) error {
		key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(value))
		if err != nil || len(key) != ed25519.PublicKeySize {
			return fmt.Errorf("invalid public key encoding")
		}
		if base64.StdEncoding.EncodeToString(key) == "Z0vlQMyRQ6f4GlrWdKOdJ5laJlIaaKYxzsfS1AXFLtI=" {
			return fmt.Errorf("development public key")
		}
		return nil
	}
	for _, e := range list.Elts {
		v, err := literal(e, map[string]bool{})
		if err != nil {
			return err
		}
		if err = validate(v); err != nil {
			return err
		}
	}
	// Runtime appends keys from this file; malformed lines must not silently pass.
	data, err := os.ReadFile(filepath.Join(root, "config/license-keys.pub"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line, _, _ = strings.Cut(line, "#")
		if strings.TrimSpace(line) != "" {
			if err := validate(line); err != nil {
				return err
			}
		}
	}
	return nil
}

// Accept only runtime readers whose data flow is known to this checker. A changed
// reader must be reviewed with the parser rather than silently guessing its keys.
func checkKeyRuntime(root string, target *ast.File) error {
	reference, err := parser.ParseFile(token.NewFileSet(), "runtime.go", releaseKeyRuntime, 0)
	if err != nil {
		return err
	}
	functions := func(f *ast.File) map[string]string {
		out := map[string]string{}
		for _, d := range f.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok {
				var b bytes.Buffer
				_ = format.Node(&b, token.NewFileSet(), fn)
				out[fn.Name.Name] = b.String()
			}
		}
		return out
	}
	expected, actual := functions(reference), functions(target)
	for _, name := range []string{"BuiltinKeys", "PublicKeys", "parseKey", "KeyID"} {
		if actual[name] != expected[name] {
			return fmt.Errorf("cannot determine runtime key reader %s", name)
		}
	}
	pathOK := false
	for _, d := range target.Decls {
		if g, ok := d.(*ast.GenDecl); ok && g.Tok == token.CONST {
			for _, sp := range g.Specs {
				if v, ok := sp.(*ast.ValueSpec); ok && len(v.Names) == 1 && v.Names[0].Name == "LicenseKeysFile" && len(v.Values) == 1 {
					if lit, ok := v.Values[0].(*ast.BasicLit); ok {
						value, _ := strconv.Unquote(lit.Value)
						pathOK = value == "config/license-keys.pub"
					}
				}
			}
		}
	}
	if !pathOK {
		return fmt.Errorf("cannot determine runtime key file")
	}
	files, err := filepath.Glob(filepath.Join(root, "core/plugin/*.go"))
	if err != nil {
		return err
	}
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		for _, d := range f.Decls {
			if filepath.Base(path) == "pubkey.go" {
				if fn, ok := d.(*ast.FuncDecl); ok && fn.Name.Name == "BuiltinKeys" {
					continue
				}
				if g, ok := d.(*ast.GenDecl); ok {
					declaration := false
					for _, sp := range g.Specs {
						if v, ok := sp.(*ast.ValueSpec); ok {
							for _, n := range v.Names {
								if n.Name == "builtinPublicKeysB64" {
									declaration = true
								}
							}
						}
					}
					if declaration {
						continue
					}
				}
			}
			uncertain := false
			ast.Inspect(d, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok && id.Name == "builtinPublicKeysB64" {
					uncertain = true
				}
				return true
			})
			if uncertain {
				return fmt.Errorf("key collection referenced outside known reader: %s", path)
			}
		}
	}
	return nil
}
