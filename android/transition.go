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

package android

import "github.com/google/blueprint"

// TransitionMutator implements a top-down mechanism where a module tells its
// direct dependencies what variation they should be built in but the dependency
// has the final say.
//
// When implementing a transition mutator, one needs to implement four methods:
//   - Split() that tells what variations a module has by itself
//   - OutgoingTransition() where a module tells what it wants from its
//     dependency
//   - IncomingTransition() where a module has the final say about its own
//     variation
//   - Mutate() that changes the state of a module depending on its variation
//
// That the effective variation of module B when depended on by module A is the
// composition the outgoing transition of module A and the incoming transition
// of module B.
//
// the outgoing transition should not take the properties of the dependency into
// account, only those of the module that depends on it. For this reason, the
// dependency is not even passed into it as an argument. Likewise, the incoming
// transition should not take the properties of the depending module into
// account and is thus not informed about it. This makes for a nice
// decomposition of the decision logic.
//
// A given transition mutator only affects its own variation; other variations
// stay unchanged along the dependency edges.
//
// Soong makes sure that all modules are created in the desired variations and
// that dependency edges are set up correctly. This ensures that "missing
// variation" errors do not happen and allows for more flexible changes in the
// value of the variation among dependency edges (as oppposed to bottom-up
// mutators where if module A in variation X depends on module B and module B
// has that variation X, A must depend on variation X of B)
//
// The limited power of the context objects passed to individual mutators
// methods also makes it more difficult to shoot oneself in the foot. Complete
// safety is not guaranteed because no one prevents individual transition
// mutators from mutating modules in illegal ways and for e.g. Split() or
// Mutate() to run their own visitations of the transitive dependency of the
// module and both of these are bad ideas, but it's better than no guardrails at
// all.
//
// This model is pretty close to Bazel's configuration transitions. The mapping
// between concepts in Soong and Bazel is as follows:
//   - Module == configured target
//   - Variant == configuration
//   - Variation name == configuration flag
//   - Variation == configuration flag value
//   - Outgoing transition == attribute transition
//   - Incoming transition == rule transition
//
// The Split() method does not have a Bazel equivalent and Bazel split
// transitions do not have a Soong equivalent.
//
// Mutate() does not make sense in Bazel due to the different models of the
// two systems: when creating new variations, Soong clones the old module and
// thus some way is needed to change it state whereas Bazel creates each
// configuration of a given configured target anew.
type TransitionMutator interface {
	// Split returns the set of variations that should be created for a module no
	// matter who depends on it. Used when Make depends on a particular variation
	// or when the module knows its variations just based on information given to
	// it in the Blueprint file. This method should not mutate the module it is
	// called on.
	Split(ctx BaseModuleContext) []string

	// OutgoingTransition is called on a module to determine which variation it wants
	// from its direct dependencies. The dependency itself can override this decision.
	// This method should not mutate the module itself.
	OutgoingTransition(ctx OutgoingTransitionContext, sourceVariation string) string

	// IncomingTransition is called on a module to determine which variation it should
	// be in based on the variation modules that depend on it want. This gives the module
	// a final say about its own variations. This method should not mutate the module
	// itself.
	IncomingTransition(ctx IncomingTransitionContext, incomingVariation string) string

	// Mutate is called after a module was split into multiple variations on each variation.
	// It should not split the module any further but adding new dependencies is
	// fine. Unlike all the other methods on TransitionMutator, this method is
	// allowed to mutate the module.
	Mutate(ctx BottomUpMutatorContext, variation string)
}

type IncomingTransitionContext interface {
	ArchModuleContext
	ModuleProviderContext
	ModuleErrorContext

	// Module returns the target of the dependency edge for which the transition
	// is being computed
	Module() Module

	// Config returns the configuration for the build.
	Config() Config

	DeviceConfig() DeviceConfig

	// IsAddingDependency returns true if the transition is being called while adding a dependency
	// after the transition mutator has already run, or false if it is being called when the transition
	// mutator is running.  This should be used sparingly, all uses will have to be removed in order
	// to support creating variants on demand.
	IsAddingDependency() bool
}

type OutgoingTransitionContext interface {
	ArchModuleContext
	ModuleProviderContext

	// Module returns the target of the dependency edge for which the transition
	// is being computed
	Module() Module

	// DepTag() Returns the dependency tag through which this dependency is
	// reached
	DepTag() blueprint.DependencyTag

	// Config returns the configuration for the build.
	Config() Config

	DeviceConfig() DeviceConfig
}

