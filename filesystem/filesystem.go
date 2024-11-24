// Copyright (C) 2020 The Android Open Source Project
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

package filesystem

import (
	"crypto/sha256"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"android/soong/android"
	"android/soong/cc"
	"android/soong/linkerconfig"

	"github.com/google/blueprint"
	"github.com/google/blueprint/proptools"
)

func init() {
	registerBuildComponents(android.InitRegistrationContext)
}

func registerBuildComponents(ctx android.RegistrationContext) {
	ctx.RegisterModuleType("android_filesystem", FilesystemFactory)
	ctx.RegisterModuleType("android_filesystem_defaults", filesystemDefaultsFactory)
	ctx.RegisterModuleType("android_system_image", SystemImageFactory)
	ctx.RegisterModuleType("avb_add_hash_footer", avbAddHashFooterFactory)
	ctx.RegisterModuleType("avb_add_hash_footer_defaults", avbAddHashFooterDefaultsFactory)
	ctx.RegisterModuleType("avb_gen_vbmeta_image", avbGenVbmetaImageFactory)
	ctx.RegisterModuleType("avb_gen_vbmeta_image_defaults", avbGenVbmetaImageDefaultsFactory)
}

type filesystem struct {
	android.ModuleBase
	android.PackagingBase
	android.DefaultableModuleBase

	properties FilesystemProperties

	output     android.Path
	installDir android.InstallPath

	fileListFile android.Path

	// Keeps the entries installed from this filesystem
	entries []string

	filesystemBuilder filesystemBuilder
}

type filesystemBuilder interface {
	BuildLinkerConfigFile(ctx android.ModuleContext, builder *android.RuleBuilder, rebasedDir android.OutputPath)
	// Function that filters PackagingSpec in PackagingBase.GatherPackagingSpecs()
	FilterPackagingSpec(spec android.PackagingSpec) bool
	// Function that modifies PackagingSpec in PackagingBase.GatherPackagingSpecs() to customize.
	// For example, GSI system.img contains system_ext and product artifacts and their
	// relPathInPackage need to be rebased to system/system_ext and system/system_product.
	ModifyPackagingSpec(spec *android.PackagingSpec)
}

var _ filesystemBuilder = (*filesystem)(nil)

type SymlinkDefinition struct {
	Target *string
	Name   *string
}

type FilesystemProperties struct {
	// When set to true, sign the image with avbtool. Default is false.
	Use_avb *bool

	// Path to the private key that avbtool will use to sign this filesystem image.
	// TODO(jiyong): allow apex_key to be specified here
	Avb_private_key *string `android:"path"`

	// Signing algorithm for avbtool. Default is SHA256_RSA4096.
	Avb_algorithm *string

	// Hash algorithm used for avbtool (for descriptors). This is passed as hash_algorithm to
	// avbtool. Default used by avbtool is sha1.
	Avb_hash_algorithm *string

	// The index used to prevent rollback of the image. Only used if use_avb is true.
	Rollback_index *int64

	// Name of the partition stored in vbmeta desc. Defaults to the name of this module.
	Partition_name *string

	// Type of the filesystem. Currently, ext4, erofs, cpio, and compressed_cpio are supported. Default
	// is ext4.
	Type *string

	// Identifies which partition this is for //visibility:any_system_image (and others) visibility
	// checks, and will be used in the future for API surface checks.
	Partition_type *string

	// file_contexts file to make image. Currently, only ext4 is supported.
	File_contexts *string `android:"path"`

	// Base directory relative to root, to which deps are installed, e.g. "system". Default is "."
	// (root).
	Base_dir *string

	// Directories to be created under root. e.g. /dev, /proc, etc.
	Dirs proptools.Configurable[[]string]

	// Symbolic links to be created under root with "ln -sf <target> <name>".
	Symlinks []SymlinkDefinition

	// Seconds since unix epoch to override timestamps of file entries
	Fake_timestamp *string

	// When set, passed to mkuserimg_mke2fs --mke2fs_uuid & --mke2fs_hash_seed.
	// Otherwise, they'll be set as random which might cause indeterministic build output.
	Uuid *string

	// Mount point for this image. Default is "/"
	Mount_point *string

	// If set to the name of a partition ("system", "vendor", etc), this filesystem module
	// will also include the contents of the make-built staging directories. If any soong
	// modules would be installed to the same location as a make module, they will overwrite
	// the make version.
	Include_make_built_files string

	// When set, builds etc/event-log-tags file by merging logtags from all dependencies.
	// Default is false
	Build_logtags *bool

	// Install aconfig_flags.pb file for the modules installed in this partition.
	Gen_aconfig_flags_pb *bool

	Fsverity fsverityProperties

	// If this property is set to true, the filesystem will call ctx.UncheckedModule(), causing
	// it to not be built on checkbuilds. Used for the automatic migration from make to soong
	// build modules, where we want to emit some not-yet-working filesystems and we don't want them
	// to be built.
	Unchecked_module *bool `blueprint:"mutated"`

	Erofs ErofsProperties

	F2fs F2fsProperties

	Linker_config LinkerConfigProperties

	// Determines if the module is auto-generated from Soong or not. If the module is
	// auto-generated, its deps are exempted from visibility enforcement.
	Is_auto_generated *bool

	// Path to the dev nodes description file. This is only needed for building the ramdisk
	// partition and should not be explicitly specified.
	Dev_nodes_description_file *string `android:"path" blueprint:"mutated"`
}

