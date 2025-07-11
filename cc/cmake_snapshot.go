// Copyright 2024 Google Inc. All rights reserved.
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

package cc

import (
	"bytes"
	_ "embed"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"text/template"

	"android/soong/android"

	"github.com/google/blueprint"
	"github.com/google/blueprint/proptools"
)

const veryVerbose bool = false

//go:embed cmake_main.txt
var templateCmakeMainRaw string
var templateCmakeMain *template.Template = parseTemplate(templateCmakeMainRaw)

//go:embed cmake_module_cc.txt
var templateCmakeModuleCcRaw string
var templateCmakeModuleCc *template.Template = parseTemplate(templateCmakeModuleCcRaw)

//go:embed cmake_module_aidl.txt
var templateCmakeModuleAidlRaw string
var templateCmakeModuleAidl *template.Template = parseTemplate(templateCmakeModuleAidlRaw)

//go:embed cmake_ext_add_aidl_library.txt
var cmakeExtAddAidlLibrary string

//go:embed cmake_ext_append_flags.txt
var cmakeExtAppendFlags string

var defaultUnportableFlags []string = []string{
	"-Wno-c99-designator",
	"-Wno-class-memaccess",
	"-Wno-exit-time-destructors",
	"-Winconsistent-missing-override",
	"-Wno-inconsistent-missing-override",
	"-Wreorder-init-list",
	"-Wno-reorder-init-list",
	"-Wno-restrict",
	"-Wno-stringop-overread",
	"-Wno-subobject-linkage",
}

var ignoredSystemLibs []string = []string{
	"crtbegin_dynamic",
	"crtend_android",
	"libc",
	"libc++",
	"libc++_static",
	"libc++demangle",
	"libc_musl",
	"libc_musl_crtbegin_so",
	"libc_musl_crtbegin_static",
	"libc_musl_crtend",
	"libc_musl_crtend_so",
	"libdl",
	"libm",
	"prebuilt_libclang_rt.builtins",
	"prebuilt_libclang_rt.ubsan_minimal",
}

// Mapping entry between Android's library name and the one used when building outside Android tree.
type LibraryMappingProperty struct {
	// Android library name.
	Android_name string

	// Library name used when building outside Android.
	Mapped_name string

	// If the make file is already present in Android source tree, specify its location.
	Package_pregenerated string

	// If the package is expected to be installed on the build host OS, specify its name.
	Package_system string
}

type CmakeSnapshotProperties struct {
	// Host modules to add to the snapshot package. Their dependencies are pulled in automatically.
	Modules_host []string

	// System modules to add to the snapshot package. Their dependencies are pulled in automatically.
	Modules_system []string

	// Vendor modules to add to the snapshot package. Their dependencies are pulled in automatically.
	Modules_vendor []string

	// Host prebuilts to bundle with the snapshot. These are tools needed to build outside Android.
	Prebuilts []string

	// Global cflags to add when building outside Android.
	Cflags []string

	// Flags to skip when building outside Android.
	Cflags_ignored []string

	// Mapping between library names used in Android tree and externally.
	Library_mapping []LibraryMappingProperty

	// List of cflags that are not portable between compilers that could potentially be used to
	// build a generated package. If left empty, it's initialized with a default list.
	Unportable_flags []string

	// Whether to include source code as part of the snapshot package.
	Include_sources bool
}

var cmakeSnapshotSourcesProvider = blueprint.NewProvider[android.Paths]()

type CmakeSnapshot struct {
	android.ModuleBase

	Properties CmakeSnapshotProperties

	zipPath android.WritablePath
}

type cmakeProcessedProperties struct {
	LibraryMapping       map[string]LibraryMappingProperty
	PregeneratedPackages []string
	SystemPackages       []string
}

type cmakeSnapshotDependencyTag struct {
	blueprint.BaseDependencyTag
	name string
}

var (
	cmakeSnapshotModuleTag   = cmakeSnapshotDependencyTag{name: "cmake-snapshot-module"}
	cmakeSnapshotPrebuiltTag = cmakeSnapshotDependencyTag{name: "cmake-snapshot-prebuilt"}
)

