// Copyright 2021 Google Inc. All rights reserved.
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
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"android/soong/android"
)

func TestJavaSdkLibrary(t *testing.T) {
	t.Parallel()
	result := android.GroupFixturePreparers(
		prepareForJavaTest,
		PrepareForTestWithJavaSdkLibraryFiles,
		FixtureWithPrebuiltApis(map[string][]string{
			"28": {"foo"},
			"29": {"foo"},
			"30": {"bar", "barney", "baz", "betty", "foo", "fred", "quuz", "wilma"},
		}),
		android.PrepareForTestWithBuildFlag("RELEASE_HIDDEN_API_EXPORTABLE_STUBS", "true"),
	).RunTestWithBp(t, `
		droiddoc_exported_dir {
			name: "droiddoc-templates-sdk",
			path: ".",
		}
		java_sdk_library {
			name: "foo",
			srcs: ["a.java", "b.java"],
			api_packages: ["foo"],
		}
		java_sdk_library {
			name: "bar",
			srcs: ["a.java", "b.java"],
			api_packages: ["bar"],
			exclude_kotlinc_generated_files: true,
		}
		java_library {
			name: "baz",
			srcs: ["c.java"],
			libs: ["foo.stubs.system", "bar.stubs"],
			sdk_version: "system_current",
		}
		java_sdk_library {
			name: "barney",
			srcs: ["c.java"],
			api_only: true,
		}
		java_sdk_library {
			name: "betty",
			srcs: ["c.java"],
			shared_library: false,
		}
		java_sdk_library_import {
		    name: "quuz",
				public: {
					jars: ["c.jar"],
					current_api: "api/current.txt",
					removed_api: "api/removed.txt",
				},
		}
		java_sdk_library_import {
		    name: "fred",
				public: {
					jars: ["b.jar"],
				},
		}
		java_sdk_library_import {
		    name: "wilma",
				public: {
					jars: ["b.jar"],
				},
				shared_library: false,
		}
		java_library {
		    name: "qux",
		    srcs: ["c.java"],
		    libs: ["baz", "fred.stubs", "quuz.stubs", "wilma.stubs", "barney.stubs.system", "betty.stubs.system"],
		    sdk_version: "system_current",
		}
		java_library {
			name: "baz-test",
			srcs: ["c.java"],
			libs: ["foo.stubs.test"],
			sdk_version: "test_current",
		}
		java_library {
			name: "baz-29",
			srcs: ["c.java"],
			libs: ["sdk_system_29_foo"],
			sdk_version: "system_29",
		}
		java_library {
			name: "baz-module-30",
			srcs: ["c.java"],
			libs: ["sdk_module-lib_30_foo"],
			sdk_version: "module_30",
		}
	`)

	// check the existence of the internal modules
	foo := result.ModuleForTests(t, "foo", "android_common")
	result.ModuleForTests(t, apiScopePublic.stubsLibraryModuleName("foo"), "android_common")
	result.ModuleForTests(t, apiScopeSystem.stubsLibraryModuleName("foo"), "android_common")
	result.ModuleForTests(t, apiScopeTest.stubsLibraryModuleName("foo"), "android_common")
	result.ModuleForTests(t, apiScopePublic.stubsSourceModuleName("foo"), "android_common")
	result.ModuleForTests(t, apiScopeSystem.stubsSourceModuleName("foo"), "android_common")
	result.ModuleForTests(t, apiScopeTest.stubsSourceModuleName("foo"), "android_common")
	result.ModuleForTests(t, apiScopePublic.stubsSourceModuleName("foo")+".api.contribution", "")
	result.ModuleForTests(t, apiScopePublic.apiLibraryModuleName("foo"), "android_common")
	result.ModuleForTests(t, "foo"+sdkXmlFileSuffix, "android_common")
	result.ModuleForTests(t, "foo.api.public.28", "")
	result.ModuleForTests(t, "foo.api.system.28", "")
	result.ModuleForTests(t, "foo.api.test.28", "")

	exportedComponentsInfo, _ := android.OtherModuleProvider(result, foo.Module(), android.ExportedComponentsInfoProvider)
	expectedFooExportedComponents := []string{
		"foo-removed.api.combined.public.latest",
		"foo-removed.api.combined.system.latest",
		"foo.api.combined.public.latest",
		"foo.api.combined.system.latest",
		"foo.stubs",
		"foo.stubs.exportable",
		"foo.stubs.exportable.system",
		"foo.stubs.exportable.test",
		"foo.stubs.source",
		"foo.stubs.source.system",
		"foo.stubs.source.test",
		"foo.stubs.system",
		"foo.stubs.test",
	}
	android.AssertArrayString(t, "foo exported components", expectedFooExportedComponents, exportedComponentsInfo.Components)

	bazJavac := result.ModuleForTests(t, "baz", "android_common").Rule("javac")
	// tests if baz is actually linked to the stubs lib
	android.AssertStringDoesContain(t, "baz javac classpath", bazJavac.Args["classpath"], "foo.stubs.system.from-text.jar")
	// ... and not to the impl lib
	android.AssertStringDoesNotContain(t, "baz javac classpath", bazJavac.Args["classpath"], "foo.jar")
	// test if baz is not linked to the system variant of foo
	android.AssertStringDoesNotContain(t, "baz javac classpath", bazJavac.Args["classpath"], "foo.stubs.jar")

	bazTestJavac := result.ModuleForTests(t, "baz-test", "android_common").Rule("javac")
	// tests if baz-test is actually linked to the test stubs lib
	android.AssertStringDoesContain(t, "baz-test javac classpath", bazTestJavac.Args["classpath"], "foo.stubs.test.from-text.jar")

	baz29Javac := result.ModuleForTests(t, "baz-29", "android_common").Rule("javac")
	// tests if baz-29 is actually linked to the system 29 stubs lib
	android.AssertStringDoesContain(t, "baz-29 javac classpath", baz29Javac.Args["classpath"], "prebuilts/sdk/sdk_system_29_foo/android_common/local-combined/sdk_system_29_foo.jar")

	bazModule30Javac := result.ModuleForTests(t, "baz-module-30", "android_common").Rule("javac")
	// tests if "baz-module-30" is actually linked to the module 30 stubs lib
	android.AssertStringDoesContain(t, "baz-module-30 javac classpath", bazModule30Javac.Args["classpath"], "prebuilts/sdk/sdk_module-lib_30_foo/android_common/local-combined/sdk_module-lib_30_foo.jar")

	// test if baz has exported SDK lib names foo and bar to qux
	qux := result.ModuleForTests(t, "qux", "android_common")
	if quxLib, ok := qux.Module().(*Library); ok {
		requiredSdkLibs, optionalSdkLibs := quxLib.ClassLoaderContexts().UsesLibs()
		android.AssertDeepEquals(t, "qux exports (required)", []string{"fred", "quuz", "foo", "bar"}, requiredSdkLibs)
		android.AssertDeepEquals(t, "qux exports (optional)", []string{}, optionalSdkLibs)
	}

	// test if quuz have created the api_contribution module
	result.ModuleForTests(t, apiScopePublic.stubsSourceModuleName("quuz")+".api.contribution", "")

	fooImplDexJar := result.ModuleForTests(t, "foo.impl", "android_common").Rule("d8")
	// tests if kotlinc generated files are NOT excluded from output of foo.impl.
	android.AssertStringDoesNotContain(t, "foo.impl dex", fooImplDexJar.BuildParams.Args["mergeZipsFlags"], "-stripFile META-INF/*.kotlin_module")

	barImplDexJar := result.ModuleForTests(t, "bar.impl", "android_common").Rule("d8")
	// tests if kotlinc generated files are excluded from output of bar.impl.
	android.AssertStringDoesContain(t, "bar.impl dex", barImplDexJar.BuildParams.Args["mergeZipsFlags"], "-stripFile META-INF/*.kotlin_module")
}