// Additional properties required to generate erofs FS partitions.
type ErofsProperties struct {
	// Compressor and Compression level passed to mkfs.erofs. e.g. (lz4hc,9)
	// Please see external/erofs-utils/README for complete documentation.
	Compressor *string

	// Used as --compress-hints for mkfs.erofs
	Compress_hints *string `android:"path"`

	Sparse *bool
}

// Additional properties required to generate f2fs FS partitions.
type F2fsProperties struct {
	Sparse *bool
}

type LinkerConfigProperties struct {

	// Build a linker.config.pb file
	Gen_linker_config *bool

	// List of files (in .json format) that will be converted to a linker config file (in .pb format).
	// The linker config file be installed in the filesystem at /etc/linker.config.pb
	Linker_config_srcs []string `android:"path"`
}

// android_filesystem packages a set of modules and their transitive dependencies into a filesystem
// image. The filesystem images are expected to be mounted in the target device, which means the
// modules in the filesystem image are built for the target device (i.e. Android, not Linux host).
// The modules are placed in the filesystem image just like they are installed to the ordinary
// partitions like system.img. For example, cc_library modules are placed under ./lib[64] directory.
func FilesystemFactory() android.Module {
	module := &filesystem{}
	module.filesystemBuilder = module
	initFilesystemModule(module, module)
	return module
}

func initFilesystemModule(module android.DefaultableModule, filesystemModule *filesystem) {
	module.AddProperties(&filesystemModule.properties)
	android.InitPackageModule(filesystemModule)
	filesystemModule.PackagingBase.DepsCollectFirstTargetOnly = true
	filesystemModule.PackagingBase.AllowHighPriorityDeps = true
	android.InitAndroidMultiTargetsArchModule(module, android.DeviceSupported, android.MultilibCommon)
	android.InitDefaultableModule(module)

	android.AddLoadHook(module, func(ctx android.LoadHookContext) {
		filesystemModule.setDevNodesDescriptionProp()
	})
}

type depTag struct {
	blueprint.BaseDependencyTag
	android.PackagingItemAlwaysDepTag
}

var dependencyTag = depTag{}

type depTagWithVisibilityEnforcementBypass struct {
	depTag
}

var _ android.ExcludeFromVisibilityEnforcementTag = (*depTagWithVisibilityEnforcementBypass)(nil)

func (t depTagWithVisibilityEnforcementBypass) ExcludeFromVisibilityEnforcement() {}

var dependencyTagWithVisibilityEnforcementBypass = depTagWithVisibilityEnforcementBypass{}

// ramdiskDevNodesDescription is the name of the filegroup module that provides the file that
// contains the description of dev nodes added to the CPIO archive for the ramdisk partition.
const ramdiskDevNodesDescription = "ramdisk_node_list"

func (f *filesystem) setDevNodesDescriptionProp() {
	if proptools.String(f.properties.Partition_name) == "ramdisk" {
		f.properties.Dev_nodes_description_file = proptools.StringPtr(":" + ramdiskDevNodesDescription)
	}
}

func (f *filesystem) DepsMutator(ctx android.BottomUpMutatorContext) {
	if proptools.Bool(f.properties.Is_auto_generated) {
		f.AddDeps(ctx, dependencyTagWithVisibilityEnforcementBypass)
	} else {
		f.AddDeps(ctx, dependencyTag)
	}
}

type fsType int

const (
	ext4Type fsType = iota
	erofsType
	f2fsType
	compressedCpioType
	cpioType // uncompressed
	unknown
)

func (fs fsType) IsUnknown() bool {
	return fs == unknown
}

type FilesystemInfo struct {
	// A text file containing the list of paths installed on the partition.
	FileListFile android.Path
}

var FilesystemProvider = blueprint.NewProvider[FilesystemInfo]()

func GetFsTypeFromString(ctx android.EarlyModuleContext, typeStr string) fsType {
	switch typeStr {
	case "ext4":
		return ext4Type
	case "erofs":
		return erofsType
	case "f2fs":
		return f2fsType
	case "compressed_cpio":
		return compressedCpioType
	case "cpio":
		return cpioType
	default:
		return unknown
	}
}

