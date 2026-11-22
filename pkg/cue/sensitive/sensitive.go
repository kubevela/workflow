/*
Copyright 2026 The KubeVela Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package sensitive reads the `// +sensitive` markers out of a step template,
// so the engine can tell which step outputs hold secret data and keep them out
// of the plaintext workflow context ConfigMap.
//
// The marker is the one KubeVela already has: SourceDefinitions mark a field
// `// +sensitive` and the mark covers everything beneath it (KEP-2.16). This is
// a deliberate copy of kubevela's extractor in pkg/sources/source_sensitive.go
// -- workflow cannot import kubevela -- and sharing one implementation between
// the two is tracked in kubevela/pkg#162. Three things differ from that copy,
// and whoever does #162 should fold them in rather than regress them:
//
//  1. Paths are absolute from the template root, not relative to a named block.
//     A source read is relative to the schema root, so kubevela scans `schema:`
//     and `output:` and records paths beneath them. A step output's `valueFrom`
//     is a path from the template root ("output.value.data"), so every
//     top-level field is walked and its name becomes the first segment. That
//     also means no block allowlist: a marker anywhere in the template is
//     honoured, which is the safe direction to be wrong in.
//
//  2. `if` and `for` bodies are descended into. kubevela's walker looks at
//     *ast.Field elements only and so skips *ast.Comprehension, which in a
//     step template is where most of the fields live -- read-object's whole
//     `output:` is two `if` arms. A mark inside one would have been missed.
//     Embedded expressions (`op.#Read & {...}`) are descended into for the same
//     reason.
//
//  3. A template that does not parse is an error, not an empty result.
//     Returning no paths for a template whose markers could not be read is
//     silent under-redaction: the caller cannot tell "nothing is secret" from
//     "I could not tell", and would route a credential to the ConfigMap.
package sensitive

import (
	"strconv"
	"strings"
	"sync"

	"cuelang.org/go/cue/ast"
	cueparser "cuelang.org/go/cue/parser"
	"github.com/pkg/errors"
)

// Marker is the comment a definition author writes to say a field holds secret
// data. The mark covers the field and everything beneath it.
const Marker = "+sensitive"

// ExtractPaths reports the dotted template paths marked `// +sensitive`.
//
// Paths are absolute from the template root, so they compare directly against
// a step output's valueFrom. A marked struct is reported once, at its own path;
// its fields are covered by Covers rather than enumerated, since a mark covers
// everything beneath it and an open struct has no fields to enumerate.
//
// An unparseable template is an error. See the package comment: a caller given
// an empty list cannot tell it apart from a template with nothing to protect.
func ExtractPaths(template string) ([]string, error) {
	f, err := cueparser.ParseFile("-", template, cueparser.ParseComments)
	if err != nil {
		return nil, errors.Wrap(err, "parse template for +sensitive markers")
	}
	if f == nil {
		return nil, errors.New("parse template for +sensitive markers: no file")
	}
	var paths []string
	seen := map[string]bool{}
	for _, decl := range f.Decls {
		collect(decl, nil, &paths, seen)
	}
	return paths, nil
}

// collect records the dotted path of a declaration carrying the marker, then
// descends into whatever structs sit under it.
func collect(decl ast.Decl, prefix []string, out *[]string, seen map[string]bool) {
	switch d := decl.(type) {
	case *ast.Field:
		name := labelName(d.Label)
		if name == "" {
			// An interpolated or otherwise dynamic label names no path we could
			// match a valueFrom against, but a mark under it still has to be
			// found, so descend without extending the prefix.
			descend(d.Value, prefix, out, seen)
			return
		}
		// A fresh slice each time: appending into prefix would share its backing
		// array with every sibling, and one retained path would then change
		// under another.
		path := append(append([]string{}, prefix...), name)
		if hasMarker(d) {
			record(strings.Join(path, "."), out, seen)
		}
		descend(d.Value, path, out, seen)

	case *ast.Comprehension:
		// An `if` or `for` body contributes its fields at the enclosing level:
		// a mark inside `if parameter.withAuth { // +sensitive \n token: ... }`
		// is "token", because that is the path the field has once the
		// comprehension is evaluated.
		descend(d.Value, prefix, out, seen)

	case *ast.EmbedDecl:
		// `op.#Read & {...}` embeds a struct whose fields are the enclosing
		// struct's fields, so they share its path.
		descend(d.Expr, prefix, out, seen)
	}
}

// descend walks the struct literals an expression contributes, recording marks
// beneath the given prefix.
func descend(v ast.Expr, prefix []string, out *[]string, seen map[string]bool) {
	for _, st := range structsUnder(v) {
		for _, elt := range st.Elts {
			collect(elt, prefix, out, seen)
		}
	}
}

// structsUnder returns the struct literals an expression contributes: the value
// itself, the elements of a list, or the operands of a conjunction.
func structsUnder(v ast.Expr) []*ast.StructLit {
	switch val := v.(type) {
	case *ast.StructLit:
		return []*ast.StructLit{val}
	case *ast.ParenExpr:
		return structsUnder(val.X)
	case *ast.BinaryExpr:
		// `#Read & {...}` and `*{...} | {...}`: a mark can sit in either side.
		return append(structsUnder(val.X), structsUnder(val.Y)...)
	case *ast.UnaryExpr:
		return structsUnder(val.X)
	case *ast.ListLit:
		// A list element shares its parent's path: a mark inside
		// `members: [...{token: string}]` is "members.token", applying to the
		// token of every member. Covers treats indices the same way.
		var structs []*ast.StructLit
		for _, elt := range val.Elts {
			if e, ok := elt.(*ast.Ellipsis); ok {
				structs = append(structs, structsUnder(e.Type)...)
				continue
			}
			structs = append(structs, structsUnder(elt)...)
		}
		return structs
	case *ast.Ellipsis:
		return structsUnder(val.Type)
	}
	return nil
}

func record(path string, out *[]string, seen map[string]bool) {
	if path == "" || seen[path] {
		return
	}
	seen[path] = true
	*out = append(*out, path)
}

// hasMarker reports whether the marker sits on this field. It has to be on the
// field itself: a marker on a block does not mark every field in it.
func hasMarker(field *ast.Field) bool {
	for _, cg := range field.Comments() {
		for _, c := range cg.List {
			if strings.Contains(c.Text, Marker) {
				return true
			}
		}
	}
	return false
}

func labelName(label ast.Label) string {
	switch v := label.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.BasicLit:
		return strings.Trim(v.Value, "\"")
	default:
		return ""
	}
}

// Covers reports whether reading path would expose data that one of the marked
// paths protects.
//
// The test runs in both directions, which is what makes it safe:
//
//   - path at or beneath a mark is the ordinary case -- reading
//     "output.value.data.password" against a mark of "output.value.data".
//   - a mark at or beneath path matters too, because extracting a parent
//     extracts the child with it: reading "output.value.data" against a mark of
//     "output.value.data.password" hands over the password inside the struct.
//     kubevela's MaskedPath descends for this same reason, since a source can
//     only put a marker where its schema declares a field.
//
// Indices are dropped before matching. A read through a list arrives as
// "members.0.token" because a reference carries its indices as decimal
// segments, while a mark can only ever be "members.token" -- the walk records
// no indices, since a mark on one element applies to them all.
func Covers(path string, marks []string) bool {
	if len(marks) == 0 || path == "" {
		return false
	}
	if coversExact(path, marks) {
		return true
	}
	if stripped := dropIndexSegments(path); stripped != path {
		return coversExact(stripped, marks)
	}
	return false
}

func coversExact(path string, marks []string) bool {
	for _, mark := range marks {
		if path == mark ||
			strings.HasPrefix(path, mark+".") ||
			strings.HasPrefix(mark, path+".") {
			return true
		}
	}
	return false
}

// dropIndexSegments removes the decimal segments of a read path. A map key that
// happens to be decimal is indistinguishable from an index here, so the caller
// tries the path unchanged first and only falls back to this.
func dropIndexSegments(path string) string {
	segments := strings.Split(path, ".")
	kept := segments[:0]
	for _, s := range segments {
		if _, err := strconv.Atoi(s); err == nil {
			continue
		}
		kept = append(kept, s)
	}
	return strings.Join(kept, ".")
}

// IsPlainFieldPath reports whether s is a simple dotted field path rather than
// an expression. A valueFrom that is an expression cannot be proven not to
// embed a marked value, so a caller treats it as sensitive when the template
// marked anything at all.
func IsPlainFieldPath(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '_', r == '-', r == '$':
		default:
			return false
		}
	}
	return true
}

// pathCache memoises ExtractPaths by template content. Keyed by the template
// itself rather than by definition name: a definition that is edited has to be
// re-read, and two definitions sharing a template should share the answer.
var pathCache sync.Map // map[string]cachedPaths

type cachedPaths struct {
	paths []string
	err   error
}

// ExtractPathsCached is ExtractPaths, parsed once per distinct template.
//
// Parsing a template to find its markers is pure work on text that does not
// change between runs of the same step, so doing it per run would pay for the
// same answer on every reconcile of every workflow.
//
// The returned slice is shared with other callers and must not be mutated.
func ExtractPathsCached(template string) ([]string, error) {
	if cached, ok := pathCache.Load(template); ok {
		c := cached.(cachedPaths)
		return c.paths, c.err
	}
	paths, err := ExtractPaths(template)
	pathCache.Store(template, cachedPaths{paths: paths, err: err})
	return paths, err
}
