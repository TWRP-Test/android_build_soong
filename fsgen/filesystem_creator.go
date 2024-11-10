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
	"path/filepath"
	"strconv"
	"strings"

	"android/soong/android"
	"android/soong/filesystem"
	"android/soong/kernel"

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

type filesystemCreatorProps struct {
	Generated_partition_types   []string `blueprint:"mutated"`
	Unsupported_partition_types []string `blueprint:"mutated"`

	Vbmeta_module_names    []string `blueprint:"mutated"`
	Vbmeta_partition_names []string `blueprint:"mutated"`
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
		generatedPrebuiltEtcModuleNames := createPrebuiltEtcModules(ctx)
		createFsGenState(ctx, generatedPrebuiltEtcModuleNames)
		module.createInternalModules(ctx)
	})

	return module
}

func (f *filesystemCreator) createInternalModules(ctx android.LoadHookContext) {
	soongGeneratedPartitions := generatedPartitions(ctx)
	finalSoongGeneratedPartitions := make([]string, 0, len(soongGeneratedPartitions))
	for _, partitionType := range soongGeneratedPartitions {
		if f.createPartition(ctx, partitionType) {
			f.properties.Generated_partition_types = append(f.properties.Generated_partition_types, partitionType)
			finalSoongGeneratedPartitions = append(finalSoongGeneratedPartitions, partitionType)
		} else {
			f.properties.Unsupported_partition_types = append(f.properties.Unsupported_partition_types, partitionType)
		}
	}

	for _, x := range createVbmetaPartitions(ctx, finalSoongGeneratedPartitions) {
		f.properties.Vbmeta_module_names = append(f.properties.Vbmeta_module_names, x.moduleName)
		f.properties.Vbmeta_partition_names = append(f.properties.Vbmeta_partition_names, x.partitionName)
	}

	ctx.Config().Get(fsGenStateOnceKey).(*FsGenState).soongGeneratedPartitions = finalSoongGeneratedPartitions
	f.createDeviceModule(ctx, finalSoongGeneratedPartitions, f.properties.Vbmeta_module_names)
}

func generatedModuleName(cfg android.Config, suffix string) string {
	prefix := "soong"
	if cfg.HasDeviceProduct() {
		prefix = cfg.DeviceProduct()
	}
	return fmt.Sprintf("%s_generated_%s", prefix, suffix)
}

func generatedModuleNameForPartition(cfg android.Config, partitionType string) string {
	return generatedModuleName(cfg, fmt.Sprintf("%s_image", partitionType))
}

func (f *filesystemCreator) createDeviceModule(
	ctx android.LoadHookContext,
	generatedPartitionTypes []string,
	vbmetaPartitions []string,
) {
	baseProps := &struct {
		Name *string
	}{
		Name: proptools.StringPtr(generatedModuleName(ctx.Config(), "device")),
	}

	// Currently, only the system and system_ext partition module is created.
	partitionProps := &filesystem.PartitionNameProperties{}
	if android.InList("system", generatedPartitionTypes) {
		partitionProps.System_partition_name = proptools.StringPtr(generatedModuleNameForPartition(ctx.Config(), "system"))
	}
	if android.InList("system_ext", generatedPartitionTypes) {
		partitionProps.System_ext_partition_name = proptools.StringPtr(generatedModuleNameForPartition(ctx.Config(), "system_ext"))
	}
	if android.InList("vendor", generatedPartitionTypes) {
		partitionProps.Vendor_partition_name = proptools.StringPtr(generatedModuleNameForPartition(ctx.Config(), "vendor"))
	}
	if android.InList("product", generatedPartitionTypes) {
		partitionProps.Product_partition_name = proptools.StringPtr(generatedModuleNameForPartition(ctx.Config(), "product"))
	}
	if android.InList("odm", generatedPartitionTypes) {
		partitionProps.Odm_partition_name = proptools.StringPtr(generatedModuleNameForPartition(ctx.Config(), "odm"))
	}
	if android.InList("userdata", f.properties.Generated_partition_types) {
		partitionProps.Userdata_partition_name = proptools.StringPtr(generatedModuleNameForPartition(ctx.Config(), "userdata"))
	}
	partitionProps.Vbmeta_partitions = vbmetaPartitions

	ctx.CreateModule(filesystem.AndroidDeviceFactory, baseProps, partitionProps)
}

