// Copyright 2019 Google Inc. All rights reserved.
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

package sdk

import (
	"testing"
)

// Ensure that module_exports generates a module_exports_snapshot module.
func TestModuleExportsSnapshot(t *testing.T) {
	t.Parallel()
	packageBp := `
		module_exports {
			name: "myexports",
			java_libs: [
				"myjavalib",
			],
		}

		java_library {
			name: "myjavalib",
			srcs: ["Test.java"],
			system_modules: "none",
			sdk_version: "none",
		}
	`

	result := testSdkWithFs(t, ``,
		map[string][]byte{
			"package/Test.java":  nil,
			"package/Android.bp": []byte(packageBp),
		})

	CheckSnapshot(t, result, "myexports", "package",
		checkAndroidBpContents(`
// This is auto-generated. DO NOT EDIT.

apex_contributions_defaults {
    name: "myexports.contributions",
    contents: ["prebuilt_myjavalib"],
}

java_import {
    name: "myjavalib",
    prefer: false,
    visibility: ["//visibility:public"],
    apex_available: ["//apex_available:platform"],
    jars: ["java/myjavalib.jar"],
}
`),
	)
}