func TestJavaSdkLibrary_UpdatableLibrary(t *testing.T) {
	t.Parallel()
	result := android.GroupFixturePreparers(
		prepareForJavaTest,
		PrepareForTestWithJavaSdkLibraryFiles,
		FixtureWithPrebuiltApis(map[string][]string{
			"28": {"foo"},
			"29": {"foo"},
			"30": {"foo", "fooUpdatable", "fooUpdatableErr"},
		}),
		android.FixtureModifyProductVariables(func(variables android.FixtureProductVariables) {
			variables.Platform_version_active_codenames = []string{"Tiramisu", "U", "V", "W", "X"}
		}),
	).RunTestWithBp(t,
		`
		java_sdk_library {
			name: "fooUpdatable",
			srcs: ["a.java", "b.java"],
			api_packages: ["foo"],
			on_bootclasspath_since: "U",
			on_bootclasspath_before: "V",
			min_device_sdk: "W",
			max_device_sdk: "X",
			min_sdk_version: "S",
		}
		java_sdk_library {
			name: "foo",
			srcs: ["a.java", "b.java"],
			api_packages: ["foo"],
		}
`)

	// test that updatability attributes are passed on correctly
	fooUpdatable := result.ModuleForTests(t, "fooUpdatable.xml", "android_common").Output("fooUpdatable.xml")
	fooUpdatableContents := android.ContentFromFileRuleForTests(t, result.TestContext, fooUpdatable)
	android.AssertStringDoesContain(t, "fooUpdatable.xml contents", fooUpdatableContents, `on-bootclasspath-since="U"`)
	android.AssertStringDoesContain(t, "fooUpdatable.xml contents", fooUpdatableContents, `on-bootclasspath-before="V"`)
	android.AssertStringDoesContain(t, "fooUpdatable.xml contents", fooUpdatableContents, `min-device-sdk="W"`)
	android.AssertStringDoesContain(t, "fooUpdatable.xml contents", fooUpdatableContents, `max-device-sdk="X"`)

	// double check that updatability attributes are not written if they don't exist in the bp file
	// the permissions file for the foo library defined above
	fooPermissions := result.ModuleForTests(t, "foo.xml", "android_common").Output("foo.xml")
	fooPermissionsContents := android.ContentFromFileRuleForTests(t, result.TestContext, fooPermissions)
	android.AssertStringDoesNotContain(t, "foo.xml contents", fooPermissionsContents, `on-bootclasspath-since`)
	android.AssertStringDoesNotContain(t, "foo.xml contents", fooPermissionsContents, `on-bootclasspath-before`)
	android.AssertStringDoesNotContain(t, "foo.xml contents", fooPermissionsContents, `min-device-sdk`)
	android.AssertStringDoesNotContain(t, "foo.xml contents", fooPermissionsContents, `max-device-sdk`)
}

func TestJavaSdkLibrary_UpdatableLibrary_Validation_ValidVersion(t *testing.T) {
	t.Parallel()
	android.GroupFixturePreparers(
		prepareForJavaTest,
		PrepareForTestWithJavaSdkLibraryFiles,
		FixtureWithPrebuiltApis(map[string][]string{
			"30": {"fooUpdatable", "fooUpdatableErr"},
		}),
	).ExtendWithErrorHandler(android.FixtureExpectsAllErrorsToMatchAPattern(
		[]string{
			`on_bootclasspath_since: "aaa" could not be parsed as an integer and is not a recognized codename`,
			`on_bootclasspath_before: "bbc" could not be parsed as an integer and is not a recognized codename`,
			`min_device_sdk: "ccc" could not be parsed as an integer and is not a recognized codename`,
			`max_device_sdk: "current" is not an allowed value for this attribute`,
		})).RunTestWithBp(t,
		`
	java_sdk_library {
			name: "fooUpdatableErr",
			srcs: ["a.java", "b.java"],
			api_packages: ["foo"],
			on_bootclasspath_since: "aaa",
			on_bootclasspath_before: "bbc",
			min_device_sdk: "ccc",
			max_device_sdk: "current",
		}
`)
}

func TestJavaSdkLibrary_UpdatableLibrary_Validation_AtLeastTAttributes(t *testing.T) {
	t.Parallel()
	android.GroupFixturePreparers(
		prepareForJavaTest,
		PrepareForTestWithJavaSdkLibraryFiles,
		FixtureWithPrebuiltApis(map[string][]string{
			"28": {"foo"},
		}),
	).ExtendWithErrorHandler(android.FixtureExpectsAllErrorsToMatchAPattern(
		[]string{
			"on_bootclasspath_since: Attribute value needs to be at least T",
			"on_bootclasspath_before: Attribute value needs to be at least T",
			"min_device_sdk: Attribute value needs to be at least T",
			"max_device_sdk: Attribute value needs to be at least T",
		},
	)).RunTestWithBp(t,
		`
		java_sdk_library {
			name: "foo",
			srcs: ["a.java", "b.java"],
			api_packages: ["foo"],
			on_bootclasspath_since: "S",
			on_bootclasspath_before: "S",
			min_device_sdk: "S",
			max_device_sdk: "S",
			min_sdk_version: "S",
		}
`)
}

func TestJavaSdkLibrary_UpdatableLibrary_Validation_MinAndMaxDeviceSdk(t *testing.T) {
	t.Parallel()
	android.GroupFixturePreparers(
		prepareForJavaTest,
		PrepareForTestWithJavaSdkLibraryFiles,
		FixtureWithPrebuiltApis(map[string][]string{
			"28": {"foo"},
		}),
		android.FixtureModifyProductVariables(func(variables android.FixtureProductVariables) {
			variables.Platform_version_active_codenames = []string{"Tiramisu", "U", "V"}
		}),
	).ExtendWithErrorHandler(android.FixtureExpectsAllErrorsToMatchAPattern(
		[]string{
			"min_device_sdk can't be greater than max_device_sdk",
		},
	)).RunTestWithBp(t,
		`
		java_sdk_library {
			name: "foo",
			srcs: ["a.java", "b.java"],
			api_packages: ["foo"],
			min_device_sdk: "V",
			max_device_sdk: "U",
			min_sdk_version: "S",
		}
`)
}

func TestJavaSdkLibrary_UpdatableLibrary_Validation_MinAndMaxDeviceSdkAndModuleMinSdk(t *testing.T) {
	t.Parallel()
	android.GroupFixturePreparers(
		prepareForJavaTest,
		PrepareForTestWithJavaSdkLibraryFiles,
		FixtureWithPrebuiltApis(map[string][]string{
			"28": {"foo"},
		}),
		android.FixtureModifyProductVariables(func(variables android.FixtureProductVariables) {
			variables.Platform_version_active_codenames = []string{"Tiramisu", "U", "V"}
		}),
	).ExtendWithErrorHandler(android.FixtureExpectsAllErrorsToMatchAPattern(
		[]string{
			regexp.QuoteMeta("min_device_sdk: Can't be less than module's min sdk (V)"),
			regexp.QuoteMeta("max_device_sdk: Can't be less than module's min sdk (V)"),
		},
	)).RunTestWithBp(t,
		`
		java_sdk_library {
			name: "foo",
			srcs: ["a.java", "b.java"],
			api_packages: ["foo"],
			min_device_sdk: "U",
			max_device_sdk: "U",
			min_sdk_version: "V",
		}
`)
}

func TestJavaSdkLibrary_UpdatableLibrary_usesNewTag(t *testing.T) {
	t.Parallel()
	result := android.GroupFixturePreparers(
		prepareForJavaTest,
		PrepareForTestWithJavaSdkLibraryFiles,
		FixtureWithPrebuiltApis(map[string][]string{
			"30": {"foo"},
		}),
	).RunTestWithBp(t,
		`
		java_sdk_library {
			name: "foo",
			srcs: ["a.java", "b.java"],
			min_device_sdk: "Tiramisu",
			min_sdk_version: "S",
		}
`)
	// test that updatability attributes are passed on correctly
	fooUpdatable := result.ModuleForTests(t, "foo.xml", "android_common").Output("foo.xml")
	fooUpdatableContents := android.ContentFromFileRuleForTests(t, result.TestContext, fooUpdatable)
	android.AssertStringDoesContain(t, "foo.xml contents", fooUpdatableContents, `<apex-library`)
	android.AssertStringDoesNotContain(t, "foo.xml contents", fooUpdatableContents, `<library`)
}

