// Copyright 2017 Google Inc. All rights reserved.
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

package build

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"android/soong/ui/tracer"

	"android/soong/ui/metrics"
	"android/soong/ui/metrics/metrics_proto"
	"android/soong/ui/status"

	"android/soong/shared"

	"github.com/google/blueprint"
	"github.com/google/blueprint/bootstrap"
	"github.com/google/blueprint/microfactory"
	"github.com/google/blueprint/pathtools"

	"google.golang.org/protobuf/proto"
)

const (
	availableEnvFile = "soong.environment.available"
	usedEnvFile      = "soong.environment.used"

	soongBuildTag      = "build"
	jsonModuleGraphTag = "modulegraph"
	soongDocsTag       = "soong_docs"

	// bootstrapEpoch is used to determine if an incremental build is incompatible with the current
	// version of bootstrap and needs cleaning before continuing the build.  Increment this for
	// incompatible changes, for example when moving the location of a microfactory binary that is
	// executed during bootstrap before the primary builder has had a chance to update the path.
	bootstrapEpoch = 1
)

var (
	// Used during parallel update of symlinks in out directory to reflect new
	// TOP dir.
	symlinkWg            sync.WaitGroup
	numFound, numUpdated uint32
)

func writeEnvironmentFile(_ Context, envFile string, envDeps map[string]string) error {
	data, err := shared.EnvFileContents(envDeps)
	if err != nil {
		return err
	}

	return os.WriteFile(envFile, data, 0644)
}

// This uses Android.bp files and various tools to generate <builddir>/build.ninja.
//
// However, the execution of <builddir>/build.ninja happens later in
// build/soong/ui/build/build.go#Build()
//
// We want to rely on as few prebuilts as possible, so we need to bootstrap
// Soong. The process is as follows:
//
// 1. We use "Microfactory", a simple tool to compile Go code, to build
//    first itself, then soong_ui from soong_ui.bash. This binary contains
//    parts of soong_build that are needed to build itself.
// 2. This simplified version of soong_build then reads the Blueprint files
//    that describe itself and emits .bootstrap/build.ninja that describes
//    how to build its full version and use that to produce the final Ninja
//    file Soong emits.
// 3. soong_ui executes .bootstrap/build.ninja
//
// (After this, Kati is executed to parse the Makefiles, but that's not part of
// bootstrapping Soong)

// A tiny struct used to tell Blueprint that it's in bootstrap mode. It would
// probably be nicer to use a flag in bootstrap.Args instead.
type BlueprintConfig struct {
	toolDir                   string
	soongOutDir               string
	outDir                    string
	runGoTests                bool
	debugCompilation          bool
	subninjas                 []string
	primaryBuilderInvocations []bootstrap.PrimaryBuilderInvocation
}

func (c BlueprintConfig) HostToolDir() string {
	return c.toolDir
}

func (c BlueprintConfig) SoongOutDir() string {
	return c.soongOutDir
}

func (c BlueprintConfig) OutDir() string {
	return c.outDir
}

func (c BlueprintConfig) RunGoTests() bool {
	return c.runGoTests
}

func (c BlueprintConfig) DebugCompilation() bool {
	return c.debugCompilation
}

func (c BlueprintConfig) Subninjas() []string {
	return c.subninjas
}

func (c BlueprintConfig) PrimaryBuilderInvocations() []bootstrap.PrimaryBuilderInvocation {
	return c.primaryBuilderInvocations
}

func environmentArgs(config Config, tag string) []string {
	return []string{
		"--available_env", shared.JoinPath(config.SoongOutDir(), availableEnvFile),
		"--used_env", config.UsedEnvFile(tag),
	}
}

func writeEmptyFile(ctx Context, path string) {
	err := os.MkdirAll(filepath.Dir(path), 0777)
	if err != nil {
		ctx.Fatalf("Failed to create parent directories of empty file '%s': %s", path, err)
	}

	if exists, err := fileExists(path); err != nil {
		ctx.Fatalf("Failed to check if file '%s' exists: %s", path, err)
	} else if !exists {
		err = os.WriteFile(path, nil, 0666)
		if err != nil {
			ctx.Fatalf("Failed to create empty file '%s': %s", path, err)
		}
	}
}

func fileExists(path string) (bool, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return true, nil
}

