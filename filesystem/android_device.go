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
	// Name of the Boot_partition_name partition filesystem module
	Boot_partition_name *string
	// Name of the System partition filesystem module
	System_partition_name *string
	// Name of the System_ext partition filesystem module
	System_ext_partition_name *string
	// Name of the Product partition filesystem module
	Product_partition_name *string
	// Name of the Vendor partition filesystem module
	Vendor_partition_name *string
	// Name of the Odm partition filesystem module
	Odm_partition_name *string
	// The vbmeta partition and its "chained" partitions
	Vbmeta_partitions []string
	// Name of the Userdata partition filesystem module
	Userdata_partition_name *string
}

type androidDevice struct {
	android.ModuleBase

	partitionProps PartitionNameProperties
}

func AndroidDeviceFactory() android.Module {
	module := &androidDevice{}
	module.AddProperties(&module.partitionProps)
	android.InitAndroidMultiTargetsArchModule(module, android.DeviceSupported, android.MultilibCommon)
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
	if a.partitionProps.Vendor_partition_name != nil {
		fsInfo := a.getFilesystemInfo(ctx, *a.partitionProps.Vendor_partition_name)
		builder.Command().Textf("mkdir -p %s/VENDOR", targetFilesDir.String())
		builder.Command().
			BuiltTool("acp").
			Textf("-rd %s/. %s/VENDOR", fsInfo.RootDir, targetFilesDir).
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