func TestJavaSdkLibrary_StubOrImplOnlyLibs(t *testing.T) {
	t.Parallel()
	result := android.GroupFixturePreparers(
		prepareForJavaTest,
		PrepareForTestWithJavaSdkLibraryFiles,
		FixtureWithLastReleaseApis("sdklib"),
	).RunTestWithBp(t, `
		java_sdk_library {
			name: "sdklib",
			srcs: ["a.java"],
			libs: ["lib"],
			static_libs: ["static-lib"],
			impl_only_libs: ["impl-only-lib"],
			stub_only_libs: ["stub-only-lib"],
			stub_only_static_libs: ["stub-only-static-lib"],
		}
		java_defaults {
			name: "defaults",
			srcs: ["a.java"],
			sdk_version: "current",
		}
		java_library { name: "lib", defaults: ["defaults"] }
		java_library { name: "static-lib", defaults: ["defaults"] }
		java_library { name: "impl-only-lib", defaults: ["defaults"] }
		java_library { name: "stub-only-lib", defaults: ["defaults"] }
		java_library { name: "stub-only-static-lib", defaults: ["defaults"] }
		`)
	var expectations = []struct {
		lib               string
		on_impl_classpath bool
		on_stub_classpath bool
		in_impl_combined  bool
		in_stub_combined  bool
	}{
		{lib: "lib", on_impl_classpath: true},
		{lib: "static-lib", in_impl_combined: true},
		{lib: "impl-only-lib", on_impl_classpath: true},
		{lib: "stub-only-lib", on_stub_classpath: true},
		{lib: "stub-only-static-lib", in_stub_combined: true},
	}
	verify := func(sdklib, dep string, cp, combined bool) {
		sdklibCp := result.ModuleForTests(t, sdklib, "android_common").Rule("javac").Args["classpath"]
		expected := cp || combined // Every combined jar is also on the classpath.
		android.AssertStringContainsEquals(t, "bad classpath for "+sdklib, sdklibCp, "/"+dep+".jar", expected)

		combineJarInputs := result.ModuleForTests(t, sdklib, "android_common").Rule("combineJar").Inputs.Strings()
		depPath := filepath.Join("out", "soong", ".intermediates", dep, "android_common", "turbine", dep+".jar")
		android.AssertStringListContainsEquals(t, "bad combined inputs for "+sdklib, combineJarInputs, depPath, combined)
	}
	for _, expectation := range expectations {
		verify("sdklib.impl", expectation.lib, expectation.on_impl_classpath, expectation.in_impl_combined)

		stubName := apiScopePublic.sourceStubsLibraryModuleName("sdklib")
		verify(stubName, expectation.lib, expectation.on_stub_classpath, expectation.in_stub_combined)
	}
}

func TestJavaSdkLibrary_DoNotAccessImplWhenItIsNotBuilt(t *testing.T) {
	t.Parallel()
	result := android.GroupFixturePreparers(
		prepareForJavaTest,
		PrepareForTestWithJavaSdkLibraryFiles,
		FixtureWithLastReleaseApis("foo"),
	).RunTestWithBp(t, `
		java_sdk_library {
			name: "foo",
			srcs: ["a.java"],
			api_only: true,
			public: {
				enabled: true,
			},
		}

		java_library {
			name: "bar",
			srcs: ["b.java"],
			libs: ["foo.stubs"],
		}
		`)

	// The bar library should depend on the stubs jar.
	barLibrary := result.ModuleForTests(t, "bar", "android_common").Rule("javac")
	if expected, actual := `^-classpath .*:out/soong/[^:]*/foo\.stubs\.from-text/foo\.stubs\.from-text\.jar$`, barLibrary.Args["classpath"]; !regexp.MustCompile(expected).MatchString(actual) {
		t.Errorf("expected %q, found %#q", expected, actual)
	}
}

func TestJavaSdkLibrary_AccessOutputFiles(t *testing.T) {
	t.Parallel()
	android.GroupFixturePreparers(
		prepareForJavaTest,
		PrepareForTestWithJavaSdkLibraryFiles,
		FixtureWithLastReleaseApis("foo"),
	).RunTestWithBp(t, `
		java_sdk_library {
			name: "foo",
			srcs: ["a.java"],
			api_packages: ["foo"],
			annotations_enabled: true,
			public: {
				enabled: true,
			},
		}
		java_library {
			name: "bar",
			srcs: ["b.java", ":foo{.public.stubs.source}"],
			java_resources: [":foo{.public.annotations.zip}"],
		}
		`)
}

func TestJavaSdkLibrary_AccessOutputFiles_NoAnnotations(t *testing.T) {
	t.Parallel()
	android.GroupFixturePreparers(
		prepareForJavaTest,
		PrepareForTestWithJavaSdkLibraryFiles,
		FixtureWithLastReleaseApis("foo"),
	).
		ExtendWithErrorHandler(android.FixtureExpectsAtLeastOneErrorMatchingPattern(`module "bar" variant "android_common": unsupported output tag ".public.annotations.zip"`)).
		RunTestWithBp(t, `
		java_sdk_library {
			name: "foo",
			srcs: ["a.java"],
			api_packages: ["foo"],
			public: {
				enabled: true,
			},
		}

		java_library {
			name: "bar",
			srcs: ["b.java", ":foo{.public.stubs.source}"],
			java_resources: [":foo{.public.annotations.zip}"],
		}
		`)
}

func TestJavaSdkLibrary_AccessOutputFiles_MissingScope(t *testing.T) {
	t.Parallel()
	android.GroupFixturePreparers(
		prepareForJavaTest,
		PrepareForTestWithJavaSdkLibraryFiles,
		FixtureWithLastReleaseApis("foo"),
	).
		ExtendWithErrorHandler(android.FixtureExpectsAtLeastOneErrorMatchingPattern(`module "bar" variant "android_common": unsupported output tag ".system.stubs.source"`)).
		RunTestWithBp(t, `
		java_sdk_library {
			name: "foo",
			srcs: ["a.java"],
			api_packages: ["foo"],
			public: {
				enabled: true,
			},
		}

		java_library {
			name: "bar",
			srcs: ["b.java", ":foo{.system.stubs.source}"],
		}
		`)
}

func TestJavaSdkLibrary_Deps(t *testing.T) {
	t.Parallel()
	result := android.GroupFixturePreparers(
		prepareForJavaTest,
		PrepareForTestWithJavaSdkLibraryFiles,
		FixtureWithLastReleaseApis("sdklib"),
		android.PrepareForTestWithBuildFlag("RELEASE_HIDDEN_API_EXPORTABLE_STUBS", "true"),
	).RunTestWithBp(t, `
		java_sdk_library {
			name: "sdklib",
			srcs: ["a.java"],
			sdk_version: "none",
			system_modules: "none",
			public: {
				enabled: true,
			},
		}
		`)

	CheckModuleDependencies(t, result.TestContext, "sdklib", "android_common", []string{
		`dex2oatd`,
		`sdklib-removed.api.combined.public.latest`,
		`sdklib.api.combined.public.latest`,
		`sdklib.impl`,
		`sdklib.stubs`,
		`sdklib.stubs.exportable`,
		`sdklib.stubs.source`,
		`sdklib.xml`,
	})
}

func TestJavaSdkLibraryImport_AccessOutputFiles(t *testing.T) {
	t.Parallel()
	prepareForJavaTest.RunTestWithBp(t, `
		java_sdk_library_import {
			name: "foo",
			public: {
				jars: ["a.jar"],
				stub_srcs: ["a.java"],
				current_api: "api/current.txt",
				removed_api: "api/removed.txt",
				annotations: "x/annotations.zip",
			},
		}

		java_library {
			name: "bar",
			srcs: [":foo{.public.stubs.source}"],
			java_resources: [
				":foo{.public.api.txt}",
				":foo{.public.removed-api.txt}",
				":foo{.public.annotations.zip}",
			],
		}
		`)
}