func parseTemplate(templateContents string) *template.Template {
	funcMap := template.FuncMap{
		"setList": func(name string, nameSuffix string, itemPrefix string, items []string) string {
			var list strings.Builder
			list.WriteString("set(" + name + nameSuffix)
			templateListBuilder(&list, itemPrefix, items)
			return list.String()
		},
		"toStrings": func(files android.Paths) []string {
			return files.Strings()
		},
		"concat5": func(list1 []string, list2 []string, list3 []string, list4 []string, list5 []string) []string {
			return append(append(append(append(list1, list2...), list3...), list4...), list5...)
		},
		"cflagsList": func(name string, nameSuffix string, flags []string,
			unportableFlags []string, ignoredFlags []string) string {
			if len(unportableFlags) == 0 {
				unportableFlags = defaultUnportableFlags
			}

			var filteredPortable []string
			var filteredUnportable []string
			for _, flag := range flags {
				if slices.Contains(ignoredFlags, flag) {
					continue
				} else if slices.Contains(unportableFlags, flag) {
					filteredUnportable = append(filteredUnportable, flag)
				} else {
					filteredPortable = append(filteredPortable, flag)
				}
			}

			var list strings.Builder

			list.WriteString("set(" + name + nameSuffix)
			templateListBuilder(&list, "", filteredPortable)

			if len(filteredUnportable) > 0 {
				list.WriteString("\nappend_cxx_flags_if_supported(" + name + nameSuffix)
				templateListBuilder(&list, "", filteredUnportable)
			}

			return list.String()
		},
		"getSources": func(ctx android.ModuleContext, info *CcInfo) android.Paths {
			return info.CompilerInfo.Srcs
		},
		"getModuleType": getModuleType,
		"getAidlInterface": func(info *CcInfo) AidlInterfaceInfo {
			return info.CompilerInfo.AidlInterfaceInfo
		},
		"getCflagsProperty": func(ctx android.ModuleContext, info *CcInfo) []string {
			return info.CompilerInfo.Cflags
		},
		"getWholeStaticLibsProperty": func(ctx android.ModuleContext, info *CcInfo) []string {
			return info.LinkerInfo.WholeStaticLibs
		},
		"getStaticLibsProperty": func(ctx android.ModuleContext, info *CcInfo) []string {
			return info.LinkerInfo.StaticLibs
		},
		"getSharedLibsProperty": func(ctx android.ModuleContext, info *CcInfo) []string {
			return info.LinkerInfo.SharedLibs
		},
		"getHeaderLibsProperty": func(ctx android.ModuleContext, info *CcInfo) []string {
			return info.LinkerInfo.HeaderLibs
		},
		"getExtraLibs":   getExtraLibs,
		"getIncludeDirs": getIncludeDirs,
		"mapLibraries": func(ctx android.ModuleContext, m android.ModuleProxy, libs []string, mapping map[string]LibraryMappingProperty) []string {
			var mappedLibs []string
			for _, lib := range libs {
				mappedLib, exists := mapping[lib]
				if exists {
					lib = mappedLib.Mapped_name
				} else {
					if !ctx.OtherModuleExists(lib) {
						ctx.OtherModuleErrorf(m, "Dependency %s doesn't exist", lib)
					}
					lib = "android::" + lib
				}
				if lib == "" {
					continue
				}
				mappedLibs = append(mappedLibs, lib)
			}
			sort.Strings(mappedLibs)
			mappedLibs = slices.Compact(mappedLibs)
			return mappedLibs
		},
		"getAidlSources": func(info *CcInfo) []string {
			aidlInterface := info.CompilerInfo.AidlInterfaceInfo
			aidlRoot := aidlInterface.AidlRoot + string(filepath.Separator)
			if aidlInterface.AidlRoot == "" {
				aidlRoot = ""
			}
			var sources []string
			for _, src := range aidlInterface.Sources {
				if !strings.HasPrefix(src, aidlRoot) {
					panic(fmt.Sprintf("Aidl source '%v' doesn't start with '%v'", src, aidlRoot))
				}
				sources = append(sources, src[len(aidlRoot):])
			}
			return sources
		},
	}

	return template.Must(template.New("").Delims("<<", ">>").Funcs(funcMap).Parse(templateContents))
}

func sliceWithPrefix(prefix string, slice []string) []string {
	output := make([]string, len(slice))
	for i, elem := range slice {
		output[i] = prefix + elem
	}
	return output
}

func templateListBuilder(builder *strings.Builder, itemPrefix string, items []string) {
	if len(items) > 0 {
		builder.WriteString("\n")
		for _, item := range items {
			builder.WriteString("    " + itemPrefix + item + "\n")
		}
	}
	builder.WriteString(")")
}