type PrimaryBuilderFactory struct {
	name         string
	description  string
	config       Config
	output       string
	specificArgs []string
	debugPort    string
}

func getGlobPathName(config Config) string {
	globPathName, ok := config.TargetProductOrErr()
	if ok != nil {
		globPathName = soongBuildTag
	}
	return globPathName
}

func getGlobPathNameFromPrimaryBuilderFactory(config Config, pb PrimaryBuilderFactory) string {
	if pb.name == soongBuildTag {
		// Glob path for soong build would be separated per product target
		return getGlobPathName(config)
	}
	return pb.name
}

func (pb PrimaryBuilderFactory) primaryBuilderInvocation(config Config) bootstrap.PrimaryBuilderInvocation {
	commonArgs := make([]string, 0, 0)

	commonArgs = append(commonArgs, "--kati_suffix", config.KatiSuffix())

	if !pb.config.skipSoongTests {
		commonArgs = append(commonArgs, "-t")
	}

	if pb.config.buildFromSourceStub {
		commonArgs = append(commonArgs, "--build-from-source-stub")
	}

	if pb.config.moduleDebugFile != "" {
		commonArgs = append(commonArgs, "--soong_module_debug")
		commonArgs = append(commonArgs, pb.config.moduleDebugFile)
	}

	commonArgs = append(commonArgs, "-l", filepath.Join(pb.config.FileListDir(), "Android.bp.list"))
	invocationEnv := make(map[string]string)
	if pb.debugPort != "" {
		//debug mode
		commonArgs = append(commonArgs, "--delve_listen", pb.debugPort,
			"--delve_path", shared.ResolveDelveBinary())
		// GODEBUG=asyncpreemptoff=1 disables the preemption of goroutines. This
		// is useful because the preemption happens by sending SIGURG to the OS
		// thread hosting the goroutine in question and each signal results in
		// work that needs to be done by Delve; it uses ptrace to debug the Go
		// process and the tracer process must deal with every signal (it is not
		// possible to selectively ignore SIGURG). This makes debugging slower,
		// sometimes by an order of magnitude depending on luck.
		// The original reason for adding async preemption to Go is here:
		// https://github.com/golang/proposal/blob/master/design/24543-non-cooperative-preemption.md
		invocationEnv["GODEBUG"] = "asyncpreemptoff=1"
	}

	var allArgs []string
	allArgs = append(allArgs, pb.specificArgs...)

	allArgs = append(allArgs, commonArgs...)
	allArgs = append(allArgs, environmentArgs(pb.config, pb.name)...)
	if profileCpu := os.Getenv("SOONG_PROFILE_CPU"); profileCpu != "" {
		allArgs = append(allArgs, "--cpuprofile", profileCpu+"."+pb.name)
	}
	if profileMem := os.Getenv("SOONG_PROFILE_MEM"); profileMem != "" {
		allArgs = append(allArgs, "--memprofile", profileMem+"."+pb.name)
	}
	allArgs = append(allArgs, "Android.bp")

	return bootstrap.PrimaryBuilderInvocation{
		Implicits:   []string{pb.output + ".glob_results"},
		Outputs:     []string{pb.output},
		Args:        allArgs,
		Description: pb.description,
		// NB: Changing the value of this environment variable will not result in a
		// rebuild. The bootstrap Ninja file will change, but apparently Ninja does
		// not consider changing the pool specified in a statement a change that's
		// worth rebuilding for.
		Console: os.Getenv("SOONG_UNBUFFERED_OUTPUT") == "1",
		Env:     invocationEnv,
	}
}