func TestJavaSdkLibraryImport_AccessOutputFiles_Invalid(t *testing.T) {
	t.Parallel()
	bp := `
		java_sdk_library_import {
			name: "foo",
			public: {
				jars: ["a.jar"],
			},
		}
		`

	t.Run("stubs.source", func(t *testing.T) {
		t.Parallel()
		prepareForJavaTest.
			ExtendWithErrorHandler(android.FixtureExpectsAtLeastOneErrorMatchingPattern(`module "foo" is not a SourceFileProducer or having valid output file for tag ".public.stubs.source"`)).
			RunTestWithBp(t, bp+`
				java_library {
					name: "bar",
					srcs: [":foo{.public.stubs.source}"],
					java_resources: [
						":foo{.public.api.txt}",
						":foo{.public.removed-api.txt}",
					],
				}
			`)
	})

	t.Run("api.txt", func(t *testing.T) {
		t.Parallel()
		prepareForJavaTest.
			ExtendWithErrorHandler(android.FixtureExpectsAtLeastOneErrorMatchingPattern(`module "foo" is not a SourceFileProducer or having valid output file for tag ".public.api.txt"`)).
			RunTestWithBp(t, bp+`
				java_library {
					name: "bar",
					srcs: ["a.java"],
					java_resources: [
						":foo{.public.api.txt}",
					],
				}
			`)
	})

	t.Run("removed-api.txt", func(t *testing.T) {
		t.Parallel()
		prepareForJavaTest.
			ExtendWithErrorHandler(android.FixtureExpectsAtLeastOneErrorMatchingPattern(`module "foo" is not a SourceFileProducer or having valid output file for tag ".public.removed-api.txt"`)).
			RunTestWithBp(t, bp+`
				java_library {
					name: "bar",
					srcs: ["a.java"],
					java_resources: [
						":foo{.public.removed-api.txt}",
					],
				}
			`)
	})
}

func TestJavaSdkLibrary_InvalidScopes(t *testing.T) {
	t.Parallel()
	prepareForJavaTest.
		ExtendWithErrorHandler(android.FixtureExpectsAtLeastOneErrorMatchingPattern(`module "foo": enabled api scope "system" depends on disabled scope "public"`)).
		RunTestWithBp(t, `
			java_sdk_library {
				name: "foo",
				srcs: ["a.java", "b.java"],
				api_packages: ["foo"],
				// Explicitly disable public to test the check that ensures the set of enabled
				// scopes is consistent.
				public: {
					enabled: false,
				},
				system: {
					enabled: true,
				},
			}
		`)
}

func TestJavaSdkLibrary_SdkVersion_ForScope(t *testing.T) {
	t.Parallel()
	android.GroupFixturePreparers(
		prepareForJavaTest,
		PrepareForTestWithJavaSdkLibraryFiles,
		FixtureWithLastReleaseApis("foo"),
	).RunTestWithBp(t, `
		java_sdk_library {
			name: "foo",
			srcs: ["a.java", "b.java"],
			api_packages: ["foo"],
			system: {
				enabled: true,
				sdk_version: "module_current",
			},
		}
		`)
}

func TestJavaSdkLibrary_ModuleLib(t *testing.T) {
	t.Parallel()
	android.GroupFixturePreparers(
		prepareForJavaTest,
		PrepareForTestWithJavaSdkLibraryFiles,
		FixtureWithLastReleaseApis("foo"),
	).RunTestWithBp(t, `
		java_sdk_library {
			name: "foo",
			srcs: ["a.java", "b.java"],
			api_packages: ["foo"],
			system: {
				enabled: true,
			},
			module_lib: {
				enabled: true,
			},
		}
		`)
}

func TestJavaSdkLibrary_SystemServer(t *testing.T) {
	t.Parallel()
	android.GroupFixturePreparers(
		prepareForJavaTest,
		PrepareForTestWithJavaSdkLibraryFiles,
		FixtureWithLastReleaseApis("foo"),
	).RunTestWithBp(t, `
		java_sdk_library {
			name: "foo",
			srcs: ["a.java", "b.java"],
			api_packages: ["foo"],
			system: {
				enabled: true,
			},
			system_server: {
				enabled: true,
			},
		}
		`)
}

func TestJavaSdkLibrary_SystemServer_AccessToStubScopeLibs(t *testing.T) {
	t.Parallel()
	result := android.GroupFixturePreparers(
		prepareForJavaTest,
		PrepareForTestWithJavaSdkLibraryFiles,
		FixtureWithLastReleaseApis("foo-public", "foo-system", "foo-module-lib", "foo-system-server"),
	).RunTestWithBp(t, `
		java_sdk_library {
			name: "foo-public",
			srcs: ["a.java"],
			api_packages: ["foo"],
			public: {
				enabled: true,
			},
		}

		java_sdk_library {
			name: "foo-system",
			srcs: ["a.java"],
			api_packages: ["foo"],
			system: {
				enabled: true,
			},
		}

		java_sdk_library {
			name: "foo-module-lib",
			srcs: ["a.java"],
			api_packages: ["foo"],
			system: {
				enabled: true,
			},
			module_lib: {
				enabled: true,
			},
		}

		java_sdk_library {
			name: "foo-system-server",
			srcs: ["a.java"],
			api_packages: ["foo"],
			system_server: {
				enabled: true,
			},
		}

		java_library {
			name: "bar",
			srcs: ["a.java"],
			libs: ["foo-public.stubs", "foo-system.stubs.system", "foo-module-lib.stubs.module_lib", "foo-system-server.stubs.system_server"],
			sdk_version: "system_server_current",
		}
		`)

	stubsPath := func(name string, scope *apiScope) string {
		name = scope.stubsLibraryModuleName(name)
		return fmt.Sprintf("out/soong/.intermediates/%[1]s.from-text/android_common/%[1]s.from-text/%[1]s.from-text.jar", name)
	}

	// The bar library should depend on the highest (where system server is highest and public is
	// lowest) API scopes provided by each of the foo-* modules. The highest API scope provided by the
	// foo-<x> module is <x>.
	barLibrary := result.ModuleForTests(t, "bar", "android_common").Rule("javac")
	stubLibraries := []string{
		stubsPath("foo-public", apiScopePublic),
		stubsPath("foo-system", apiScopeSystem),
		stubsPath("foo-module-lib", apiScopeModuleLib),
		stubsPath("foo-system-server", apiScopeSystemServer),
	}
	expectedPattern := fmt.Sprintf(`^-classpath .*:\Q%s\E$`, strings.Join(stubLibraries, ":"))
	if expected, actual := expectedPattern, barLibrary.Args["classpath"]; !regexp.MustCompile(expected).MatchString(actual) {
		t.Errorf("expected pattern %q to match %#q", expected, actual)
	}
}

func TestJavaSdkLibraryImport(t *testing.T) {
	t.Parallel()
	result := prepareForJavaTest.RunTestWithBp(t, `
		java_library {
			name: "foo",
			srcs: ["a.java"],
			libs: ["sdklib.stubs"],
			sdk_version: "current",
		}

		java_library {
			name: "foo.system",
			srcs: ["a.java"],
			libs: ["sdklib.stubs.system"],
			sdk_version: "system_current",
		}

		java_library {
			name: "foo.test",
			srcs: ["a.java"],
			libs: ["sdklib.stubs.test"],
			sdk_version: "test_current",
		}

		java_sdk_library_import {
			name: "sdklib",
			public: {
				jars: ["a.jar"],
			},
			system: {
				jars: ["b.jar"],
			},
			test: {
				jars: ["c.jar"],
				stub_srcs: ["c.java"],
			},
		}
		`)

	for _, scope := range []string{"", ".system", ".test"} {
		fooModule := result.ModuleForTests(t, "foo"+scope, "android_common")
		javac := fooModule.Rule("javac")

		sdklibStubsJar := result.ModuleForTests(t, "sdklib.stubs"+scope, "android_common").Output("local-combined/sdklib.stubs" + scope + ".jar").Output
		android.AssertStringDoesContain(t, "foo classpath", javac.Args["classpath"], sdklibStubsJar.String())
	}

	CheckModuleDependencies(t, result.TestContext, "sdklib", "android_common", []string{
		`all_apex_contributions`,
		`dex2oatd`,
		`prebuilt_sdklib.stubs`,
		`prebuilt_sdklib.stubs.source.test`,
		`prebuilt_sdklib.stubs.system`,
		`prebuilt_sdklib.stubs.test`,
	})
}