func partitionSpecificFsProps(fsProps *filesystem.FilesystemProperties, partitionType string) {
	switch partitionType {
	case "system":
		fsProps.Build_logtags = proptools.BoolPtr(true)
		// https://source.corp.google.com/h/googleplex-android/platform/build//639d79f5012a6542ab1f733b0697db45761ab0f3:core/packaging/flags.mk;l=21;drc=5ba8a8b77507f93aa48cc61c5ba3f31a4d0cbf37;bpv=1;bpt=0
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
		fsProps.Fsverity.Libs = []string{":framework-res{.export-package.apk}"}
		// TODO(b/377734331): only generate the symlinks if the relevant partitions exist
		fsProps.Symlinks = []filesystem.SymlinkDefinition{
			filesystem.SymlinkDefinition{
				Target: proptools.StringPtr("/product"),
				Name:   proptools.StringPtr("system/product"),
			},
			filesystem.SymlinkDefinition{
				Target: proptools.StringPtr("/system_ext"),
				Name:   proptools.StringPtr("system/system_ext"),
			},
			filesystem.SymlinkDefinition{
				Target: proptools.StringPtr("/vendor"),
				Name:   proptools.StringPtr("system/vendor"),
			},
			filesystem.SymlinkDefinition{
				Target: proptools.StringPtr("/system_dlkm/lib/modules"),
				Name:   proptools.StringPtr("system/lib/modules"),
			},
		}
		fsProps.Base_dir = proptools.StringPtr("system")
	case "system_ext":
		fsProps.Fsverity.Inputs = []string{
			"framework/*",
			"framework/*/*",     // framework/{arch}
			"framework/oat/*/*", // framework/oat/{arch}
		}
		fsProps.Fsverity.Libs = []string{":framework-res{.export-package.apk}"}
	case "product":
		fsProps.Gen_aconfig_flags_pb = proptools.BoolPtr(true)
	case "vendor":
		fsProps.Gen_aconfig_flags_pb = proptools.BoolPtr(true)
		fsProps.Symlinks = []filesystem.SymlinkDefinition{
			filesystem.SymlinkDefinition{
				Target: proptools.StringPtr("/odm"),
				Name:   proptools.StringPtr("vendor/odm"),
			},
			filesystem.SymlinkDefinition{
				Target: proptools.StringPtr("/vendor_dlkm/lib/modules"),
				Name:   proptools.StringPtr("vendor/lib/modules"),
			},
		}
		fsProps.Base_dir = proptools.StringPtr("vendor")
	case "odm":
		fsProps.Symlinks = []filesystem.SymlinkDefinition{
			filesystem.SymlinkDefinition{
				Target: proptools.StringPtr("/odm_dlkm/lib/modules"),
				Name:   proptools.StringPtr("odm/lib/modules"),
			},
		}
		fsProps.Base_dir = proptools.StringPtr("odm")
	case "userdata":
		fsProps.Base_dir = proptools.StringPtr("data")

	}
}

// Creates a soong module to build the given partition. Returns false if we can't support building
// it.
func (f *filesystemCreator) createPartition(ctx android.LoadHookContext, partitionType string) bool {
	baseProps := generateBaseProps(proptools.StringPtr(generatedModuleNameForPartition(ctx.Config(), partitionType)))

	fsProps, supported := generateFsProps(ctx, partitionType)
	if !supported {
		return false
	}

	if partitionType == "vendor" || partitionType == "product" {
		fsProps.Linkerconfig.Gen_linker_config = proptools.BoolPtr(true)
		fsProps.Linkerconfig.Linker_config_srcs = f.createLinkerConfigSourceFilegroups(ctx, partitionType)
	}

	if partitionType == "system_dlkm" {
		kernelModules := ctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse.SystemKernelModules
		f.createPrebuiltKernelModules(ctx, partitionType, kernelModules)
	}

	var module android.Module
	if partitionType == "system" {
		module = ctx.CreateModule(filesystem.SystemImageFactory, baseProps, fsProps)
	} else {
		// Explicitly set the partition.
		fsProps.Partition_type = proptools.StringPtr(partitionType)
		module = ctx.CreateModule(filesystem.FilesystemFactory, baseProps, fsProps)
	}
	module.HideFromMake()
	if partitionType == "vendor" {
		f.createVendorBuildProp(ctx)
	}
	return true
}

