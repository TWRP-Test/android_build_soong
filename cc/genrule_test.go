// Copyright 2018 Google Inc. All rights reserved.
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

package cc

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"android/soong/android"
)

func testGenruleContext(config android.Config) *android.TestContext {
	ctx := android.NewTestArchContext(config)
	ctx.RegisterModuleType("cc_genrule", GenRuleFactory)
	ctx.Register()

	return ctx
}

func TestArchGenruleCmd(t *testing.T) {
	fs := map[string][]byte{
		"tool": nil,
		"foo":  nil,
		"bar":  nil,
	}
	bp := `
				cc_genrule {
					name: "gen",
					tool_files: ["tool"],
					cmd: "$(location tool) $(in) $(out)",
					out: ["out_arm"],
					arch: {
						arm: {
							srcs: ["foo"],
						},
						arm64: {
							srcs: ["bar"],
						},
					},
				}
			`
	config := android.TestArchConfig(t.TempDir(), nil, bp, fs)

	ctx := testGenruleContext(config)

	_, errs := ctx.ParseFileList(".", []string{"Android.bp"})
	if errs == nil {
		_, errs = ctx.PrepareBuildActions(config)
	}
	if errs != nil {
		t.Fatal(errs)
	}

	gen := ctx.ModuleForTests(t, "gen", "android_arm_armv7-a-neon").Output("out_arm")
	expected := []string{"foo"}
	if !reflect.DeepEqual(expected, gen.Implicits.Strings()[:len(expected)]) {
		t.Errorf(`want arm inputs %v, got %v`, expected, gen.Implicits.Strings())
	}

	gen = ctx.ModuleForTests(t, "gen", "android_arm64_armv8-a").Output("out_arm")
	expected = []string{"bar"}
	if !reflect.DeepEqual(expected, gen.Implicits.Strings()[:len(expected)]) {
		t.Errorf(`want arm64 inputs %v, got %v`, expected, gen.Implicits.Strings())
	}
}

func TestLibraryGenruleCmd(t *testing.T) {
	bp := `
		cc_library {
			name: "libboth",
		}

		cc_library_shared {
			name: "libshared",
		}

		cc_library_static {
			name: "libstatic",
		}

		cc_genrule {
			name: "gen",
			tool_files: ["tool"],
			srcs: [
				":libboth",
				":libshared",
				":libstatic",
			],
			cmd: "$(location tool) $(in) $(out)",
			out: ["out"],
		}
		`
	ctx := testCc(t, bp)

	gen := ctx.ModuleForTests(t, "gen", "android_arm_armv7-a-neon").Output("out")
	expected := []string{"libboth.so", "libshared.so", "libstatic.a"}
	var got []string
	for _, input := range gen.Implicits {
		got = append(got, input.Base())
	}
	if !reflect.DeepEqual(expected, got[:len(expected)]) {
		t.Errorf(`want inputs %v, got %v`, expected, got)
	}
}

func TestCmdPrefix(t *testing.T) {
	bp := `
		cc_genrule {
			name: "gen",
			cmd: "echo foo",
			out: ["out"],
			native_bridge_supported: true,
		}
		`

	testCases := []struct {
		name     string
		variant  string
		preparer android.FixturePreparer

		arch         string
		nativeBridge string
		multilib     string
	}{
		{
			name:     "arm",
			variant:  "android_arm_armv7-a-neon",
			arch:     "arm",
			multilib: "lib32",
		},
		{
			name:     "arm64",
			variant:  "android_arm64_armv8-a",
			arch:     "arm64",
			multilib: "lib64",
		},
		{
			name:    "nativebridge",
			variant: "android_native_bridge_arm_armv7-a-neon",
			preparer: android.FixtureModifyConfig(func(config android.Config) {
				config.Targets[android.Android] = []android.Target{
					{
						Os:           android.Android,
						Arch:         android.Arch{ArchType: android.X86, ArchVariant: "silvermont", Abi: []string{"armeabi-v7a"}},
						NativeBridge: android.NativeBridgeDisabled,
					},
					{
						Os:                       android.Android,
						Arch:                     android.Arch{ArchType: android.Arm, ArchVariant: "armv7-a-neon", Abi: []string{"armeabi-v7a"}},
						NativeBridge:             android.NativeBridgeEnabled,
						NativeBridgeHostArchName: "x86",
						NativeBridgeRelativePath: "arm",
					},
				}
			}),
			arch:         "arm",
			multilib:     "lib32",
			nativeBridge: "arm",
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			result := android.GroupFixturePreparers(
				PrepareForIntegrationTestWithCc,
				android.OptionalFixturePreparer(tt.preparer),
			).RunTestWithBp(t, bp)
			gen := result.ModuleForTests(t, "gen", tt.variant)
			sboxProto := android.RuleBuilderSboxProtoForTests(t, result.TestContext, gen.Output("genrule.sbox.textproto"))
			cmd := *sboxProto.Commands[0].Command
			android.AssertStringDoesContain(t, "incorrect CC_ARCH", cmd, "CC_ARCH="+tt.arch+" ")
			android.AssertStringDoesContain(t, "incorrect CC_NATIVE_BRIDGE", cmd, "CC_NATIVE_BRIDGE="+tt.nativeBridge+" ")
			android.AssertStringDoesContain(t, "incorrect CC_MULTILIB", cmd, "CC_MULTILIB="+tt.multilib+" ")
		})
	}
}

