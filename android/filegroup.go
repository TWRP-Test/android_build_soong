// Copyright 2016 Google Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package android

import (
	"maps"
	"strings"

	"github.com/google/blueprint"
	"github.com/google/blueprint/proptools"
)

func init() {
	RegisterFilegroupBuildComponents(InitRegistrationContext)
}

var PrepareForTestWithFilegroup = FixtureRegisterWithContext(func(ctx RegistrationContext) {
	RegisterFilegroupBuildComponents(ctx)
})

func RegisterFilegroupBuildComponents(ctx RegistrationContext) {
	ctx.RegisterModuleType("filegroup", FileGroupFactory)
	ctx.RegisterModuleType("filegroup_defaults", FileGroupDefaultsFactory)
}

type fileGroupProperties struct {
	// srcs lists files that will be included in this filegroup
	Srcs proptools.Configurable[[]string] `android:"path"`

	Exclude_srcs proptools.Configurable[[]string] `android:"path"`

	// Sources that will be included in the filegroup, but any module dependencies will be added
	// using the device os and the device's first architecture's variant.
	Device_first_srcs proptools.Configurable[[]string] `android:"path_device_first"`

	// Sources that will be included in the filegroup, but any module dependencies will be added
	// using the device os and the common architecture's variant.
	Device_common_srcs proptools.Configurable[[]string] `android:"path_device_common"`

	// The base path to the files.  May be used by other modules to determine which portion
	// of the path to use.  For example, when a filegroup is used as data in a cc_test rule,
	// the base path is stripped off the path and the remaining path is used as the
	// installation directory.
	Path *string

	// Create a make variable with the specified name that contains the list of files in the
	// filegroup, relative to the root of the source tree.
	Export_to_make_var *string
}

type fileGroup struct {
	ModuleBase
	DefaultableModuleBase
	properties fileGroupProperties
	srcs       Paths
}

var _ SourceFileProducer = (*fileGroup)(nil)

// filegroup contains a list of files that are referenced by other modules
// properties (such as "srcs") using the syntax ":<name>". filegroup are
// also be used to export files across package boundaries.
func FileGroupFactory() Module {
	module := &fileGroup{}
	module.AddProperties(&module.properties)
	InitAndroidModule(module)
	InitDefaultableModule(module)
	return module
}

var _ blueprint.JSONActionSupplier = (*fileGroup)(nil)

func (fg *fileGroup) JSONActions() []blueprint.JSONAction {
	ins := make([]string, 0, len(fg.srcs))
	outs := make([]string, 0, len(fg.srcs))
	for _, p := range fg.srcs {
		ins = append(ins, p.String())
		outs = append(outs, p.Rel())
	}
	return []blueprint.JSONAction{
		blueprint.JSONAction{
			Inputs:  ins,
			Outputs: outs,
		},
	}
}

func (fg *fileGroup) GenerateAndroidBuildActions(ctx ModuleContext) {
	srcs := PathsForModuleSrcExcludes(ctx, fg.properties.Srcs.GetOrDefault(ctx, nil), fg.properties.Exclude_srcs.GetOrDefault(ctx, nil))
	srcs = append(srcs, PathsForModuleSrc(ctx, fg.properties.Device_first_srcs.GetOrDefault(ctx, nil))...)
	srcs = append(srcs, PathsForModuleSrc(ctx, fg.properties.Device_common_srcs.GetOrDefault(ctx, nil))...)
	if fg.properties.Path != nil {
		srcs = PathsWithModuleSrcSubDir(ctx, srcs, String(fg.properties.Path))
	}

	var aconfigDeclarations []string
	var intermediateCacheOutputPaths Paths
	var srcjars Paths
	modeInfos := make(map[string]ModeInfo)
	ctx.VisitDirectDeps(func(module Module) {
		if dep, ok := OtherModuleProvider(ctx, module, CodegenInfoProvider); ok {
			aconfigDeclarations = append(aconfigDeclarations, dep.AconfigDeclarations...)
			intermediateCacheOutputPaths = append(intermediateCacheOutputPaths, dep.IntermediateCacheOutputPaths...)
			srcjars = append(srcjars, dep.Srcjars...)
			maps.Copy(modeInfos, dep.ModeInfos)
		}
	})

	fg.srcs = srcs
	SetProvider(ctx, CodegenInfoProvider, CodegenInfo{
		AconfigDeclarations:          aconfigDeclarations,
		IntermediateCacheOutputPaths: intermediateCacheOutputPaths,
		Srcjars:                      srcjars,
		ModeInfos:                    modeInfos,
	})
}

func (fg *fileGroup) Srcs() Paths {
	return append(Paths{}, fg.srcs...)
}

func (fg *fileGroup) MakeVars(_ MakeVarsModuleContext) []ModuleMakeVarsValue {
	if makeVar := String(fg.properties.Export_to_make_var); makeVar != "" {
		return []ModuleMakeVarsValue{{makeVar, strings.Join(fg.srcs.Strings(), " ")}}
	}
	return nil
}

// Defaults
type FileGroupDefaults struct {
	ModuleBase
	DefaultsModuleBase
}

func FileGroupDefaultsFactory() Module {
	module := &FileGroupDefaults{}
	module.AddProperties(&fileGroupProperties{})
	InitDefaultsModule(module)

	return module
}

// Collect information for opening IDE project files in java/jdeps.go.
// Copied from build/soong/genrule/genrule.go
func (fg *fileGroup) IDEInfo(ctx BaseModuleContext, dpInfo *IdeInfo) {
	dpInfo.Srcs = append(dpInfo.Srcs, fg.Srcs().Strings()...)
	for _, src := range fg.properties.Srcs.GetOrDefault(ctx, nil) {
		if mod, _ := SrcIsModuleWithTag(src); mod != "" {
			// Register the module name without any tags in `Deps`
			dpInfo.Deps = append(dpInfo.Deps, mod)
		}
	}
}