func executeTemplate(templ *template.Template, buffer *bytes.Buffer, data any) string {
	buffer.Reset()
	if err := templ.Execute(buffer, data); err != nil {
		panic(err)
	}
	output := strings.TrimSpace(buffer.String())
	buffer.Reset()
	return output
}

func (m *CmakeSnapshot) DepsMutator(ctx android.BottomUpMutatorContext) {
	deviceVariations := ctx.Config().AndroidFirstDeviceTarget.Variations()
	deviceSystemVariations := append(deviceVariations, blueprint.Variation{"image", ""})
	deviceVendorVariations := append(deviceVariations, blueprint.Variation{"image", "vendor"})
	hostVariations := ctx.Config().BuildOSTarget.Variations()

	ctx.AddVariationDependencies(hostVariations, cmakeSnapshotModuleTag, m.Properties.Modules_host...)
	ctx.AddVariationDependencies(deviceSystemVariations, cmakeSnapshotModuleTag, m.Properties.Modules_system...)
	ctx.AddVariationDependencies(deviceVendorVariations, cmakeSnapshotModuleTag, m.Properties.Modules_vendor...)

	if len(m.Properties.Prebuilts) > 0 {
		prebuilts := append(m.Properties.Prebuilts, "libc++")
		ctx.AddVariationDependencies(hostVariations, cmakeSnapshotPrebuiltTag, prebuilts...)
	}
}

