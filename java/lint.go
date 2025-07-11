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
	"sort"
	"strings"

	"github.com/google/blueprint"
	"github.com/google/blueprint/depset"
	"github.com/google/blueprint/proptools"

	"android/soong/android"
	"android/soong/java/config"
	"android/soong/remoteexec"
)

// lint checks automatically enforced for modules that have different min_sdk_version than
// sdk_version
var updatabilityChecks = []string{"NewApi"}

type LintProperties struct {
	// Controls for running Android Lint on the module.
	Lint struct {

		// If true, run Android Lint on the module.  Defaults to true.
		Enabled *bool

		// Flags to pass to the Android Lint tool.
		Flags []string

		// Checks that should be treated as fatal.
		Fatal_checks []string

		// Checks that should be treated as errors.
		Error_checks []string

		// Checks that should be treated as warnings.
		Warning_checks []string

		// Checks that should be skipped.
		Disabled_checks []string

		// Modules that provide extra lint checks
		Extra_check_modules []string

		// The lint baseline file to use. If specified, lint warnings listed in this file will be
		// suppressed during lint checks.
		Baseline_filename *string

		// If true, baselining updatability lint checks (e.g. NewApi) is prohibited. Defaults to false.
		Strict_updatability_linting *bool

		// Treat the code in this module as test code for @VisibleForTesting enforcement.
		// This will be true by default for test module types, false otherwise.
		// If soong gets support for testonly, this flag should be replaced with that.
		Test *bool

		// Same as the regular Test property, but set by internal soong code based on if the module
		// type is a test module type. This will act as the default value for the test property,
		// but can be overridden by the user.
		Test_module_type *bool `blueprint:"mutated"`

		// Whether to ignore the exit code of Android lint. This is the --exit_code
		// option. Defaults to false.
		Suppress_exit_code *bool
	}
}

type linter struct {
	name                    string
	manifest                android.Path
	mergedManifest          android.Path
	srcs                    android.Paths
	srcJars                 android.Paths
	resources               android.Paths
	classpath               android.Paths
	classes                 android.Path
	extraLintCheckJars      android.Paths
	library                 bool
	minSdkVersion           android.ApiLevel
	targetSdkVersion        android.ApiLevel
	compileSdkVersion       android.ApiLevel
	compileSdkKind          android.SdkKind
	javaLanguageLevel       string
	kotlinLanguageLevel     string
	properties              LintProperties
	extraMainlineLintErrors []string
	compile_data            android.Paths

	reports android.Paths

	buildModuleReportZip bool
}

type LintDepSets struct {
	HTML, Text, XML, Baseline depset.DepSet[android.Path]
}

type LintDepSetsBuilder struct {
	HTML, Text, XML, Baseline *depset.Builder[android.Path]
}

func NewLintDepSetBuilder() LintDepSetsBuilder {
	return LintDepSetsBuilder{
		HTML:     depset.NewBuilder[android.Path](depset.POSTORDER),
		Text:     depset.NewBuilder[android.Path](depset.POSTORDER),
		XML:      depset.NewBuilder[android.Path](depset.POSTORDER),
		Baseline: depset.NewBuilder[android.Path](depset.POSTORDER),
	}
}

func (l LintDepSetsBuilder) Direct(html, text, xml android.Path, baseline android.OptionalPath) LintDepSetsBuilder {
	l.HTML.Direct(html)
	l.Text.Direct(text)
	l.XML.Direct(xml)
	if baseline.Valid() {
		l.Baseline.Direct(baseline.Path())
	}
	return l
}

func (l LintDepSetsBuilder) Transitive(info *LintInfo) LintDepSetsBuilder {
	l.HTML.Transitive(info.TransitiveHTML)
	l.Text.Transitive(info.TransitiveText)
	l.XML.Transitive(info.TransitiveXML)
	l.Baseline.Transitive(info.TransitiveBaseline)
	return l
}

func (l LintDepSetsBuilder) Build() LintDepSets {
	return LintDepSets{
		HTML:     l.HTML.Build(),
		Text:     l.Text.Build(),
		XML:      l.XML.Build(),
		Baseline: l.Baseline.Build(),
	}
}

type lintDatabaseFiles struct {
	apiVersionsModule       string
	apiVersionsCopiedName   string
	apiVersionsPrebuiltPath string
	annotationsModule       string
	annotationCopiedName    string
	annotationPrebuiltpath  string
}