func (f *filesystem) fsType(ctx android.ModuleContext) fsType {
	typeStr := proptools.StringDefault(f.properties.Type, "ext4")
	fsType := GetFsTypeFromString(ctx, typeStr)
	if fsType == unknown {
		ctx.PropertyErrorf("type", "%q not supported", typeStr)
	}
	return fsType
}

func (f *filesystem) installFileName() string {
	return f.BaseModuleName() + ".img"
}

func (f *filesystem) partitionName() string {
	return proptools.StringDefault(f.properties.Partition_name, f.Name())
}

func (f *filesystem) FilterPackagingSpec(ps android.PackagingSpec) bool {
	// Filesystem module respects the installation semantic. A PackagingSpec from a module with
	// IsSkipInstall() is skipped.
	if ps.SkipInstall() {
		return false
	}
	if proptools.Bool(f.properties.Is_auto_generated) { // TODO (spandandas): Remove this.
		pt := f.PartitionType()
		return ps.Partition() == pt || strings.HasPrefix(ps.Partition(), pt+"/")
	}
	return true
}

func (f *filesystem) ModifyPackagingSpec(ps *android.PackagingSpec) {
	// Sometimes, android.modulePartition() returns a path with >1 path components.
	// This makes the partition field of packagingSpecs have multiple components, like
	// "system/product". Right now, the filesystem module doesn't look at the partition field
	// when deciding what path to install the file under, only the RelPathInPackage field, so
	// we move the later path components from partition to relPathInPackage. This should probably
	// be revisited in the future.
	prefix := f.PartitionType() + "/"
	if strings.HasPrefix(ps.Partition(), prefix) {
		subPartition := strings.TrimPrefix(ps.Partition(), prefix)
		ps.SetPartition(f.PartitionType())
		ps.SetRelPathInPackage(filepath.Join(subPartition, ps.RelPathInPackage()))
	}
}

var pctx = android.NewPackageContext("android/soong/filesystem")

func (f *filesystem) GenerateAndroidBuildActions(ctx android.ModuleContext) {
	validatePartitionType(ctx, f)
	switch f.fsType(ctx) {
	case ext4Type, erofsType, f2fsType:
		f.output = f.buildImageUsingBuildImage(ctx)
	case compressedCpioType:
		f.output = f.buildCpioImage(ctx, true)
	case cpioType:
		f.output = f.buildCpioImage(ctx, false)
	default:
		return
	}

	f.installDir = android.PathForModuleInstall(ctx, "etc")
	ctx.InstallFile(f.installDir, f.installFileName(), f.output)
	ctx.SetOutputFiles([]android.Path{f.output}, "")

	fileListFile := android.PathForModuleOut(ctx, "fileList")
	android.WriteFileRule(ctx, fileListFile, f.installedFilesList())

	android.SetProvider(ctx, FilesystemProvider, FilesystemInfo{
		FileListFile: fileListFile,
	})
	f.fileListFile = fileListFile

	if proptools.Bool(f.properties.Unchecked_module) {
		ctx.UncheckedModule()
	}
}

func (f *filesystem) appendToEntry(ctx android.ModuleContext, installedFile android.Path) {
	partitionBaseDir := android.PathForModuleOut(ctx, "root", f.partitionName()).String() + "/"

	relPath, inTargetPartition := strings.CutPrefix(installedFile.String(), partitionBaseDir)
	if inTargetPartition {
		f.entries = append(f.entries, relPath)
	}
}

func (f *filesystem) installedFilesList() string {
	installedFilePaths := android.FirstUniqueStrings(f.entries)
	slices.Sort(installedFilePaths)

	return strings.Join(installedFilePaths, "\n")
}

func validatePartitionType(ctx android.ModuleContext, p partition) {
	if !android.InList(p.PartitionType(), validPartitions) {
		ctx.PropertyErrorf("partition_type", "partition_type must be one of %s, found: %s", validPartitions, p.PartitionType())
	}

	ctx.VisitDirectDepsWithTag(android.DefaultsDepTag, func(m android.Module) {
		if fdm, ok := m.(*filesystemDefaults); ok {
			if p.PartitionType() != fdm.PartitionType() {
				ctx.PropertyErrorf("partition_type",
					"%s doesn't match with the partition type %s of the filesystem default module %s",
					p.PartitionType(), fdm.PartitionType(), m.Name())
			}
		}
	})
}