// bootstrapEpochCleanup deletes files used by bootstrap during incremental builds across
// incompatible changes.  Incompatible changes are marked by incrementing the bootstrapEpoch
// constant.  A tree is considered out of date for the current epoch of the
// .soong.bootstrap.epoch.<epoch> file doesn't exist.
func bootstrapEpochCleanup(ctx Context, config Config) {
	epochFile := fmt.Sprintf(".soong.bootstrap.epoch.%d", bootstrapEpoch)
	epochPath := filepath.Join(config.SoongOutDir(), epochFile)
	if exists, err := fileExists(epochPath); err != nil {
		ctx.Fatalf("failed to check if bootstrap epoch file %q exists: %q", epochPath, err)
	} else if !exists {
		// The tree is out of date for the current epoch, delete files used by bootstrap
		// and force the primary builder to rerun.
		soongNinjaFile := config.SoongNinjaFile()
		os.Remove(soongNinjaFile)
		for _, file := range blueprint.GetNinjaShardFiles(soongNinjaFile) {
			if ok, _ := fileExists(file); ok {
				os.Remove(file)
			}
		}
		os.Remove(soongNinjaFile + ".globs")
		os.Remove(soongNinjaFile + ".globs_time")
		os.Remove(soongNinjaFile + ".glob_results")

		// Mark the tree as up to date with the current epoch by writing the epoch marker file.
		writeEmptyFile(ctx, epochPath)
	}
}

func bootstrapBlueprint(ctx Context, config Config) {
	ctx.BeginTrace(metrics.RunSoong, "blueprint bootstrap")
	defer ctx.EndTrace()

	st := ctx.Status.StartTool()
	defer st.Finish()
	st.SetTotalActions(1)
	action := &status.Action{
		Description: "bootstrap blueprint",
		Outputs:     []string{"bootstrap blueprint"},
	}
	st.StartAction(action)

	// Clean up some files for incremental builds across incompatible changes.
	bootstrapEpochCleanup(ctx, config)

	baseArgs := []string{"--soong_variables", config.SoongVarsFile()}

	mainSoongBuildExtraArgs := append(baseArgs, "-o", config.SoongNinjaFile())
	if config.EmptyNinjaFile() {
		mainSoongBuildExtraArgs = append(mainSoongBuildExtraArgs, "--empty-ninja-file")
	}
	if config.buildFromSourceStub {
		mainSoongBuildExtraArgs = append(mainSoongBuildExtraArgs, "--build-from-source-stub")
	}
	if config.ensureAllowlistIntegrity {
		mainSoongBuildExtraArgs = append(mainSoongBuildExtraArgs, "--ensure-allowlist-integrity")
	}
	if config.incrementalBuildActions {
		mainSoongBuildExtraArgs = append(mainSoongBuildExtraArgs, "--incremental-build-actions")
	}

	pbfs := []PrimaryBuilderFactory{
		{
			name:         soongBuildTag,
			description:  fmt.Sprintf("analyzing Android.bp files and generating ninja file at %s", config.SoongNinjaFile()),
			config:       config,
			output:       config.SoongNinjaFile(),
			specificArgs: mainSoongBuildExtraArgs,
		},
		{
			name:        jsonModuleGraphTag,
			description: fmt.Sprintf("generating the Soong module graph at %s", config.ModuleGraphFile()),
			config:      config,
			output:      config.ModuleGraphFile(),
			specificArgs: append(baseArgs,
				"--module_graph_file", config.ModuleGraphFile(),
				"--module_actions_file", config.ModuleActionsFile(),
			),
		},
		{
			name:        soongDocsTag,
			description: fmt.Sprintf("generating Soong docs at %s", config.SoongDocsHtml()),
			config:      config,
			output:      config.SoongDocsHtml(),
			specificArgs: append(baseArgs,
				"--soong_docs", config.SoongDocsHtml(),
			),
		},
	}

	// Figure out which invocations will be run under the debugger:
	//   * SOONG_DELVE if set specifies listening port
	//   * SOONG_DELVE_STEPS if set specifies specific invocations to be debugged, otherwise all are
	debuggedInvocations := make(map[string]bool)
	delvePort := os.Getenv("SOONG_DELVE")
	if delvePort != "" {
		if steps := os.Getenv("SOONG_DELVE_STEPS"); steps != "" {
			var validSteps []string
			for _, pbf := range pbfs {
				debuggedInvocations[pbf.name] = false
				validSteps = append(validSteps, pbf.name)

			}
			for _, step := range strings.Split(steps, ",") {
				if _, ok := debuggedInvocations[step]; ok {
					debuggedInvocations[step] = true
				} else {
					ctx.Fatalf("SOONG_DELVE_STEPS contains unknown soong_build step %s\n"+
						"Valid steps are %v", step, validSteps)
				}
			}
		} else {
			//  SOONG_DELVE_STEPS is not set, run all steps in the debugger
			for _, pbf := range pbfs {
				debuggedInvocations[pbf.name] = true
			}
		}
	}

	var invocations []bootstrap.PrimaryBuilderInvocation
	for _, pbf := range pbfs {
		if debuggedInvocations[pbf.name] {
			pbf.debugPort = delvePort
		}
		pbi := pbf.primaryBuilderInvocation(config)
		invocations = append(invocations, pbi)
	}

	blueprintArgs := bootstrap.Args{
		ModuleListFile: filepath.Join(config.FileListDir(), "Android.bp.list"),
		OutFile:        shared.JoinPath(config.SoongOutDir(), "bootstrap.ninja"),
		EmptyNinjaFile: false,
	}

	blueprintCtx := blueprint.NewContext()
	blueprintCtx.AddSourceRootDirs(config.GetSourceRootDirs()...)
	blueprintCtx.SetIgnoreUnknownModuleTypes(true)
	blueprintConfig := BlueprintConfig{
		soongOutDir: config.SoongOutDir(),
		toolDir:     config.HostToolDir(),
		outDir:      config.OutDir(),
		runGoTests:  !config.skipSoongTests,
		// If we want to debug soong_build, we need to compile it for debugging
		debugCompilation:          delvePort != "",
		primaryBuilderInvocations: invocations,
	}

	// since `bootstrap.ninja` is regenerated unconditionally, we ignore the deps, i.e. little
	// reason to write a `bootstrap.ninja.d` file
	_, err := bootstrap.RunBlueprint(blueprintArgs, bootstrap.DoEverything, blueprintCtx, blueprintConfig)

	result := status.ActionResult{
		Action: action,
	}
	if err != nil {
		result.Error = err
		result.Output = err.Error()
	}
	st.FinishAction(result)
	if err != nil {
		ctx.Fatalf("bootstrap failed")
	}
}