// createPrebuiltKernelModules creates `prebuilt_kernel_modules`. These modules will be added to deps of the
// autogenerated *_dlkm filsystem modules. Each _dlkm partition should have a single prebuilt_kernel_modules dependency.
// This ensures that the depmod artifacts (modules.* installed in /lib/modules/) are generated with a complete view.

// The input `kernelModules` is a space separated list of .ko files in the workspace.
func (f *filesystemCreator) createPrebuiltKernelModules(ctx android.LoadHookContext, partitionType string, kernelModules []string) {
	fsGenState := ctx.Config().Get(fsGenStateOnceKey).(*FsGenState)
	name := generatedModuleName(ctx.Config(), fmt.Sprintf("%s-kernel-modules", partitionType))
	props := &struct {
		Name                 *string
		Srcs                 []string
		System_dlkm_specific *bool
		Vendor_dlkm_specific *bool // TODO (b/377562851)
		Odm_dlkm_specific    *bool // TODO (b/377563262)
	}{
		Name: proptools.StringPtr(name),
		Srcs: kernelModules,
	}
	if partitionType == "system_dlkm" {
		props.System_dlkm_specific = proptools.BoolPtr(true)
	}
	kernelModule := ctx.CreateModuleInDirectory(
		kernel.PrebuiltKernelModulesFactory,
		".", // create in root directory for now
		props,
	)
	kernelModule.HideFromMake()
	// Add to deps
	(*fsGenState.fsDeps[partitionType])[name] = defaultDepCandidateProps(ctx.Config())
}

// Create a build_prop and android_info module. This will be used to create /vendor/build.prop
func (f *filesystemCreator) createVendorBuildProp(ctx android.LoadHookContext) {
	// Create a android_info for vendor
	// The board info files might be in a directory outside the root soong namespace, so create
	// the module in "."
	partitionVars := ctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse
	androidInfoProps := &struct {
		Name                  *string
		Board_info_files      []string
		Bootloader_board_name *string
	}{
		Name:             proptools.StringPtr(generatedModuleName(ctx.Config(), "android-info.prop")),
		Board_info_files: partitionVars.BoardInfoFiles,
	}
	if len(androidInfoProps.Board_info_files) == 0 {
		androidInfoProps.Bootloader_board_name = proptools.StringPtr(partitionVars.BootLoaderBoardName)
	}
	androidInfoProp := ctx.CreateModuleInDirectory(
		android.AndroidInfoFactory,
		".",
		androidInfoProps,
	)
	androidInfoProp.HideFromMake()
	// Create a build prop for vendor
	vendorBuildProps := &struct {
		Name           *string
		Vendor         *bool
		Stem           *string
		Product_config *string
		Android_info   *string
	}{
		Name:           proptools.StringPtr(generatedModuleName(ctx.Config(), "vendor-build.prop")),
		Vendor:         proptools.BoolPtr(true),
		Stem:           proptools.StringPtr("build.prop"),
		Product_config: proptools.StringPtr(":product_config"),
		Android_info:   proptools.StringPtr(":" + androidInfoProp.Name()),
	}
	vendorBuildProp := ctx.CreateModule(
		android.BuildPropFactory,
		vendorBuildProps,
	)
	vendorBuildProp.HideFromMake()
}

// createLinkerConfigSourceFilegroups creates filegroup modules to generate linker.config.pb for the following partitions
// 1. vendor: Using PRODUCT_VENDOR_LINKER_CONFIG_FRAGMENTS (space separated file list)
// 1. product: Using PRODUCT_PRODUCT_LINKER_CONFIG_FRAGMENTS (space separated file list)
// It creates a filegroup for each file in the fragment list
// The filegroup modules are then added to `linker_config_srcs` of the autogenerated vendor `android_filesystem`.
func (f *filesystemCreator) createLinkerConfigSourceFilegroups(ctx android.LoadHookContext, partitionType string) []string {
	ret := []string{}
	partitionVars := ctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse
	var linkerConfigSrcs []string
	if partitionType == "vendor" {
		linkerConfigSrcs = android.FirstUniqueStrings(partitionVars.VendorLinkerConfigSrcs)
	} else if partitionType == "product" {
		linkerConfigSrcs = android.FirstUniqueStrings(partitionVars.ProductLinkerConfigSrcs)
	} else {
		ctx.ModuleErrorf("linker.config.pb is only supported for vendor and product partitions. For system partition, use `android_system_image`")
	}

	if len(linkerConfigSrcs) > 0 {
		// Create a filegroup, and add `:<filegroup_name>` to ret.
		for index, linkerConfigSrc := range linkerConfigSrcs {
			dir := filepath.Dir(linkerConfigSrc)
			base := filepath.Base(linkerConfigSrc)
			fgName := generatedModuleName(ctx.Config(), fmt.Sprintf("%s-linker-config-src%s", partitionType, strconv.Itoa(index)))
			srcs := []string{base}
			fgProps := &struct {
				Name *string
				Srcs proptools.Configurable[[]string]
			}{
				Name: proptools.StringPtr(fgName),
				Srcs: proptools.NewSimpleConfigurable(srcs),
			}
			ctx.CreateModuleInDirectory(
				android.FileGroupFactory,
				dir,
				fgProps,
			)
			ret = append(ret, ":"+fgName)
		}
	}
	return ret
}