// Copy extra files/dirs that are not from the `deps` property to `rootDir`, checking for conflicts with files
// already in `rootDir`.
func (f *filesystem) buildNonDepsFiles(ctx android.ModuleContext, builder *android.RuleBuilder, rootDir android.OutputPath) {
	// create dirs and symlinks
	for _, dir := range f.properties.Dirs.GetOrDefault(ctx, nil) {
		// OutputPath.Join verifies dir
		builder.Command().Text("mkdir -p").Text(rootDir.Join(ctx, dir).String())
	}

	for _, symlink := range f.properties.Symlinks {
		name := strings.TrimSpace(proptools.String(symlink.Name))
		target := strings.TrimSpace(proptools.String(symlink.Target))

		if name == "" {
			ctx.PropertyErrorf("symlinks", "Name can't be empty")
			continue
		}

		if target == "" {
			ctx.PropertyErrorf("symlinks", "Target can't be empty")
			continue
		}

		// OutputPath.Join verifies name. don't need to verify target.
		dst := rootDir.Join(ctx, name)
		builder.Command().Textf("(! [ -e %s -o -L %s ] || (echo \"%s already exists from an earlier stage of the build\" && exit 1))", dst, dst, dst)
		builder.Command().Text("mkdir -p").Text(filepath.Dir(dst.String()))
		builder.Command().Text("ln -sf").Text(proptools.ShellEscape(target)).Text(dst.String())
		f.appendToEntry(ctx, dst)
	}
}

func (f *filesystem) copyPackagingSpecs(ctx android.ModuleContext, builder *android.RuleBuilder, specs map[string]android.PackagingSpec, rootDir, rebasedDir android.WritablePath) []string {
	rootDirSpecs := make(map[string]android.PackagingSpec)
	rebasedDirSpecs := make(map[string]android.PackagingSpec)

	for rel, spec := range specs {
		if spec.Partition() == "root" {
			rootDirSpecs[rel] = spec
		} else {
			rebasedDirSpecs[rel] = spec
		}
	}

	dirsToSpecs := make(map[android.WritablePath]map[string]android.PackagingSpec)
	dirsToSpecs[rootDir] = rootDirSpecs
	dirsToSpecs[rebasedDir] = rebasedDirSpecs

	return f.CopySpecsToDirs(ctx, builder, dirsToSpecs)
}

func (f *filesystem) copyFilesToProductOut(ctx android.ModuleContext, builder *android.RuleBuilder, rebasedDir android.OutputPath) {
	if f.Name() != ctx.Config().SoongDefinedSystemImage() {
		return
	}
	installPath := android.PathForModuleInPartitionInstall(ctx, f.partitionName())
	builder.Command().Textf("cp -prf %s/* %s", rebasedDir, installPath)
}

func (f *filesystem) buildImageUsingBuildImage(ctx android.ModuleContext) android.Path {
	rootDir := android.PathForModuleOut(ctx, "root").OutputPath
	rebasedDir := rootDir
	if f.properties.Base_dir != nil {
		rebasedDir = rootDir.Join(ctx, *f.properties.Base_dir)
	}
	builder := android.NewRuleBuilder(pctx, ctx)
	// Wipe the root dir to get rid of leftover files from prior builds
	builder.Command().Textf("rm -rf %s && mkdir -p %s", rootDir, rootDir)
	specs := f.gatherFilteredPackagingSpecs(ctx)
	f.entries = f.copyPackagingSpecs(ctx, builder, specs, rootDir, rebasedDir)

	f.buildNonDepsFiles(ctx, builder, rootDir)
	f.addMakeBuiltFiles(ctx, builder, rootDir)
	f.buildFsverityMetadataFiles(ctx, builder, specs, rootDir, rebasedDir)
	f.buildEventLogtagsFile(ctx, builder, rebasedDir)
	f.buildAconfigFlagsFiles(ctx, builder, specs, rebasedDir)
	f.filesystemBuilder.BuildLinkerConfigFile(ctx, builder, rebasedDir)
	f.copyFilesToProductOut(ctx, builder, rebasedDir)

	// run host_init_verifier
	// Ideally we should have a concept of pluggable linters that verify the generated image.
	// While such concept is not implement this will do.
	// TODO(b/263574231): substitute with pluggable linter.
	builder.Command().
		BuiltTool("host_init_verifier").
		FlagWithArg("--out_system=", rootDir.String()+"/system")

	propFile, toolDeps := f.buildPropFile(ctx)
	output := android.PathForModuleOut(ctx, f.installFileName())
	builder.Command().BuiltTool("build_image").
		Text(rootDir.String()). // input directory
		Input(propFile).
		Implicits(toolDeps).
		Output(output).
		Text(rootDir.String()) // directory where to find fs_config_files|dirs

	// rootDir is not deleted. Might be useful for quick inspection.
	builder.Build("build_filesystem_image", fmt.Sprintf("Creating filesystem %s", f.BaseModuleName()))

	return output
}

func (f *filesystem) buildFileContexts(ctx android.ModuleContext) android.Path {
	builder := android.NewRuleBuilder(pctx, ctx)
	fcBin := android.PathForModuleOut(ctx, "file_contexts.bin")
	builder.Command().BuiltTool("sefcontext_compile").
		FlagWithOutput("-o ", fcBin).
		Input(android.PathForModuleSrc(ctx, proptools.String(f.properties.File_contexts)))
	builder.Build("build_filesystem_file_contexts", fmt.Sprintf("Creating filesystem file contexts for %s", f.BaseModuleName()))
	return fcBin
}