func (m *CmakeSnapshot) GenerateAndroidBuildActions(ctx android.ModuleContext) {
	var templateBuffer bytes.Buffer
	var pprop cmakeProcessedProperties
	m.zipPath = android.PathForModuleOut(ctx, ctx.ModuleName()+".zip")

	// Process Library_mapping for more efficient lookups
	pprop.LibraryMapping = map[string]LibraryMappingProperty{}
	for _, elem := range m.Properties.Library_mapping {
		pprop.LibraryMapping[elem.Android_name] = elem

		if elem.Package_pregenerated != "" {
			pprop.PregeneratedPackages = append(pprop.PregeneratedPackages, elem.Package_pregenerated)
		}
		sort.Strings(pprop.PregeneratedPackages)
		pprop.PregeneratedPackages = slices.Compact(pprop.PregeneratedPackages)

		if elem.Package_system != "" {
			pprop.SystemPackages = append(pprop.SystemPackages, elem.Package_system)
		}
		sort.Strings(pprop.SystemPackages)
		pprop.SystemPackages = slices.Compact(pprop.SystemPackages)
	}

	// Generating CMakeLists.txt rules for all modules in dependency tree
	moduleDirs := map[string][]string{}
	sourceFiles := map[string]android.Path{}
	visitedModules := map[string]bool{}
	var pregeneratedModules []android.ModuleProxy
	ctx.WalkDepsProxy(func(dep, parent android.ModuleProxy) bool {
		moduleName := ctx.OtherModuleName(dep)
		if visited := visitedModules[moduleName]; visited {
			return false // visit only once
		}
		visitedModules[moduleName] = true
		ccInfo, ok := android.OtherModuleProvider(ctx, dep, CcInfoProvider)
		if !ok {
			return false // not a cc module
		}
		if mapping, ok := pprop.LibraryMapping[moduleName]; ok {
			if mapping.Package_pregenerated != "" {
				pregeneratedModules = append(pregeneratedModules, dep)
			}
			return false // mapped to system or pregenerated (we'll handle these later)
		}
		if ctx.OtherModuleDependencyTag(dep) == cmakeSnapshotPrebuiltTag {
			return false // we'll handle cmakeSnapshotPrebuiltTag later
		}
		if slices.Contains(ignoredSystemLibs, moduleName) {
			return false // system libs built in-tree for Android
		}
		if ccInfo.IsPrebuilt {
			return false // prebuilts are not supported
		}
		if ccInfo.CompilerInfo == nil {
			return false // unsupported module type
		}
		isAidlModule := ccInfo.CompilerInfo.AidlInterfaceInfo.Lang != ""

		if !ccInfo.CmakeSnapshotSupported {
			ctx.OtherModulePropertyErrorf(dep, "cmake_snapshot_supported",
				"CMake snapshots not supported, despite being a dependency for %s",
				ctx.OtherModuleName(parent))
			return false
		}

		if veryVerbose {
			fmt.Println("WalkDeps: " + ctx.OtherModuleName(parent) + " -> " + moduleName)
		}

		// Generate CMakeLists.txt fragment for this module
		templateToUse := templateCmakeModuleCc
		if isAidlModule {
			templateToUse = templateCmakeModuleAidl
		}
		moduleFragment := executeTemplate(templateToUse, &templateBuffer, struct {
			Ctx      *android.ModuleContext
			M        android.ModuleProxy
			CcInfo   *CcInfo
			Snapshot *CmakeSnapshot
			Pprop    *cmakeProcessedProperties
		}{
			&ctx,
			dep,
			ccInfo,
			m,
			&pprop,
		})
		moduleDir := ctx.OtherModuleDir(dep)
		moduleDirs[moduleDir] = append(moduleDirs[moduleDir], moduleFragment)

		if m.Properties.Include_sources {
			files, _ := android.OtherModuleProvider(ctx, dep, cmakeSnapshotSourcesProvider)
			for _, file := range files {
				sourceFiles[file.String()] = file
			}
		}

		// if it's AIDL module, no need to dive into their dependencies
		return !isAidlModule
	})

	// Enumerate sources for pregenerated modules
	if m.Properties.Include_sources {
		for _, dep := range pregeneratedModules {
			if !android.OtherModuleProviderOrDefault(ctx, dep, CcInfoProvider).CmakeSnapshotSupported {
				ctx.OtherModulePropertyErrorf(dep, "cmake_snapshot_supported",
					"Pregenerated CMake snapshots not supported, despite being requested for %s",
					ctx.ModuleName())
				continue
			}

			files, _ := android.OtherModuleProvider(ctx, dep, cmakeSnapshotSourcesProvider)
			for _, file := range files {
				sourceFiles[file.String()] = file
			}
		}
	}

	// Merging CMakeLists.txt contents for every module directory
	var makefilesList android.Paths
	for _, moduleDir := range android.SortedKeys(moduleDirs) {
		fragments := moduleDirs[moduleDir]
		moduleCmakePath := android.PathForModuleGen(ctx, moduleDir, "CMakeLists.txt")
		makefilesList = append(makefilesList, moduleCmakePath)
		sort.Strings(fragments)
		android.WriteFileRule(ctx, moduleCmakePath, strings.Join(fragments, "\n\n\n"))
	}

	// Generating top-level CMakeLists.txt
	mainCmakePath := android.PathForModuleGen(ctx, "CMakeLists.txt")
	makefilesList = append(makefilesList, mainCmakePath)
	mainContents := executeTemplate(templateCmakeMain, &templateBuffer, struct {
		Ctx        *android.ModuleContext
		M          *CmakeSnapshot
		ModuleDirs map[string][]string
		Pprop      *cmakeProcessedProperties
	}{
		&ctx,
		m,
		moduleDirs,
		&pprop,
	})
	android.WriteFileRule(ctx, mainCmakePath, mainContents)

	// Generating CMake extensions
	extPath := android.PathForModuleGen(ctx, "cmake", "AppendCxxFlagsIfSupported.cmake")
	makefilesList = append(makefilesList, extPath)
	android.WriteFileRuleVerbatim(ctx, extPath, cmakeExtAppendFlags)
	extPath = android.PathForModuleGen(ctx, "cmake", "AddAidlLibrary.cmake")
	makefilesList = append(makefilesList, extPath)
	android.WriteFileRuleVerbatim(ctx, extPath, cmakeExtAddAidlLibrary)

	// Generating the final zip file
	zipRule := android.NewRuleBuilder(pctx, ctx)
	zipCmd := zipRule.Command().
		BuiltTool("soong_zip").
		FlagWithOutput("-o ", m.zipPath)

	// Packaging all sources into the zip file
	if m.Properties.Include_sources {
		var sourcesList android.Paths
		for _, file := range android.SortedKeys(sourceFiles) {
			path := sourceFiles[file]
			sourcesList = append(sourcesList, path)
		}

		sourcesRspFile := android.PathForModuleObj(ctx, ctx.ModuleName()+"_sources.rsp")
		zipCmd.FlagWithRspFileInputList("-r ", sourcesRspFile, sourcesList)
	}

	// Packaging all make files into the zip file
	makefilesRspFile := android.PathForModuleObj(ctx, ctx.ModuleName()+"_makefiles.rsp")
	zipCmd.
		FlagWithArg("-C ", android.PathForModuleGen(ctx).String()).
		FlagWithRspFileInputList("-r ", makefilesRspFile, makefilesList)

	// Packaging all prebuilts into the zip file
	if len(m.Properties.Prebuilts) > 0 {
		var prebuiltsList android.Paths

		ctx.VisitDirectDepsProxyWithTag(cmakeSnapshotPrebuiltTag, func(dep android.ModuleProxy) {
			for _, file := range android.OtherModuleProviderOrDefault(
				ctx, dep, android.InstallFilesProvider).InstallFiles {
				prebuiltsList = append(prebuiltsList, file)
			}
		})

		prebuiltsRspFile := android.PathForModuleObj(ctx, ctx.ModuleName()+"_prebuilts.rsp")
		zipCmd.
			FlagWithArg("-C ", android.PathForArbitraryOutput(ctx).String()).
			FlagWithArg("-P ", "prebuilts").
			FlagWithRspFileInputList("-r ", prebuiltsRspFile, prebuiltsList)
	}

	// Finish generating the final zip file
	zipRule.Build(m.zipPath.String(), "archiving "+ctx.ModuleName())

	ctx.SetOutputFiles(android.Paths{m.zipPath}, "")
}