type androidTransitionMutator struct {
	finalPhase bool
	mutator    TransitionMutator
	name       string
}

func (a *androidTransitionMutator) Split(ctx blueprint.BaseModuleContext) []string {
	if a.finalPhase {
		panic("TransitionMutator not allowed in FinalDepsMutators")
	}
	if m, ok := ctx.Module().(Module); ok {
		moduleContext := m.base().baseModuleContextFactory(ctx)
		return a.mutator.Split(&moduleContext)
	} else {
		return []string{""}
	}
}

func (a *androidTransitionMutator) OutgoingTransition(bpctx blueprint.OutgoingTransitionContext, sourceVariation string) string {
	if m, ok := bpctx.Module().(Module); ok {
		ctx := outgoingTransitionContextPool.Get().(*outgoingTransitionContextImpl)
		defer outgoingTransitionContextPool.Put(ctx)
		*ctx = outgoingTransitionContextImpl{
			archModuleContext: m.base().archModuleContextFactory(bpctx),
			bp:                bpctx,
		}
		return a.mutator.OutgoingTransition(ctx, sourceVariation)
	} else {
		return ""
	}
}

func (a *androidTransitionMutator) IncomingTransition(bpctx blueprint.IncomingTransitionContext, incomingVariation string) string {
	if m, ok := bpctx.Module().(Module); ok {
		ctx := incomingTransitionContextPool.Get().(*incomingTransitionContextImpl)
		defer incomingTransitionContextPool.Put(ctx)
		*ctx = incomingTransitionContextImpl{
			archModuleContext: m.base().archModuleContextFactory(bpctx),
			bp:                bpctx,
		}
		return a.mutator.IncomingTransition(ctx, incomingVariation)
	} else {
		return ""
	}
}

func (a *androidTransitionMutator) Mutate(ctx blueprint.BottomUpMutatorContext, variation string) {
	if am, ok := ctx.Module().(Module); ok {
		if variation != "" {
			// TODO: this should really be checking whether the TransitionMutator affected this module, not
			//  the empty variant, but TransitionMutator has no concept of skipping a module.
			base := am.base()
			base.commonProperties.DebugMutators = append(base.commonProperties.DebugMutators, a.name)
			base.commonProperties.DebugVariations = append(base.commonProperties.DebugVariations, variation)
		}

		mctx := bottomUpMutatorContextFactory(ctx, am, a.finalPhase)
		defer bottomUpMutatorContextPool.Put(mctx)
		a.mutator.Mutate(mctx, variation)
	}
}

type incomingTransitionContextImpl struct {
	archModuleContext
	bp blueprint.IncomingTransitionContext
}

func (c *incomingTransitionContextImpl) Module() Module {
	return c.bp.Module().(Module)
}

func (c *incomingTransitionContextImpl) Config() Config {
	return c.bp.Config().(Config)
}

func (c *incomingTransitionContextImpl) DeviceConfig() DeviceConfig {
	return DeviceConfig{c.bp.Config().(Config).deviceConfig}
}

func (c *incomingTransitionContextImpl) IsAddingDependency() bool {
	return c.bp.IsAddingDependency()
}

func (c *incomingTransitionContextImpl) provider(provider blueprint.AnyProviderKey) (any, bool) {
	return c.bp.Provider(provider)
}

func (c *incomingTransitionContextImpl) ModuleErrorf(fmt string, args ...interface{}) {
	c.bp.ModuleErrorf(fmt, args)
}

func (c *incomingTransitionContextImpl) PropertyErrorf(property, fmt string, args ...interface{}) {
	c.bp.PropertyErrorf(property, fmt, args)
}

type outgoingTransitionContextImpl struct {
	archModuleContext
	bp blueprint.OutgoingTransitionContext
}

func (c *outgoingTransitionContextImpl) Module() Module {
	return c.bp.Module().(Module)
}

func (c *outgoingTransitionContextImpl) DepTag() blueprint.DependencyTag {
	return c.bp.DepTag()
}

func (c *outgoingTransitionContextImpl) Config() Config {
	return c.bp.Config().(Config)
}

func (c *outgoingTransitionContextImpl) DeviceConfig() DeviceConfig {
	return DeviceConfig{c.bp.Config().(Config).deviceConfig}
}

func (c *outgoingTransitionContextImpl) provider(provider blueprint.AnyProviderKey) (any, bool) {
	return c.bp.Provider(provider)
}
