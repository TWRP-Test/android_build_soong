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

package android

import (
	"slices"
	"sync"

	"github.com/google/blueprint"
)

// SingletonContext
type SingletonContext interface {
	blueprintSingletonContext() blueprint.SingletonContext

	Config() Config
	DeviceConfig() DeviceConfig

	ModuleName(module blueprint.Module) string
	ModuleDir(module blueprint.Module) string
	ModuleSubDir(module blueprint.Module) string
	ModuleType(module blueprint.Module) string
	BlueprintFile(module blueprint.Module) string

	// ModuleVariantsFromName returns the list of module variants named `name` in the same namespace as `referer` enforcing visibility rules.
	// Allows generating build actions for `referer` based on the metadata for `name` deferred until the singleton context.
	ModuleVariantsFromName(referer ModuleProxy, name string) []ModuleProxy

	otherModuleProvider(module blueprint.Module, provider blueprint.AnyProviderKey) (any, bool)

	ModuleErrorf(module blueprint.Module, format string, args ...interface{})
	Errorf(format string, args ...interface{})
	Failed() bool

	Variable(pctx PackageContext, name, value string)
	Rule(pctx PackageContext, name string, params blueprint.RuleParams, argNames ...string) blueprint.Rule
	Build(pctx PackageContext, params BuildParams)

	// Phony creates a Make-style phony rule, a rule with no commands that can depend on other
	// phony rules or real files.  Phony can be called on the same name multiple times to add
	// additional dependencies.
	Phony(name string, deps ...Path)

	RequireNinjaVersion(major, minor, micro int)

	// SetOutDir sets the value of the top-level "builddir" Ninja variable
	// that controls where Ninja stores its build log files.  This value can be
	// set at most one time for a single build, later calls are ignored.
	SetOutDir(pctx PackageContext, value string)

	// Eval takes a string with embedded ninja variables, and returns a string
	// with all of the variables recursively expanded. Any variables references
	// are expanded in the scope of the PackageContext.
	Eval(pctx PackageContext, ninjaStr string) (string, error)

	VisitAllModulesBlueprint(visit func(blueprint.Module))
	VisitAllModules(visit func(Module))
	VisitAllModuleProxies(visit func(proxy ModuleProxy))
	VisitAllModulesIf(pred func(Module) bool, visit func(Module))

	VisitDirectDeps(module Module, visit func(Module))
	VisitDirectDepsIf(module Module, pred func(Module) bool, visit func(Module))

	// Deprecated: use WalkDeps instead to support multiple dependency tags on the same module
	VisitDepsDepthFirst(module Module, visit func(Module))
	// Deprecated: use WalkDeps instead to support multiple dependency tags on the same module
	VisitDepsDepthFirstIf(module Module, pred func(Module) bool,
		visit func(Module))

	VisitAllModuleVariants(module Module, visit func(Module))

	VisitAllModuleVariantProxies(module Module, visit func(proxy ModuleProxy))

	PrimaryModule(module Module) Module

	PrimaryModuleProxy(module ModuleProxy) ModuleProxy

	IsFinalModule(module Module) bool

	AddNinjaFileDeps(deps ...string)

	// GlobWithDeps returns a list of files that match the specified pattern but do not match any
	// of the patterns in excludes.  It also adds efficient dependencies to rerun the primary
	// builder whenever a file matching the pattern as added or removed, without rerunning if a
	// file that does not match the pattern is added to a searched directory.
	GlobWithDeps(pattern string, excludes []string) ([]string, error)

	// OtherModulePropertyErrorf reports an error on the line number of the given property of the given module
	OtherModulePropertyErrorf(module Module, property string, format string, args ...interface{})

	// HasMutatorFinished returns true if the given mutator has finished running.
	// It will panic if given an invalid mutator name.
	HasMutatorFinished(mutatorName string) bool

	// DistForGoals creates a rule to copy one or more Paths to the artifacts
	// directory on the build server when any of the specified goals are built.
	DistForGoal(goal string, paths ...Path)

	// DistForGoalWithFilename creates a rule to copy a Path to the artifacts
	// directory on the build server with the given filename when the specified
	// goal is built.
	DistForGoalWithFilename(goal string, path Path, filename string)

	// DistForGoals creates a rule to copy one or more Paths to the artifacts
	// directory on the build server when any of the specified goals are built.
	DistForGoals(goals []string, paths ...Path)

	// DistForGoalsWithFilename creates a rule to copy a Path to the artifacts
	// directory on the build server with the given filename when any of the
	// specified goals are built.
	DistForGoalsWithFilename(goals []string, path Path, filename string)
}