func TestJavaSdkLibraryImport_WithSource(t *testing.T) {
	t.Parallel()
	result := android.GroupFixturePreparers(
		prepareForJavaTest,
		PrepareForTestWithJavaSdkLibraryFiles,
		FixtureWithLastReleaseApis("sdklib"),
		android.PrepareForTestWithBuildFlag("RELEASE_HIDDEN_API_EXPORTABLE_STUBS", "true"),
	).RunTestWithBp(t, `
		java_sdk_library {
			name: "sdklib",
			srcs: ["a.java"],
			sdk_version: "none",
			system_modules: "none",
			public: {
				enabled: true,
			},
		}

		java_sdk_library_import {
			name: "sdklib",
			public: {
				jars: ["a.jar"],
			},
		}
		`)

	CheckModuleDependencies(t, result.TestContext, "sdklib", "android_common", []string{
		`dex2oatd`,
		`prebuilt_sdklib`,
		`sdklib-removed.api.combined.public.latest`,
		`sdklib.api.combined.public.latest`,
		`sdklib.impl`,
		`sdklib.stubs`,
		`sdklib.stubs.exportable`,
		`sdklib.stubs.source`,
		`sdklib.xml`,
	})

	CheckModuleDependencies(t, result.TestContext, "prebuilt_sdklib", "android_common", []string{
		`all_apex_contributions`,
		`prebuilt_sdklib.stubs`,
		`sdklib.impl`,
		// This should be prebuilt_sdklib.stubs but is set to sdklib.stubs because the
		// dependency is added after prebuilts may have been renamed and so has to use
		// the renamed name.
		`sdklib.xml`,
	})
}

func testJavaSdkLibraryImport_Preferred(t *testing.T, prefer string, preparer android.FixturePreparer) {
	result := android.GroupFixturePreparers(
		prepareForJavaTest,
		PrepareForTestWithJavaSdkLibraryFiles,
		FixtureWithLastReleaseApis("sdklib"),
		preparer,
		android.PrepareForTestWithBuildFlag("RELEASE_HIDDEN_API_EXPORTABLE_STUBS", "true"),
	).RunTestWithBp(t, `
		java_sdk_library {
			name: "sdklib",
			srcs: ["a.java"],
			sdk_version: "none",
			system_modules: "none",
			public: {
				enabled: true,
			},
		}

		java_sdk_library_import {
			name: "sdklib",
			`+prefer+`
			public: {
				jars: ["a.jar"],
				stub_srcs: ["a.java"],
				current_api: "current.txt",
				removed_api: "removed.txt",
				annotations: "annotations.zip",
			},
		}

		java_library {
			name: "combined",
			static_libs: [
				"sdklib.stubs",
			],
			java_resources: [
				":sdklib.stubs.source",
				":sdklib{.public.api.txt}",
				":sdklib{.public.removed-api.txt}",
				":sdklib{.public.annotations.zip}",
			],
			sdk_version: "none",
			system_modules: "none",
		}

		java_library {
			name: "public",
			srcs: ["a.java"],
			libs: ["sdklib.stubs"],
			sdk_version: "current",
		}
		`)

	CheckModuleDependencies(t, result.TestContext, "sdklib", "android_common", []string{
		`prebuilt_sdklib`,
		`sdklib-removed.api.combined.public.latest`,
		`sdklib.api.combined.public.latest`,
		`sdklib.impl`,
		`sdklib.stubs`,
		`sdklib.stubs.exportable`,
		`sdklib.stubs.source`,
		`sdklib.xml`,
	})

	CheckModuleDependencies(t, result.TestContext, "prebuilt_sdklib", "android_common", []string{
		`all_apex_contributions`,
		`dex2oatd`,
		`prebuilt_sdklib.stubs`,
		`prebuilt_sdklib.stubs.source`,
		`sdklib.impl`,
		`sdklib.xml`,
	})

	// Make sure that dependencies on child modules use the prebuilt when preferred.
	CheckModuleDependencies(t, result.TestContext, "combined", "android_common", []string{
		// Each use of :sdklib{...} adds a dependency onto prebuilt_sdklib.
		`prebuilt_sdklib`,
		`prebuilt_sdklib.stubs`,
		`prebuilt_sdklib.stubs.source`,
	})

	// Make sure that dependencies on sdklib that resolve to one of the child libraries use the
	// prebuilt library.
	public := result.ModuleForTests(t, "public", "android_common")
	rule := public.Output("javac/public.jar")
	inputs := rule.Implicits.Strings()
	expected := "out/soong/.intermediates/prebuilt_sdklib.stubs/android_common/local-combined/sdklib.stubs.jar"
	if !android.InList(expected, inputs) {
		t.Errorf("expected %q to contain %q", inputs, expected)
	}
}

func TestJavaSdkLibraryImport_Preferred(t *testing.T) {
	t.Parallel()
	t.Run("prefer", func(t *testing.T) {
		t.Parallel()
		testJavaSdkLibraryImport_Preferred(t, "prefer: true,", android.NullFixturePreparer)
	})
}

// If a module is listed in `mainline_module_contributions, it should be used
// It will supersede any other source vs prebuilt selection mechanism like `prefer` attribute
func TestSdkLibraryImport_MetadataModuleSupersedesPreferred(t *testing.T) {
	t.Parallel()
	bp := `
		apex_contributions {
			name: "my_mainline_module_contributions",
			api_domain: "my_mainline_module",
			contents: [
				// legacy mechanism prefers the prebuilt
				// mainline_module_contributions supersedes this since source is listed explicitly
				"sdklib.prebuilt_preferred_using_legacy_flags",

				// legacy mechanism prefers the source
				// mainline_module_contributions supersedes this since prebuilt is listed explicitly
				"prebuilt_sdklib.source_preferred_using_legacy_flags",
			],
		}
		java_sdk_library {
			name: "sdklib.prebuilt_preferred_using_legacy_flags",
			srcs: ["a.java"],
			sdk_version: "none",
			system_modules: "none",
			public: {
				enabled: true,
			},
			system: {
				enabled: true,
			}
		}
		java_sdk_library_import {
			name: "sdklib.prebuilt_preferred_using_legacy_flags",
			prefer: true, // prebuilt is preferred using legacy mechanism
			public: {
				jars: ["a.jar"],
				stub_srcs: ["a.java"],
				current_api: "current.txt",
				removed_api: "removed.txt",
				annotations: "annotations.zip",
			},
			system: {
				jars: ["a.jar"],
				stub_srcs: ["a.java"],
				current_api: "current.txt",
				removed_api: "removed.txt",
				annotations: "annotations.zip",
			},
		}
		java_sdk_library {
			name: "sdklib.source_preferred_using_legacy_flags",
			srcs: ["a.java"],
			sdk_version: "none",
			system_modules: "none",
			public: {
				enabled: true,
			},
			system: {
				enabled: true,
			}
		}
		java_sdk_library_import {
			name: "sdklib.source_preferred_using_legacy_flags",
			prefer: false, // source is preferred using legacy mechanism
			public: {
				jars: ["a.jar"],
				stub_srcs: ["a.java"],
				current_api: "current.txt",
				removed_api: "removed.txt",
				annotations: "annotations.zip",
			},
			system: {
				jars: ["a.jar"],
				stub_srcs: ["a.java"],
				current_api: "current.txt",
				removed_api: "removed.txt",
				annotations: "annotations.zip",
			},
		}

		// rdeps
		java_library {
			name: "public",
			srcs: ["a.java"],
			libs: [
				// this should get source since source is listed in my_mainline_module_contributions
				"sdklib.prebuilt_preferred_using_legacy_flags.stubs",
				"sdklib.prebuilt_preferred_using_legacy_flags.stubs.system",

				// this should get prebuilt since source is listed in my_mainline_module_contributions
				"sdklib.source_preferred_using_legacy_flags.stubs",
				"sdklib.source_preferred_using_legacy_flags.stubs.system",

			],
		}
	`
	result := android.GroupFixturePreparers(
		prepareForJavaTest,
		PrepareForTestWithJavaSdkLibraryFiles,
		FixtureWithLastReleaseApis("sdklib.source_preferred_using_legacy_flags", "sdklib.prebuilt_preferred_using_legacy_flags"),
		android.PrepareForTestWithBuildFlag("RELEASE_APEX_CONTRIBUTIONS_ADSERVICES", "my_mainline_module_contributions"),
	).RunTestWithBp(t, bp)

	// Make sure that rdeps get the correct source vs prebuilt based on mainline_module_contributions
	public := result.ModuleForTests(t, "public", "android_common")
	rule := public.Output("javac/public.jar")
	inputs := rule.Implicits.Strings()
	expectedInputs := []string{
		// source
		"out/soong/.intermediates/sdklib.prebuilt_preferred_using_legacy_flags.stubs.from-text/android_common/sdklib.prebuilt_preferred_using_legacy_flags.stubs.from-text/sdklib.prebuilt_preferred_using_legacy_flags.stubs.from-text.jar",
		"out/soong/.intermediates/sdklib.prebuilt_preferred_using_legacy_flags.stubs.system.from-text/android_common/sdklib.prebuilt_preferred_using_legacy_flags.stubs.system.from-text/sdklib.prebuilt_preferred_using_legacy_flags.stubs.system.from-text.jar",

		// prebuilt
		"out/soong/.intermediates/prebuilt_sdklib.source_preferred_using_legacy_flags.stubs/android_common/local-combined/sdklib.source_preferred_using_legacy_flags.stubs.jar",
		"out/soong/.intermediates/prebuilt_sdklib.source_preferred_using_legacy_flags.stubs.system/android_common/local-combined/sdklib.source_preferred_using_legacy_flags.stubs.system.jar",
	}
	for _, expected := range expectedInputs {
		if !android.InList(expected, inputs) {
			t.Errorf("expected %q to contain %q", inputs, expected)
		}
	}
}

