package fsgen

import (
	"android/soong/android"
	"android/soong/filesystem"
	"path/filepath"

	"github.com/google/blueprint/proptools"
)

func createBootImage(ctx android.LoadHookContext) bool {
	partitionVariables := ctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse

	if partitionVariables.TargetKernelPath == "" {
		// There are potentially code paths that don't set TARGET_KERNEL_PATH
		return false
	}

	kernelDir := filepath.Dir(partitionVariables.TargetKernelPath)
	kernelBase := filepath.Base(partitionVariables.TargetKernelPath)
	kernelFilegroupName := generatedModuleName(ctx.Config(), "kernel")

	ctx.CreateModuleInDirectory(
		android.FileGroupFactory,
		kernelDir,
		&struct {
			Name       *string
			Srcs       []string
			Visibility []string
		}{
			Name:       proptools.StringPtr(kernelFilegroupName),
			Srcs:       []string{kernelBase},
			Visibility: []string{"//visibility:public"},
		},
	)

	bootImageName := generatedModuleNameForPartition(ctx.Config(), "boot")

	ctx.CreateModule(
		filesystem.BootimgFactory,
		&filesystem.BootimgProperties{
			Kernel_prebuilt: proptools.StringPtr(":" + kernelFilegroupName),
			Ramdisk_module:  proptools.StringPtr(generatedModuleNameForPartition(ctx.Config(), "ramdisk")),
			Header_version:  proptools.StringPtr(partitionVariables.BoardBootHeaderVersion),
		},
		&struct {
			Name *string
		}{
			Name: proptools.StringPtr(bootImageName),
		},
	)
	return true
}

// Returns the equivalent of the BUILDING_BOOT_IMAGE variable in make. Derived from this logic:
// https://cs.android.com/android/platform/superproject/main/+/main:build/make/core/board_config.mk;l=458;drc=5b55f926830963c02ab1d2d91e46442f04ba3af0
func buildingBootImage(partitionVars android.PartitionVariables) bool {
	if partitionVars.BoardUsesRecoveryAsBoot {
		return false
	}

	if partitionVars.ProductBuildBootImage {
		return true
	}

	if len(partitionVars.BoardPrebuiltBootimage) > 0 {
		return false
	}

	if len(partitionVars.BoardBootimagePartitionSize) > 0 {
		return true
	}

	// TODO: return true if BOARD_KERNEL_BINARIES is set and has a *_BOOTIMAGE_PARTITION_SIZE
	// variable. However, I don't think BOARD_KERNEL_BINARIES is ever set in practice.

	return false
}