var allLintDatabasefiles = map[android.SdkKind]lintDatabaseFiles{
	android.SdkPublic: {
		apiVersionsModule:       "api_versions_public",
		apiVersionsCopiedName:   "api_versions_public.xml",
		apiVersionsPrebuiltPath: "prebuilts/sdk/current/public/data/api-versions.xml",
		annotationsModule:       "sdk-annotations.zip",
		annotationCopiedName:    "annotations-public.zip",
		annotationPrebuiltpath:  "prebuilts/sdk/current/public/data/annotations.zip",
	},
	android.SdkSystem: {
		apiVersionsModule:       "api_versions_system",
		apiVersionsCopiedName:   "api_versions_system.xml",
		apiVersionsPrebuiltPath: "prebuilts/sdk/current/system/data/api-versions.xml",
		annotationsModule:       "sdk-annotations-system.zip",
		annotationCopiedName:    "annotations-system.zip",
		annotationPrebuiltpath:  "prebuilts/sdk/current/system/data/annotations.zip",
	},
	android.SdkModule: {
		apiVersionsModule:       "api_versions_module_lib",
		apiVersionsCopiedName:   "api_versions_module_lib.xml",
		apiVersionsPrebuiltPath: "prebuilts/sdk/current/module-lib/data/api-versions.xml",
		annotationsModule:       "sdk-annotations-module-lib.zip",
		annotationCopiedName:    "annotations-module-lib.zip",
		annotationPrebuiltpath:  "prebuilts/sdk/current/module-lib/data/annotations.zip",
	},
	android.SdkSystemServer: {
		apiVersionsModule:       "api_versions_system_server",
		apiVersionsCopiedName:   "api_versions_system_server.xml",
		apiVersionsPrebuiltPath: "prebuilts/sdk/current/system-server/data/api-versions.xml",
		annotationsModule:       "sdk-annotations-system-server.zip",
		annotationCopiedName:    "annotations-system-server.zip",
		annotationPrebuiltpath:  "prebuilts/sdk/current/system-server/data/annotations.zip",
	},
}

var LintProvider = blueprint.NewProvider[*LintInfo]()

type LintInfo struct {
	HTML              android.Path
	Text              android.Path
	XML               android.Path
	ReferenceBaseline android.Path

	TransitiveHTML     depset.DepSet[android.Path]
	TransitiveText     depset.DepSet[android.Path]
	TransitiveXML      depset.DepSet[android.Path]
	TransitiveBaseline depset.DepSet[android.Path]
}

func (l *linter) enabled() bool {
	return BoolDefault(l.properties.Lint.Enabled, true)
}

func (l *linter) deps(ctx android.BottomUpMutatorContext) {
	if !l.enabled() {
		return
	}

	extraCheckModules := l.properties.Lint.Extra_check_modules

	if extraCheckModulesEnv := ctx.Config().Getenv("ANDROID_LINT_CHECK_EXTRA_MODULES"); extraCheckModulesEnv != "" {
		extraCheckModules = append(extraCheckModules, strings.Split(extraCheckModulesEnv, ",")...)
	}

	ctx.AddFarVariationDependencies(ctx.Config().BuildOSCommonTarget.Variations(),
		extraLintCheckTag, extraCheckModules...)
}

// lintPaths contains the paths to lint's inputs and outputs to make it easier to pass them
// around.
type lintPaths struct {
	projectXML android.WritablePath
	configXML  android.WritablePath
	cacheDir   android.WritablePath
	homeDir    android.WritablePath
	srcjarDir  android.WritablePath
}

func lintRBEExecStrategy(ctx android.ModuleContext) string {
	return ctx.Config().GetenvWithDefault("RBE_LINT_EXEC_STRATEGY", remoteexec.LocalExecStrategy)
}