// Calculates avb_salt from entry list (sorted) for deterministic output.
func (f *filesystem) salt() string {
	return sha1sum(f.entries)
}

func (f *filesystem) buildPropFile(ctx android.ModuleContext) (android.Path, android.Paths) {
	var deps android.Paths
	var propFileString strings.Builder
	addStr := func(name string, value string) {
		propFileString.WriteString(name)
		propFileString.WriteRune('=')
		propFileString.WriteString(value)
		propFileString.WriteRune('\n')
	}
	addPath := func(name string, path android.Path) {
		addStr(name, path.String())
		deps = append(deps, path)
	}

	// Type string that build_image.py accepts.
	fsTypeStr := func(t fsType) string {
		switch t {
		// TODO(372522486): add more types like f2fs, erofs, etc.
		case ext4Type:
			return "ext4"
		case erofsType:
			return "erofs"
		case f2fsType:
			return "f2fs"
		}
		panic(fmt.Errorf("unsupported fs type %v", t))
	}

	addStr("fs_type", fsTypeStr(f.fsType(ctx)))
	addStr("mount_point", proptools.StringDefault(f.properties.Mount_point, "/"))
	addStr("use_dynamic_partition_size", "true")
	addPath("ext_mkuserimg", ctx.Config().HostToolPath(ctx, "mkuserimg_mke2fs"))
	// b/177813163 deps of the host tools have to be added. Remove this.
	for _, t := range []string{"mke2fs", "e2fsdroid", "tune2fs"} {
		deps = append(deps, ctx.Config().HostToolPath(ctx, t))
	}

	if proptools.Bool(f.properties.Use_avb) {
		addStr("avb_hashtree_enable", "true")
		addPath("avb_avbtool", ctx.Config().HostToolPath(ctx, "avbtool"))
		algorithm := proptools.StringDefault(f.properties.Avb_algorithm, "SHA256_RSA4096")
		addStr("avb_algorithm", algorithm)
		key := android.PathForModuleSrc(ctx, proptools.String(f.properties.Avb_private_key))
		addPath("avb_key_path", key)
		addStr("partition_name", f.partitionName())
		avb_add_hashtree_footer_args := "--do_not_generate_fec"
		if hashAlgorithm := proptools.String(f.properties.Avb_hash_algorithm); hashAlgorithm != "" {
			avb_add_hashtree_footer_args += " --hash_algorithm " + hashAlgorithm
		}
		if f.properties.Rollback_index != nil {
			rollbackIndex := proptools.Int(f.properties.Rollback_index)
			if rollbackIndex < 0 {
				ctx.PropertyErrorf("rollback_index", "Rollback index must be non-negative")
			}
			avb_add_hashtree_footer_args += " --rollback_index " + strconv.Itoa(rollbackIndex)
		}
		securityPatchKey := "com.android.build." + f.partitionName() + ".security_patch"
		securityPatchValue := ctx.Config().PlatformSecurityPatch()
		avb_add_hashtree_footer_args += " --prop " + securityPatchKey + ":" + securityPatchValue
		addStr("avb_add_hashtree_footer_args", avb_add_hashtree_footer_args)
		addStr("avb_salt", f.salt())
	}

	if proptools.String(f.properties.File_contexts) != "" {
		addPath("selinux_fc", f.buildFileContexts(ctx))
	}
	if timestamp := proptools.String(f.properties.Fake_timestamp); timestamp != "" {
		addStr("timestamp", timestamp)
	}
	if uuid := proptools.String(f.properties.Uuid); uuid != "" {
		addStr("uuid", uuid)
		addStr("hash_seed", uuid)
	}

	fst := f.fsType(ctx)
	switch fst {
	case erofsType:
		// Add erofs properties
		if compressor := f.properties.Erofs.Compressor; compressor != nil {
			addStr("erofs_default_compressor", proptools.String(compressor))
		}
		if compressHints := f.properties.Erofs.Compress_hints; compressHints != nil {
			addPath("erofs_default_compress_hints", android.PathForModuleSrc(ctx, *compressHints))
		}
		if proptools.BoolDefault(f.properties.Erofs.Sparse, true) {
			// https://source.corp.google.com/h/googleplex-android/platform/build/+/88b1c67239ca545b11580237242774b411f2fed9:core/Makefile;l=2292;bpv=1;bpt=0;drc=ea8f34bc1d6e63656b4ec32f2391e9d54b3ebb6b
			addStr("erofs_sparse_flag", "-s")
		}
	case f2fsType:
		if proptools.BoolDefault(f.properties.F2fs.Sparse, true) {
			// https://source.corp.google.com/h/googleplex-android/platform/build/+/88b1c67239ca545b11580237242774b411f2fed9:core/Makefile;l=2294;drc=ea8f34bc1d6e63656b4ec32f2391e9d54b3ebb6b;bpv=1;bpt=0
			addStr("f2fs_sparse_flag", "-S")
		}
	}
	f.checkFsTypePropertyError(ctx, fst, fsTypeStr(fst))

	propFile := android.PathForModuleOut(ctx, "prop")
	android.WriteFileRuleVerbatim(ctx, propFile, propFileString.String())
	return propFile, deps
}

