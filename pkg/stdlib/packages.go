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
	//go:embed pkgs op.cue
	fs embed.FS
	// builtinImport is the builtin import for cue
	builtinImport *build.Instance
	// GeneralImports is the general imports for cue
	GeneralImports []*build.Instance
)

const (
	builtinPackageName = "vela/op"
)

func SetupGeneralImports(general []*build.Instance) {
	GeneralImports = general
}

// GetPackages Get Stdlib packages
func GetPackages() (string, error) {
	files, err := fs.ReadDir("pkgs")
	if err != nil {
		return "", err
	}

	opBytes, err := fs.ReadFile("op.cue")
	if err != nil {
		return "", err
	}

	opContent := string(opBytes) + "\n"
	for _, file := range files {
		body, err := fs.ReadFile("pkgs/" + file.Name())
		if err != nil {
			return "", err
		}
		pkgContent := fmt.Sprintf("%s: {\n%s\n}\n", strings.TrimSuffix(file.Name(), ".cue"), string(body))
		opContent += pkgContent
	}

	return opContent, nil
}

// AddImportsFor install imports for build.Instance.
func AddImportsFor(inst *build.Instance, tagTempl string) error {
	inst.Imports = append(inst.Imports, builtinImport)
	for _, a := range GeneralImports {
		if a.PkgName == builtinPackageName {
			inst.Imports[len(inst.Imports)-1] = a
			continue
		}
		inst.Imports = append(inst.Imports, a)
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

func initBuiltinImports() (*build.Instance, error) {
	pkg, err := GetPackages()
	if err != nil {
		return nil, err
	}
	p := &build.Instance{
		PkgName:    filepath.Base(builtinPackageName),
		ImportPath: builtinPackageName,
	}
	file, err := parser.ParseFile("-", pkg, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	if err := p.AddSyntax(file); err != nil {
		return nil, err
	}
	return p, nil
}