func (l *linter) writeLintProjectXML(ctx android.ModuleContext, rule *android.RuleBuilder, srcsList android.Path,
	baselines android.Paths) lintPaths {

	projectXMLPath := android.PathForModuleOut(ctx, "lint", "project.xml")
	// Lint looks for a lint.xml file next to the project.xml file, give it one.
	configXMLPath := android.PathForModuleOut(ctx, "lint", "lint.xml")
	cacheDir := android.PathForModuleOut(ctx, "lint", "cache")
	homeDir := android.PathForModuleOut(ctx, "lint", "home")

	srcJarDir := android.PathForModuleOut(ctx, "lint", "srcjars")
	srcJarList := zipSyncCmd(ctx, rule, srcJarDir, l.srcJars)

	cmd := rule.Command().
		BuiltTool("lint_project_xml").
		FlagWithOutput("--project_out ", projectXMLPath).
		FlagWithOutput("--config_out ", configXMLPath).
		FlagWithArg("--name ", ctx.ModuleName())

	if l.library {
		cmd.Flag("--library")
	}

	test := proptools.BoolDefault(l.properties.Lint.Test_module_type, false)
	if l.properties.Lint.Test != nil {
		test = *l.properties.Lint.Test
	}
	if test {
		cmd.Flag("--test")
	}
	if l.manifest != nil {
		cmd.FlagWithInput("--manifest ", l.manifest)
	}
	if l.mergedManifest != nil {
		cmd.FlagWithInput("--merged_manifest ", l.mergedManifest)
	}

	// TODO(ccross): some of the files in l.srcs are generated sources and should be passed to
	// lint separately.
	cmd.FlagWithInput("--srcs ", srcsList)

	cmd.FlagWithInput("--generated_srcs ", srcJarList)

	if len(l.resources) > 0 {
		resourcesList := android.PathForModuleOut(ctx, "lint-resources.list")
		cmd.FlagWithRspFileInputList("--resources ", resourcesList, l.resources)
	}

	if l.classes != nil {
		cmd.FlagWithInput("--classes ", l.classes)
	}

	cmd.FlagForEachInput("--classpath ", l.classpath)

	cmd.FlagForEachInput("--extra_checks_jar ", l.extraLintCheckJars)

	cmd.FlagWithArg("--root_dir ", "$PWD")

	// The cache tag in project.xml is relative to the root dir, or the project.xml file if
	// the root dir is not set.
	cmd.FlagWithArg("--cache_dir ", cacheDir.String())

	cmd.FlagWithInput("@",
		android.PathForSource(ctx, "build/soong/java/lint_defaults.txt"))

	cmd.FlagForEachArg("--error_check ", l.extraMainlineLintErrors)
	cmd.FlagForEachArg("--disable_check ", l.properties.Lint.Disabled_checks)
	cmd.FlagForEachArg("--warning_check ", l.properties.Lint.Warning_checks)
	cmd.FlagForEachArg("--error_check ", l.properties.Lint.Error_checks)
	cmd.FlagForEachArg("--fatal_check ", l.properties.Lint.Fatal_checks)

	if Bool(l.properties.Lint.Strict_updatability_linting) && len(baselines) > 0 {
		// Verify the module does not baseline issues that endanger safe updatability.
		strictUpdatabilityChecksOutputFile := VerifyStrictUpdatabilityChecks(ctx, baselines)
		cmd.Validation(strictUpdatabilityChecksOutputFile)
	}

	return lintPaths{
		projectXML: projectXMLPath,
		configXML:  configXMLPath,
		cacheDir:   cacheDir,
		homeDir:    homeDir,
	}

}

func VerifyStrictUpdatabilityChecks(ctx android.ModuleContext, baselines android.Paths) android.Path {
	rule := android.NewRuleBuilder(pctx, ctx)
	baselineRspFile := android.PathForModuleOut(ctx, "lint_strict_updatability_check_baselines.rsp")
	outputFile := android.PathForModuleOut(ctx, "lint_strict_updatability_check.stamp")
	rule.Command().Text("rm -f").Output(outputFile)
	rule.Command().
		BuiltTool("lint_strict_updatability_checks").
		FlagWithArg("--name ", ctx.ModuleName()).
		FlagWithRspFileInputList("--baselines ", baselineRspFile, baselines).
		FlagForEachArg("--disallowed_issues ", updatabilityChecks)
	rule.Command().Text("touch").Output(outputFile)
	rule.Build("lint_strict_updatability_checks", "lint strict updatability checks")

	return outputFile
}

// generateManifest adds a command to the rule to write a simple manifest that contains the
// minSdkVersion and targetSdkVersion for modules (like java_library) that don't have a manifest.
func (l *linter) generateManifest(ctx android.ModuleContext, rule *android.RuleBuilder) android.WritablePath {
	manifestPath := android.PathForModuleOut(ctx, "lint", "AndroidManifest.xml")

	rule.Command().Text("(").
		Text(`echo "<?xml version='1.0' encoding='utf-8'?>" &&`).
		Text(`echo "<manifest xmlns:android='http://schemas.android.com/apk/res/android'" &&`).
		Text(`echo "    android:versionCode='1' android:versionName='1' >" &&`).
		Textf(`echo "  <uses-sdk android:minSdkVersion='%s' android:targetSdkVersion='%s'/>" &&`,
			l.minSdkVersion.String(), l.targetSdkVersion.String()).
		Text(`echo "</manifest>"`).
		Text(") >").Output(manifestPath)

	return manifestPath
}