func TestVendorProductVariantGenrule(t *testing.T) {
	bp := `
	cc_genrule {
		name: "gen",
		tool_files: ["tool"],
		cmd: "$(location tool) $(in) $(out)",
		out: ["out"],
		vendor_available: true,
		product_available: true,
	}
	`
	t.Helper()
	ctx := PrepareForIntegrationTestWithCc.RunTestWithBp(t, bp)

	variants := ctx.ModuleVariantsForTests("gen")
	if !slices.Contains(variants, "android_vendor_arm64_armv8-a") {
		t.Errorf(`expected vendor variant, but does not exist in %v`, variants)
	}
	if !slices.Contains(variants, "android_product_arm64_armv8-a") {
		t.Errorf(`expected product variant, but does not exist in %v`, variants)
	}
}

// cc_genrule is initialized to android.InitAndroidArchModule
// that is an architecture-specific Android module.
// So testing properties tagged with `android:"arch_variant"`
// for cc_genrule.
func TestMultilibGenruleOut(t *testing.T) {
	bp := `
	cc_genrule {
		name: "gen",
		cmd: "cp $(in) $(out)",
		srcs: ["foo"],
		multilib: {
			lib32: {
				out: [
					"subdir32/external-module32",
				],
			},
			lib64: {
				out: [
					"subdir64/external-module64",
				],
			},
		},
	}
	`
	result := PrepareForIntegrationTestWithCc.RunTestWithBp(t, bp)
	gen_32bit := result.ModuleForTests(t, "gen", "android_arm_armv7-a-neon").OutputFiles(result.TestContext, t, "")
	android.AssertPathsEndWith(t,
		"genrule_out",
		[]string{
			"subdir32/external-module32",
		},
		gen_32bit,
	)

	gen_64bit := result.ModuleForTests(t, "gen", "android_arm64_armv8-a").OutputFiles(result.TestContext, t, "")
	android.AssertPathsEndWith(t,
		"genrule_out",
		[]string{
			"subdir64/external-module64",
		},
		gen_64bit,
	)
}

// Test that a genrule can depend on a tool with symlinks. The symlinks are ignored, but
// at least it doesn't cause errors.
func TestGenruleToolWithSymlinks(t *testing.T) {
	bp := `
	genrule {
		name: "gen",
		tools: ["tool_with_symlinks"],
		cmd: "$(location tool_with_symlinks) $(in) $(out)",
		out: ["out"],
	}

	cc_binary_host {
		name: "tool_with_symlinks",
		symlinks: ["symlink1", "symlink2"],
	}
	`
	ctx := PrepareForIntegrationTestWithCc.
		ExtendWithErrorHandler(android.FixtureExpectsNoErrors).
		RunTestWithBp(t, bp)
	gen := ctx.ModuleForTests(t, "gen", "").Output("out")
	toolFound := false
	symlinkFound := false
	for _, dep := range gen.RuleParams.CommandDeps {
		if strings.HasSuffix(dep, "/tool_with_symlinks") {
			toolFound = true
		}
		if strings.HasSuffix(dep, "/symlink1") || strings.HasSuffix(dep, "/symlink2") {
			symlinkFound = true
		}
	}
	if !toolFound {
		t.Errorf("Tool not found")
	}
	// We may want to change genrules to include symlinks later
	if symlinkFound {
		t.Errorf("Symlinks found")
	}
}
