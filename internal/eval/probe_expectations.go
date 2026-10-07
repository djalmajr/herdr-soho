package eval

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// expectedProbe identifies one hidden top-level test function by the probe
// package import path (which probe package binary compiles it) and the test
// name (which anchored -test.run executes it). Per-probe process isolation
// keeps a same-named test in the worker package from overwriting or
// inflating the hidden results: the worker package never enters the probe
// binary, and each probe runs alone in its own owned process.
type expectedProbe struct {
	pkg  string
	test string
}

// deriveExpectedProbes parses the hidden probe sources and returns the
// top-level Test functions (func TestXxx(t *testing.T)) in deterministic
// order: file order as discovered by listProbeFiles, then declaration order
// inside each file. TestMain, methods, helpers and anything outside the
// probe files are not part of the expected set. The package import path is
// the module path joined with the probe file's directory relative to the
// module root, which is exactly the go test -json Package field.
func deriveExpectedProbes(modulePath string, moduleRoot string, probeFiles []string) ([]expectedProbe, error) {
	expected := []expectedProbe{}
	for _, probeFile := range probeFiles {
		relative, err := filepath.Rel(moduleRoot, probeFile)
		if err != nil {
			return nil, err
		}
		dir := filepath.ToSlash(filepath.Dir(relative))
		pkg := modulePath
		if dir != "." {
			pkg += "/" + dir
		}
		fset := token.NewFileSet()
		source, err := parser.ParseFile(fset, probeFile, nil, 0)
		if err != nil {
			return nil, fmt.Errorf("probe source does not parse: %s: %v", relative, err)
		}
		for _, decl := range source.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Name.Name == "TestMain" {
				continue
			}
			if !strings.HasPrefix(fn.Name.Name, "Test") || !takesTestingT(fn) {
				continue
			}
			expected = append(expected, expectedProbe{pkg: pkg, test: fn.Name.Name})
		}
	}
	return expected, nil
}

// takesTestingT reports whether the function has exactly one parameter,
// *testing.T, as go test requires of a top-level test function.
func takesTestingT(fn *ast.FuncDecl) bool {
	params := fn.Type.Params
	if params == nil || len(params.List) != 1 {
		return false
	}
	star, ok := params.List[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	sel, ok := star.X.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkgIdent, ok := sel.X.(*ast.Ident)
	return ok && pkgIdent.Name == "testing" && sel.Sel.Name == "T"
}

// modulePathFromGoMod reads the module declaration of a go.mod file, in the
// single-line form the runner writes or the fixture supplies.
func modulePathFromGoMod(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(data), "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		fields := strings.Fields(line)
		if fields[0] != "module" {
			continue
		}
		if len(fields) >= 2 {
			return cleanModulePath(fields[1]), nil
		}
		if len(fields) == 1 && fields[0] == "module(" {
			// The block form: module ( \n \tpath \n )
			for j := i + 1; j < len(lines); j++ {
				body := strings.TrimSpace(lines[j])
				if body == ")" {
					break
				}
				if body == "" || strings.HasPrefix(body, "//") {
					continue
				}
				return cleanModulePath(strings.Fields(body)[0]), nil
			}
		}
		return "", fmt.Errorf("go.mod has a malformed module declaration")
	}
	return "", fmt.Errorf("go.mod has no module declaration")
}

func cleanModulePath(value string) string {
	value = strings.Trim(value, "`\"")
	if value == "" {
		return ""
	}
	return value
}
