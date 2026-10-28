package i18n

import (
	"errors"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The catalog test (v1.10.1) reads the server's code and finds what it
// says to people: the messages of errors (apperr.New and WithCode, and the
// packages' own invalid, notFound, newErr, apiError) and the texts marked
// with N. Each must have English, with as many values, and the catalog has
// nothing the code no longer says.

var cjk = regexp.MustCompile(`[\p{Han}]`)

// skipped: the command line and the demo data speak Chinese to the
// administrator; the parsers and prompts are not shown to people.
var skippedDirs = map[string]bool{"demo": true, "i18n": true}

type found struct {
	key, pos string
}

func scanCode(t *testing.T) (keys []found, unresolved []string) {
	t.Helper()
	fset := token.NewFileSet()
	// only the files this build compiles (v1.13: the paid modules' with -tags ee)
	bctx := build.Default
	if eeBuild {
		bctx.BuildTags = []string{"ee"}
	}
	var err error
	for _, root := range []string{"..", "../../ee"} {
		if _, statErr := os.Stat(root); statErr != nil {
			continue // the open-source tree has no ee
		}
		err = errors.Join(err, filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if skippedDirs[d.Name()] {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || strings.Contains(path, string(filepath.Separator)+"gen"+string(filepath.Separator)) {
				return nil
			}
			if ok, err := bctx.MatchFile(filepath.Dir(path), filepath.Base(path)); err != nil || !ok {
				return err
			}
			f, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				return err
			}
			ast.Inspect(f, func(n ast.Node) bool {
				var arg ast.Expr
				switch x := n.(type) {
				case *ast.CallExpr:
					arg = messageArg(x)
				case *ast.CompositeLit:
					if id, ok := x.Type.(*ast.Ident); ok && id.Name == "apiError" && len(x.Elts) == 3 {
						arg = x.Elts[2]
					}
					// Error{Message: …}: the parser's errors, shown as a failed upload's reason
					for _, el := range x.Elts {
						if kv, ok := el.(*ast.KeyValueExpr); ok {
							if k, ok := kv.Key.(*ast.Ident); ok && k.Name == "Message" {
								arg = kv.Value
							}
						}
					}
				}
				if arg == nil {
					return true
				}
				pos := fset.Position(arg.Pos()).String()
				key, ok := literal(arg)
				if !ok {
					if hasCJK(arg) { // Chinese glued to a value: write it as fmt.Sprintf
						unresolved = append(unresolved, pos)
					}
					return true
				}
				if cjk.MatchString(key) {
					keys = append(keys, found{key, pos})
				}
				return true
			})
			return nil
		}))
	}
	if err != nil {
		t.Fatal(err)
	}
	return keys, unresolved
}

// hasCJK reports whether an expression contains a Chinese string.
func hasCJK(e ast.Expr) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if b, ok := n.(*ast.BasicLit); ok && b.Kind == token.STRING && cjk.MatchString(b.Value) {
			found = true
		}
		return !found
	})
	return found
}

// messageArg is the message argument of a call that makes a message.
func messageArg(c *ast.CallExpr) ast.Expr {
	at := func(i int) ast.Expr {
		if i < len(c.Args) {
			return c.Args[i]
		}
		return nil
	}
	switch fn := c.Fun.(type) {
	case *ast.SelectorExpr:
		pkg, ok := fn.X.(*ast.Ident)
		if !ok {
			return nil
		}
		switch pkg.Name + "." + fn.Sel.Name {
		case "apperr.New", "i18n.N":
			if pkg.Name == "i18n" {
				return at(0)
			}
			return at(1)
		case "apperr.WithCode":
			return at(2)
		}
	case *ast.Ident:
		switch fn.Name {
		case "invalid", "notFound":
			return at(0)
		case "newErr":
			return at(1)
		case "N":
			return at(0)
		}
	}
	return nil
}

// literal is the text of a message argument: a string, a concatenation of
// strings, or the format of fmt.Sprintf.
func literal(e ast.Expr) (string, bool) {
	switch x := e.(type) {
	case *ast.BasicLit:
		if x.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(x.Value)
		return s, err == nil
	case *ast.BinaryExpr:
		if x.Op != token.ADD {
			return "", false
		}
		a, ok1 := literal(x.X)
		b, ok2 := literal(x.Y)
		return a + b, ok1 && ok2
	case *ast.ParenExpr:
		return literal(x.X)
	case *ast.CallExpr:
		if sel, ok := x.Fun.(*ast.SelectorExpr); ok {
			if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "fmt" && sel.Sel.Name == "Sprintf" && len(x.Args) > 0 {
				return literal(x.Args[0])
			}
			if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "i18n" && sel.Sel.Name == "N" && len(x.Args) > 0 {
				return literal(x.Args[0])
			}
		}
	}
	return "", false
}

func TestMessagesAreFormats(t *testing.T) {
	_, unresolved := scanCode(t)
	sort.Strings(unresolved)
	if len(unresolved) > 0 {
		t.Errorf("%d messages glue Chinese to values; write them with fmt.Sprintf so they can be translated:\n%s", len(unresolved), strings.Join(unresolved, "\n"))
	}
}

func TestCatalogCoversTheCode(t *testing.T) {
	keys, _ := scanCode(t)
	en := Entries(En)
	var missing []string
	used := map[string]bool{}
	for _, k := range keys {
		used[k.key] = true
		if _, ok := en[k.key]; !ok {
			missing = append(missing, k.pos+" "+k.key)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("%d messages have no English:\n%s", len(missing), strings.Join(missing, "\n"))
	}
	var unused []string
	for k := range en {
		if !used[k] {
			unused = append(unused, k)
		}
	}
	sort.Strings(unused)
	if len(unused) > 0 {
		t.Errorf("%d catalog entries the code no longer says:\n%s", len(unused), strings.Join(unused, "\n"))
	}
}

func TestCatalogKeepsTheValues(t *testing.T) {
	for zh, en := range Entries(En) {
		if a, b := len(Verbs(zh)), len(Verbs(en)); a != b {
			t.Errorf("%q has %d values, the English %q has %d", zh, a, en, b)
		}
		if cjk.MatchString(en) && !strings.Contains(en, "“") {
			// English may quote Chinese names, never be Chinese itself
			t.Errorf("English for %q is not English: %q", zh, en)
		}
	}
}

// The catalog files are merged into one map: a text in two files must have
// the same English there, or which one wins depends on the order of init.
func TestCatalogFilesAgree(t *testing.T) {
	files, _ := filepath.Glob("en_*.go")
	seen := map[string][2]string{} // key -> file, English
	fset := token.NewFileSet()
	var clash []string
	for _, name := range files {
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			kv, ok := n.(*ast.KeyValueExpr)
			if !ok {
				return true
			}
			k, ok1 := literal(kv.Key)
			v, ok2 := literal(kv.Value)
			if !ok1 || !ok2 {
				return true
			}
			if prev, ok := seen[k]; ok && prev[1] != v {
				clash = append(clash, k+": "+prev[0]+" “"+prev[1]+"” / "+name+" “"+v+"”")
			}
			seen[k] = [2]string{name, v}
			return true
		})
	}
	sort.Strings(clash)
	if len(clash) > 0 {
		t.Errorf("%d texts have different English in two files:\n%s", len(clash), strings.Join(clash, "\n"))
	}
}
