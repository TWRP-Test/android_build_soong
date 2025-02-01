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

package filesystem

import (
	"android/soong/android"
	"strconv"

	"github.com/google/blueprint"
	"github.com/google/blueprint/proptools"
)

type installedAconfigFlagsInfo struct {
	aconfigFiles android.Paths
}

var installedAconfigFlagsProvider = blueprint.NewProvider[installedAconfigFlagsInfo]()

type importAconfigDepDag struct {
	blueprint.BaseDependencyTag
}

var importAconfigDependencyTag = interPartitionDepTag{}

func (f *filesystem) buildAconfigFlagsFiles(
	ctx android.ModuleContext,
	builder *android.RuleBuilder,
	specs map[string]android.PackagingSpec,
	dir android.OutputPath,
	fullInstallPaths *[]FullInstallPathInfo,
) {
	var caches []android.Path
	for _, ps := range specs {
		caches = append(caches, ps.GetAconfigPaths()...)
	}

	ctx.VisitDirectDepsWithTag(importAconfigDependencyTag, func(m android.Module) {
		info, ok := android.OtherModuleProvider(ctx, m, installedAconfigFlagsProvider)
		if !ok {
			ctx.ModuleErrorf("expected dependency %s to have an installedAconfigFlagsProvider", m.Name())
			return
		}
		caches = append(caches, info.aconfigFiles...)
	})
	caches = android.SortedUniquePaths(caches)

	android.SetProvider(ctx, installedAconfigFlagsProvider, installedAconfigFlagsInfo{
		aconfigFiles: caches,
	})

	if !proptools.Bool(f.properties.Gen_aconfig_flags_pb) {
		return
	}

	container := f.PartitionType()

	installAconfigFlagsPath := dir.Join(ctx, "etc", "aconfig_flags.pb")
	cmd := builder.Command().
		BuiltTool("aconfig").
		Text(" dump-cache --dedup --format protobuf --out").
		Output(installAconfigFlagsPath).
		Textf("--filter container:%s+state:ENABLED", container).
		Textf("--filter container:%s+permission:READ_WRITE", container)
	for _, cache := range caches {
		cmd.FlagWithInput("--cache ", cache)
	}
	*fullInstallPaths = append(*fullInstallPaths, FullInstallPathInfo{
		FullInstallPath: android.PathForModuleInPartitionInstall(ctx, f.PartitionType(), "etc/aconfig_flags.pb"),
		SourcePath:      installAconfigFlagsPath,
	})
	f.appendToEntry(ctx, installAconfigFlagsPath)

	installAconfigStorageDir := dir.Join(ctx, "etc", "aconfig")
	builder.Command().Text("mkdir -p").Text(installAconfigStorageDir.String())

	// To enable fingerprint, we need to have v2 storage files. The default version is 1.
	storageFilesVersion := 1
	if ctx.Config().ReleaseFingerprintAconfigPackages() {
		storageFilesVersion = 2
	}

	generatePartitionAconfigStorageFile := func(fileType, fileName string) {
		outputPath := installAconfigStorageDir.Join(ctx, fileName)
		builder.Command().
			BuiltTool("aconfig").
			FlagWithArg("create-storage --container ", container).
			FlagWithArg("--file ", fileType).
			FlagWithOutput("--out ", outputPath).
			FlagWithArg("--cache ", installAconfigFlagsPath.String()).
			FlagWithArg("--version ", strconv.Itoa(storageFilesVersion))
		*fullInstallPaths = append(*fullInstallPaths, FullInstallPathInfo{
			FullInstallPath: android.PathForModuleInPartitionInstall(ctx, f.PartitionType(), "etc/aconfig", fileName),
			SourcePath:      outputPath,
		})
		f.appendToEntry(ctx, outputPath)
	}

	if ctx.Config().ReleaseCreateAconfigStorageFile() {
		generatePartitionAconfigStorageFile("package_map", "package.map")
		generatePartitionAconfigStorageFile("flag_map", "flag.map")
		generatePartitionAconfigStorageFile("flag_val", "flag.val")
		generatePartitionAconfigStorageFile("flag_info", "flag.info")
	}
}
