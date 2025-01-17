// Copyright (C) 2025 The Android Open Source Project
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

var (
	copyStagingDirRule = pctx.AndroidStaticRule("copy_staging_dir", blueprint.RuleParams{
		Command: "rsync -a --checksum $dir/ $dest && touch $out",
	}, "dir", "dest")
)

func (a *androidDevice) copyToProductOut(ctx android.ModuleContext, builder *android.RuleBuilder, src android.Path, dest string) {
	destPath := android.PathForModuleInPartitionInstall(ctx, "").Join(ctx, dest)
	builder.Command().Text("rsync").Flag("-a").Flag("--checksum").Input(src).Text(destPath.String())
}

func (a *androidDevice) copyFilesToProductOutForSoongOnly(ctx android.ModuleContext) android.Path {
	filesystemInfos := a.getFsInfos(ctx)

	// The current logic to copy the staging directories to PRODUCT_OUT isn't very sound.
	// We only track dependencies on the image file, so if the image file wasn't changed, the
	// staging directory won't be re-copied. If you do an installclean, it would remove the copied
	// staging directories but not affect the intermediates path image file, so the next build
	// wouldn't re-copy them. As a hack, create a presence detector that would be deleted on
	// an installclean to use as a dep for the staging dir copies.
	productOutPresenceDetector := android.PathForModuleInPartitionInstall(ctx, "", "product_out_presence_detector.txt")
	ctx.Build(pctx, android.BuildParams{
		Rule:   android.Touch,
		Output: productOutPresenceDetector,
	})

	var deps android.Paths

	for _, partition := range android.SortedKeys(filesystemInfos) {
		info := filesystemInfos[partition]
		imgInstallPath := android.PathForModuleInPartitionInstall(ctx, "", partition+".img")
		ctx.Build(pctx, android.BuildParams{
			Rule:   android.Cp,
			Input:  info.Output,
			Output: imgInstallPath,
		})
		dirStamp := android.PathForModuleOut(ctx, partition+"_staging_dir_copy_stamp.txt")
		dirInstallPath := android.PathForModuleInPartitionInstall(ctx, "", partition)
		ctx.Build(pctx, android.BuildParams{
			Rule:   copyStagingDirRule,
			Output: dirStamp,
			Implicits: []android.Path{
				info.Output,
				productOutPresenceDetector,
			},
			Args: map[string]string{
				"dir":  info.RebasedDir.String(),
				"dest": dirInstallPath.String(),
			},
		})

		// Make it so doing `m <moduleName>` or `m <partitionType>` will copy the files to
		// PRODUCT_OUT
		ctx.Phony(info.ModuleName, dirStamp, imgInstallPath)
		ctx.Phony(partition, dirStamp, imgInstallPath)

		deps = append(deps, imgInstallPath, dirStamp)
	}

	// List all individual files to be copied to PRODUCT_OUT here
	if a.deviceProps.Bootloader != nil {
		bootloaderInstallPath := android.PathForModuleInPartitionInstall(ctx, "", "bootloader")
		ctx.Build(pctx, android.BuildParams{
			Rule:   android.Cp,
			Input:  android.PathForModuleSrc(ctx, *a.deviceProps.Bootloader),
			Output: bootloaderInstallPath,
		})
		deps = append(deps, bootloaderInstallPath)
	}

	copyBootImg := func(prop *string, type_ string) {
		if proptools.String(prop) != "" {
			partition := ctx.GetDirectDepWithTag(*prop, filesystemDepTag)
			if info, ok := android.OtherModuleProvider(ctx, partition, BootimgInfoProvider); ok {
				installPath := android.PathForModuleInPartitionInstall(ctx, "", type_+".img")
				ctx.Build(pctx, android.BuildParams{
					Rule:   android.Cp,
					Input:  info.Output,
					Output: installPath,
				})
				deps = append(deps, installPath)
			} else {
				ctx.ModuleErrorf("%s does not set BootimgInfo\n", *prop)
			}
		}
	}

	copyBootImg(a.partitionProps.Init_boot_partition_name, "init_boot")
	copyBootImg(a.partitionProps.Boot_partition_name, "boot")
	copyBootImg(a.partitionProps.Vendor_boot_partition_name, "vendor_boot")

	for _, vbmetaModName := range a.partitionProps.Vbmeta_partitions {
		partition := ctx.GetDirectDepWithTag(vbmetaModName, filesystemDepTag)
		if info, ok := android.OtherModuleProvider(ctx, partition, vbmetaPartitionProvider); ok {
			installPath := android.PathForModuleInPartitionInstall(ctx, "", info.Name+".img")
			ctx.Build(pctx, android.BuildParams{
				Rule:   android.Cp,
				Input:  info.Output,
				Output: installPath,
			})
			deps = append(deps, installPath)
		} else {
			ctx.ModuleErrorf("%s does not set vbmetaPartitionProvider\n", vbmetaModName)
		}
	}

	if proptools.String(a.partitionProps.Super_partition_name) != "" {
		partition := ctx.GetDirectDepWithTag(*a.partitionProps.Super_partition_name, superPartitionDepTag)
		if info, ok := android.OtherModuleProvider(ctx, partition, SuperImageProvider); ok {
			installPath := android.PathForModuleInPartitionInstall(ctx, "", "super.img")
			ctx.Build(pctx, android.BuildParams{
				Rule:   android.Cp,
				Input:  info.SuperImage,
				Output: installPath,
			})
			deps = append(deps, installPath)
		} else {
			ctx.ModuleErrorf("%s does not set SuperImageProvider\n", *a.partitionProps.Super_partition_name)
		}
	}

	if proptools.String(a.deviceProps.Android_info) != "" {
		installPath := android.PathForModuleInPartitionInstall(ctx, "", "android_info.txt")
		ctx.Build(pctx, android.BuildParams{
			Rule:   android.Cp,
			Input:  android.PathForModuleSrc(ctx, *a.deviceProps.Android_info),
			Output: installPath,
		})
		deps = append(deps, installPath)
	}

	copyToProductOutTimestamp := android.PathForModuleOut(ctx, "product_out_copy_timestamp")
	ctx.Build(pctx, android.BuildParams{
		Rule:      android.Touch,
		Output:    copyToProductOutTimestamp,
		Implicits: deps,
	})

	return copyToProductOutTimestamp
}

