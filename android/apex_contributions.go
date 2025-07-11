// Copyright 2023 Google Inc. All rights reserved.
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
	"github.com/google/blueprint"
	"github.com/google/blueprint/proptools"
)

func init() {
	RegisterApexContributionsBuildComponents(InitRegistrationContext)
}

func RegisterApexContributionsBuildComponents(ctx RegistrationContext) {
	ctx.RegisterModuleType("apex_contributions", apexContributionsFactory)
	ctx.RegisterModuleType("apex_contributions_defaults", apexContributionsDefaultsFactory)
	ctx.RegisterModuleType("all_apex_contributions", allApexContributionsFactory)
}

type apexContributions struct {
	ModuleBase
	DefaultableModuleBase
	properties contributionProps
}

type contributionProps struct {
	// Name of the mainline module
	Api_domain *string
	// A list of module names that should be used when this contribution
	// is selected via product_config
	// The name should be explicit (foo or prebuilt_foo)
	Contents []string
}

func (m *apexContributions) ApiDomain() string {
	return proptools.String(m.properties.Api_domain)
}

func (m *apexContributions) Contents() []string {
	return m.properties.Contents
}

// apex_contributions contains a list of module names (source or
// prebuilt) belonging to the mainline module
// An apex can have multiple apex_contributions modules
// with different combinations of source or prebuilts, but only one can be
// selected via product_config.
func apexContributionsFactory() Module {
	module := &apexContributions{}
	module.AddProperties(&module.properties)
	InitAndroidModule(module)
	InitDefaultableModule(module)
	return module
}

// This module type does not have any build actions.
// It provides metadata that is used in post-deps mutator phase for source vs
// prebuilts selection.
func (m *apexContributions) GenerateAndroidBuildActions(ctx ModuleContext) {
}

type apexContributionsDefaults struct {
	ModuleBase
	DefaultsModuleBase
}

func apexContributionsDefaultsFactory() Module {
	module := &apexContributionsDefaults{}
	module.AddProperties(&contributionProps{})
	InitDefaultsModule(module)
	return module
}

// A container for apex_contributions.
// Based on product_config, it will create a dependency on the selected
// apex_contributions per mainline module
type allApexContributions struct {
	ModuleBase
}

func allApexContributionsFactory() Module {
	module := &allApexContributions{}
	InitAndroidModule(module)
	return module
}

type apexContributionsDepTag struct {
	blueprint.BaseDependencyTag
}

var (
	AcDepTag = apexContributionsDepTag{}
)

// Set PrebuiltSelectionInfoProvider in post deps phase
func (a *allApexContributions) SetPrebuiltSelectionInfoProvider(ctx BottomUpMutatorContext) {
	addContentsToProvider := func(p *PrebuiltSelectionInfoMap, m *apexContributions) {
		for _, content := range m.Contents() {
			// Verify that the module listed in contents exists in the tree
			// Remove the prebuilt_ prefix to account for partner worksapces where the source module does not
			// exist, and PrebuiltRenameMutator renames `prebuilt_foo` to `foo`
			if !ctx.OtherModuleExists(content) && !ctx.OtherModuleExists(RemoveOptionalPrebuiltPrefix(content)) && !ctx.Config().AllowMissingDependencies() {
				ctx.ModuleErrorf("%s listed in apex_contributions %s does not exist\n", content, m.Name())
			}
			pi := &PrebuiltSelectionInfo{
				selectedModuleName: content,
				metadataModuleName: m.Name(),
				apiDomain:          m.ApiDomain(),
			}
			p.Add(ctx, pi)
		}
	}

	p := PrebuiltSelectionInfoMap{}
	// Skip apex_contributions if BuildApexContributionContents is true
	// This product config var allows some products in the same family to use mainline modules from source
	// (e.g. shiba and shiba_fullmte)
	// Eventually these product variants will have their own release config maps.
	if !proptools.Bool(ctx.Config().BuildIgnoreApexContributionContents()) {
		deps := ctx.AddDependency(ctx.Module(), AcDepTag, ctx.Config().AllApexContributions()...)
		for _, child := range deps {
			if child == nil {
				continue
			}
			if m, ok := child.(*apexContributions); ok {
				addContentsToProvider(&p, m)
			} else {
				ctx.ModuleErrorf("%s is not an apex_contributions module\n", child.Name())
			}
		}
	}
	SetProvider(ctx, PrebuiltSelectionInfoProvider, p)
}

// A provider containing metadata about whether source or prebuilt should be used
// This provider will be used in prebuilt_select mutator to redirect deps
var PrebuiltSelectionInfoProvider = blueprint.NewMutatorProvider[PrebuiltSelectionInfoMap]("prebuilt_select")

// Map of selected module names to a metadata object
// The metadata contains information about the api_domain of the selected module
type PrebuiltSelectionInfoMap map[string]PrebuiltSelectionInfo

// Add a new entry to the map with some validations
func (pm *PrebuiltSelectionInfoMap) Add(ctx BaseModuleContext, p *PrebuiltSelectionInfo) {
	if p == nil {
		return
	}
	(*pm)[p.selectedModuleName] = *p
}

type PrebuiltSelectionInfo struct {
	// e.g. (libc|prebuilt_libc)
	selectedModuleName string
	// Name of the apex_contributions module
	metadataModuleName string
	// e.g. com.android.runtime
	apiDomain string
}

// Returns true if `name` is explicitly requested using one of the selected
// apex_contributions metadata modules.
func (p *PrebuiltSelectionInfoMap) IsSelected(name string) bool {
	_, exists := (*p)[name]
	return exists
}

// Return the list of soong modules selected for this api domain
// In the case of apexes, it is the canonical name of the apex on device (/apex/<apex_name>)
func (p *PrebuiltSelectionInfoMap) GetSelectedModulesForApiDomain(apiDomain string) []string {
	selected := []string{}
	for _, entry := range *p {
		if entry.apiDomain == apiDomain {
			selected = append(selected, entry.selectedModuleName)
		}
	}
	return selected
}

// This module type does not have any build actions.
func (a *allApexContributions) GenerateAndroidBuildActions(ctx ModuleContext) {
	if ctx.ModuleName() != "all_apex_contributions" {
		ctx.ModuleErrorf("There can only be 1 all_apex_contributions module in build/soong")
	}
}
