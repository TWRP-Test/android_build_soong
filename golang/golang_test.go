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

package golang

import (
	"android/soong/android"
	"regexp"
	"testing"

	"github.com/google/blueprint/bootstrap"
)

func TestGolang(t *testing.T) {
	bp := `
		bootstrap_go_package {
			name: "gopkg",
			pkgPath: "test/pkg",
		}

		blueprint_go_binary {
			name: "gobin",
			deps: ["gopkg"],
		}
	`

	result := android.GroupFixturePreparers(
		android.PrepareForTestWithArchMutator,
		android.FixtureRegisterWithContext(func(ctx android.RegistrationContext) {
			RegisterGoModuleTypes(ctx)
			ctx.PreDepsMutators(func(ctx android.RegisterMutatorsContext) {
				ctx.BottomUpBlueprint("bootstrap_deps", bootstrap.BootstrapDeps).UsesReverseDependencies()
			})
		}),
	).RunTestWithBp(t, bp)

	bin := result.ModuleForTests(t, "gobin", result.Config.BuildOSTarget.String())

	expected := "^out/host/" + result.Config.PrebuiltOS() + "/bin/go/gobin/?[^/]*/obj/gobin$"
	actual := android.PathsRelativeToTop(bin.OutputFiles(result.TestContext, t, ""))
	if len(actual) != 1 {
		t.Fatalf("Expected 1 output file, got %v", actual)
	}
	if match, err := regexp.Match(expected, []byte(actual[0])); err != nil || !match {
		t.Fatalf("Expected output file to match %q, but got %q", expected, actual[0])
	}
}
