/*
Copyright 2022 The KubeVela Authors.

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

package value

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/parser"
	"github.com/pkg/errors"

	"github.com/kubevela/pkg/cue/util"
	"github.com/kubevela/workflow/pkg/cue/model/sets"
	workflowerrors "github.com/kubevela/workflow/pkg/errors"
)

// DefaultPackageHeader describes the default package header for CUE files.
const DefaultPackageHeader = "package main\n"

// FillRaw unify the value with the cue format string x at the given path.
func FillRaw(val cue.Value, x string, paths ...string) (cue.Value, error) {
	file, err := parser.ParseFile("-", x, parser.ParseComments)
	if err != nil {
		return cue.Value{}, err
	}
	xInst := val.Context().BuildFile(file)
	v := val.FillPath(FieldPath(paths...), xInst)
	if v.Err() != nil {
		return cue.Value{}, v.Err()
	}
	return v, nil
}

func setValue(orig ast.Node, expr ast.Expr, selectors []cue.Selector) error {
	if len(selectors) == 0 {
		return nil
	}
	key := selectors[0]
	selectors = selectors[1:]
	switch x := orig.(type) {
	case *ast.ListLit:
		if key.Type() != cue.IndexLabel {
			return fmt.Errorf("invalid key type %s in list lit", key.Type())
		}
		if len(selectors) == 0 {
			for key.Index() >= len(x.Elts) {
				x.Elts = append(x.Elts, ast.NewStruct())
			}
			x.Elts[key.Index()] = expr
			return nil
		}
		return setValue(x.Elts[key.Index()], expr, selectors)
	case *ast.StructLit:
		if len(x.Elts) == 0 || (key.Type() == cue.StringLabel && len(sets.LookUpAll(x, key.String())) == 0) {
			if len(selectors) == 0 {
				x.Elts = append(x.Elts, &ast.Field{
					Label: ast.NewString(key.String()),
					Value: expr,
				})
			} else {
				x.Elts = append(x.Elts, &ast.Field{
					Label: ast.NewString(key.String()),
					Value: ast.NewStruct(),
				})
			}
			return setValue(x.Elts[len(x.Elts)-1].(*ast.Field).Value, expr, selectors)
		}
		for i := range x.Elts {
			switch elem := x.Elts[i].(type) {
			case *ast.Field:
				if len(selectors) == 0 {
					if key.Type() == cue.StringLabel && strings.Trim(sets.LabelStr(elem.Label), `"`) == strings.Trim(key.String(), `"`) {
						x.Elts[i].(*ast.Field).Value = expr
						return nil
					}
				}
				if key.Type() == cue.StringLabel && strings.Trim(sets.LabelStr(elem.Label), `"`) == strings.Trim(key.String(), `"`) {
					return setValue(x.Elts[i].(*ast.Field).Value, expr, selectors)
				}
			default:
				return fmt.Errorf("not support type %T", elem)
			}
		}
	default:
		return fmt.Errorf("not support type %T", orig)
	}
	return nil
}

// SetValueByScript set the value v at the given script path.
// nolint:staticcheck
func SetValueByScript(base, v cue.Value, path ...string) (cue.Value, error) {
	cuepath := FieldPath(path...)
	selectors := cuepath.Selectors()
	node := base.Syntax(cue.ResolveReferences(true))
	if err := setValue(node, v.Syntax(cue.ResolveReferences(true)).(ast.Expr), selectors); err != nil {
		return cue.Value{}, err
	}
	b, err := format.Node(node)
	if err != nil {
		return cue.Value{}, err
	}
	return base.Context().CompileBytes(b), nil
}

func isScript(content string) (bool, error) {
	content = strings.TrimSpace(content)
	scriptFile, err := parser.ParseFile("-", content, parser.ParseComments)
	if err != nil {
		return false, errors.WithMessage(err, "parse script")
	}
	if len(scriptFile.Imports) != 0 {
		return true, nil
	}
	if len(scriptFile.Decls) == 0 || len(scriptFile.Decls) > 1 {
		return true, nil
	}

	return !isSelector(scriptFile.Decls[0]), nil
}

func isSelector(node ast.Node) bool {
	switch v := node.(type) {
	case *ast.EmbedDecl:
		return isSelector(v.Expr)
	case *ast.SelectorExpr, *ast.IndexExpr, *ast.Ident:
		return true
	default:
		return false
	}
}

// LookupValueByScript reports the value by cue script.
func LookupValueByScript(val cue.Value, script string) (cue.Value, error) {
	var outputKey = "zz_output__"
	script = strings.TrimSpace(script)
	scriptFile, err := parser.ParseFile("-", script, parser.ParseComments)
	if err != nil {
		return cue.Value{}, errors.WithMessage(err, "parse script")
	}
	isScriptPath, err := isScript(script)
	if err != nil {
		return cue.Value{}, err
	}

	if !isScriptPath {
		v := val.LookupPath(cue.ParsePath(script))
		if !v.Exists() {
			return cue.Value{}, workflowerrors.LookUpNotFoundErr(script)
		}
	}

	raw, err := util.ToString(val)
	if err != nil {
		return cue.Value{}, err
	}

	rawFile, err := parser.ParseFile("-", raw, parser.ParseComments)
	if err != nil {
		return cue.Value{}, errors.WithMessage(err, "parse script")
	}

	behindKey(scriptFile, outputKey)

	newV, err := makeValueWithFiles(rawFile, scriptFile)
	if err != nil {
		return cue.Value{}, err
	}

	v := newV.LookupPath(cue.ParsePath(outputKey))
	if !v.Exists() {
		return cue.Value{}, workflowerrors.LookUpNotFoundErr(outputKey)
	}
	return v, nil
}

func makeValueWithFiles(files ...*ast.File) (cue.Value, error) {
	builder := &build.Instance{}
	newFile := &ast.File{}
	imports := map[string]*ast.ImportSpec{}
	for _, f := range files {
		for _, importSpec := range f.Imports {
			if _, ok := imports[importSpec.Name.String()]; !ok {
				imports[importSpec.Name.String()] = importSpec
			}
		}
		newFile.Decls = append(newFile.Decls, f.Decls...)
	}

	for _, imp := range imports {
		newFile.Imports = append(newFile.Imports, imp)
	}

	if err := builder.AddSyntax(newFile); err != nil {
		return cue.Value{}, err
	}

	v := cuecontext.New().BuildInstance(builder)
	if v.Err() != nil {
		return cue.Value{}, v.Err()
	}

	return v, nil
}

func behindKey(file *ast.File, key string) {
	var (
		implDecls []ast.Decl
		decls     []ast.Decl
	)

	for i, decl := range file.Decls {
		if _, ok := decl.(*ast.ImportDecl); ok {
			implDecls = append(implDecls, file.Decls[i])
		} else {
			decls = append(decls, file.Decls[i])
		}
	}

	file.Decls = implDecls
	if len(decls) == 1 {
		target := decls[0]
		if embed, ok := target.(*ast.EmbedDecl); ok {
			file.Decls = append(file.Decls, &ast.Field{
				Label: ast.NewIdent(key),
				Value: embed.Expr,
			})
			return
		}
	}
	file.Decls = append(file.Decls, &ast.Field{
		Label: ast.NewIdent(key),
		Value: &ast.StructLit{
			Elts: decls,
		},
	})

}

// makePath creates a Path from a sequence of string.
func makePath(paths ...string) string {
	mergedPath := ""
	if len(paths) == 0 {
		return mergedPath
	}
	mergedPath = paths[0]
	if mergedPath == "" || (len(paths) == 1 && (strings.Contains(mergedPath, ".") || strings.Contains(mergedPath, "[") || isNumber(mergedPath))) {
		return unquoteString(paths[0])
	}
	if !strings.HasPrefix(mergedPath, "_") && !strings.HasPrefix(mergedPath, "#") {
		mergedPath = fmt.Sprintf("\"%s\"", unquoteString(mergedPath))
	}
	for _, p := range paths[1:] {
		p = unquoteString(p)
		if !strings.HasPrefix(p, "#") {
			mergedPath += fmt.Sprintf("[\"%s\"]", p)
		} else {
			mergedPath += fmt.Sprintf(".%s", p)
		}
	}
	return mergedPath
}

func unquoteString(s string) string {
	if unquote, err := strconv.Unquote(s); err == nil {
		return unquote
	}
	return s
}

func isNumber(s string) bool {
	_, err := strconv.ParseInt(s, 10, 64)
	return err == nil
}

// FieldPath return the cue path of the given paths
func FieldPath(paths ...string) cue.Path {
	s := makePath(paths...)
	if isNumber(s) {
		return cue.MakePath(cue.Str(s))
	}
	return cue.ParsePath(s)
}

// UnmarshalTo unmarshal value into golang object
func UnmarshalTo(val cue.Value, x interface{}) error {
	data, err := val.MarshalJSON()
	if err != nil {
		return err
	}
	return json.Unmarshal(data, x)
}