func (m *CmakeSnapshot) AndroidMkEntries() []android.AndroidMkEntries {
	return []android.AndroidMkEntries{{
		Class:      "DATA",
		OutputFile: android.OptionalPathForPath(m.zipPath),
		ExtraEntries: []android.AndroidMkExtraEntriesFunc{
			func(ctx android.AndroidMkExtraEntriesContext, entries *android.AndroidMkEntries) {
				entries.SetBool("LOCAL_UNINSTALLABLE_MODULE", true)
			},
		},
	}}
}

func getModuleType(info *CcInfo) string {
	if info.LinkerInfo.BinaryDecoratorInfo != nil {
		return "executable"
	} else if info.LinkerInfo.LibraryDecoratorInfo != nil {
		return "library"
	} else if info.LinkerInfo.TestBinaryInfo != nil || info.LinkerInfo.BenchmarkDecoratorInfo != nil {
		return "test"
	} else if info.LinkerInfo.ObjectLinkerInfo != nil {
		return "object"
	}
	panic(fmt.Sprintf("Unexpected module type for LinkerInfo"))
}

func getExtraLibs(info *CcInfo) []string {
	if info.LinkerInfo.TestBinaryInfo != nil {
		if info.LinkerInfo.TestBinaryInfo.Gtest {
			return []string{
				"libgtest",
				"libgtest_main",
			}
		}
	} else if info.LinkerInfo.BenchmarkDecoratorInfo != nil {
		return []string{"libgoogle-benchmark"}
	}
	return nil
}

func getIncludeDirs(ctx android.ModuleContext, m android.ModuleProxy, info *CcInfo) []string {
	moduleDir := ctx.OtherModuleDir(m) + string(filepath.Separator)
	if info.CompilerInfo.LibraryDecoratorInfo != nil {
		return sliceWithPrefix(moduleDir, info.CompilerInfo.LibraryDecoratorInfo.ExportIncludeDirs)
	}
	return nil
}

func cmakeSnapshotLoadHook(ctx android.LoadHookContext) {
	props := struct {
		Target struct {
			Darwin struct {
				Enabled *bool
			}
			Windows struct {
				Enabled *bool
			}
		}
	}{}
	props.Target.Darwin.Enabled = proptools.BoolPtr(false)
	props.Target.Windows.Enabled = proptools.BoolPtr(false)
	ctx.AppendProperties(&props)
}

// cmake_snapshot allows defining source packages for release outside of Android build tree.
// As a result of cmake_snapshot module build, a zip file is generated with CMake build definitions
// for selected source modules, their dependencies and optionally also the source code itself.
func CmakeSnapshotFactory() android.Module {
	module := &CmakeSnapshot{}
	module.AddProperties(&module.Properties)
	android.AddLoadHook(module, cmakeSnapshotLoadHook)
	android.InitAndroidArchModule(module, android.HostSupported, android.MultilibFirst)
	return module
}

func init() {
	android.InitRegistrationContext.RegisterModuleType("cc_cmake_snapshot", CmakeSnapshotFactory)
}