type singletonAdaptor struct {
	Singleton

	buildParams []BuildParams
	ruleParams  map[blueprint.Rule]blueprint.RuleParams
}

var _ testBuildProvider = (*singletonAdaptor)(nil)

func (s *singletonAdaptor) GenerateBuildActions(ctx blueprint.SingletonContext) {
	sctx := &singletonContextAdaptor{SingletonContext: ctx}
	if sctx.Config().captureBuild {
		sctx.ruleParams = make(map[blueprint.Rule]blueprint.RuleParams)
	}

	s.Singleton.GenerateBuildActions(sctx)

	s.buildParams = sctx.buildParams
	s.ruleParams = sctx.ruleParams

	if len(sctx.dists) > 0 {
		dists := getSingletonDists(sctx.Config())
		dists.lock.Lock()
		defer dists.lock.Unlock()
		dists.dists = append(dists.dists, sctx.dists...)
	}
}

func (s *singletonAdaptor) BuildParamsForTests() []BuildParams {
	return s.buildParams
}

func (s *singletonAdaptor) RuleParamsForTests() map[blueprint.Rule]blueprint.RuleParams {
	return s.ruleParams
}

var singletonDistsKey = NewOnceKey("singletonDistsKey")

type singletonDistsAndLock struct {
	dists []dist
	lock  sync.Mutex
}

func getSingletonDists(config Config) *singletonDistsAndLock {
	return config.Once(singletonDistsKey, func() interface{} {
		return &singletonDistsAndLock{}
	}).(*singletonDistsAndLock)
}

type Singleton interface {
	GenerateBuildActions(SingletonContext)
}

type singletonContextAdaptor struct {
	blueprint.SingletonContext

	buildParams []BuildParams
	ruleParams  map[blueprint.Rule]blueprint.RuleParams
	dists       []dist
}

func (s *singletonContextAdaptor) blueprintSingletonContext() blueprint.SingletonContext {
	return s.SingletonContext
}

func (s *singletonContextAdaptor) Config() Config {
	return s.SingletonContext.Config().(Config)
}

func (s *singletonContextAdaptor) DeviceConfig() DeviceConfig {
	return DeviceConfig{s.Config().deviceConfig}
}

func (s *singletonContextAdaptor) Variable(pctx PackageContext, name, value string) {
	s.SingletonContext.Variable(pctx.PackageContext, name, value)
}

func (s *singletonContextAdaptor) Rule(pctx PackageContext, name string, params blueprint.RuleParams, argNames ...string) blueprint.Rule {
	if s.Config().UseRemoteBuild() {
		if params.Pool == nil {
			// When USE_GOMA=true or USE_RBE=true are set and the rule is not supported by goma/RBE, restrict
			// jobs to the local parallelism value
			params.Pool = localPool
		} else if params.Pool == remotePool {
			// remotePool is a fake pool used to identify rule that are supported for remoting. If the rule's
			// pool is the remotePool, replace with nil so that ninja runs it at NINJA_REMOTE_NUM_JOBS
			// parallelism.
			params.Pool = nil
		}
	}
	rule := s.SingletonContext.Rule(pctx.PackageContext, name, params, argNames...)
	if s.Config().captureBuild {
		s.ruleParams[rule] = params
	}
	return rule
}

func (s *singletonContextAdaptor) Build(pctx PackageContext, params BuildParams) {
	if s.Config().captureBuild {
		s.buildParams = append(s.buildParams, params)
	}
	bparams := convertBuildParams(params)
	s.SingletonContext.Build(pctx.PackageContext, bparams)
}