func TestJavaSdkLibraryDist(t *testing.T) {
	t.Parallel()
	result := android.GroupFixturePreparers(
		PrepareForTestWithJavaBuildComponents,
		PrepareForTestWithJavaDefaultModules,
		PrepareForTestWithJavaSdkLibraryFiles,
		FixtureWithLastReleaseApis(
			"sdklib_no_group",
			"sdklib_group_foo",
			"sdklib_owner_foo",
			"foo"),
		android.PrepareForTestWithBuildFlag("RELEASE_HIDDEN_API_EXPORTABLE_STUBS", "true"),
	).RunTestWithBp(t, `
		java_sdk_library {
			name: "sdklib_no_group",
			srcs: ["foo.java"],
		}

		java_sdk_library {
			name: "sdklib_group_foo",
			srcs: ["foo.java"],
			dist_group: "foo",
		}

		java_sdk_library {
			name: "sdklib_owner_foo",
			srcs: ["foo.java"],
			owner: "foo",
		}

		java_sdk_library {
			name: "sdklib_stem_foo",
			srcs: ["foo.java"],
			dist_stem: "foo",
		}
	`)

	type testCase struct {
		module   string
		distDir  string
		distStem string
	}
	testCases := []testCase{
		{
			module:   "sdklib_no_group",
			distDir:  "apistubs/unknown/public",
			distStem: "sdklib_no_group.jar",
		},
		{
			module:   "sdklib_group_foo",
			distDir:  "apistubs/foo/public",
			distStem: "sdklib_group_foo.jar",
		},
		{
			// Owner doesn't affect distDir after b/186723288.
			module:   "sdklib_owner_foo",
			distDir:  "apistubs/unknown/public",
			distStem: "sdklib_owner_foo.jar",
		},
		{
			module:   "sdklib_stem_foo",
			distDir:  "apistubs/unknown/public",
			distStem: "foo.jar",
		},
	}

	for _, tt := range testCases {
		t.Run(tt.module, func(t *testing.T) {
			t.Parallel()
			m := result.ModuleForTests(t, apiScopePublic.exportableStubsLibraryModuleName(tt.module), "android_common").Module().(*Library)
			dists := m.Dists()
			if len(dists) != 1 {
				t.Fatalf("expected exactly 1 dist entry, got %d", len(dists))
			}
			if g, w := String(dists[0].Dir), tt.distDir; g != w {
				t.Errorf("expected dist dir %q, got %q", w, g)
			}
			if g, w := String(dists[0].Dest), tt.distStem; g != w {
				t.Errorf("expected dist stem %q, got %q", w, g)
			}
		})
	}
}

func TestSdkLibrary_CheckMinSdkVersion(t *testing.T) {
	t.Parallel()
	preparer := android.GroupFixturePreparers(
		PrepareForTestWithJavaBuildComponents,
		PrepareForTestWithJavaDefaultModules,
		PrepareForTestWithJavaSdkLibraryFiles,
	)

	preparer.RunTestWithBp(t, `
		java_sdk_library {
			name: "sdklib",
			srcs: ["a.java"],
			static_libs: ["util"],
			min_sdk_version: "30",
			unsafe_ignore_missing_latest_api: true,
		}

		java_library {
			name: "util",
			srcs: ["a.java"],
			min_sdk_version: "30",
		}
	`)

	preparer.
		RunTestWithBp(t, `
			java_sdk_library {
				name: "sdklib",
				srcs: ["a.java"],
				libs: ["util"],
				impl_only_libs: ["util"],
				stub_only_libs: ["util"],
				stub_only_static_libs: ["util"],
				min_sdk_version: "30",
				unsafe_ignore_missing_latest_api: true,
			}

			java_library {
				name: "util",
				srcs: ["a.java"],
			}
		`)

	preparer.ExtendWithErrorHandler(android.FixtureExpectsAtLeastOneErrorMatchingPattern(`module "util".*should support min_sdk_version\(30\)`)).
		RunTestWithBp(t, `
			java_sdk_library {
				name: "sdklib",
				srcs: ["a.java"],
				static_libs: ["util"],
				min_sdk_version: "30",
				unsafe_ignore_missing_latest_api: true,
			}

			java_library {
				name: "util",
				srcs: ["a.java"],
				min_sdk_version: "31",
			}
		`)

	preparer.ExtendWithErrorHandler(android.FixtureExpectsAtLeastOneErrorMatchingPattern(`module "another_util".*should support min_sdk_version\(30\)`)).
		RunTestWithBp(t, `
			java_sdk_library {
				name: "sdklib",
				srcs: ["a.java"],
				static_libs: ["util"],
				min_sdk_version: "30",
				unsafe_ignore_missing_latest_api: true,
			}

			java_library {
				name: "util",
				srcs: ["a.java"],
				static_libs: ["another_util"],
				min_sdk_version: "30",
			}

			java_library {
				name: "another_util",
				srcs: ["a.java"],
				min_sdk_version: "31",
			}
		`)
}

func TestJavaSdkLibrary_StubOnlyLibs_PassedToDroidstubs(t *testing.T) {
	t.Parallel()
	result := android.GroupFixturePreparers(
		prepareForJavaTest,
		PrepareForTestWithJavaSdkLibraryFiles,
		FixtureWithLastReleaseApis("foo"),
	).RunTestWithBp(t, `
		java_sdk_library {
			name: "foo",
			srcs: ["a.java"],
			public: {
				enabled: true,
			},
			stub_only_libs: ["bar-lib"],
		}

		java_library {
			name: "bar-lib",
			srcs: ["b.java"],
		}
		`)

	// The foo.stubs.source should depend on bar-lib
	fooStubsSources := result.ModuleForTests(t, "foo.stubs.source", "android_common").Module().(*Droidstubs)
	eval := fooStubsSources.ConfigurableEvaluator(android.PanickingConfigAndErrorContext(result.TestContext))
	android.AssertStringListContains(t, "foo stubs should depend on bar-lib", fooStubsSources.Javadoc.properties.Libs.GetOrDefault(eval, nil), "bar-lib")
}

