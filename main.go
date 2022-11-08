package main

import (
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sourcegraph/go-diff/diff"
	"golang.org/x/tools/go/packages"
)

var (
	dir       = flag.String("dir", ".", "base directory for the patch")
	rootNames = stringSlice{
		"github.com/cosmos/cosmos-sdk/baseapp.BaseApp.DeliverTx",
	}
)

func init() {
	flag.Var(&rootNames, "roots", "comma-separated list of root functions")
}

func main() {
	flag.Parse()
	*dir, _ = filepath.Abs(*dir)
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "statediff: %v\n", err)
		if errors.Is(err, errStatePatch) {
			// Use a special exit code for patches that touches state code.
			os.Exit(128)
		}
		os.Exit(2)
	}
}

var errStatePatch = errors.New("patch edits state code")

// rootFunction is a representation of a method such as
//
//	example.com/pkg/path.Type.Method
//
// or a function such as
//
//	example.com/pkg/path.Function
type rootFunction struct {
	typ string
	fun string
}

func run() error {
	fset := new(token.FileSet)
	cfg := &packages.Config{
		Fset: fset,
		Mode: packages.NeedImports | packages.NeedSyntax | packages.NeedDeps | packages.NeedName | packages.NeedTypes | packages.NeedTypesInfo,
	}
	var pkgNames []string
	rootFuncs := make(map[rootFunction]bool)
	for _, root := range rootNames {
		var f rootFunction
		lastSlash := strings.LastIndex(root, "/")
		idx := strings.LastIndex(root, ".")
		if idx <= lastSlash {
			return fmt.Errorf("malformed function or method: %s", root)
		}
		f.fun = root[idx+1:]
		root = root[:idx]
		f.typ = root
		if idx := strings.LastIndex(root, "."); idx > lastSlash {
			root = root[:idx]
		}
		pkgNames = append(pkgNames, root)
		rootFuncs[f] = true
	}
	pkgs, err := packages.Load(cfg, pkgNames...)
	if err != nil {
		return err
	}
	patch, err := parsePatch(os.Stdin)
	if err != nil {
		return err
	}
	state := &analyzerState{
		fset:  fset,
		funcs: make(map[*types.Func]BodyInfo),
	}
	imported := make(map[*packages.Package]bool)
	var roots []*types.Func
	var addPkg func(pkg *packages.Package)
	addPkg = func(pkg *packages.Package) {
		if imported[pkg] {
			return
		}
		imported[pkg] = true
		for _, f := range pkg.Syntax {
			for _, decl := range f.Decls {
				switch decl := decl.(type) {
				case *ast.FuncDecl:
					td := pkg.TypesInfo.Defs[decl.Name].(*types.Func)
					inf := BodyInfo{decl, pkg.TypesInfo}
					state.funcs[td] = inf
					rf := rootFunction{fun: td.Name()}
					if recv := td.Type().(*types.Signature).Recv(); recv != nil {
						t := recv.Type()
						if pt, isPointer := t.(*types.Pointer); isPointer {
							t = pt.Elem()
						}
						rf.typ = types.TypeString(t, nil)
					} else {
						rf.typ = pkg.PkgPath
					}
					if rootFuncs[rf] {
						delete(rootFuncs, rf)
						roots = append(roots, td)
					}
				}
			}
		}
		for _, pkg := range pkg.Imports {
			addPkg(pkg)
		}
	}
	for _, pkg := range pkgs {
		addPkg(pkg)
	}
	var missing []string
	for n := range rootFuncs {
		missing = append(missing, n.typ+"."+n.fun)
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing roots: %v", strings.Join(missing, ","))
	}
	for _, root := range roots {
		inspect(state, patch, root, nil)
	}
	for _, hunk := range patch {
		if len(hunk.stack) > 0 {
			err = errStatePatch
			fmt.Println("Patch hunk is potentially state sensitive:")
			fmt.Printf("\n%s\n\n", hunk.hunk.Body)
			fmt.Println("Callstack:")
			for _, e := range hunk.stack {
				pos := fset.Position(e.pos)
				fmt.Printf("%s (%s:%d)\n", e.fun.FullName(), pos.Filename, pos.Line)
			}
		}
	}
	return err
}