func checkEnvironmentFile(ctx Context, currentEnv *Environment, envFile string) {
	getenv := func(k string) string {
		v, _ := currentEnv.Get(k)
		return v
	}

	// Log the changed environment variables to ChangedEnvironmentVariable field
	if stale, changedEnvironmentVariableList, _ := shared.StaleEnvFile(envFile, getenv); stale {
		for _, changedEnvironmentVariable := range changedEnvironmentVariableList {
			ctx.Metrics.AddChangedEnvironmentVariable(changedEnvironmentVariable)
		}
		os.Remove(envFile)
	}
}

func updateSymlinks(ctx Context, dir, prevCWD, cwd string, updateSemaphore chan struct{}) error {
	defer symlinkWg.Done()

	visit := func(path string, d fs.DirEntry, err error) error {
		if d.IsDir() && path != dir {
			symlinkWg.Add(1)
			go updateSymlinks(ctx, path, prevCWD, cwd, updateSemaphore)
			return filepath.SkipDir
		}
		f, err := d.Info()
		if err != nil {
			return err
		}
		// If the file is not a symlink, we don't have to update it.
		if f.Mode()&os.ModeSymlink != os.ModeSymlink {
			return nil
		}

		atomic.AddUint32(&numFound, 1)
		target, err := os.Readlink(path)
		if err != nil {
			return err
		}
		if strings.HasPrefix(target, prevCWD) &&
			(len(target) == len(prevCWD) || target[len(prevCWD)] == '/') {
			target = filepath.Join(cwd, target[len(prevCWD):])
			if err := os.Remove(path); err != nil {
				return err
			}
			if err := os.Symlink(target, path); err != nil {
				return err
			}
			atomic.AddUint32(&numUpdated, 1)
		}
		return nil
	}

	<-updateSemaphore
	defer func() { updateSemaphore <- struct{}{} }()
	if err := filepath.WalkDir(dir, visit); err != nil {
		return err
	}
	return nil
}

// b/376466642: If the concurrency of updateSymlinks is unbounded, Go's runtime spawns a
// theoretically unbounded number of threads to handle blocking syscalls. This causes the runtime to
// panic due to hitting thread limits in rare cases. Limiting to GOMAXPROCS concurrent symlink
// updates should make this a non-issue.
func newUpdateSemaphore() chan struct{} {
	numPermits := runtime.GOMAXPROCS(0)
	c := make(chan struct{}, numPermits)
	for i := 0; i < numPermits; i++ {
		c <- struct{}{}
	}
	return c
}