func TestJavaSdkLibrary_Scope_Libs_PassedToDroidstubs(t *testing.T) {
	t.Parallel()
	result := android.GroupFixturePreparers(
		prepareForJavaTest,
		PrepareForTestWithJavaSdkLibraryFiles,
		FixtureWithLastReleaseApis("foo"),
	).RunTestWithBp(t, `
		java_sdk_library {
			name: "foo",
			srcs: ["a.java"],
			public: {
				enabled: true,
				libs: ["bar-lib"],
			},
		}

		java_library {
			name: "bar-lib",
			srcs: ["b.java"],
		}
		`)

	// The foo.stubs.source should depend on bar-lib
	fooStubsSources := result.ModuleForTests(t, "foo.stubs.source", "android_common").Module().(*Droidstubs)
	eval := fooStubsSources.ConfigurableEvaluator(android.PanickingConfigAndErrorContext(result.TestContext))
	android.AssertStringListContains(t, "foo stubs should depend on bar-lib", fooStubsSources.Javadoc.properties.Libs.GetOrDefault(eval, nil), "bar-lib")
}

func TestJavaSdkLibrary_ApiLibrary(t *testing.T) {
	t.Parallel()
	result := android.GroupFixturePreparers(
		prepareForJavaTest,
		PrepareForTestWithJavaSdkLibraryFiles,
		FixtureWithLastReleaseApis("foo"),
	).RunTestWithBp(t, `
		java_sdk_library {
			name: "foo",
			srcs: ["a.java", "b.java"],
			api_packages: ["foo"],
			system: {
				enabled: true,
			},
			module_lib: {
				enabled: true,
			},
			test: {
				enabled: true,
			},
		}
	`)

	testCases := []struct {
		scope            *apiScope
		apiContributions []string
	}{
		{
			scope:            apiScopePublic,
			apiContributions: []string{"foo.stubs.source.api.contribution"},
		},
		{
			scope:            apiScopeSystem,
			apiContributions: []string{"foo.stubs.source.system.api.contribution", "foo.stubs.source.api.contribution"},
		},
		{
			scope:            apiScopeTest,
			apiContributions: []string{"foo.stubs.source.test.api.contribution", "foo.stubs.source.system.api.contribution", "foo.stubs.source.api.contribution"},
		},
		{
			scope:            apiScopeModuleLib,
			apiContributions: []string{"foo.stubs.source.module_lib.api.contribution", "foo.stubs.source.system.api.contribution", "foo.stubs.source.api.contribution"},
		},
	}

	for _, c := range testCases {
		m := result.ModuleForTests(t, c.scope.apiLibraryModuleName("foo"), "android_common").Module().(*ApiLibrary)
		android.AssertArrayString(t, "Module expected to contain api contributions", c.apiContributions, m.properties.Api_contributions)
	}
}

func TestStaticDepStubLibrariesVisibility(t *testing.T) {
	t.Parallel()
	android.GroupFixturePreparers(
		prepareForJavaTest,
		PrepareForTestWithJavaSdkLibraryFiles,
		FixtureWithLastReleaseApis("foo"),
		android.FixtureMergeMockFs(
			map[string][]byte{
				"A.java": nil,
				"dir/Android.bp": []byte(
					`
					java_library {
						name: "bar",
						srcs: ["A.java"],
						libs: ["foo.stubs.from-source"],
					}
					`),
				"dir/A.java": nil,
			},
		).ExtendWithErrorHandler(
			android.FixtureExpectsAtLeastOneErrorMatchingPattern(
				`module "bar" variant "android_common": depends on //.:foo.stubs.from-source which is not visible to this module`)),
	).RunTestWithBp(t, `
		java_sdk_library {
			name: "foo",
			srcs: ["A.java"],
		}
	`)
}

func TestSdkLibraryDependency(t *testing.T) {
	t.Parallel()
	result := android.GroupFixturePreparers(
		prepareForJavaTest,
		PrepareForTestWithJavaSdkLibraryFiles,
		FixtureWithPrebuiltApis(map[string][]string{
			"30": {"bar", "foo"},
		}),
	).RunTestWithBp(t,
		`
		java_sdk_library {
			name: "foo",
			srcs: ["a.java", "b.java"],
			api_packages: ["foo"],
		}

		java_sdk_library {
			name: "bar",
			srcs: ["c.java", "b.java"],
			libs: [
				"foo.stubs",
			],
			uses_libs: [
				"foo",
			],
		}
`)

	barPermissions := result.ModuleForTests(t, "bar.xml", "android_common").Output("bar.xml")
	barContents := android.ContentFromFileRuleForTests(t, result.TestContext, barPermissions)
	android.AssertStringDoesContain(t, "bar.xml java_sdk_xml command", barContents, `dependency="foo"`)
}

func TestSdkLibraryExportableStubsLibrary(t *testing.T) {
	t.Parallel()
	result := android.GroupFixturePreparers(
		prepareForJavaTest,
		PrepareForTestWithJavaSdkLibraryFiles,
		FixtureWithLastReleaseApis("foo"),
	).RunTestWithBp(t, `
		aconfig_declarations {
			name: "bar",
			package: "com.example.package",
			container: "com.android.foo",
			srcs: [
				"bar.aconfig",
			],
		}
		java_sdk_library {
			name: "foo",
			srcs: ["a.java", "b.java"],
			api_packages: ["foo"],
			system: {
				enabled: true,
			},
			module_lib: {
				enabled: true,
			},
			test: {
				enabled: true,
			},
			aconfig_declarations: [
				"bar",
			],
		}
	`)

	exportableStubsLibraryModuleName := apiScopePublic.exportableStubsLibraryModuleName("foo")
	exportableSourceStubsLibraryModuleName := apiScopePublic.exportableSourceStubsLibraryModuleName("foo")

	// Check modules generation
	result.ModuleForTests(t, exportableStubsLibraryModuleName, "android_common")
	result.ModuleForTests(t, exportableSourceStubsLibraryModuleName, "android_common")

	// Check static lib dependency
	android.AssertBoolEquals(t, "exportable top level stubs library module depends on the"+
		"exportable source stubs library module", true,
		CheckModuleHasDependencyWithTag(t, result.TestContext, exportableStubsLibraryModuleName,
			"android_common", staticLibTag, exportableSourceStubsLibraryModuleName),
	)
}

// For java libraries depending on java_sdk_library(_import) via libs, assert that
// rdep gets stubs of source if source is listed in apex_contributions and prebuilt has prefer (legacy mechanism)
func TestStubResolutionOfJavaSdkLibraryInLibs(t *testing.T) {
	t.Parallel()
	bp := `
		apex_contributions {
			name: "my_mainline_module_contributions",
			api_domain: "my_mainline_module",
			contents: ["sdklib"], // source is selected using apex_contributions, but prebuilt is selected using prefer
		}
		java_sdk_library {
			name: "sdklib",
			srcs: ["a.java"],
			sdk_version: "none",
			system_modules: "none",
			public: {
				enabled: true,
			},
		}
		java_sdk_library_import {
			name: "sdklib",
			public: {
				jars: ["a.jar"],
				stub_srcs: ["a.java"],
				current_api: "current.txt",
				removed_api: "removed.txt",
				annotations: "annotations.zip",
			},
			prefer: true, // Set prefer explicitly on the prebuilt. We will assert that rdep gets source in a test case.
		}
		// rdeps
		java_library {
			name: "mymodule",
			srcs: ["a.java"],
			sdk_version: "current",
			libs: ["sdklib.stubs",], // this should be dynamically resolved to sdklib.stubs (source) or prebuilt_sdklib.stubs (prebuilt)
		}
	`

	fixture := android.GroupFixturePreparers(
		prepareForJavaTest,
		PrepareForTestWithJavaSdkLibraryFiles,
		FixtureWithLastReleaseApis("sdklib"),
		// We can use any of the apex contribution build flags from build/soong/android/config.go#mainlineApexContributionBuildFlags here
		android.PrepareForTestWithBuildFlag("RELEASE_APEX_CONTRIBUTIONS_ADSERVICES", "my_mainline_module_contributions"),
	)

	result := fixture.RunTestWithBp(t, bp)
	// Make sure that rdeps get the correct source vs prebuilt based on mainline_module_contributions
	public := result.ModuleForTests(t, "mymodule", "android_common")
	rule := public.Output("javac/mymodule.jar")
	inputs := rule.Implicits.Strings()
	android.AssertStringListContains(t, "Could not find the expected stub on classpath", inputs,
		"out/soong/.intermediates/sdklib.stubs.from-text/android_common/sdklib.stubs.from-text/sdklib.stubs.from-text.jar")
}

