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
	"android/soong/android"
	"android/soong/etc"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/google/blueprint/proptools"
)

type srcBaseFileInstallBaseFileTuple struct {
	srcBaseFile     string
	installBaseFile string
}

// prebuilt src files grouped by the install partitions.
// Each groups are a mapping of the relative install path to the name of the files
type prebuiltSrcGroupByInstallPartition struct {
	system     map[string][]srcBaseFileInstallBaseFileTuple
	system_ext map[string][]srcBaseFileInstallBaseFileTuple
	product    map[string][]srcBaseFileInstallBaseFileTuple
	vendor     map[string][]srcBaseFileInstallBaseFileTuple
}

func newPrebuiltSrcGroupByInstallPartition() *prebuiltSrcGroupByInstallPartition {
	return &prebuiltSrcGroupByInstallPartition{
		system:     map[string][]srcBaseFileInstallBaseFileTuple{},
		system_ext: map[string][]srcBaseFileInstallBaseFileTuple{},
		product:    map[string][]srcBaseFileInstallBaseFileTuple{},
		vendor:     map[string][]srcBaseFileInstallBaseFileTuple{},
	}
}

func isSubdirectory(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return !strings.HasPrefix(rel, "..")
}

func appendIfCorrectInstallPartition(partitionToInstallPathList []partitionToInstallPath, destPath, srcPath string, srcGroup *prebuiltSrcGroupByInstallPartition) {
	for _, part := range partitionToInstallPathList {
		partition := part.name
		installPath := part.installPath

		if isSubdirectory(installPath, destPath) {
			relativeInstallPath, _ := filepath.Rel(installPath, destPath)
			relativeInstallDir := filepath.Dir(relativeInstallPath)
			var srcMap map[string][]srcBaseFileInstallBaseFileTuple
			switch partition {
			case "system":
				srcMap = srcGroup.system
			case "system_ext":
				srcMap = srcGroup.system_ext
			case "product":
				srcMap = srcGroup.product
			case "vendor":
				srcMap = srcGroup.vendor
			}
			if srcMap != nil {
				srcMap[relativeInstallDir] = append(srcMap[relativeInstallDir], srcBaseFileInstallBaseFileTuple{
					srcBaseFile:     filepath.Base(srcPath),
					installBaseFile: filepath.Base(destPath),
				})
			}
			return
		}
	}
}

func uniqueExistingProductCopyFileMap(ctx android.LoadHookContext) map[string]string {
	seen := make(map[string]bool)
	filtered := make(map[string]string)

	for src, dest := range ctx.Config().ProductVariables().PartitionVarsForSoongMigrationOnlyDoNotUse.ProductCopyFiles {
		if _, ok := seen[dest]; !ok {
			if optionalPath := android.ExistentPathForSource(ctx, src); optionalPath.Valid() {
				seen[dest] = true
				filtered[src] = dest
			}
		}
	}

	return filtered
}

type partitionToInstallPath struct {
	name        string
	installPath string
}

func processProductCopyFiles(ctx android.LoadHookContext) map[string]*prebuiltSrcGroupByInstallPartition {
	// Filter out duplicate dest entries and non existing src entries
	productCopyFileMap := uniqueExistingProductCopyFileMap(ctx)

	// System is intentionally added at the last to consider the scenarios where
	// non-system partitions are installed as part of the system partition
	partitionToInstallPathList := []partitionToInstallPath{
		{name: "vendor", installPath: ctx.DeviceConfig().VendorPath()},
		{name: "product", installPath: ctx.DeviceConfig().ProductPath()},
		{name: "system_ext", installPath: ctx.DeviceConfig().SystemExtPath()},
		{name: "system", installPath: "system"},
	}

	groupedSources := map[string]*prebuiltSrcGroupByInstallPartition{}
	for _, src := range android.SortedKeys(productCopyFileMap) {
		dest := productCopyFileMap[src]
		srcFileDir := filepath.Dir(src)
		if _, ok := groupedSources[srcFileDir]; !ok {
			groupedSources[srcFileDir] = newPrebuiltSrcGroupByInstallPartition()
		}
		appendIfCorrectInstallPartition(partitionToInstallPathList, dest, filepath.Base(src), groupedSources[srcFileDir])
	}

	return groupedSources
}