// This method checks if there is any property set for the fstype(s) other than
// the current fstype.
func (f *filesystem) checkFsTypePropertyError(ctx android.ModuleContext, t fsType, fs string) {
	raiseError := func(otherFsType, currentFsType string) {
		errMsg := fmt.Sprintf("%s is non-empty, but FS type is %s\n. Please delete %s properties if this partition should use %s\n", otherFsType, currentFsType, otherFsType, currentFsType)
		ctx.PropertyErrorf(otherFsType, errMsg)
	}

	if t != erofsType {
		if f.properties.Erofs.Compressor != nil || f.properties.Erofs.Compress_hints != nil || f.properties.Erofs.Sparse != nil {
			raiseError("erofs", fs)
		}
	}
	if t != f2fsType {
		if f.properties.F2fs.Sparse != nil {
			raiseError("f2fs", fs)
		}
	}
}

func (f *filesystem) buildCpioImage(ctx android.ModuleContext, compressed bool) android.Path {
	if proptools.Bool(f.properties.Use_avb) {
		ctx.PropertyErrorf("use_avb", "signing compresed cpio image using avbtool is not supported."+
			"Consider adding this to bootimg module and signing the entire boot image.")
	}

	if proptools.String(f.properties.File_contexts) != "" {
		ctx.PropertyErrorf("file_contexts", "file_contexts is not supported for compressed cpio image.")
	}

	if f.properties.Include_make_built_files != "" {
		ctx.PropertyErrorf("include_make_built_files", "include_make_built_files is not supported for compressed cpio image.")
	}

	rootDir := android.PathForModuleOut(ctx, "root").OutputPath
	rebasedDir := rootDir
	if f.properties.Base_dir != nil {
		rebasedDir = rootDir.Join(ctx, *f.properties.Base_dir)
	}
	builder := android.NewRuleBuilder(pctx, ctx)
	// Wipe the root dir to get rid of leftover files from prior builds
	builder.Command().Textf("rm -rf %s && mkdir -p %s", rootDir, rootDir)
	specs := f.gatherFilteredPackagingSpecs(ctx)
	f.entries = f.copyPackagingSpecs(ctx, builder, specs, rootDir, rebasedDir)

	f.buildNonDepsFiles(ctx, builder, rootDir)
	f.buildFsverityMetadataFiles(ctx, builder, specs, rootDir, rebasedDir)
	f.buildEventLogtagsFile(ctx, builder, rebasedDir)
	f.buildAconfigFlagsFiles(ctx, builder, specs, rebasedDir)
	f.filesystemBuilder.BuildLinkerConfigFile(ctx, builder, rebasedDir)
	f.copyFilesToProductOut(ctx, builder, rebasedDir)

	output := android.PathForModuleOut(ctx, f.installFileName())
	cmd := builder.Command().
		BuiltTool("mkbootfs").
		Text(rootDir.String()) // input directory
	if nodeList := f.properties.Dev_nodes_description_file; nodeList != nil {
		cmd.FlagWithInput("-n ", android.PathForModuleSrc(ctx, proptools.String(nodeList)))
	}
	if compressed {
		cmd.Text("|").
			BuiltTool("lz4").
			Flag("--favor-decSpeed"). // for faster boot
			Flag("-12").              // maximum compression level
			Flag("-l").               // legacy format for kernel
			Text(">").Output(output)
	} else {
		cmd.Text(">").Output(output)
	}

	// rootDir is not deleted. Might be useful for quick inspection.
	builder.Build("build_cpio_image", fmt.Sprintf("Creating filesystem %s", f.BaseModuleName()))

	return output
}

var validPartitions = []string{
	"system",
	"userdata",
	"cache",
	"system_other",
	"vendor",
	"product",
	"system_ext",
	"odm",
	"vendor_dlkm",
	"odm_dlkm",
	"system_dlkm",
	"ramdisk",
	"vendor_ramdisk",
}