func (l *linter) lint(ctx android.ModuleContext) {
	if !l.enabled() {
		return
	}

	for _, flag := range l.properties.Lint.Flags {
		if strings.Contains(flag, "--disable") || strings.Contains(flag, "--enable") || strings.Contains(flag, "--check") {
			ctx.PropertyErrorf("lint.flags", "Don't use --disable, --enable, or --check in the flags field, instead use the dedicated disabled_checks, warning_checks, error_checks, or fatal_checks fields")
		}
	}

	if l.minSdkVersion.CompareTo(l.compileSdkVersion) == -1 {
		l.extraMainlineLintErrors = append(l.extraMainlineLintErrors, updatabilityChecks...)
		// Skip lint warning checks for NewApi warnings for libcore where they come from source
		// files that reference the API they are adding (b/208656169).
		if !strings.HasPrefix(ctx.ModuleDir(), "libcore") {
			_, filtered := android.FilterList(l.properties.Lint.Warning_checks, updatabilityChecks)

			if len(filtered) != 0 {
				ctx.PropertyErrorf("lint.warning_checks",
					"Can't treat %v checks as warnings if min_sdk_version is different from sdk_version.", filtered)
			}
		}

		_, filtered := android.FilterList(l.properties.Lint.Disabled_checks, updatabilityChecks)
		if len(filtered) != 0 {
			ctx.PropertyErrorf("lint.disabled_checks",
				"Can't disable %v checks if min_sdk_version is different from sdk_version.", filtered)
		}

		// TODO(b/238784089): Remove this workaround when the NewApi issues have been addressed in PermissionController
		if ctx.ModuleName() == "PermissionController" {
			l.extraMainlineLintErrors = android.FilterListPred(l.extraMainlineLintErrors, func(s string) bool {
				return s != "NewApi"
			})
			l.properties.Lint.Warning_checks = append(l.properties.Lint.Warning_checks, "NewApi")
		}
	}

	extraLintCheckModules := ctx.GetDirectDepsProxyWithTag(extraLintCheckTag)
	for _, extraLintCheckModule := range extraLintCheckModules {
		if dep, ok := android.OtherModuleProvider(ctx, extraLintCheckModule, JavaInfoProvider); ok {
			l.extraLintCheckJars = append(l.extraLintCheckJars, dep.ImplementationAndResourcesJars...)
		} else {
			ctx.PropertyErrorf("lint.extra_check_modules",
				"%s is not a java module", ctx.OtherModuleName(extraLintCheckModule))
		}
	}

	l.extraLintCheckJars = append(l.extraLintCheckJars, android.PathForSource(ctx,
		"prebuilts/cmdline-tools/AndroidGlobalLintChecker.jar"))

	var baseline android.OptionalPath
	if l.properties.Lint.Baseline_filename != nil {
		baseline = android.OptionalPathForPath(android.PathForModuleSrc(ctx, *l.properties.Lint.Baseline_filename))
	}

	html := android.PathForModuleOut(ctx, "lint", "lint-report.html")
	text := android.PathForModuleOut(ctx, "lint", "lint-report.txt")
	xml := android.PathForModuleOut(ctx, "lint", "lint-report.xml")
	referenceBaseline := android.PathForModuleOut(ctx, "lint", "lint-baseline.xml")

	depSetsBuilder := NewLintDepSetBuilder().Direct(html, text, xml, baseline)

	ctx.VisitDirectDepsProxyWithTag(staticLibTag, func(dep android.ModuleProxy) {
		if info, ok := android.OtherModuleProvider(ctx, dep, LintProvider); ok {
			depSetsBuilder.Transitive(info)
		}
	})

	depSets := depSetsBuilder.Build()

	rule := android.NewRuleBuilder(pctx, ctx).
		Sbox(android.PathForModuleOut(ctx, "lint"),
			android.PathForModuleOut(ctx, "lint.sbox.textproto")).
		SandboxInputs()

	if ctx.Config().UseRBE() && ctx.Config().IsEnvTrue("RBE_LINT") {
		pool := ctx.Config().GetenvWithDefault("RBE_LINT_POOL", "java16")
		rule.Remoteable(android.RemoteRuleSupports{RBE: true})
		rule.Rewrapper(&remoteexec.REParams{
			Labels:          map[string]string{"type": "tool", "name": "lint"},
			ExecStrategy:    lintRBEExecStrategy(ctx),
			ToolchainInputs: []string{config.JavaCmd(ctx).String()},
			Platform:        map[string]string{remoteexec.PoolKey: pool},
		})
	}

	if l.manifest == nil {
		manifest := l.generateManifest(ctx, rule)
		l.manifest = manifest
		rule.Temporary(manifest)
	}

	srcsList := android.PathForModuleOut(ctx, "lint", "lint-srcs.list")
	srcsListRsp := android.PathForModuleOut(ctx, "lint-srcs.list.rsp")
	rule.Command().Text("cp").FlagWithRspFileInputList("", srcsListRsp, l.srcs).Output(srcsList).Implicits(l.compile_data)

	baselines := depSets.Baseline.ToList()

	lintPaths := l.writeLintProjectXML(ctx, rule, srcsList, baselines)

	rule.Command().Text("rm -rf").Flag(lintPaths.cacheDir.String()).Flag(lintPaths.homeDir.String())
	rule.Command().Text("mkdir -p").Flag(lintPaths.cacheDir.String()).Flag(lintPaths.homeDir.String())
	rule.Command().Text("rm -f").Output(html).Output(text).Output(xml)

	files, ok := allLintDatabasefiles[l.compileSdkKind]
	if !ok {
		files = allLintDatabasefiles[android.SdkPublic]
	}
	var annotationsZipPath, apiVersionsXMLPath android.Path
	if ctx.Config().AlwaysUsePrebuiltSdks() {
		annotationsZipPath = android.PathForSource(ctx, files.annotationPrebuiltpath)
		apiVersionsXMLPath = android.PathForSource(ctx, files.apiVersionsPrebuiltPath)
	} else {
		annotationsZipPath = copiedLintDatabaseFilesPath(ctx, files.annotationCopiedName)
		apiVersionsXMLPath = copiedLintDatabaseFilesPath(ctx, files.apiVersionsCopiedName)
	}

	cmd := rule.Command()

	cmd.Flag(`JAVA_OPTS="-Xmx4096m --add-opens java.base/java.util=ALL-UNNAMED"`).
		FlagWithArg("ANDROID_SDK_HOME=", lintPaths.homeDir.String()).
		FlagWithInput("SDK_ANNOTATIONS=", annotationsZipPath).
		FlagWithInput("LINT_OPTS=-DLINT_API_DATABASE=", apiVersionsXMLPath)

	cmd.BuiltTool("lint").ImplicitTool(ctx.Config().HostJavaToolPath(ctx, "lint.jar")).
		Flag("--quiet").
		Flag("--include-aosp-issues").
		FlagWithInput("--project ", lintPaths.projectXML).
		FlagWithInput("--config ", lintPaths.configXML).
		FlagWithOutput("--html ", html).
		FlagWithOutput("--text ", text).
		FlagWithOutput("--xml ", xml).
		FlagWithArg("--compile-sdk-version ", l.compileSdkVersion.String()).
		FlagWithArg("--java-language-level ", l.javaLanguageLevel).
		FlagWithArg("--kotlin-language-level ", l.kotlinLanguageLevel).
		FlagWithArg("--url ", fmt.Sprintf(".=.,%s=out", android.PathForOutput(ctx).String())).
		Flag("--apply-suggestions"). // applies suggested fixes to files in the sandbox
		Flags(l.properties.Lint.Flags).
		Implicit(annotationsZipPath).
		Implicit(apiVersionsXMLPath)

	rule.Temporary(lintPaths.projectXML)
	rule.Temporary(lintPaths.configXML)

	suppressExitCode := BoolDefault(l.properties.Lint.Suppress_exit_code, false)
	if exitCode := ctx.Config().Getenv("ANDROID_LINT_SUPPRESS_EXIT_CODE"); exitCode == "" && !suppressExitCode {
		cmd.Flag("--exitcode")
	}

	if checkOnly := ctx.Config().Getenv("ANDROID_LINT_CHECK"); checkOnly != "" {
		cmd.FlagWithArg("--check ", checkOnly)
	}

	if baseline.Valid() {
		cmd.FlagWithInput("--baseline ", baseline.Path())
	}

	cmd.FlagWithOutput("--write-reference-baseline ", referenceBaseline)

	cmd.Text("; EXITCODE=$?; ")

	// The sources in the sandbox may have been modified by --apply-suggestions, zip them up and
	// export them out of the sandbox.  Do this before exiting so that the suggestions exit even after
	// a fatal error.
	cmd.BuiltTool("soong_zip").
		FlagWithOutput("-o ", android.PathForModuleOut(ctx, "lint", "suggested-fixes.zip")).
		FlagWithArg("-C ", cmd.PathForInput(android.PathForSource(ctx))).
		FlagWithInput("-r ", srcsList)

	cmd.Text("; if [ $EXITCODE != 0 ]; then if [ -e").Input(text).Text("]; then cat").Input(text).Text("; fi; exit $EXITCODE; fi")

	rule.Command().Text("rm -rf").Flag(lintPaths.cacheDir.String()).Flag(lintPaths.homeDir.String())

	// The HTML output contains a date, remove it to make the output deterministic.
	rule.Command().Text(`sed -i.tmp -e 's|Check performed at .*\(</nav>\)|\1|'`).Output(html)

	rule.Build("lint", "lint")

	android.SetProvider(ctx, LintProvider, &LintInfo{
		HTML:              html,
		Text:              text,
		XML:               xml,
		ReferenceBaseline: referenceBaseline,

		TransitiveHTML:     depSets.HTML,
		TransitiveText:     depSets.Text,
		TransitiveXML:      depSets.XML,
		TransitiveBaseline: depSets.Baseline,
	})

	if l.buildModuleReportZip {
		l.reports = BuildModuleLintReportZips(ctx, depSets, nil)
	}

	// Create a per-module phony target to run the lint check.
	phonyName := ctx.ModuleName() + "-lint"
	ctx.Phony(phonyName, xml)

	ctx.SetOutputFiles(android.Paths{xml}, ".lint")
}