// Returns a mapping from partition type -> FilesystemInfo. This includes filesystems that are
// nested inside of other partitions, such as the partitions inside super.img, or ramdisk inside
// of boot.
func (a *androidDevice) getFsInfos(ctx android.ModuleContext) map[string]FilesystemInfo {
	type propToType struct {
		prop *string
		ty   string
	}

	filesystemInfos := make(map[string]FilesystemInfo)

	partitionDefinitions := []propToType{
		propToType{a.partitionProps.System_partition_name, "system"},
		propToType{a.partitionProps.System_ext_partition_name, "system_ext"},
		propToType{a.partitionProps.Product_partition_name, "product"},
		propToType{a.partitionProps.Vendor_partition_name, "vendor"},
		propToType{a.partitionProps.Odm_partition_name, "odm"},
		propToType{a.partitionProps.Recovery_partition_name, "recovery"},
		propToType{a.partitionProps.System_dlkm_partition_name, "system_dlkm"},
		propToType{a.partitionProps.Vendor_dlkm_partition_name, "vendor_dlkm"},
		propToType{a.partitionProps.Odm_dlkm_partition_name, "odm_dlkm"},
		propToType{a.partitionProps.Userdata_partition_name, "userdata"},
		// filesystemInfo from init_boot and vendor_boot actually are re-exports of the ramdisk
		// images inside of them
		propToType{a.partitionProps.Init_boot_partition_name, "ramdisk"},
		propToType{a.partitionProps.Vendor_boot_partition_name, "vendor_ramdisk"},
	}
	for _, partitionDefinition := range partitionDefinitions {
		if proptools.String(partitionDefinition.prop) != "" {
			partition := ctx.GetDirectDepWithTag(*partitionDefinition.prop, filesystemDepTag)
			if info, ok := android.OtherModuleProvider(ctx, partition, FilesystemProvider); ok {
				filesystemInfos[partitionDefinition.ty] = info
			} else {
				ctx.ModuleErrorf("Super partition %s does not set FilesystemProvider\n", partition.Name())
			}
		}
	}
	if a.partitionProps.Super_partition_name != nil {
		superPartition := ctx.GetDirectDepWithTag(*a.partitionProps.Super_partition_name, superPartitionDepTag)
		if info, ok := android.OtherModuleProvider(ctx, superPartition, SuperImageProvider); ok {
			for partition := range info.SubImageInfo {
				filesystemInfos[partition] = info.SubImageInfo[partition]
			}
		} else {
			ctx.ModuleErrorf("Super partition %s does not set SuperImageProvider\n", superPartition.Name())
		}
	}

	return filesystemInfos
}