func (f *filesystem) addMakeBuiltFiles(ctx android.ModuleContext, builder *android.RuleBuilder, rootDir android.Path) {
	partition := f.properties.Include_make_built_files
	if partition == "" {
		return
	}
	if !slices.Contains(validPartitions, partition) {
		ctx.PropertyErrorf("include_make_built_files", "Expected one of %#v, found %q", validPartitions, partition)
		return
	}
	stampFile := fmt.Sprintf("target/product/%s/obj/PACKAGING/%s_intermediates/staging_dir.stamp", ctx.Config().DeviceName(), partition)
	fileListFile := fmt.Sprintf("target/product/%s/obj/PACKAGING/%s_intermediates/file_list.txt", ctx.Config().DeviceName(), partition)
	stagingDir := fmt.Sprintf("target/product/%s/%s", ctx.Config().DeviceName(), partition)

	builder.Command().BuiltTool("merge_directories").
		Implicit(android.PathForArbitraryOutput(ctx, stampFile)).
		Text("--ignore-duplicates").
		FlagWithInput("--file-list", android.PathForArbitraryOutput(ctx, fileListFile)).
		Text(rootDir.String()).
		Text(android.PathForArbitraryOutput(ctx, stagingDir).String())
}

func (f *filesystem) buildEventLogtagsFile(ctx android.ModuleContext, builder *android.RuleBuilder, rebasedDir android.OutputPath) {
	if !proptools.Bool(f.properties.Build_logtags) {
		return
	}

	logtagsFilePaths := make(map[string]bool)
	ctx.WalkDeps(func(child, parent android.Module) bool {
		if logtagsInfo, ok := android.OtherModuleProvider(ctx, child, android.LogtagsProviderKey); ok {
			for _, path := range logtagsInfo.Logtags {
				logtagsFilePaths[path.String()] = true
			}
		}
		return true
	})

	if len(logtagsFilePaths) == 0 {
		return
	}

	etcPath := rebasedDir.Join(ctx, "etc")
	eventLogtagsPath := etcPath.Join(ctx, "event-log-tags")
	builder.Command().Text("mkdir").Flag("-p").Text(etcPath.String())
	cmd := builder.Command().BuiltTool("merge-event-log-tags").
		FlagWithArg("-o ", eventLogtagsPath.String()).
		FlagWithInput("-m ", android.MergedLogtagsPath(ctx))

	for _, path := range android.SortedKeys(logtagsFilePaths) {
		cmd.Text(path)
	}

	f.appendToEntry(ctx, eventLogtagsPath)
}

func (f *filesystem) BuildLinkerConfigFile(ctx android.ModuleContext, builder *android.RuleBuilder, rebasedDir android.OutputPath) {
	if !proptools.Bool(f.properties.Linker_config.Gen_linker_config) {
		return
	}

	provideModules, _ := f.getLibsForLinkerConfig(ctx)
	output := rebasedDir.Join(ctx, "etc", "linker.config.pb")
	linkerconfig.BuildLinkerConfig(ctx, builder, android.PathsForModuleSrc(ctx, f.properties.Linker_config.Linker_config_srcs), provideModules, nil, output)

	f.appendToEntry(ctx, output)
}

type partition interface {
	PartitionType() string
}

func (f *filesystem) PartitionType() string {
	return proptools.StringDefault(f.properties.Partition_type, "system")
}

var _ partition = (*filesystem)(nil)

var _ android.AndroidMkEntriesProvider = (*filesystem)(nil)

// Implements android.AndroidMkEntriesProvider
func (f *filesystem) AndroidMkEntries() []android.AndroidMkEntries {
	return []android.AndroidMkEntries{android.AndroidMkEntries{
		Class:      "ETC",
		OutputFile: android.OptionalPathForPath(f.output),
		ExtraEntries: []android.AndroidMkExtraEntriesFunc{
			func(ctx android.AndroidMkExtraEntriesContext, entries *android.AndroidMkEntries) {
				entries.SetString("LOCAL_MODULE_PATH", f.installDir.String())
				entries.SetString("LOCAL_INSTALLED_MODULE_STEM", f.installFileName())
				entries.SetString("LOCAL_FILESYSTEM_FILELIST", f.fileListFile.String())
			},
		},
	}}
}

// Filesystem is the public interface for the filesystem struct. Currently, it's only for the apex
// package to have access to the output file.
type Filesystem interface {
	android.Module
	OutputPath() android.Path

	// Returns the output file that is signed by avbtool. If this module is not signed, returns
	// nil.
	SignedOutputPath() android.Path
}

var _ Filesystem = (*filesystem)(nil)

func (f *filesystem) OutputPath() android.Path {
	return f.output
}

func (f *filesystem) SignedOutputPath() android.Path {
	if proptools.Bool(f.properties.Use_avb) {
		return f.OutputPath()
	}
	return nil
}