func BuildModuleLintReportZips(ctx android.ModuleContext, depSets LintDepSets, validations android.Paths) android.Paths {
	htmlList := android.SortedUniquePaths(depSets.HTML.ToList())
	textList := android.SortedUniquePaths(depSets.Text.ToList())
	xmlList := android.SortedUniquePaths(depSets.XML.ToList())

	if len(htmlList) == 0 && len(textList) == 0 && len(xmlList) == 0 {
		return nil
	}

	htmlZip := android.PathForModuleOut(ctx, "lint-report-html.zip")
	lintZip(ctx, htmlList, htmlZip, validations)

	textZip := android.PathForModuleOut(ctx, "lint-report-text.zip")
	lintZip(ctx, textList, textZip, validations)

	xmlZip := android.PathForModuleOut(ctx, "lint-report-xml.zip")
	lintZip(ctx, xmlList, xmlZip, validations)

	return android.Paths{htmlZip, textZip, xmlZip}
}

type lintSingleton struct {
	htmlZip              android.WritablePath
	textZip              android.WritablePath
	xmlZip               android.WritablePath
	referenceBaselineZip android.WritablePath
}

func (l *lintSingleton) GenerateBuildActions(ctx android.SingletonContext) {
	l.generateLintReportZips(ctx)
	l.copyLintDependencies(ctx)
}

