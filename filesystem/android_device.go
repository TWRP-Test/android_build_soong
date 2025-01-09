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

	"github.com/google/blueprint"
	"github.com/google/blueprint/proptools"
)

type PartitionNameProperties struct {
	// Name of the boot partition filesystem module
	Boot_partition_name *string
	// Name of the vendor boot partition filesystem module
	Vendor_boot_partition_name *string
	// Name of the init boot partition filesystem module
	Init_boot_partition_name *string
	// Name of the system partition filesystem module
	System_partition_name *string
	// Name of the system_ext partition filesystem module
	System_ext_partition_name *string
	// Name of the product partition filesystem module
	Product_partition_name *string
	// Name of the vendor partition filesystem module
	Vendor_partition_name *string
	// Name of the odm partition filesystem module
	Odm_partition_name *string
	// Name of the recovery partition filesystem module
	Recovery_partition_name *string
	// The vbmeta partition and its "chained" partitions
	Vbmeta_partitions []string
	// Name of the userdata partition filesystem module
	Userdata_partition_name *string
	// Name of the system_dlkm partition filesystem module
	System_dlkm_partition_name *string
	// Name of the vendor_dlkm partition filesystem module
	Vendor_dlkm_partition_name *string
	// Name of the odm_dlkm partition filesystem module
	Odm_dlkm_partition_name *string
}

type androidDevice struct {
	android.ModuleBase

	partitionProps PartitionNameProperties
}

func AndroidDeviceFactory() android.Module {
	module := &androidDevice{}
	module.AddProperties(&module.partitionProps)
	android.InitAndroidMultiTargetsArchModule(module, android.DeviceSupported, android.MultilibFirst)
	return module
}

type partitionDepTagType struct {
	blueprint.BaseDependencyTag
}

var filesystemDepTag partitionDepTagType

func (a *androidDevice) DepsMutator(ctx android.BottomUpMutatorContext) {
	addDependencyIfDefined := func(dep *string) {
		if dep != nil {
			ctx.AddDependency(ctx.Module(), filesystemDepTag, proptools.String(dep))
		}
	}

	addDependencyIfDefined(a.partitionProps.Boot_partition_name)
	addDependencyIfDefined(a.partitionProps.System_partition_name)
	addDependencyIfDefined(a.partitionProps.System_ext_partition_name)
	addDependencyIfDefined(a.partitionProps.Product_partition_name)
	addDependencyIfDefined(a.partitionProps.Vendor_partition_name)
	addDependencyIfDefined(a.partitionProps.Odm_partition_name)
	addDependencyIfDefined(a.partitionProps.Userdata_partition_name)
	addDependencyIfDefined(a.partitionProps.System_dlkm_partition_name)
	addDependencyIfDefined(a.partitionProps.Vendor_dlkm_partition_name)
	addDependencyIfDefined(a.partitionProps.Odm_dlkm_partition_name)
	for _, vbmetaPartition := range a.partitionProps.Vbmeta_partitions {
		ctx.AddDependency(ctx.Module(), filesystemDepTag, vbmetaPartition)
	}
}

func (a *androidDevice) GenerateAndroidBuildActions(ctx android.ModuleContext) {
	a.buildTargetFilesZip(ctx)
}

func (a *androidDevice) buildTargetFilesZip(ctx android.ModuleContext) {
	targetFilesDir := android.PathForModuleOut(ctx, "target_files_dir")
	targetFilesZip := android.PathForModuleOut(ctx, "target_files.zip")

	builder := android.NewRuleBuilder(pctx, ctx)
	builder.Command().Textf("rm -rf %s", targetFilesDir.String())
	builder.Command().Textf("mkdir -p %s", targetFilesDir.String())
	partitionToSubdir := map[*string]string{
		a.partitionProps.System_partition_name:      "SYSTEM",
		a.partitionProps.System_ext_partition_name:  "SYSTEM_EXT",
		a.partitionProps.Product_partition_name:     "PRODUCT",
		a.partitionProps.Vendor_partition_name:      "VENDOR",
		a.partitionProps.Odm_partition_name:         "ODM",
		a.partitionProps.System_dlkm_partition_name: "SYSTEM_DLKM",
		a.partitionProps.Vendor_dlkm_partition_name: "VENDOR_DLKM",
		a.partitionProps.Odm_dlkm_partition_name:    "ODM_DLKM",
	}
	for partition, subdir := range partitionToSubdir {
		if partition == nil {
			continue
		}
		fsInfo := a.getFilesystemInfo(ctx, *partition)
		builder.Command().Textf("mkdir -p %s/%s", targetFilesDir.String(), subdir)
		builder.Command().
			BuiltTool("acp").
			Textf("-rd %s/. %s/%s", fsInfo.RootDir, targetFilesDir, subdir).
			Implicit(fsInfo.Output) // so that the staging dir is built
	}
	builder.Command().
		BuiltTool("soong_zip").
		Text("-d").
		FlagWithOutput("-o ", targetFilesZip).
		FlagWithArg("-C ", targetFilesDir.String()).
		FlagWithArg("-D ", targetFilesDir.String()).
		Text("-sha256")
	builder.Build("target_files_"+ctx.ModuleName(), "Build target_files.zip")
}

func (a *androidDevice) getFilesystemInfo(ctx android.ModuleContext, depName string) FilesystemInfo {
	fsMod := ctx.GetDirectDepWithTag(depName, filesystemDepTag)
	fsInfo, ok := android.OtherModuleProvider(ctx, fsMod, FilesystemProvider)
	if !ok {
		ctx.ModuleErrorf("Expected dependency %s to be a filesystem", depName)
	}
	return fsInfo
}
