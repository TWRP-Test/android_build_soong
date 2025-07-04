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

package aconfig

import (
	"android/soong/android"

	"github.com/google/blueprint"
)

var (
	pctx = android.NewPackageContext("android/soong/aconfig")

	// For aconfig_declarations: Generate cache file
	aconfigRule = pctx.AndroidStaticRule("aconfig",
		blueprint.RuleParams{
			Command: `${aconfig} create-cache` +
				` --package ${package}` +
				` ${container}` +
				` ${declarations}` +
				` ${values}` +
				` ${default-permission}` +
				` ${allow-read-write}` +
				` --cache ${out}.tmp` +
				` && ( if cmp -s ${out}.tmp ${out} ; then rm ${out}.tmp ; else mv ${out}.tmp ${out} ; fi )`,
			//				` --build-id ${release_version}` +
			CommandDeps: []string{
				"${aconfig}",
			},
			Restat: true,
		}, "release_version", "package", "container", "declarations", "values", "default-permission", "allow-read-write")

	// For create-device-config-sysprops: Generate aconfig flag value map text file
	aconfigTextRule = pctx.AndroidStaticRule("aconfig_text",
		blueprint.RuleParams{
			Command: `${aconfig} dump-cache --dedup --format='{fully_qualified_name}:{permission}={state:bool}'` +
				` --cache ${in}` +
				` --out ${out}.tmp` +
				` && ( if cmp -s ${out}.tmp ${out} ; then rm ${out}.tmp ; else mv ${out}.tmp ${out} ; fi )`,
			CommandDeps: []string{
				"${aconfig}",
			},
			Restat: true,
		})

	// For all_aconfig_declarations: Combine all parsed_flags proto files
	AllDeclarationsRule = pctx.AndroidStaticRule("All_aconfig_declarations_dump",
		blueprint.RuleParams{
			Command: `${aconfig} dump-cache --dedup --format protobuf --out ${out} ${cache_files}`,
			CommandDeps: []string{
				"${aconfig}",
			},
		}, "cache_files")
	AllDeclarationsRuleTextProto = pctx.AndroidStaticRule("All_aconfig_declarations_dump_textproto",
		blueprint.RuleParams{
			Command: `${aconfig} dump-cache --dedup --format textproto --out ${out} ${cache_files}`,
			CommandDeps: []string{
				"${aconfig}",
			},
		}, "cache_files")
	RecordFinalizedFlagsRule = pctx.AndroidStaticRule("RecordFinalizedFlagsRule",
		blueprint.RuleParams{
			Command: `${record-finalized-flags} ${parsed_flags_file} ${finalized_flags_file} ${api_signature_files} > ${out}`,
			CommandDeps: []string{
				"${record-finalized-flags}",
			},
		}, "api_signature_files", "finalized_flags_file", "parsed_flags_file")
	ExportedFlagCheckRule = pctx.AndroidStaticRule("ExportedFlagCheckRule",
		blueprint.RuleParams{
			Command: `${exported-flag-check} ${parsed_flags_file} ${finalized_flags_file} ${api_signature_files} > ${out}`,
			CommandDeps: []string{
				"${exported-flag-check}",
			},
		}, "api_signature_files", "finalized_flags_file", "parsed_flags_file")

	CreateStorageRule = pctx.AndroidStaticRule("aconfig_create_storage",
		blueprint.RuleParams{
			Command: `${aconfig} create-storage --container ${container} --file ${file_type} --out ${out} ${cache_files} --version ${version}`,
			CommandDeps: []string{
				"${aconfig}",
			},
		}, "container", "file_type", "cache_files", "version")

	// For exported_java_aconfig_library: Generate a JAR from all
	// java_aconfig_libraries to be consumed by apps built outside the
	// platform
	exportedJavaRule = pctx.AndroidStaticRule("exported_java_aconfig_library",
		// For each aconfig cache file, if the cache contains any
		// exported flags, generate Java flag lookup code for the
		// exported flags (only). Finally collect all generated code
		// into the ${out} JAR file.
		blueprint.RuleParams{
			// LINT.IfChange
			Command: `rm -rf ${out}.tmp` +
				`&& for cache in ${cache_files}; do ` +
				`  if [ -n "$$(${aconfig} dump-cache --dedup --cache $$cache --filter=is_exported:true --format='{fully_qualified_name}')" ]; then ` +
				`    ${aconfig} create-java-lib` +
				`        --cache $$cache` +
				`        --mode=exported` +
				`        --allow-instrumentation ${use_new_storage}` +
				`        --new-exported ${use_new_exported}` +
				`        --single-exported-file true` +
				`        --check-api-level ${check_api_level}` +
				`        --out ${out}.tmp; ` +
				`  fi ` +
				`done` +
				`&& $soong_zip -write_if_changed -jar -o ${out} -C ${out}.tmp -D ${out}.tmp` +
				`&& rm -rf ${out}.tmp`,
			// LINT.ThenChange(/aconfig/codegen/init.go)
			CommandDeps: []string{
				"$aconfig",
				"$soong_zip",
			},
		}, "cache_files", "use_new_storage", "use_new_exported", "check_api_level")
)

func init() {
	RegisterBuildComponents(android.InitRegistrationContext)
	pctx.HostBinToolVariable("aconfig", "aconfig")
	pctx.HostBinToolVariable("soong_zip", "soong_zip")
	pctx.HostBinToolVariable("record-finalized-flags", "record-finalized-flags")
	pctx.HostBinToolVariable("exported-flag-check", "exported-flag-check")
}

func RegisterBuildComponents(ctx android.RegistrationContext) {
	ctx.RegisterModuleType("aconfig_declarations", DeclarationsFactory)
	ctx.RegisterModuleType("aconfig_values", ValuesFactory)
	ctx.RegisterModuleType("aconfig_value_set", ValueSetFactory)
	ctx.RegisterSingletonModuleType("all_aconfig_declarations", AllAconfigDeclarationsFactory)
	ctx.RegisterParallelSingletonType("exported_java_aconfig_library", ExportedJavaDeclarationsLibraryFactory)
	ctx.RegisterModuleType("all_aconfig_declarations_extension", AllAconfigDeclarationsExtensionFactory)
}
