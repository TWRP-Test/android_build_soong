// Copyright (C) 2024 The Android Open Source Project
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

package fsgen

import (
	"crypto/sha256"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"

	"android/soong/android"
	"android/soong/filesystem"

	"github.com/google/blueprint"
	"github.com/google/blueprint/parser"
	"github.com/google/blueprint/proptools"
)

var pctx = android.NewPackageContext("android/soong/fsgen")

func init() {
	registerBuildComponents(android.InitRegistrationContext)
}

func registerBuildComponents(ctx android.RegistrationContext) {
	ctx.RegisterModuleType("soong_filesystem_creator", filesystemCreatorFactory)
	ctx.PreDepsMutators(RegisterCollectFileSystemDepsMutators)
}

func RegisterCollectFileSystemDepsMutators(ctx android.RegisterMutatorsContext) {
	ctx.BottomUp("fs_collect_deps", collectDepsMutator).MutatesGlobalState()
}

var fsDepsMutex = sync.Mutex{}
var collectFsDepsOnceKey = android.NewOnceKey("CollectFsDeps")
var depCandidatesOnceKey = android.NewOnceKey("DepCandidates")

func collectDepsMutator(mctx android.BottomUpMutatorContext) {
	// These additional deps are added according to the cuttlefish system image bp.
	fsDeps := mctx.Config().Once(collectFsDepsOnceKey, func() interface{} {
		deps := []string{
			"android_vintf_manifest",
			"com.android.apex.cts.shim.v1_prebuilt",
			"dex_bootjars",
			"framework_compatibility_matrix.device.xml",
			"idc_data",
			"init.environ.rc-soong",
			"keychars_data",
			"keylayout_data",
			"libclang_rt.asan",
			"libcompiler_rt",
			"libdmabufheap",
			"libgsi",
			"llndk.libraries.txt",
			"logpersist.start",
			"preloaded-classes",
			"public.libraries.android.txt",
			"update_engine_sideload",
		}
		return &deps
	}).(*[]string)

	depCandidates := mctx.Config().Once(depCandidatesOnceKey, func() interface{} {
		partitionVars := mctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse
		candidates := slices.Concat(partitionVars.ProductPackages, partitionVars.ProductPackagesDebug)
		return &candidates
	}).(*[]string)

	m := mctx.Module()
	if slices.Contains(*depCandidates, m.Name()) {
		if installInSystem(mctx, m) {
			fsDepsMutex.Lock()
			*fsDeps = append(*fsDeps, m.Name())
			fsDepsMutex.Unlock()
		}
	}
}

type filesystemCreatorProps struct {
	Generated_partition_types   []string `blueprint:"mutated"`
	Unsupported_partition_types []string `blueprint:"mutated"`
}

type filesystemCreator struct {
	android.ModuleBase

	properties filesystemCreatorProps
}

func filesystemCreatorFactory() android.Module {
	module := &filesystemCreator{}

	android.InitAndroidArchModule(module, android.DeviceSupported, android.MultilibCommon)
	module.AddProperties(&module.properties)
	android.AddLoadHook(module, func(ctx android.LoadHookContext) {
		module.createInternalModules(ctx)
	})

	return module
}

func (f *filesystemCreator) createInternalModules(ctx android.LoadHookContext) {
	for _, partitionType := range []string{"system"} {
		if f.createPartition(ctx, partitionType) {
			f.properties.Generated_partition_types = append(f.properties.Generated_partition_types, partitionType)
		} else {
			f.properties.Unsupported_partition_types = append(f.properties.Unsupported_partition_types, partitionType)
		}
	}
	f.createDeviceModule(ctx)
}

func (f *filesystemCreator) generatedModuleName(cfg android.Config, suffix string) string {
	prefix := "soong"
	if cfg.HasDeviceProduct() {
		prefix = cfg.DeviceProduct()
	}
	return fmt.Sprintf("%s_generated_%s", prefix, suffix)
}

func (f *filesystemCreator) generatedModuleNameForPartition(cfg android.Config, partitionType string) string {
	return f.generatedModuleName(cfg, fmt.Sprintf("%s_image", partitionType))
}

func (f *filesystemCreator) createDeviceModule(ctx android.LoadHookContext) {
	baseProps := &struct {
		Name *string
	}{
		Name: proptools.StringPtr(f.generatedModuleName(ctx.Config(), "device")),
	}

	// Currently, only the system partition module is created.
	partitionProps := &filesystem.PartitionNameProperties{}
	if android.InList("system", f.properties.Generated_partition_types) {
		partitionProps.System_partition_name = proptools.StringPtr(f.generatedModuleNameForPartition(ctx.Config(), "system"))
	}

	ctx.CreateModule(filesystem.AndroidDeviceFactory, baseProps, partitionProps)
}

