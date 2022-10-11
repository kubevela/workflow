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

	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/parser"
	"k8s.io/klog/v2"
)

func init() {
	var err error
	builtinImport, err = initBuiltinImports()
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
	// GeneralImports is the general imports for cue
	GeneralImports []*build.Instance
)

const (
	builtinPackageName = "vela/op"
)

// SetupGeneralImports setup general imports
func SetupGeneralImports(general []*build.Instance) {
	GeneralImports = general
}

// GetPackages Get Stdlib packages
func GetPackages() (map[string]string, error) {
	versions, err := fs.ReadDir("actions")
	if err != nil {
		return nil, err
	}
	ret := make(map[string]string)

	for _, dirs := range versions {
		files, err := fs.ReadDir("actions/" + dirs.Name() + "/pkgs")
		if err != nil {
			return nil, err
		}
		opBytes, err := fs.ReadFile("actions/" + dirs.Name() + "/op.cue")
		if err != nil {
			return nil, err
		}
		opContent := string(opBytes) + "\n"
		for _, file := range files {
			body, err := fs.ReadFile("actions/" + dirs.Name() + "/pkgs/" + file.Name())
			if err != nil {
				return nil, err
			}
			pkgContent := fmt.Sprintf("%s: {\n%s\n}\n", strings.TrimSuffix(file.Name(), ".cue"), string(body))
			opContent += pkgContent
		}
		pkgName := "vela/op"
		if dirs.Name() != "v1" {
			pkgName = pkgName + "/" + dirs.Name()
		}
		ret[pkgName] = opContent
	}
	return ret, nil
}

// AddImportsFor install imports for build.Instance.
func AddImportsFor(inst *build.Instance, tagTempl string) error {
	inst.Imports = append(inst.Imports, GeneralImports...)
	addDefault := true

	for _, a := range inst.Imports {
		if a.PkgName == filepath.Base(builtinPackageName) {
			addDefault = false
			break
		}

	}
	if addDefault {
		inst.Imports = append(inst.Imports, builtinImport[0])
	}
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

func initBuiltinImports() ([]*build.Instance, error) {

	imports := make([]*build.Instance, 0)
	pkgs, err := GetPackages()
	if err != nil {
		return nil, err
	}
	for path, content := range pkgs {
		p := &build.Instance{
			PkgName:    filepath.Base(path),
			ImportPath: path,
		}
		file, err := parser.ParseFile("-", content, parser.ParseComments)
		if err != nil {
			return nil, err
		}
		if err := p.AddSyntax(file); err != nil {
			return nil, err
		}
		imports = append(imports, p)
	}
	return imports, nil
}