func findModuleOrErr(ctx android.SingletonContext, moduleName string) *android.ModuleProxy {
	var res *android.ModuleProxy
	ctx.VisitAllModuleProxies(func(m android.ModuleProxy) {
		if ctx.ModuleName(m) == moduleName {
			if res == nil {
				res = &m
			} else {
				ctx.Errorf("lint: multiple %s modules found: %s and %s", moduleName,
					ctx.ModuleSubDir(m), ctx.ModuleSubDir(res))
			}
		}
	})
	return res
}

func (l *lintSingleton) copyLintDependencies(ctx android.SingletonContext) {
	if ctx.Config().AlwaysUsePrebuiltSdks() {
		return
	}

	for _, sdk := range android.SortedKeys(allLintDatabasefiles) {
		files := allLintDatabasefiles[sdk]
		apiVersionsDb := findModuleOrErr(ctx, files.apiVersionsModule)
		if apiVersionsDb == nil {
			if !ctx.Config().AllowMissingDependencies() {
				ctx.Errorf("lint: missing module %s", files.apiVersionsModule)
			}
			return
		}

		sdkAnnotations := findModuleOrErr(ctx, files.annotationsModule)
		if sdkAnnotations == nil {
			if !ctx.Config().AllowMissingDependencies() {
				ctx.Errorf("lint: missing module %s", files.annotationsModule)
			}
			return
		}

		ctx.Build(pctx, android.BuildParams{
			Rule:   android.CpIfChanged,
			Input:  android.OutputFileForModule(ctx, *sdkAnnotations, ""),
			Output: copiedLintDatabaseFilesPath(ctx, files.annotationCopiedName),
		})

		ctx.Build(pctx, android.BuildParams{
			Rule:   android.CpIfChanged,
			Input:  android.OutputFileForModule(ctx, *apiVersionsDb, ".api_versions.xml"),
			Output: copiedLintDatabaseFilesPath(ctx, files.apiVersionsCopiedName),
		})
	}
}