func fixOutDirSymlinks(ctx Context, config Config, outDir string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	// Record the .top as the very last thing in the function.
	tf := filepath.Join(outDir, ".top")
	defer func() {
		if err := os.WriteFile(tf, []byte(cwd), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "Unable to log CWD: %v", err)
		}
	}()

	// Find the previous working directory if it was recorded.
	var prevCWD string
	pcwd, err := os.ReadFile(tf)
	if err != nil {
		if os.IsNotExist(err) {
			// No previous working directory recorded, nothing to do.
			return nil
		}
		return err
	}
	prevCWD = strings.Trim(string(pcwd), "\n")

	if prevCWD == cwd {
		// We are in the same source dir, nothing to update.
		return nil
	}

	symlinkWg.Add(1)
	if err := updateSymlinks(ctx, outDir, prevCWD, cwd, newUpdateSemaphore()); err != nil {
		return err
	}
	symlinkWg.Wait()
	ctx.Println(fmt.Sprintf("Updated %d/%d symlinks in dir %v", numUpdated, numFound, outDir))
	return nil
}

func migrateOutputSymlinks(ctx Context, config Config) error {
	// Figure out the real out directory ("out" could be a symlink).
	outDir := config.OutDir()
	s, err := os.Lstat(outDir)
	if err != nil {
		if os.IsNotExist(err) {
			// No out dir exists, no symlinks to migrate.
			return nil
		}
		return err
	}
	if s.Mode()&os.ModeSymlink == os.ModeSymlink {
		target, err := filepath.EvalSymlinks(outDir)
		if err != nil {
			return err
		}
		outDir = target
	}
	return fixOutDirSymlinks(ctx, config, outDir)
}