// Creates a soong module to build the given partition. Returns false if we can't support building
// it.
func (f *filesystemCreator) createPartition(ctx android.LoadHookContext, partitionType string) bool {
	baseProps := &struct {
		Name *string
	}{
		Name: proptools.StringPtr(f.generatedModuleNameForPartition(ctx.Config(), partitionType)),
	}

	fsProps := &filesystem.FilesystemProperties{}

	// Don't build this module on checkbuilds, the soong-built partitions are still in-progress
	// and sometimes don't build.
	fsProps.Unchecked_module = proptools.BoolPtr(true)

	partitionVars := ctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse
	specificPartitionVars := partitionVars.PartitionQualifiedVariables[partitionType]

	// BOARD_AVB_ENABLE
	fsProps.Use_avb = proptools.BoolPtr(partitionVars.BoardAvbEnable)
	// BOARD_AVB_KEY_PATH
	fsProps.Avb_private_key = proptools.StringPtr(specificPartitionVars.BoardAvbKeyPath)
	// BOARD_AVB_ALGORITHM
	fsProps.Avb_algorithm = proptools.StringPtr(specificPartitionVars.BoardAvbAlgorithm)
	// BOARD_AVB_SYSTEM_ROLLBACK_INDEX
	if rollbackIndex, err := strconv.ParseInt(specificPartitionVars.BoardAvbRollbackIndex, 10, 64); err == nil {
		fsProps.Rollback_index = proptools.Int64Ptr(rollbackIndex)
	}

	fsProps.Partition_name = proptools.StringPtr(partitionType)
	// BOARD_SYSTEMIMAGE_FILE_SYSTEM_TYPE
	fsProps.Type = proptools.StringPtr(specificPartitionVars.BoardFileSystemType)
	if *fsProps.Type != "ext4" {
		// TODO(b/372522486): Support other FS types.
		// Currently the android_filesystem module type only supports ext4:
		// https://cs.android.com/android/platform/superproject/main/+/main:build/soong/filesystem/filesystem.go;l=416;drc=98047cfd07944b297a12d173453bc984806760d2
		return false
	}

	fsProps.Base_dir = proptools.StringPtr(partitionType)

	fsProps.Gen_aconfig_flags_pb = proptools.BoolPtr(true)

	// Identical to that of the generic_system_image
	fsProps.Fsverity.Inputs = []string{
		"etc/boot-image.prof",
		"etc/dirty-image-objects",
		"etc/preloaded-classes",
		"etc/classpaths/*.pb",
		"framework/*",
		"framework/*/*",     // framework/{arch}
		"framework/oat/*/*", // framework/oat/{arch}
	}

	// system_image properties that are not set:
	// - filesystemProperties.Avb_hash_algorithm
	// - filesystemProperties.File_contexts
	// - filesystemProperties.Dirs
	// - filesystemProperties.Symlinks
	// - filesystemProperties.Fake_timestamp
	// - filesystemProperties.Uuid
	// - filesystemProperties.Mount_point
	// - filesystemProperties.Include_make_built_files
	// - filesystemProperties.Build_logtags
	// - filesystemProperties.Fsverity.Libs
	// - systemImageProperties.Linker_config_src
	var module android.Module
	if partitionType == "system" {
		module = ctx.CreateModule(filesystem.SystemImageFactory, baseProps, fsProps)
	} else {
		module = ctx.CreateModule(filesystem.FilesystemFactory, baseProps, fsProps)
	}
	module.HideFromMake()
	return true
}