// Filter the result of GatherPackagingSpecs to discard items targeting outside "system" partition.
// Note that "apex" module installs its contents to "apex"(fake partition) as well
// for symbol lookup by imitating "activated" paths.
func (f *filesystem) gatherFilteredPackagingSpecs(ctx android.ModuleContext) map[string]android.PackagingSpec {
	specs := f.PackagingBase.GatherPackagingSpecsWithFilterAndModifier(ctx, f.filesystemBuilder.FilterPackagingSpec, f.filesystemBuilder.ModifyPackagingSpec)
	return specs
}

func sha1sum(values []string) string {
	h := sha256.New()
	for _, value := range values {
		io.WriteString(h, value)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// Base cc.UseCoverage

var _ cc.UseCoverage = (*filesystem)(nil)

func (*filesystem) IsNativeCoverageNeeded(ctx cc.IsNativeCoverageNeededContext) bool {
	return ctx.Device() && ctx.DeviceConfig().NativeCoverageEnabled()
}

// android_filesystem_defaults

type filesystemDefaults struct {
	android.ModuleBase
	android.DefaultsModuleBase

	properties FilesystemProperties
}

// android_filesystem_defaults is a default module for android_filesystem and android_system_image
func filesystemDefaultsFactory() android.Module {
	module := &filesystemDefaults{}
	module.AddProperties(&module.properties)
	module.AddProperties(&android.PackagingProperties{})
	android.InitDefaultsModule(module)
	return module
}

func (f *filesystemDefaults) PartitionType() string {
	return proptools.StringDefault(f.properties.Partition_type, "system")
}

var _ partition = (*filesystemDefaults)(nil)

func (f *filesystemDefaults) GenerateAndroidBuildActions(ctx android.ModuleContext) {
	validatePartitionType(ctx, f)
}

// getLibsForLinkerConfig returns
// 1. A list of libraries installed in this filesystem
// 2. A list of dep libraries _not_ installed in this filesystem
//
// `linkerconfig.BuildLinkerConfig` will convert these two to a linker.config.pb for the filesystem
// (1) will be added to --provideLibs if they are C libraries with a stable interface (has stubs)
// (2) will be added to --requireLibs if they are C libraries with a stable interface (has stubs)
func (f *filesystem) getLibsForLinkerConfig(ctx android.ModuleContext) ([]android.Module, []android.Module) {
	// we need "Module"s for packaging items
	modulesInPackageByModule := make(map[android.Module]bool)
	modulesInPackageByName := make(map[string]bool)

	deps := f.gatherFilteredPackagingSpecs(ctx)
	ctx.WalkDeps(func(child, parent android.Module) bool {
		for _, ps := range android.OtherModuleProviderOrDefault(
			ctx, child, android.InstallFilesProvider).PackagingSpecs {
			if _, ok := deps[ps.RelPathInPackage()]; ok {
				modulesInPackageByModule[child] = true
				modulesInPackageByName[child.Name()] = true
				return true
			}
		}
		return true
	})

	provideModules := make([]android.Module, 0, len(modulesInPackageByModule))
	for mod := range modulesInPackageByModule {
		provideModules = append(provideModules, mod)
	}

	var requireModules []android.Module
	ctx.WalkDeps(func(child, parent android.Module) bool {
		_, parentInPackage := modulesInPackageByModule[parent]
		_, childInPackageName := modulesInPackageByName[child.Name()]

		// When parent is in the package, and child (or its variant) is not, this can be from an interface.
		if parentInPackage && !childInPackageName {
			requireModules = append(requireModules, child)
		}
		return true
	})

	return provideModules, requireModules
}

// Checks that the given file doesn't exceed the given size, and will also print a warning
// if it's nearing the maximum size. Equivalent to assert-max-image-size in make:
// https://cs.android.com/android/platform/superproject/main/+/main:build/make/core/definitions.mk;l=3455;drc=993c4de29a02a6accd60ceaaee153307e1a18d10
func assertMaxImageSize(builder *android.RuleBuilder, image android.Path, maxSize int64, addAvbLater bool) {
	if addAvbLater {
		// The value 69632 is derived from MAX_VBMETA_SIZE + MAX_FOOTER_SIZE in avbtool.
		// Logic copied from make:
		// https://cs.android.com/android/platform/superproject/main/+/main:build/make/core/Makefile;l=228;drc=a6a0007ef24e16c0b79f439beac4a118416717e6
		maxSize -= 69632
	}
	cmd := builder.Command()
	cmd.Textf(`file="%s"; maxsize="%d";`+
		`total=$(stat -c "%%s" "$file" | tr -d '\n');`+
		`if [ "$total" -gt "$maxsize" ]; then `+
		`  echo "error: $file too large ($total > $maxsize)";`+
		`  false;`+
		`elif [ "$total" -gt $((maxsize - 32768)) ]; then `+
		`  echo "WARNING: $file approaching size limit ($total now; limit $maxsize)";`+
		`fi`,
		image, maxSize)
	cmd.Implicit(image)
}