func runSoong(ctx Context, config Config) {
	ctx.BeginTrace(metrics.RunSoong, "soong")
	defer ctx.EndTrace()

	if err := migrateOutputSymlinks(ctx, config); err != nil {
		ctx.Fatalf("failed to migrate output directory to current TOP dir: %v", err)
	}

	// We have two environment files: .available is the one with every variable,
	// .used with the ones that were actually used. The latter is used to
	// determine whether Soong needs to be re-run since why re-run it if only
	// unused variables were changed?
	envFile := filepath.Join(config.SoongOutDir(), availableEnvFile)

	// This is done unconditionally, but does not take a measurable amount of time
	bootstrapBlueprint(ctx, config)

	soongBuildEnv := config.Environment().Copy()
	soongBuildEnv.Set("TOP", os.Getenv("TOP"))
	soongBuildEnv.Set("LOG_DIR", config.LogsDir())

	// For Soong bootstrapping tests
	if os.Getenv("ALLOW_MISSING_DEPENDENCIES") == "true" {
		soongBuildEnv.Set("ALLOW_MISSING_DEPENDENCIES", "true")
	}

	err := writeEnvironmentFile(ctx, envFile, soongBuildEnv.AsMap())
	if err != nil {
		ctx.Fatalf("failed to write environment file %s: %s", envFile, err)
	}

	func() {
		ctx.BeginTrace(metrics.RunSoong, "environment check")
		defer ctx.EndTrace()

		checkEnvironmentFile(ctx, soongBuildEnv, config.UsedEnvFile(soongBuildTag))

		if config.JsonModuleGraph() {
			checkEnvironmentFile(ctx, soongBuildEnv, config.UsedEnvFile(jsonModuleGraphTag))
		}

		if config.SoongDocs() {
			checkEnvironmentFile(ctx, soongBuildEnv, config.UsedEnvFile(soongDocsTag))
		}
	}()

	ninja := func(targets ...string) {
		ctx.BeginTrace(metrics.RunSoong, "bootstrap")
		defer ctx.EndTrace()

		fifo := filepath.Join(config.OutDir(), ".ninja_fifo")
		nr := status.NewNinjaReader(ctx, ctx.Status.StartTool(), fifo)
		defer nr.Close()

		var ninjaCmd string
		var ninjaArgs []string
		switch config.ninjaCommand {
		case NINJA_N2:
			ninjaCmd = config.N2Bin()
			ninjaArgs = []string{
				// TODO: implement these features, or remove them.
				//"-d", "keepdepfile",
				//"-d", "stats",
				//"-o", "usesphonyoutputs=yes",
				//"-o", "preremoveoutputs=yes",
				//"-w", "dupbuild=err",
				//"-w", "outputdir=err",
				//"-w", "missingoutfile=err",
				"-v",
				"-j", strconv.Itoa(config.Parallel()),
				"--frontend-file", fifo,
				"-f", filepath.Join(config.SoongOutDir(), "bootstrap.ninja"),
			}
		case NINJA_SISO:
			ninjaCmd = config.SisoBin()
			ninjaArgs = []string{
				"ninja",
				// TODO: implement these features, or remove them.
				//"-d", "keepdepfile",
				//"-d", "stats",
				//"-o", "usesphonyoutputs=yes",
				//"-o", "preremoveoutputs=yes",
				//"-w", "dupbuild=err",
				//"-w", "outputdir=err",
				//"-w", "missingoutfile=err",
				"-v",
				"-j", strconv.Itoa(config.Parallel()),
				//"--frontend-file", fifo,
				"--log_dir", config.SoongOutDir(),
				"-f", filepath.Join(config.SoongOutDir(), "bootstrap.ninja"),
			}
		default:
			// NINJA_NINJA is the default.
			ninjaCmd = config.NinjaBin()
			ninjaArgs = []string{
				"-d", "keepdepfile",
				"-d", "stats",
				"-o", "usesphonyoutputs=yes",
				"-o", "preremoveoutputs=yes",
				"-w", "dupbuild=err",
				"-w", "outputdir=err",
				"-w", "missingoutfile=err",
				"-j", strconv.Itoa(config.Parallel()),
				"--frontend_file", fifo,
				"-f", filepath.Join(config.SoongOutDir(), "bootstrap.ninja"),
			}
		}

		if extra, ok := config.Environment().Get("SOONG_UI_NINJA_ARGS"); ok {
			ctx.Printf(`CAUTION: arguments in $SOONG_UI_NINJA_ARGS=%q, e.g. "-n", can make soong_build FAIL or INCORRECT`, extra)
			ninjaArgs = append(ninjaArgs, strings.Fields(extra)...)
		}

		ninjaArgs = append(ninjaArgs, targets...)

		cmd := Command(ctx, config, "soong bootstrap",
			ninjaCmd, ninjaArgs...)

		var ninjaEnv Environment

		// This is currently how the command line to invoke soong_build finds the
		// root of the source tree and the output root
		ninjaEnv.Set("TOP", os.Getenv("TOP"))

		cmd.Environment = &ninjaEnv
		cmd.Sandbox = soongSandbox
		cmd.RunAndStreamOrFatal()
	}

	targets := make([]string, 0, 0)

	if config.JsonModuleGraph() {
		targets = append(targets, config.ModuleGraphFile())
	}

	if config.SoongDocs() {
		targets = append(targets, config.SoongDocsHtml())
	}

	if config.SoongBuildInvocationNeeded() {
		// This build generates <builddir>/build.ninja, which is used later by build/soong/ui/build/build.go#Build().
		targets = append(targets, config.SoongNinjaFile())
	}

	for _, target := range targets {
		if err := checkGlobs(ctx, target); err != nil {
			ctx.Fatalf("Error checking globs: %s", err.Error())
		}
	}

	beforeSoongTimestamp := time.Now()

	ninja(targets...)

	loadSoongBuildMetrics(ctx, config, beforeSoongTimestamp)

	soongNinjaFile := config.SoongNinjaFile()
	distGzipFile(ctx, config, soongNinjaFile, "soong")
	for _, file := range blueprint.GetNinjaShardFiles(soongNinjaFile) {
		if ok, _ := fileExists(file); ok {
			distGzipFile(ctx, config, file, "soong")
		}
	}
	distFile(ctx, config, config.SoongVarsFile(), "soong")
	distFile(ctx, config, config.SoongExtraVarsFile(), "soong")

	if !config.SkipKati() {
		distGzipFile(ctx, config, config.SoongAndroidMk(), "soong")
		distGzipFile(ctx, config, config.SoongMakeVarsMk(), "soong")
	}

	if config.JsonModuleGraph() {
		distGzipFile(ctx, config, config.ModuleGraphFile(), "soong")
	}
}