type prebuiltModuleProperties struct {
	Name *string

	Soc_specific        *bool
	Product_specific    *bool
	System_ext_specific *bool

	Srcs []string
	Dsts []string

	No_full_install *bool

	NamespaceExportedToMake bool

	Visibility []string
}

// Split relative_install_path to a separate struct, because it is not supported for every
// modules listed in [etcInstallPathToFactoryMap]
type prebuiltSubdirProperties struct {
	// If the base file name of the src and dst all match, dsts property does not need to be
	// set, and only relative_install_path can be set.
	Relative_install_path *string
}

var (
	etcInstallPathToFactoryList = map[string]android.ModuleFactory{
		"":                etc.PrebuiltRootFactory,
		"avb":             etc.PrebuiltAvbFactory,
		"bin":             etc.PrebuiltBinaryFactory,
		"bt_firmware":     etc.PrebuiltBtFirmwareFactory,
		"cacerts":         etc.PrebuiltEtcCaCertsFactory,
		"dsp":             etc.PrebuiltDSPFactory,
		"etc":             etc.PrebuiltEtcFactory,
		"etc/dsp":         etc.PrebuiltDSPFactory,
		"etc/firmware":    etc.PrebuiltFirmwareFactory,
		"firmware":        etc.PrebuiltFirmwareFactory,
		"fonts":           etc.PrebuiltFontFactory,
		"framework":       etc.PrebuiltFrameworkFactory,
		"lib":             etc.PrebuiltRenderScriptBitcodeFactory,
		"lib64":           etc.PrebuiltRenderScriptBitcodeFactory,
		"lib/rfsa":        etc.PrebuiltRFSAFactory,
		"media":           etc.PrebuiltMediaFactory,
		"odm":             etc.PrebuiltOdmFactory,
		"optee":           etc.PrebuiltOpteeFactory,
		"overlay":         etc.PrebuiltOverlayFactory,
		"priv-app":        etc.PrebuiltPrivAppFactory,
		"res":             etc.PrebuiltResFactory,
		"rfs":             etc.PrebuiltRfsFactory,
		"tts":             etc.PrebuiltVoicepackFactory,
		"tvservice":       etc.PrebuiltTvServiceFactory,
		"usr/share":       etc.PrebuiltUserShareFactory,
		"usr/hyphen-data": etc.PrebuiltUserHyphenDataFactory,
		"usr/keylayout":   etc.PrebuiltUserKeyLayoutFactory,
		"usr/keychars":    etc.PrebuiltUserKeyCharsFactory,
		"usr/srec":        etc.PrebuiltUserSrecFactory,
		"usr/idc":         etc.PrebuiltUserIdcFactory,
		"vendor_dlkm":     etc.PrebuiltVendorDlkmFactory,
		"wallpaper":       etc.PrebuiltWallpaperFactory,
		"wlc_upt":         etc.PrebuiltWlcUptFactory,
	}
)