func parsePatch(r io.Reader) (Patch, error) {
	diffs := diff.NewMultiFileDiffReader(os.Stdin)
	var p Patch
	for {
		d, err := diffs.ReadFile()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return Patch{}, fmt.Errorf("failed to read diff: %v", err)
		}
		// The original filename without the prefix
		origName := strings.TrimPrefix(d.OrigName, "a/")
		// Make it absolute.
		origName = filepath.Join(*dir, origName)
		for _, hunk := range d.Hunks {
			startLine := int(hunk.OrigStartLine)
			p = append(p, Hunk{
				hunk:      hunk,
				file:      origName,
				startLine: startLine,
				endLine:   startLine + int(hunk.OrigLines),
			})
		}
	}
	sort.Slice(p, func(i, j int) bool {
		h1, h2 := p[i], p[j]
		switch strings.Compare(h1.file, h2.file) {
		case -1:
			return true
		case +1:
			return false
		default:
			return h1.startLine <= h2.startLine
		}
	})
	return p, nil
}

// Patch is a slice of Hunks, sorted by path then starting line.
type Patch []Hunk

type Hunk struct {
	file      string
	startLine int
	endLine   int
	hunk      *diff.Hunk
	stack     []stackEntry
}

type stackEntry struct {
	fun *types.Func
	pos token.Pos
}

// Mark any hunk that overlaps the specified range of lines in file. The stack argument
// is used when printing out clashes.
func (p Patch) Mark(stack []stackEntry, file string, startLine, endLine int) {
	idx := sort.Search(len(p), func(i int) bool {
		h := p[i]
		switch strings.Compare(file, h.file) {
		case -1:
			return true
		case +1:
			return false
		default:
			return startLine <= h.endLine
		}
	})
	for i := idx; i < len(p); i++ {
		h := p[i]
		if h.file != file || h.startLine > endLine {
			break
		}
		// Record the stack, but only if it is shorter than any previous stack.
		if len(p[i].stack) == 0 || len(p[i].stack) > len(stack) {
			p[i].stack = append(p[i].stack[0:], stack...)
		}
	}
}

type BodyInfo struct {
	fun  *ast.FuncDecl
	info *types.Info
}

type analyzerState struct {
	fset  *token.FileSet
	funcs map[*types.Func]BodyInfo
}

func inspect(state *analyzerState, patch Patch, def *types.Func, stack []stackEntry) {
	inf, ok := state.funcs[def]
	if !ok || inf.fun.Body == nil {
		return
	}
	delete(state.funcs, def)
	stack = append(stack, stackEntry{fun: def, pos: inf.fun.Pos()})
	start := state.fset.PositionFor(inf.fun.Body.Pos(), false)
	end := state.fset.PositionFor(inf.fun.Body.End(), false)
	if start.IsValid() && end.IsValid() {
		patch.Mark(stack, start.Filename, start.Line, end.Line)
	}
	ast.Inspect(inf.fun.Body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.CallExpr:
			var id *ast.Ident
			switch fun := n.Fun.(type) {
			case *ast.Ident:
				id = fun
			case *ast.SelectorExpr:
				id = fun.Sel
			}
			switch t := inf.info.Uses[id].(type) {
			case *types.Func:
				inspect(state, patch, t, stack)
			}
		}
		return true
	})
}

type stringSlice []string

func (ss *stringSlice) String() string {
	return strings.Join(*ss, ",")
}

func (ss *stringSlice) Set(flag string) error {
	for _, name := range strings.Split(flag, ",") {
		if len(name) == 0 {
			return errors.New("empty string")
		}
		*ss = append(*ss, name)
	}
	return nil
}
