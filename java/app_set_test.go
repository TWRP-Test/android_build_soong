// Copyright 2020 Google Inc. All rights reserved.
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

package java

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"android/soong/android"
)

func TestAndroidAppSet(t *testing.T) {
	t.Parallel()
	result := PrepareForTestWithJavaDefaultModules.RunTestWithBp(t, `
		android_app_set {
			name: "foo",
			set: "prebuilts/apks/app.apks",
			prerelease: true,
		}`)
	module := result.ModuleForTests(t, "foo", "android_common")
	const packedSplitApks = "foo.zip"
	params := module.Output(packedSplitApks)
	if params.Rule == nil {
		t.Errorf("expected output %s is missing", packedSplitApks)
	}
	if s := params.Args["allow-prereleased"]; s != "true" {
		t.Errorf("wrong allow-prereleased value: '%s', expected 'true'", s)
	}
	if s := params.Args["partition"]; s != "system" {
		t.Errorf("wrong partition value: '%s', expected 'system'", s)
	}

	android.AssertPathRelativeToTopEquals(t, "incorrect output path",
		"out/soong/.intermediates/foo/android_common/foo.apk", params.Output)

	android.AssertPathsRelativeToTopEquals(t, "incorrect implicit output paths",
		[]string{
			"out/soong/.intermediates/foo/android_common/foo.zip",
			"out/soong/.intermediates/foo/android_common/apkcerts.txt",
		},
		params.ImplicitOutputs.Paths())

	mkEntries := android.AndroidMkEntriesForTest(t, result.TestContext, module.Module())[0]
	actualInstallFile := mkEntries.EntryMap["LOCAL_APK_SET_INSTALL_FILE"]
	expectedInstallFile := []string{
		strings.Replace(params.ImplicitOutputs[0].String(), android.TestOutSoongDir, result.Config.SoongOutDir(), 1),
	}
	if !reflect.DeepEqual(actualInstallFile, expectedInstallFile) {
		t.Errorf("Unexpected LOCAL_APK_SET_INSTALL_FILE value: '%s', expected: '%s',",
			actualInstallFile, expectedInstallFile)
	}
}

func TestAndroidAppSet_Variants(t *testing.T) {
	t.Parallel()
	bp := `
		android_app_set {
			name: "foo",
			set: "prebuilts/apks/app.apks",
		}`
	testCases := []struct {
		name            string
		targets         []android.Target
		aaptPrebuiltDPI []string
		sdkVersion      int
		expected        map[string]string
	}{
		{
			name: "One",
			targets: []android.Target{
				{Os: android.Android, Arch: android.Arch{ArchType: android.X86}},
			},
			aaptPrebuiltDPI: []string{"ldpi", "xxhdpi"},
			sdkVersion:      29,
			expected: map[string]string{
				"abis":              "X86",
				"allow-prereleased": "false",
				"screen-densities":  "LDPI,XXHDPI",
				"sdk-version":       "29",
				"skip-sdk-check":    "false",
				"stem":              "foo",
			},
		},
		{
			name: "Two",
			targets: []android.Target{
				{Os: android.Android, Arch: android.Arch{ArchType: android.X86_64}},
				{Os: android.Android, Arch: android.Arch{ArchType: android.X86}},
			},
			aaptPrebuiltDPI: nil,
			sdkVersion:      30,
			expected: map[string]string{
				"abis":              "X86_64,X86",
				"allow-prereleased": "false",
				"screen-densities":  "all",
				"sdk-version":       "30",
				"skip-sdk-check":    "false",
				"stem":              "foo",
			},
		},
	}

	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			ctx := android.GroupFixturePreparers(
				PrepareForTestWithJavaDefaultModules,
				android.FixtureModifyProductVariables(func(variables android.FixtureProductVariables) {
					variables.AAPTPrebuiltDPI = test.aaptPrebuiltDPI
					variables.Platform_sdk_version = &test.sdkVersion
				}),
				android.FixtureModifyConfig(func(config android.Config) {
					config.Targets[android.Android] = test.targets
				}),
			).RunTestWithBp(t, bp)

			module := ctx.ModuleForTests(t, "foo", "android_common")
			const packedSplitApks = "foo.zip"
			params := module.Output(packedSplitApks)
			for k, v := range test.expected {
				android.AssertStringEquals(t, fmt.Sprintf("arg value for `%s`", k), v, params.Args[k])
			}
		})
	}
}