func (s *singletonContextAdaptor) Phony(name string, deps ...Path) {
	addSingletonPhony(s.Config(), name, deps...)
}

func (s *singletonContextAdaptor) SetOutDir(pctx PackageContext, value string) {
	s.SingletonContext.SetOutDir(pctx.PackageContext, value)
}

func (s *singletonContextAdaptor) Eval(pctx PackageContext, ninjaStr string) (string, error) {
	return s.SingletonContext.Eval(pctx.PackageContext, ninjaStr)
}

// visitAdaptor wraps a visit function that takes an android.Module parameter into
// a function that takes a blueprint.Module parameter and only calls the visit function if the
// blueprint.Module is an android.Module.
func visitAdaptor(visit func(Module)) func(blueprint.Module) {
	return func(module blueprint.Module) {
		if aModule, ok := module.(Module); ok {
			visit(aModule)
		}
	}
}

// visitProxyAdaptor wraps a visit function that takes an android.ModuleProxy parameter into
// a function that takes a blueprint.ModuleProxy parameter.
func visitProxyAdaptor(visit func(proxy ModuleProxy)) func(proxy blueprint.ModuleProxy) {
	return func(module blueprint.ModuleProxy) {
		visit(ModuleProxy{
			module: module,
		})
	}
}

// predAdaptor wraps a pred function that takes an android.Module parameter
// into a function that takes an blueprint.Module parameter and only calls the visit function if the
// blueprint.Module is an android.Module, otherwise returns false.
func predAdaptor(pred func(Module) bool) func(blueprint.Module) bool {
	return func(module blueprint.Module) bool {
		if aModule, ok := module.(Module); ok {
			return pred(aModule)
		} else {
			return false
		}
	}
}

func (s *singletonContextAdaptor) ModuleName(module blueprint.Module) string {
	return s.SingletonContext.ModuleName(getWrappedModule(module))
}

func (s *singletonContextAdaptor) ModuleDir(module blueprint.Module) string {
	return s.SingletonContext.ModuleDir(getWrappedModule(module))
}

func (s *singletonContextAdaptor) ModuleSubDir(module blueprint.Module) string {
	return s.SingletonContext.ModuleSubDir(getWrappedModule(module))
}

func (s *singletonContextAdaptor) ModuleType(module blueprint.Module) string {
	return s.SingletonContext.ModuleType(getWrappedModule(module))
}

func (s *singletonContextAdaptor) BlueprintFile(module blueprint.Module) string {
	return s.SingletonContext.BlueprintFile(getWrappedModule(module))
}

func (s *singletonContextAdaptor) VisitAllModulesBlueprint(visit func(blueprint.Module)) {
	s.SingletonContext.VisitAllModules(visit)
}

func (s *singletonContextAdaptor) VisitAllModules(visit func(Module)) {
	s.SingletonContext.VisitAllModules(visitAdaptor(visit))
}

func (s *singletonContextAdaptor) VisitAllModuleProxies(visit func(proxy ModuleProxy)) {
	s.SingletonContext.VisitAllModuleProxies(visitProxyAdaptor(visit))
}

func (s *singletonContextAdaptor) VisitAllModulesIf(pred func(Module) bool, visit func(Module)) {
	s.SingletonContext.VisitAllModulesIf(predAdaptor(pred), visitAdaptor(visit))
}

func (s *singletonContextAdaptor) VisitDirectDeps(module Module, visit func(Module)) {
	s.SingletonContext.VisitDirectDeps(module, visitAdaptor(visit))
}

func (s *singletonContextAdaptor) VisitDirectDepsIf(module Module, pred func(Module) bool, visit func(Module)) {
	s.SingletonContext.VisitDirectDepsIf(module, predAdaptor(pred), visitAdaptor(visit))
}

func (s *singletonContextAdaptor) VisitDepsDepthFirst(module Module, visit func(Module)) {
	s.SingletonContext.VisitDepsDepthFirst(module, visitAdaptor(visit))
}