func (f *filesystemCreator) createDiffTest(ctx android.ModuleContext, partitionType string) android.Path {
	partitionModuleName := f.generatedModuleNameForPartition(ctx.Config(), partitionType)
	systemImage := ctx.GetDirectDepWithTag(partitionModuleName, generatedFilesystemDepTag)
	filesystemInfo, ok := android.OtherModuleProvider(ctx, systemImage, filesystem.FilesystemProvider)
	if !ok {
		ctx.ModuleErrorf("Expected module %s to provide FileysystemInfo", partitionModuleName)
	}
	makeFileList := android.PathForArbitraryOutput(ctx, fmt.Sprintf("target/product/%s/obj/PACKAGING/%s_intermediates/file_list.txt", ctx.Config().DeviceName(), partitionType))
	// For now, don't allowlist anything. The test will fail, but that's fine in the current
	// early stages where we're just figuring out what we need
	emptyAllowlistFile := android.PathForModuleOut(ctx, fmt.Sprintf("allowlist_%s.txt", partitionModuleName))
	android.WriteFileRule(ctx, emptyAllowlistFile, "")
	diffTestResultFile := android.PathForModuleOut(ctx, fmt.Sprintf("diff_test_%s.txt", partitionModuleName))

	builder := android.NewRuleBuilder(pctx, ctx)
	builder.Command().BuiltTool("file_list_diff").
		Input(makeFileList).
		Input(filesystemInfo.FileListFile).
		Text(partitionModuleName).
		FlagWithInput("--allowlists ", emptyAllowlistFile)
	builder.Command().Text("touch").Output(diffTestResultFile)
	builder.Build(partitionModuleName+" diff test", partitionModuleName+" diff test")
	return diffTestResultFile
}

func createFailingCommand(ctx android.ModuleContext, message string) android.Path {
	hasher := sha256.New()
	hasher.Write([]byte(message))
	filename := fmt.Sprintf("failing_command_%x.txt", hasher.Sum(nil))
	file := android.PathForModuleOut(ctx, filename)
	builder := android.NewRuleBuilder(pctx, ctx)
	builder.Command().Textf("echo %s", proptools.NinjaAndShellEscape(message))
	builder.Command().Text("exit 1 #").Output(file)
	builder.Build("failing command "+filename, "failing command "+filename)
	return file
}

type systemImageDepTagType struct {
	blueprint.BaseDependencyTag
}

var generatedFilesystemDepTag systemImageDepTagType

func (f *filesystemCreator) DepsMutator(ctx android.BottomUpMutatorContext) {
	for _, partitionType := range f.properties.Generated_partition_types {
		ctx.AddDependency(ctx.Module(), generatedFilesystemDepTag, f.generatedModuleNameForPartition(ctx.Config(), partitionType))
	}
}

func (f *filesystemCreator) GenerateAndroidBuildActions(ctx android.ModuleContext) {
	if ctx.ModuleDir() != "build/soong/fsgen" {
		ctx.ModuleErrorf("There can only be one soong_filesystem_creator in build/soong/fsgen")
	}
	f.HideFromMake()

	content := generateBpContent(ctx, "system")
	generatedBp := android.PathForOutput(ctx, "soong_generated_product_config.bp")
	android.WriteFileRule(ctx, generatedBp, content)
	ctx.Phony("product_config_to_bp", generatedBp)

	var diffTestFiles []android.Path
	for _, partitionType := range f.properties.Generated_partition_types {
		diffTestFiles = append(diffTestFiles, f.createDiffTest(ctx, partitionType))
	}
	for _, partitionType := range f.properties.Unsupported_partition_types {
		diffTestFiles = append(diffTestFiles, createFailingCommand(ctx, fmt.Sprintf("Couldn't build %s partition", partitionType)))
	}
	ctx.Phony("soong_generated_filesystem_tests", diffTestFiles...)
}

func installInSystem(ctx android.BottomUpMutatorContext, m android.Module) bool {
	return m.PartitionTag(ctx.DeviceConfig()) == "system" && !m.InstallInData() &&
		!m.InstallInTestcases() && !m.InstallInSanitizerDir() && !m.InstallInVendorRamdisk() &&
		!m.InstallInDebugRamdisk() && !m.InstallInRecovery() && !m.InstallInOdm() &&
		!m.InstallInVendor()
}

// TODO: assemble baseProps and fsProps here
func generateBpContent(ctx android.EarlyModuleContext, partitionType string) string {
	// Currently only system partition is supported
	if partitionType != "system" {
		return ""
	}

	deps := ctx.Config().Get(collectFsDepsOnceKey).(*[]string)
	depProps := &android.PackagingProperties{
		Deps: android.NewSimpleConfigurable(android.SortedUniqueStrings(*deps)),
	}

	result, err := proptools.RepackProperties([]interface{}{depProps})
	if err != nil {
		ctx.ModuleErrorf(err.Error())
	}

	file := &parser.File{
		Defs: []parser.Definition{
			&parser.Module{
				Type: "module",
				Map:  *result,
			},
		},
	}
	bytes, err := parser.Print(file)
	if err != nil {
		ctx.ModuleErrorf(err.Error())
	}
	return strings.TrimSpace(string(bytes))
}