// checkGlobs manages the globs that cause soong to rerun.
//
// When soong_build runs, it will run globs. It will write all the globs
// it ran into the "{finalOutFile}.globs" file. Then every build,
// soong_ui will check that file, rerun the globs, and if they changed
// from the results that soong_build got, update the ".glob_results"
// file, causing soong_build to rerun. The ".glob_results" file will
// be empty on the first run of soong_build, because we don't know
// what the globs are yet, but also remain empty until the globs change
// so that we don't run soong_build a second time unnecessarily.
// Both soong_build and soong_ui will also update a ".globs_time" file
// with the time that they ran at every build. When soong_ui checks
// globs, it only reruns globs whose dependencies are newer than the
// time in the ".globs_time" file.
func checkGlobs(ctx Context, finalOutFile string) error {
	ctx.BeginTrace(metrics.RunSoong, "check_globs")
	defer ctx.EndTrace()
	st := ctx.Status.StartTool()
	st.Status("Running globs...")
	defer st.Finish()

	globsFile, err := os.Open(finalOutFile + ".globs")
	if errors.Is(err, fs.ErrNotExist) {
		// if the glob file doesn't exist, make sure the glob_results file exists and is empty.
		if err := os.MkdirAll(filepath.Dir(finalOutFile), 0777); err != nil {
			return err
		}
		f, err := os.Create(finalOutFile + ".glob_results")
		if err != nil {
			return err
		}
		return f.Close()
	} else if err != nil {
		return err
	}
	defer globsFile.Close()
	globsFileDecoder := json.NewDecoder(globsFile)

	globsTimeBytes, err := os.ReadFile(finalOutFile + ".globs_time")
	if err != nil {
		return err
	}
	globsTimeMicros, err := strconv.ParseInt(strings.TrimSpace(string(globsTimeBytes)), 10, 64)
	if err != nil {
		return err
	}
	globCheckStartTime := time.Now().UnixMicro()

	globsChan := make(chan pathtools.GlobResult)
	errorsChan := make(chan error)
	wg := sync.WaitGroup{}

	hasChangedGlobs := false
	var changedGlobNameMutex sync.Mutex
	var changedGlobName string

	for i := 0; i < runtime.NumCPU()*2; i++ {
		wg.Add(1)
		go func() {
			for cachedGlob := range globsChan {
				// If we've already determined we have changed globs, just finish consuming
				// the channel without doing any more checks.
				if hasChangedGlobs {
					continue
				}
				// First, check if any of the deps are newer than the last time globs were checked.
				// If not, we don't need to rerun the glob.
				hasNewDep := false
				for _, dep := range cachedGlob.Deps {
					info, err := os.Stat(dep)
					if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
						hasNewDep = true
						break
					} else if err != nil {
						errorsChan <- err
						continue
					}
					if info.ModTime().UnixMicro() > globsTimeMicros {
						hasNewDep = true
						break
					}
				}
				if !hasNewDep {
					continue
				}

				// Then rerun the glob and check if we got the same result as before.
				result, err := pathtools.Glob(cachedGlob.Pattern, cachedGlob.Excludes, pathtools.FollowSymlinks)
				if err != nil {
					errorsChan <- err
				} else {
					if !slices.Equal(result.Matches, cachedGlob.Matches) {
						hasChangedGlobs = true

						changedGlobNameMutex.Lock()
						defer changedGlobNameMutex.Unlock()
						changedGlobName = result.Pattern
						if len(result.Excludes) > 2 {
							changedGlobName += fmt.Sprintf(" (excluding %d other patterns)", len(result.Excludes))
						} else if len(result.Excludes) > 0 {
							changedGlobName += " (excluding " + strings.Join(result.Excludes, " and ") + ")"
						}
					}
				}
			}
			wg.Done()
		}()
	}
	go func() {
		wg.Wait()
		close(errorsChan)
	}()

	errorsWg := sync.WaitGroup{}
	errorsWg.Add(1)
	var errFromGoRoutines error
	go func() {
		for result := range errorsChan {
			if errFromGoRoutines == nil {
				errFromGoRoutines = result
			}
		}
		errorsWg.Done()
	}()

	var cachedGlob pathtools.GlobResult
	for globsFileDecoder.More() {
		if err := globsFileDecoder.Decode(&cachedGlob); err != nil {
			return err
		}
		// Need to clone the GlobResult because the json decoder will
		// reuse the same slice allocations.
		globsChan <- cachedGlob.Clone()
	}
	close(globsChan)
	errorsWg.Wait()
	if errFromGoRoutines != nil {
		return errFromGoRoutines
	}

	// Update the globs_time file whether or not we found changed globs,
	// so that we don't rerun globs in the future that we just saw didn't change.
	err = os.WriteFile(
		finalOutFile+".globs_time",
		[]byte(fmt.Sprintf("%d\n", globCheckStartTime)),
		0666,
	)
	if err != nil {
		return err
	}

	if hasChangedGlobs {
		fmt.Fprintf(os.Stdout, "Globs changed, rerunning soong...\n")
		fmt.Fprintf(os.Stdout, "One culprit glob (may be more): %s\n", changedGlobName)
		// Write the current time to the glob_results file. We just need
		// some unique value to trigger a rerun, it doesn't matter what it is.
		err = os.WriteFile(
			finalOutFile+".glob_results",
			[]byte(fmt.Sprintf("%d\n", globCheckStartTime)),
			0666,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

// loadSoongBuildMetrics reads out/soong_build_metrics.pb if it was generated by soong_build and copies the
// events stored in it into the soong_ui trace to provide introspection into how long the different phases of
// soong_build are taking.
func loadSoongBuildMetrics(ctx Context, config Config, oldTimestamp time.Time) {
	soongBuildMetricsFile := config.SoongBuildMetrics()
	if metricsStat, err := os.Stat(soongBuildMetricsFile); err != nil {
		ctx.Verbosef("Failed to stat %s: %s", soongBuildMetricsFile, err)
		return
	} else if !metricsStat.ModTime().After(oldTimestamp) {
		ctx.Verbosef("%s timestamp not later after running soong, expected %s > %s",
			soongBuildMetricsFile, metricsStat.ModTime(), oldTimestamp)
		return
	}

	metricsData, err := os.ReadFile(soongBuildMetricsFile)
	if err != nil {
		ctx.Verbosef("Failed to read %s: %s", soongBuildMetricsFile, err)
		return
	}

	soongBuildMetrics := metrics_proto.SoongBuildMetrics{}
	err = proto.Unmarshal(metricsData, &soongBuildMetrics)
	if err != nil {
		ctx.Verbosef("Failed to unmarshal %s: %s", soongBuildMetricsFile, err)
		return
	}
	for _, event := range soongBuildMetrics.Events {
		desc := event.GetDescription()
		if dot := strings.LastIndexByte(desc, '.'); dot >= 0 {
			desc = desc[dot+1:]
		}
		ctx.Tracer.Complete(desc, ctx.Thread,
			event.GetStartTime(), event.GetStartTime()+event.GetRealTime())
	}
	for _, event := range soongBuildMetrics.PerfCounters {
		timestamp := event.GetTime()
		for _, group := range event.Groups {
			counters := make([]tracer.Counter, 0, len(group.Counters))
			for _, counter := range group.Counters {
				counters = append(counters, tracer.Counter{
					Name:  counter.GetName(),
					Value: counter.GetValue(),
				})
			}
			ctx.Tracer.CountersAtTime(group.GetName(), ctx.Thread, timestamp, counters)
		}
	}
}

func runMicrofactory(ctx Context, config Config, name string, pkg string, mapping map[string]string) {
	ctx.BeginTrace(metrics.RunSoong, name)
	defer ctx.EndTrace()
	cfg := microfactory.Config{TrimPath: absPath(ctx, ".")}
	for pkgPrefix, pathPrefix := range mapping {
		cfg.Map(pkgPrefix, pathPrefix)
	}

	exePath := filepath.Join(config.SoongOutDir(), name)
	dir := filepath.Dir(exePath)
	if err := os.MkdirAll(dir, 0777); err != nil {
		ctx.Fatalf("cannot create %s: %s", dir, err)
	}
	if _, err := microfactory.Build(&cfg, exePath, pkg); err != nil {
		ctx.Fatalf("failed to build %s: %s", name, err)
	}
}