func (s *singletonContextAdaptor) VisitDepsDepthFirstIf(module Module, pred func(Module) bool, visit func(Module)) {
	s.SingletonContext.VisitDepsDepthFirstIf(module, predAdaptor(pred), visitAdaptor(visit))
}

func (s *singletonContextAdaptor) VisitAllModuleVariants(module Module, visit func(Module)) {
	s.SingletonContext.VisitAllModuleVariants(module, visitAdaptor(visit))
}

func (s *singletonContextAdaptor) VisitAllModuleVariantProxies(module Module, visit func(proxy ModuleProxy)) {
	s.SingletonContext.VisitAllModuleVariantProxies(getWrappedModule(module), visitProxyAdaptor(visit))
}

func (s *singletonContextAdaptor) PrimaryModule(module Module) Module {
	return s.SingletonContext.PrimaryModule(module).(Module)
}

func (s *singletonContextAdaptor) PrimaryModuleProxy(module ModuleProxy) ModuleProxy {
	return ModuleProxy{s.SingletonContext.PrimaryModuleProxy(module.module)}
}

func (s *singletonContextAdaptor) IsFinalModule(module Module) bool {
	return s.SingletonContext.IsFinalModule(getWrappedModule(module))
}

func (s *singletonContextAdaptor) ModuleVariantsFromName(referer ModuleProxy, name string) []ModuleProxy {
	// get module reference for visibility enforcement
	qualified := createVisibilityModuleProxyReference(s, s.ModuleName(referer), s.ModuleDir(referer), referer)

	modules := s.SingletonContext.ModuleVariantsFromName(referer.module, name)
	result := make([]ModuleProxy, 0, len(modules))
	for _, module := range modules {
		// enforce visibility
		depName := s.ModuleName(module)
		depDir := s.ModuleDir(module)
		depQualified := qualifiedModuleName{depDir, depName}
		// Targets are always visible to other targets in their own package.
		if depQualified.pkg != qualified.name.pkg {
			rule := effectiveVisibilityRules(s.Config(), depQualified)
			if !rule.matches(qualified) {
				s.ModuleErrorf(referer, "module %q references %q which is not visible to this module\nYou may need to add %q to its visibility",
					referer.Name(), depQualified, "//"+s.ModuleDir(referer))
				continue
			}
		}
		result = append(result, ModuleProxy{module})
	}
	return result
}

func (s *singletonContextAdaptor) otherModuleProvider(module blueprint.Module, provider blueprint.AnyProviderKey) (any, bool) {
	return s.SingletonContext.ModuleProvider(module, provider)
}

func (s *singletonContextAdaptor) OtherModulePropertyErrorf(module Module, property string, format string, args ...interface{}) {
	s.blueprintSingletonContext().OtherModulePropertyErrorf(module, property, format, args...)
}

func (s *singletonContextAdaptor) HasMutatorFinished(mutatorName string) bool {
	return s.blueprintSingletonContext().HasMutatorFinished(mutatorName)
}
func (s *singletonContextAdaptor) DistForGoal(goal string, paths ...Path) {
	s.DistForGoals([]string{goal}, paths...)
}

func (s *singletonContextAdaptor) DistForGoalWithFilename(goal string, path Path, filename string) {
	s.DistForGoalsWithFilename([]string{goal}, path, filename)
}

func (s *singletonContextAdaptor) DistForGoals(goals []string, paths ...Path) {
	var copies distCopies
	for _, path := range paths {
		copies = append(copies, distCopy{
			from: path,
			dest: path.Base(),
		})
	}
	s.dists = append(s.dists, dist{
		goals: slices.Clone(goals),
		paths: copies,
	})
}

func (s *singletonContextAdaptor) DistForGoalsWithFilename(goals []string, path Path, filename string) {
	s.dists = append(s.dists, dist{
		goals: slices.Clone(goals),
		paths: distCopies{{from: path, dest: filename}},
	})
}