type filesystemBaseProperty struct {
	Name             *string
	Compile_multilib *string
	Visibility       []string
}

func generateBaseProps(namePtr *string) *filesystemBaseProperty {
	return &filesystemBaseProperty{
		Name:             namePtr,
		Compile_multilib: proptools.StringPtr("both"),
		// The vbmeta modules are currently in the root directory and depend on the partitions
		Visibility: []string{"//.", "//build/soong:__subpackages__"},
	}
}

func generateFsProps(ctx android.EarlyModuleContext, partitionType string) (*filesystem.FilesystemProperties, bool) {
	fsProps := &filesystem.FilesystemProperties{}

	partitionVars := ctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse
	specificPartitionVars := partitionVars.PartitionQualifiedVariables[partitionType]

	// BOARD_SYSTEMIMAGE_FILE_SYSTEM_TYPE
	fsType := specificPartitionVars.BoardFileSystemType
	if fsType == "" {
		fsType = "ext4" //default
	}
	fsProps.Type = proptools.StringPtr(fsType)
	if filesystem.GetFsTypeFromString(ctx, *fsProps.Type).IsUnknown() {
		// Currently the android_filesystem module type only supports a handful of FS types like ext4, erofs
		return nil, false
	}

	// Don't build this module on checkbuilds, the soong-built partitions are still in-progress
	// and sometimes don't build.
	fsProps.Unchecked_module = proptools.BoolPtr(true)

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

	fsProps.Base_dir = proptools.StringPtr(partitionType)

	fsProps.Is_auto_generated = proptools.BoolPtr(true)

	partitionSpecificFsProps(fsProps, partitionType)

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
	// - systemImageProperties.Linker_config_src

	return fsProps, true
}

