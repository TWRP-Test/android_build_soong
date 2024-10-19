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

package fsgen

import (
	"android/soong/android"
	"android/soong/filesystem"
	"android/soong/java"
	"testing"

	"github.com/google/blueprint/proptools"
)

var prepareForTestWithFsgenBuildComponents = android.FixtureRegisterWithContext(registerBuildComponents)

func TestFileSystemCreatorSystemImageProps(t *testing.T) {
	result := android.GroupFixturePreparers(
		android.PrepareForIntegrationTestWithAndroid,
		android.PrepareForTestWithAndroidBuildComponents,
		android.PrepareForTestWithAllowMissingDependencies,
		filesystem.PrepareForTestWithFilesystemBuildComponents,
		prepareForTestWithFsgenBuildComponents,
		android.FixtureModifyConfig(func(config android.Config) {
			config.TestProductVariables.PartitionVarsForSoongMigrationOnlyDoNotUse.BoardAvbEnable = true
			config.TestProductVariables.PartitionVarsForSoongMigrationOnlyDoNotUse.PartitionQualifiedVariables =
				map[string]android.PartitionQualifiedVariablesType{
					"system": {
						BoardAvbKeyPath:       "external/avb/test/data/testkey_rsa4096.pem",
						BoardAvbAlgorithm:     "SHA256_RSA4096",
						BoardAvbRollbackIndex: "0",
						BoardFileSystemType:   "ext4",
					},
				}
		}),
		android.FixtureMergeMockFs(android.MockFS{
			"external/avb/test/data/testkey_rsa4096.pem": nil,
			"build/soong/fsgen/Android.bp": []byte(`
			soong_filesystem_creator {
				name: "foo",
			}
			`),
		}),
	).RunTest(t)

	fooSystem := result.ModuleForTests("test_product_generated_system_image", "android_common").Module().(interface {
		FsProps() filesystem.FilesystemProperties
	})
	android.AssertBoolEquals(
		t,
		"Property expected to match the product variable 'BOARD_AVB_ENABLE'",
		true,
		proptools.Bool(fooSystem.FsProps().Use_avb),
	)
	android.AssertStringEquals(
		t,
		"Property expected to match the product variable 'BOARD_AVB_KEY_PATH'",
		"external/avb/test/data/testkey_rsa4096.pem",
		proptools.String(fooSystem.FsProps().Avb_private_key),
	)
	android.AssertStringEquals(
		t,
		"Property expected to match the product variable 'BOARD_AVB_ALGORITHM'",
		"SHA256_RSA4096",
		proptools.String(fooSystem.FsProps().Avb_algorithm),
	)
	android.AssertIntEquals(
		t,
		"Property expected to match the product variable 'BOARD_AVB_SYSTEM_ROLLBACK_INDEX'",
		0,
		proptools.Int(fooSystem.FsProps().Rollback_index),
	)
	android.AssertStringEquals(
		t,
		"Property expected to match the product variable 'BOARD_SYSTEMIMAGE_FILE_SYSTEM_TYPE'",
		"ext4",
		proptools.String(fooSystem.FsProps().Type),
	)
}

func TestFileSystemCreatorSetPartitionDeps(t *testing.T) {
	result := android.GroupFixturePreparers(
		android.PrepareForIntegrationTestWithAndroid,
		android.PrepareForTestWithAndroidBuildComponents,
		android.PrepareForTestWithAllowMissingDependencies,
		filesystem.PrepareForTestWithFilesystemBuildComponents,
		prepareForTestWithFsgenBuildComponents,
		java.PrepareForTestWithJavaBuildComponents,
		java.PrepareForTestWithJavaDefaultModules,
		android.FixtureModifyConfig(func(config android.Config) {
			config.TestProductVariables.PartitionVarsForSoongMigrationOnlyDoNotUse.ProductPackages = []string{"bar", "baz"}
			config.TestProductVariables.PartitionVarsForSoongMigrationOnlyDoNotUse.PartitionQualifiedVariables =
				map[string]android.PartitionQualifiedVariablesType{
					"system": {
						BoardFileSystemType: "ext4",
					},
				}
		}),
		android.FixtureMergeMockFs(android.MockFS{
			"external/avb/test/data/testkey_rsa4096.pem": nil,
			"build/soong/fsgen/Android.bp": []byte(`
			soong_filesystem_creator {
				name: "foo",
			}
			`),
		}),
	).RunTestWithBp(t, `
	java_library {
		name: "bar",
		srcs: ["A.java"],
	}
	java_library {
		name: "baz",
		srcs: ["A.java"],
		product_specific: true,
	}
	`)

	android.AssertBoolEquals(
		t,
		"Generated system image expected to depend on system partition installed \"bar\"",
		true,
		java.CheckModuleHasDependency(t, result.TestContext, "test_product_generated_system_image", "android_common", "bar"),
	)
	android.AssertBoolEquals(
		t,
		"Generated system image expected to not depend on product partition installed \"baz\"",
		false,
		java.CheckModuleHasDependency(t, result.TestContext, "test_product_generated_system_image", "android_common", "baz"),
	)
}