func createPrebuiltEtcModule(ctx android.LoadHookContext, partition, srcDir, destDir string, destFiles []srcBaseFileInstallBaseFileTuple) string {
	moduleProps := &prebuiltModuleProperties{}
	propsList := []interface{}{moduleProps}

	// generated module name follows the pattern:
	// <install partition>-<src file path>-<relative install path from partition root>-<install file extension>
	// Note that all path separators are replaced with "_" in the name
	moduleName := partition
	if !android.InList(srcDir, []string{"", "."}) {
		moduleName += fmt.Sprintf("-%s", strings.ReplaceAll(srcDir, string(filepath.Separator), "_"))
	}
	if !android.InList(destDir, []string{"", "."}) {
		moduleName += fmt.Sprintf("-%s", strings.ReplaceAll(destDir, string(filepath.Separator), "_"))
	}
	if len(destFiles) > 0 {
		if ext := filepath.Ext(destFiles[0].srcBaseFile); ext != "" {
			moduleName += fmt.Sprintf("-%s", strings.TrimPrefix(ext, "."))
		}
	}
	moduleProps.Name = proptools.StringPtr(moduleName)

	allCopyFileNamesUnchanged := true
	var srcBaseFiles, installBaseFiles []string
	for _, tuple := range destFiles {
		if tuple.srcBaseFile != tuple.installBaseFile {
			allCopyFileNamesUnchanged = false
		}
		srcBaseFiles = append(srcBaseFiles, tuple.srcBaseFile)
		installBaseFiles = append(installBaseFiles, tuple.installBaseFile)
	}

	// Find out the most appropriate module type to generate
	var etcInstallPathKey string
	for _, etcInstallPath := range android.SortedKeys(etcInstallPathToFactoryList) {
		// Do not break when found but iterate until the end to find a module with more
		// specific install path
		if strings.HasPrefix(destDir, etcInstallPath) {
			etcInstallPathKey = etcInstallPath
		}
	}
	destDir, _ = filepath.Rel(etcInstallPathKey, destDir)

	// Set partition specific properties
	switch partition {
	case "system_ext":
		moduleProps.System_ext_specific = proptools.BoolPtr(true)
	case "product":
		moduleProps.Product_specific = proptools.BoolPtr(true)
	case "vendor":
		moduleProps.Soc_specific = proptools.BoolPtr(true)
	}

	// Set appropriate srcs, dsts, and releative_install_path based on
	// the source and install file names
	if allCopyFileNamesUnchanged {
		moduleProps.Srcs = srcBaseFiles

		// Specify relative_install_path if it is not installed in the root directory of the
		// partition
		if !android.InList(destDir, []string{"", "."}) {
			propsList = append(propsList, &prebuiltSubdirProperties{
				Relative_install_path: proptools.StringPtr(destDir),
			})
		}
	} else {
		moduleProps.Srcs = srcBaseFiles
		dsts := []string{}
		for _, installBaseFile := range installBaseFiles {
			dsts = append(dsts, filepath.Join(destDir, installBaseFile))
		}
		moduleProps.Dsts = dsts
	}

	moduleProps.No_full_install = proptools.BoolPtr(true)
	moduleProps.NamespaceExportedToMake = true
	moduleProps.Visibility = []string{"//visibility:public"}

	ctx.CreateModuleInDirectory(etcInstallPathToFactoryList[etcInstallPathKey], srcDir, propsList...)

	return moduleName
}

func createPrebuiltEtcModulesForPartition(ctx android.LoadHookContext, partition, srcDir string, destDirFilesMap map[string][]srcBaseFileInstallBaseFileTuple) (ret []string) {
	for _, destDir := range android.SortedKeys(destDirFilesMap) {
		ret = append(ret, createPrebuiltEtcModule(ctx, partition, srcDir, destDir, destDirFilesMap[destDir]))
	}
	return ret
}

// Creates prebuilt_* modules based on the install paths and returns the list of generated
// module names
func createPrebuiltEtcModules(ctx android.LoadHookContext) (ret []string) {
	groupedSources := processProductCopyFiles(ctx)
	for _, srcDir := range android.SortedKeys(groupedSources) {
		groupedSource := groupedSources[srcDir]
		ret = append(ret, createPrebuiltEtcModulesForPartition(ctx, "system", srcDir, groupedSource.system)...)
		ret = append(ret, createPrebuiltEtcModulesForPartition(ctx, "system_ext", srcDir, groupedSource.system_ext)...)
		ret = append(ret, createPrebuiltEtcModulesForPartition(ctx, "product", srcDir, groupedSource.product)...)
		ret = append(ret, createPrebuiltEtcModulesForPartition(ctx, "vendor", srcDir, groupedSource.vendor)...)
	}

	return ret
}