// test that rdep gets resolved to the correct version of a java_sdk_library (source or a specific prebuilt)
func TestMultipleSdkLibraryPrebuilts(t *testing.T) {
	t.Parallel()
	bp := `
		apex_contributions {
			name: "my_mainline_module_contributions",
			api_domain: "my_mainline_module",
			contents: ["%s"],
		}
		java_sdk_library {
			name: "sdklib",
			srcs: ["a.java"],
			sdk_version: "none",
			system_modules: "none",
			public: {
				enabled: true,
			},
		}
		java_sdk_library_import {
			name: "sdklib.v1", //prebuilt
			source_module_name: "sdklib",
			public: {
				jars: ["a.jar"],
				stub_srcs: ["a.java"],
				current_api: "current.txt",
				removed_api: "removed.txt",
				annotations: "annotations.zip",
			},
		}
		java_sdk_library_import {
			name: "sdklib.v2", //prebuilt
			source_module_name: "sdklib",
			public: {
				jars: ["a.jar"],
				stub_srcs: ["a.java"],
				current_api: "current.txt",
				removed_api: "removed.txt",
				annotations: "annotations.zip",
			},
		}
		// rdeps
		java_library {
			name: "mymodule",
			srcs: ["a.java"],
			libs: ["sdklib.stubs",],
		}
	`
	testCases := []struct {
		desc                   string
		selectedDependencyName string
		expectedStubPath       string
	}{
		{
			desc:                   "Source library is selected using apex_contributions",
			selectedDependencyName: "sdklib",
			expectedStubPath:       "out/soong/.intermediates/sdklib.stubs.from-text/android_common/sdklib.stubs.from-text/sdklib.stubs.from-text.jar",
		},
		{
			desc:                   "Prebuilt library v1 is selected using apex_contributions",
			selectedDependencyName: "prebuilt_sdklib.v1",
			expectedStubPath:       "out/soong/.intermediates/prebuilt_sdklib.v1.stubs/android_common/local-combined/sdklib.stubs.jar",
		},
		{
			desc:                   "Prebuilt library v2 is selected using apex_contributions",
			selectedDependencyName: "prebuilt_sdklib.v2",
			expectedStubPath:       "out/soong/.intermediates/prebuilt_sdklib.v2.stubs/android_common/local-combined/sdklib.stubs.jar",
		},
	}

	fixture := android.GroupFixturePreparers(
		prepareForJavaTest,
		PrepareForTestWithJavaSdkLibraryFiles,
		FixtureWithLastReleaseApis("sdklib", "sdklib.v1", "sdklib.v2"),
		android.PrepareForTestWithBuildFlag("RELEASE_APEX_CONTRIBUTIONS_ADSERVICES", "my_mainline_module_contributions"),
	)

	for _, tc := range testCases {
		result := fixture.RunTestWithBp(t, fmt.Sprintf(bp, tc.selectedDependencyName))

		// Make sure that rdeps get the correct source vs prebuilt based on mainline_module_contributions
		public := result.ModuleForTests(t, "mymodule", "android_common")
		rule := public.Output("javac/mymodule.jar")
		inputs := rule.Implicits.Strings()
		android.AssertStringListContains(t, "Could not find the expected stub on classpath", inputs, tc.expectedStubPath)
	}
}

func TestStubLinkType(t *testing.T) {
	t.Parallel()
	android.GroupFixturePreparers(
		prepareForJavaTest,
		PrepareForTestWithJavaSdkLibraryFiles,
		FixtureWithLastReleaseApis("foo"),
	).ExtendWithErrorHandler(android.FixtureExpectsOneErrorPattern(
		`module "baz" variant "android_common": compiles against system API, but dependency `+
			`"bar.stubs.system" is compiling against module API. In order to fix this, `+
			`consider adjusting sdk_version: OR platform_apis: property of the source or `+
			`target module so that target module is built with the same or smaller API set `+
			`when compared to the source.`),
	).RunTestWithBp(t, `
		java_sdk_library {
			name: "foo",
			srcs: ["a.java"],
			sdk_version: "current",
		}
		java_library {
			name: "bar.stubs.system",
			srcs: ["a.java"],
			sdk_version: "module_current",
			is_stubs_module: false,
		}

		java_library {
			name: "baz",
			srcs: ["b.java"],
			libs: [
				"foo.stubs.system",
				"bar.stubs.system",
			],
			sdk_version: "system_current",
		}
		`)
}

func TestSdkLibDirectDependency(t *testing.T) {
	t.Parallel()
	android.GroupFixturePreparers(
		prepareForJavaTest,
		PrepareForTestWithJavaSdkLibraryFiles,
		FixtureWithLastReleaseApis("foo", "bar"),
	).ExtendWithErrorHandler(android.FixtureExpectsAllErrorsToMatchAPattern([]string{
		`module "baz" variant "android_common": cannot depend directly on java_sdk_library ` +
			`"foo"; try depending on "foo.stubs", or "foo.impl" instead`,
		`module "baz" variant "android_common": cannot depend directly on java_sdk_library ` +
			`"prebuilt_bar"; try depending on "bar.stubs", or "bar.impl" instead`,
	}),
	).RunTestWithBp(t, `
		java_sdk_library {
			name: "foo",
			srcs: ["a.java"],
			sdk_version: "current",
			public: {
				enabled: true,
			},
		}

		java_sdk_library_import {
			name: "foo",
			public: {
				jars: ["a.jar"],
				stub_srcs: ["a.java"],
				current_api: "current.txt",
				removed_api: "removed.txt",
				annotations: "annotations.zip",
			},
		}

		java_sdk_library {
			name: "bar",
			srcs: ["a.java"],
			sdk_version: "current",
			public: {
				enabled: true,
			},
		}

		java_sdk_library_import {
			name: "bar",
			prefer: true,
			public: {
				jars: ["a.jar"],
				stub_srcs: ["a.java"],
				current_api: "current.txt",
				removed_api: "removed.txt",
				annotations: "annotations.zip",
			},
		}

		java_library {
			name: "baz",
			srcs: ["b.java"],
			libs: [
				"foo",
				"bar",
			],
		}
	`)
}

func TestSdkLibDirectDependencyWithPrebuiltSdk(t *testing.T) {
	t.Parallel()
	android.GroupFixturePreparers(
		prepareForJavaTest,
		PrepareForTestWithJavaSdkLibraryFiles,
		android.FixtureModifyProductVariables(func(variables android.FixtureProductVariables) {
			variables.Platform_sdk_version = intPtr(34)
			variables.Platform_sdk_codename = stringPtr("VanillaIceCream")
			variables.Platform_version_active_codenames = []string{"VanillaIceCream"}
			variables.Platform_systemsdk_versions = []string{"33", "34", "VanillaIceCream"}
			variables.DeviceSystemSdkVersions = []string{"VanillaIceCream"}
		}),
		FixtureWithPrebuiltApis(map[string][]string{
			"33": {"foo"},
			"34": {"foo"},
			"35": {"foo"},
		}),
	).ExtendWithErrorHandler(android.FixtureExpectsOneErrorPattern(
		`module "baz" variant "android_common": cannot depend directly on java_sdk_library "foo"; `+
			`try depending on "sdk_public_33_foo", "sdk_system_33_foo", "sdk_test_33_foo", `+
			`"sdk_module-lib_33_foo", or "sdk_system-server_33_foo" instead`),
	).RunTestWithBp(t, `
		java_sdk_library {
			name: "foo",
			srcs: ["a.java"],
			sdk_version: "current",
			public: {
				enabled: true,
			},
			system: {
				enabled: true,
			},
		}

		java_library {
			name: "baz",
			srcs: ["b.java"],
			libs: [
				"foo",
			],
			sdk_version: "system_33",
		}
	`)
}