func TestFileSystemCreatorDepsWithNamespace(t *testing.T) {
	result := android.GroupFixturePreparers(
		android.PrepareForIntegrationTestWithAndroid,
		android.PrepareForTestWithAndroidBuildComponents,
		android.PrepareForTestWithAllowMissingDependencies,
		android.PrepareForTestWithNamespace,
		android.PrepareForTestWithArchMutator,
		filesystem.PrepareForTestWithFilesystemBuildComponents,
		prepareForTestWithFsgenBuildComponents,
		java.PrepareForTestWithJavaBuildComponents,
		java.PrepareForTestWithJavaDefaultModules,
		android.FixtureModifyConfig(func(config android.Config) {
			config.TestProductVariables.PartitionVarsForSoongMigrationOnlyDoNotUse.ProductPackages = []string{"bar"}
			config.TestProductVariables.NamespacesToExport = []string{"a/b"}
			config.TestProductVariables.PartitionVarsForSoongMigrationOnlyDoNotUse.PartitionQualifiedVariables =
				map[string]android.PartitionQualifiedVariablesType{
					"system": {
						BoardFileSystemType: "ext4",
					},
				}
			config.Targets[android.Android] = []android.Target{
				{Os: android.Android, Arch: android.Arch{ArchType: android.X86_64, ArchVariant: "silvermont", Abi: []string{"arm64-v8a"}}, NativeBridge: android.NativeBridgeDisabled, NativeBridgeHostArchName: "", NativeBridgeRelativePath: "", HostCross: false},
				{Os: android.Android, Arch: android.Arch{ArchType: android.X86, ArchVariant: "silvermont", Abi: []string{"armeabi-v7a"}}, NativeBridge: android.NativeBridgeDisabled, NativeBridgeHostArchName: "", NativeBridgeRelativePath: "", HostCross: false},
				{Os: android.Android, Arch: android.Arch{ArchType: android.Arm64, ArchVariant: "armv8-a", Abi: []string{"arm64-v8a"}}, NativeBridge: android.NativeBridgeEnabled, NativeBridgeHostArchName: "x86_64", NativeBridgeRelativePath: "arm64", HostCross: false},
				{Os: android.Android, Arch: android.Arch{ArchType: android.Arm, ArchVariant: "armv7-a-neon", Abi: []string{"armeabi-v7a"}}, NativeBridge: android.NativeBridgeEnabled, NativeBridgeHostArchName: "x86", NativeBridgeRelativePath: "arm", HostCross: false},
			}
		}),
		android.FixtureMergeMockFs(android.MockFS{
			"external/avb/test/data/testkey_rsa4096.pem": nil,
			"build/soong/fsgen/Android.bp": []byte(`
			soong_filesystem_creator {
				name: "foo",
			}
			`),
			"a/b/Android.bp": []byte(`
			soong_namespace{
			}
			java_library {
				name: "bar",
				srcs: ["A.java"],
				compile_multilib: "64",
			}
			`),
			"c/d/Android.bp": []byte(`
			soong_namespace{
			}
			java_library {
				name: "bar",
				srcs: ["A.java"],
			}
			`),
		}),
	).RunTest(t)

	var packagingProps android.PackagingProperties
	for _, prop := range result.ModuleForTests("test_product_generated_system_image", "android_common").Module().GetProperties() {
		if packagingPropStruct, ok := prop.(*android.PackagingProperties); ok {
			packagingProps = *packagingPropStruct
		}
	}
	moduleDeps := packagingProps.Multilib.Lib64.Deps

	eval := result.ModuleForTests("test_product_generated_system_image", "android_common").Module().ConfigurableEvaluator(android.PanickingConfigAndErrorContext(result.TestContext))
	android.AssertStringListContains(
		t,
		"Generated system image expected to depend on \"bar\" defined in \"a/b\" namespace",
		moduleDeps.GetOrDefault(eval, nil),
		"//a/b:bar",
	)
	android.AssertStringListDoesNotContain(
		t,
		"Generated system image expected to not depend on \"bar\" defined in \"c/d\" namespace",
		moduleDeps.GetOrDefault(eval, nil),
		"//c/d:bar",
	)
}
