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

package stdlib

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/parser"
	"k8s.io/klog/v2"
)

func init() {
	var err error
	pkgs, err := GetPackages()
	if err != nil {
		klog.ErrorS(err, "Unable to init builtin packages for imports")
		os.Exit(1)
	}
	builtinImport, err = InitBuiltinImports(pkgs)
	if err != nil {
		klog.ErrorS(err, "Unable to init builtin imports")
		os.Exit(1)
	}
}

var (
	//go:embed actions
	fs embed.FS
	// builtinImport is the builtin import for cue
	builtinImport []*build.Instance
)

const (
	builtinPackageName = "vela/op"
	builtinActionPath  = "actions"
	packagePath        = "pkgs"
	defaultVersion     = "v1"
)

// SetupBuiltinImports setup builtin imports
func SetupBuiltinImports(pkgs map[string]string) error {
	builtin, err := GetPackages()
	if err != nil {
		return err
	}
	for k, v := range pkgs {
		file, err := parser.ParseFile("-", v, parser.ParseComments)
		if err != nil {
			return err
		}
		if original, ok := builtin[k]; ok {
			builtin[k] = mergeFiles(original, file)
		} else {
			builtin[k] = file
		}
	}
	builtinImport, err = InitBuiltinImports(builtin)
	if err != nil {
		return err
	}
	return nil
}

// GetPackages Get Stdlib packages
func GetPackages() (map[string]*ast.File, error) {
	versions, err := fs.ReadDir(builtinActionPath)
	if err != nil {
		return nil, err
	}
	ret := make(map[string]*ast.File)

	for _, dirs := range versions {
		pathPrefix := fmt.Sprintf("%s/%s", builtinActionPath, dirs.Name())
		files, err := fs.ReadDir(fmt.Sprintf("%s/%s", pathPrefix, packagePath))
		if err != nil {
			return nil, err
		}
		opBytes, err := fs.ReadFile(fmt.Sprintf("%s/%s", pathPrefix, "op.cue"))
		if err != nil {
			return nil, err
		}
		opContent := string(opBytes) + "\n"
		for _, file := range files {
			body, err := fs.ReadFile(fmt.Sprintf("%s/%s/%s", pathPrefix, packagePath, file.Name()))
			if err != nil {
				return nil, err
			}
			pkgContent := fmt.Sprintf("%s: {\n%s\n}\n", strings.TrimSuffix(file.Name(), ".cue"), string(body))
			opContent += pkgContent
		}
		f, err := parser.ParseFile("-", opContent, parser.ParseComments)
		if err != nil {
			return nil, err
		}
		if dirs.Name() == defaultVersion {
			ret[builtinPackageName] = f
		}
		ret[filepath.Join(builtinPackageName, dirs.Name())] = f
	}
	return ret, nil
}

// AddImportsFor install imports for build.Instance.
func AddImportsFor(inst *build.Instance, tagTempl string) error {
	inst.Imports = append(inst.Imports, builtinImport...)
	if tagTempl != "" {
		p := &build.Instance{
			PkgName:    filepath.Base("vela/custom"),
			ImportPath: "vela/custom",
		}
		file, err := parser.ParseFile("-", tagTempl, parser.ParseComments)
		if err != nil {
			return err
		}
		if err := p.AddSyntax(file); err != nil {
			return err
		}
		inst.Imports = append(inst.Imports, p)
	}
	return nil
}

// InitBuiltinImports init built in imports
func InitBuiltinImports(pkgs map[string]*ast.File) ([]*build.Instance, error) {
	imports := make([]*build.Instance, 0)
	for path, content := range pkgs {
		p := &build.Instance{
			PkgName:    filepath.Base(path),
			ImportPath: path,
		}
		if err := p.AddSyntax(content); err != nil {
			return nil, err
		}
		imports = append(imports, p)
	}
	return imports, nil
}

func mergeFiles(base, file *ast.File) *ast.File {
	base.Decls = append(base.Decls, file.Decls...)
	return base
}