func copiedLintDatabaseFilesPath(ctx android.PathContext, name string) android.WritablePath {
	return android.PathForOutput(ctx, "lint", name)
}

func (l *lintSingleton) generateLintReportZips(ctx android.SingletonContext) {
	if ctx.Config().UnbundledBuild() {
		return
	}

	var outputs []*LintInfo
	var dirs []string
	ctx.VisitAllModuleProxies(func(m android.ModuleProxy) {
		commonInfo := android.OtherModulePointerProviderOrDefault(ctx, m, android.CommonModuleInfoProvider)
		if ctx.Config().KatiEnabled() && !commonInfo.ExportedToMake {
			return
		}

		if commonInfo.IsApexModule && commonInfo.NotAvailableForPlatform {
			apexInfo, _ := android.OtherModuleProvider(ctx, m, android.ApexInfoProvider)
			if apexInfo.IsForPlatform() {
				// There are stray platform variants of modules in apexes that are not available for
				// the platform, and they sometimes can't be built.  Don't depend on them.
				return
			}
		}

		if lintInfo, ok := android.OtherModuleProvider(ctx, m, LintProvider); ok {
			outputs = append(outputs, lintInfo)
		}
	})

	dirs = android.SortedUniqueStrings(dirs)

	zip := func(outputPath android.WritablePath, get func(*LintInfo) android.Path) {
		var paths android.Paths

		for _, output := range outputs {
			if p := get(output); p != nil {
				paths = append(paths, p)
			}
		}

		lintZip(ctx, paths, outputPath, nil)
	}

	l.htmlZip = android.PathForOutput(ctx, "lint-report-html.zip")
	zip(l.htmlZip, func(l *LintInfo) android.Path { return l.HTML })

	l.textZip = android.PathForOutput(ctx, "lint-report-text.zip")
	zip(l.textZip, func(l *LintInfo) android.Path { return l.Text })

	l.xmlZip = android.PathForOutput(ctx, "lint-report-xml.zip")
	zip(l.xmlZip, func(l *LintInfo) android.Path { return l.XML })

	l.referenceBaselineZip = android.PathForOutput(ctx, "lint-report-reference-baselines.zip")
	zip(l.referenceBaselineZip, func(l *LintInfo) android.Path { return l.ReferenceBaseline })

	ctx.Phony("lint-check", l.htmlZip, l.textZip, l.xmlZip, l.referenceBaselineZip)

	if !ctx.Config().UnbundledBuild() {
		ctx.DistForGoal("lint-check", l.htmlZip, l.textZip, l.xmlZip, l.referenceBaselineZip)
	}
}

func init() {
	android.RegisterParallelSingletonType("lint",
		func() android.Singleton { return &lintSingleton{} })
}

func lintZip(ctx android.BuilderContext, paths android.Paths, outputPath android.WritablePath, validations android.Paths) {
	paths = android.SortedUniquePaths(android.CopyOfPaths(paths))

	sort.Slice(paths, func(i, j int) bool {
		return paths[i].String() < paths[j].String()
	})

	rule := android.NewRuleBuilder(pctx, ctx)

	rule.Command().BuiltTool("soong_zip").
		FlagWithOutput("-o ", outputPath).
		FlagWithArg("-C ", android.PathForIntermediates(ctx).String()).
		FlagWithRspFileInputList("-r ", outputPath.ReplaceExtension(ctx, "rsp"), paths).
		Validations(validations)

	rule.Build(outputPath.Base(), outputPath.Base())
}