func (f *filesystemCreator) createDiffTest(ctx android.ModuleContext, partitionType string) android.Path {
	partitionModuleName := generatedModuleNameForPartition(ctx.Config(), partitionType)
	systemImage := ctx.GetDirectDepWithTag(partitionModuleName, generatedFilesystemDepTag)
	filesystemInfo, ok := android.OtherModuleProvider(ctx, systemImage, filesystem.FilesystemProvider)
	if !ok {
		ctx.ModuleErrorf("Expected module %s to provide FileysystemInfo", partitionModuleName)
	}
	makeFileList := android.PathForArbitraryOutput(ctx, fmt.Sprintf("target/product/%s/obj/PACKAGING/%s_intermediates/file_list.txt", ctx.Config().DeviceName(), partitionType))
	diffTestResultFile := android.PathForModuleOut(ctx, fmt.Sprintf("diff_test_%s.txt", partitionModuleName))

	builder := android.NewRuleBuilder(pctx, ctx)
	builder.Command().BuiltTool("file_list_diff").
		Input(makeFileList).
		Input(filesystemInfo.FileListFile).
		Text(partitionModuleName)
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

func createVbmetaDiff(ctx android.ModuleContext, vbmetaModuleName string, vbmetaPartitionName string) android.Path {
	vbmetaModule := ctx.GetDirectDepWithTag(vbmetaModuleName, generatedVbmetaPartitionDepTag)
	outputFilesProvider, ok := android.OtherModuleProvider(ctx, vbmetaModule, android.OutputFilesProvider)
	if !ok {
		ctx.ModuleErrorf("Expected module %s to provide OutputFiles", vbmetaModule)
	}
	if len(outputFilesProvider.DefaultOutputFiles) != 1 {
		ctx.ModuleErrorf("Expected 1 output file from module %s", vbmetaModule)
	}
	soongVbMetaFile := outputFilesProvider.DefaultOutputFiles[0]
	makeVbmetaFile := android.PathForArbitraryOutput(ctx, fmt.Sprintf("target/product/%s/%s.img", ctx.Config().DeviceName(), vbmetaPartitionName))

	diffTestResultFile := android.PathForModuleOut(ctx, fmt.Sprintf("diff_test_%s.txt", vbmetaModuleName))
	builder := android.NewRuleBuilder(pctx, ctx)
	builder.Command().Text("diff").
		Input(soongVbMetaFile).
		Input(makeVbmetaFile)
	builder.Command().Text("touch").Output(diffTestResultFile)
	builder.Build(vbmetaModuleName+" diff test", vbmetaModuleName+" diff test")
	return diffTestResultFile
}

type systemImageDepTagType struct {
	blueprint.BaseDependencyTag
}

var generatedFilesystemDepTag systemImageDepTagType
var generatedVbmetaPartitionDepTag systemImageDepTagType

func (f *filesystemCreator) DepsMutator(ctx android.BottomUpMutatorContext) {
	for _, partitionType := range f.properties.Generated_partition_types {
		ctx.AddDependency(ctx.Module(), generatedFilesystemDepTag, generatedModuleNameForPartition(ctx.Config(), partitionType))
	}
	for _, vbmetaModule := range f.properties.Vbmeta_module_names {
		ctx.AddDependency(ctx.Module(), generatedVbmetaPartitionDepTag, vbmetaModule)
	}
}

func (f *filesystemCreator) GenerateAndroidBuildActions(ctx android.ModuleContext) {
	if ctx.ModuleDir() != "build/soong/fsgen" {
		ctx.ModuleErrorf("There can only be one soong_filesystem_creator in build/soong/fsgen")
	}
	f.HideFromMake()

	var content strings.Builder
	generatedBp := android.PathForModuleOut(ctx, "soong_generated_product_config.bp")
	for _, partition := range ctx.Config().Get(fsGenStateOnceKey).(*FsGenState).soongGeneratedPartitions {
		content.WriteString(generateBpContent(ctx, partition))
		content.WriteString("\n")
	}
	android.WriteFileRule(ctx, generatedBp, content.String())

	ctx.Phony("product_config_to_bp", generatedBp)

	var diffTestFiles []android.Path
	for _, partitionType := range f.properties.Generated_partition_types {
		diffTestFile := f.createDiffTest(ctx, partitionType)
		diffTestFiles = append(diffTestFiles, diffTestFile)
		ctx.Phony(fmt.Sprintf("soong_generated_%s_filesystem_test", partitionType), diffTestFile)
	}
	for _, partitionType := range f.properties.Unsupported_partition_types {
		diffTestFile := createFailingCommand(ctx, fmt.Sprintf("Couldn't build %s partition", partitionType))
		diffTestFiles = append(diffTestFiles, diffTestFile)
		ctx.Phony(fmt.Sprintf("soong_generated_%s_filesystem_test", partitionType), diffTestFile)
	}
	for i, vbmetaModule := range f.properties.Vbmeta_module_names {
		diffTestFile := createVbmetaDiff(ctx, vbmetaModule, f.properties.Vbmeta_partition_names[i])
		diffTestFiles = append(diffTestFiles, diffTestFile)
		ctx.Phony(fmt.Sprintf("soong_generated_%s_filesystem_test", f.properties.Vbmeta_partition_names[i]), diffTestFile)
	}
	ctx.Phony("soong_generated_filesystem_tests", diffTestFiles...)
}

func generateBpContent(ctx android.EarlyModuleContext, partitionType string) string {
	fsProps, fsTypeSupported := generateFsProps(ctx, partitionType)
	if !fsTypeSupported {
		return ""
	}

	baseProps := generateBaseProps(proptools.StringPtr(generatedModuleNameForPartition(ctx.Config(), partitionType)))
	deps := ctx.Config().Get(fsGenStateOnceKey).(*FsGenState).fsDeps[partitionType]
	depProps := generateDepStruct(*deps)

	result, err := proptools.RepackProperties([]interface{}{baseProps, fsProps, depProps})
	if err != nil {
		ctx.ModuleErrorf("%s", err.Error())
		return ""
	}

	moduleType := "android_filesystem"
	if partitionType == "system" {
		moduleType = "android_system_image"
	}

	file := &parser.File{
		Defs: []parser.Definition{
			&parser.Module{
				Type: moduleType,
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
