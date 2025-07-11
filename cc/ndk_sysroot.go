// Copyright 2016 Google Inc. All rights reserved.
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

// The platform needs to provide the following artifacts for the NDK:
// 1. Bionic headers.
// 2. Platform API headers.
// 3. NDK stub shared libraries.
// 4. Bionic static libraries.
//
// TODO(danalbert): All of the above need to include NOTICE files.
//
// Components 1 and 2: Headers
// The bionic and platform API headers are generalized into a single
// `ndk_headers` rule. This rule has a `from` property that indicates a base
// directory from which headers are to be taken, and a `to` property that
// indicates where in the sysroot they should reside relative to usr/include.
// There is also a `srcs` property that is glob compatible for specifying which
// headers to include.
//
// Component 3: Stub Libraries
// The shared libraries in the NDK are not the actual shared libraries they
// refer to (to prevent people from accidentally loading them), but stub
// libraries with placeholder implementations of everything for use at build time
// only.
//
// Since we don't actually need to know anything about the stub libraries aside
// from a list of functions and globals to be exposed, we can create these for
// every platform level in the current tree. This is handled by the
// ndk_library rule.
//
// Component 4: Static Libraries
// The NDK only provides static libraries for bionic, not the platform APIs.
// Since these need to be the actual implementation, we can't build old versions
// in the current platform tree. As such, legacy versions are checked in
// prebuilt to development/ndk, and a current version is built and archived as
// part of the platform build. The platfrom already builds these libraries, our
// NDK build rules only need to archive them for retrieval so they can be added
// to the prebuilts.
//
// TODO(danalbert): Write `ndk_static_library` rule.

import (
	"fmt"
	"path/filepath"
	"strings"

	"android/soong/android"

	"github.com/google/blueprint"
)

var (
	verifyCCompat = pctx.AndroidStaticRule("verifyCCompat",
		blueprint.RuleParams{
			Command:     "$ccCmd -x c -fsyntax-only $flags $in && touch $out",
			CommandDeps: []string{"$ccCmd"},
		},
		"ccCmd",
		"flags",
	)
)

func init() {
	RegisterNdkModuleTypes(android.InitRegistrationContext)
}

func RegisterNdkModuleTypes(ctx android.RegistrationContext) {
	ctx.RegisterModuleType("ndk_headers", NdkHeadersFactory)
	ctx.RegisterModuleType("ndk_library", NdkLibraryFactory)
	ctx.RegisterModuleType("preprocessed_ndk_headers", preprocessedNdkHeadersFactory)
	ctx.RegisterParallelSingletonType("ndk", NdkSingleton)
}

func getNdkInstallBase(ctx android.PathContext) android.OutputPath {
	return android.PathForNdkInstall(ctx)
}

// Returns the main install directory for the NDK sysroot. Usable with --sysroot.
func getNdkSysrootBase(ctx android.PathContext) android.OutputPath {
	return getNdkInstallBase(ctx).Join(ctx, "sysroot")
}

// The base timestamp file depends on the NDK headers and stub shared libraries,
// but not the static libraries. This distinction is needed because the static
// libraries themselves might need to depend on the base sysroot.
func getNdkBaseTimestampFile(ctx android.PathContext) android.WritablePath {
	return android.PathForOutput(ctx, "ndk_base.timestamp")
}

// The headers timestamp file depends only on the NDK headers.
// This is used mainly for .tidy files that do not need any stub libraries.
func getNdkHeadersTimestampFile(ctx android.PathContext) android.WritablePath {
	return android.PathForOutput(ctx, "ndk_headers.timestamp")
}

// The full timestamp file depends on the base timestamp *and* the static
// libraries.
func getNdkFullTimestampFile(ctx android.PathContext) android.WritablePath {
	return android.PathForOutput(ctx, "ndk.timestamp")
}

// The list of all NDK headers as they are located in the repo.
// Used for ABI monitoring to track only structures defined in NDK headers.
func getNdkABIHeadersFile(ctx android.PathContext) android.WritablePath {
	return android.PathForOutput(ctx, "ndk_abi_headers.txt")
}

func verifyNdkHeaderIsCCompatible(ctx android.SingletonContext,
	src android.Path, dest android.Path) android.Path {
	sysrootInclude := getCurrentIncludePath(ctx)
	baseOutputDir := android.PathForOutput(ctx, "c-compat-verification")
	installRelPath, err := filepath.Rel(sysrootInclude.String(), dest.String())
	if err != nil {
		ctx.Errorf("filepath.Rel(%q, %q) failed: %s", dest, sysrootInclude, err)
	}
	output := baseOutputDir.Join(ctx, installRelPath)
	ctx.Build(pctx, android.BuildParams{
		Rule:        verifyCCompat,
		Description: fmt.Sprintf("Verifying C compatibility of %s", src),
		Output:      output,
		Input:       dest,
		// Ensures that all the headers in the sysroot are already installed
		// before testing any of the headers for C compatibility, and also that
		// the check will be re-run whenever the sysroot changes. This is
		// necessary because many of the NDK headers depend on other NDK
		// headers, but we don't have explicit dependency tracking for that.
		Implicits: []android.Path{getNdkHeadersTimestampFile(ctx)},
		Args: map[string]string{
			"ccCmd": "${config.ClangBin}/clang",
			"flags": fmt.Sprintf(
				// Ideally we'd check each ABI, multiple API levels,
				// fortify/non-fortify, and a handful of other variations. It's
				// a lot more difficult to do that though, and would eat up more
				// build time. All the problems we've seen so far that this
				// check would catch have been in arch-generic and
				// minSdkVersion-generic code in frameworks though, so this is a
				// good place to start.
				"-target aarch64-linux-android%d --sysroot %s",
				android.FutureApiLevel.FinalOrFutureInt(),
				getNdkSysrootBase(ctx).String(),
			),
		},
	})
	return output
}

func NdkSingleton() android.Singleton {
	return &ndkSingleton{}
}

// Collect all NDK exported headers paths into a file that is used to
// detect public types that should be ABI monitored.
//
// Assume that we have the following code in exported header:
//
//	typedef struct Context Context;
//	typedef struct Output {
//	    ...
//	} Output;
//	void DoSomething(Context* ctx, Output* output);
//
// If none of public headers exported to end-users contain definition of
// "struct Context", then "struct Context" layout and members shouldn't be
// monitored. However we use DWARF information from a real library, which
// may have access to the definition of "string Context" from
// implementation headers, and it will leak to ABI.
//
// STG tool doesn't access source and header files, only DWARF information
// from compiled library. And the DWARF contains file name where a type is
// defined. So we need a rule to build a list of paths to public headers,
// so STG can distinguish private types from public and do not monitor
// private types that are not accessible to library users.
func writeNdkAbiSrcFilter(ctx android.BuilderContext,
	headerSrcPaths android.Paths, outputFile android.WritablePath) {
	var filterBuilder strings.Builder
	filterBuilder.WriteString("[decl_file_allowlist]\n")
	for _, headerSrcPath := range headerSrcPaths {
		filterBuilder.WriteString(headerSrcPath.String())
		filterBuilder.WriteString("\n")
	}

	android.WriteFileRule(ctx, outputFile, filterBuilder.String())
}

type ndkSingleton struct{}

type srcDestPair struct {
	src  android.Path
	dest android.Path
}

func (n *ndkSingleton) GenerateBuildActions(ctx android.SingletonContext) {
	var staticLibInstallPaths android.Paths
	var headerSrcPaths android.Paths
	var headerInstallPaths android.Paths
	var headersToVerify []srcDestPair
	var headerCCompatVerificationTimestampPaths android.Paths
	var installPaths android.Paths
	var licensePaths android.Paths
	ctx.VisitAllModuleProxies(func(module android.ModuleProxy) {

		if !android.OtherModulePointerProviderOrDefault(ctx, module, android.CommonModuleInfoProvider).Enabled {
			return
		}

		if m, ok := android.OtherModuleProvider(ctx, module, NdkHeaderInfoProvider); ok {
			headerSrcPaths = append(headerSrcPaths, m.SrcPaths...)
			headerInstallPaths = append(headerInstallPaths, m.InstallPaths...)
			if !m.SkipVerification {
				for i, installPath := range m.InstallPaths {
					headersToVerify = append(headersToVerify, srcDestPair{
						src:  m.SrcPaths[i],
						dest: installPath,
					})
				}
			}
			installPaths = append(installPaths, m.InstallPaths...)
			licensePaths = append(licensePaths, m.LicensePath)
		}

		if m, ok := android.OtherModuleProvider(ctx, module, NdkPreprocessedHeaderInfoProvider); ok {
			headerSrcPaths = append(headerSrcPaths, m.SrcPaths...)
			headerInstallPaths = append(headerInstallPaths, m.InstallPaths...)
			if !m.SkipVerification {
				for i, installPath := range m.InstallPaths {
					headersToVerify = append(headersToVerify, srcDestPair{
						src:  m.SrcPaths[i],
						dest: installPath,
					})
				}
			}
			installPaths = append(installPaths, m.InstallPaths...)
			licensePaths = append(licensePaths, m.LicensePath)
		}

		if ccInfo, ok := android.OtherModuleProvider(ctx, module, CcInfoProvider); ok {
			if installer := ccInfo.InstallerInfo; installer != nil && installer.StubDecoratorInfo != nil &&
				ccInfo.LibraryInfo != nil && ccInfo.LibraryInfo.BuildStubs {
				installPaths = append(installPaths, installer.StubDecoratorInfo.InstallPath)
			}

			if ccInfo.LinkerInfo != nil {
				if library := ccInfo.LinkerInfo.LibraryDecoratorInfo; library != nil {
					if library.NdkSysrootPath != nil {
						staticLibInstallPaths = append(
							staticLibInstallPaths, library.NdkSysrootPath)
					}
				}

				if object := ccInfo.LinkerInfo.ObjectLinkerInfo; object != nil {
					if object.NdkSysrootPath != nil {
						staticLibInstallPaths = append(
							staticLibInstallPaths, object.NdkSysrootPath)
					}
				}
			}
		}
	})

	// Include only a single copy of each license file. The Bionic NOTICE is
	// long and is referenced by multiple Bionic modules.
	licensePaths = android.FirstUniquePaths(licensePaths)

	combinedLicense := getNdkInstallBase(ctx).Join(ctx, "NOTICE")
	ctx.Build(pctx, android.BuildParams{
		Rule:        android.Cat,
		Description: "combine licenses",
		Output:      combinedLicense,
		Inputs:      licensePaths,
	})

	baseDepPaths := append(installPaths, combinedLicense)

	ctx.Build(pctx, android.BuildParams{
		Rule:       android.Touch,
		Output:     getNdkBaseTimestampFile(ctx),
		Implicits:  baseDepPaths,
		Validation: getNdkAbiDiffTimestampFile(ctx),
	})

	ctx.Build(pctx, android.BuildParams{
		Rule:      android.Touch,
		Output:    getNdkHeadersTimestampFile(ctx),
		Implicits: headerInstallPaths,
	})

	for _, srcDestPair := range headersToVerify {
		headerCCompatVerificationTimestampPaths = append(
			headerCCompatVerificationTimestampPaths,
			verifyNdkHeaderIsCCompatible(ctx, srcDestPair.src, srcDestPair.dest))
	}

	writeNdkAbiSrcFilter(ctx, headerSrcPaths, getNdkABIHeadersFile(ctx))

	fullDepPaths := append(staticLibInstallPaths, getNdkBaseTimestampFile(ctx))

	// There's a phony "ndk" rule defined in core/main.mk that depends on this.
	// `m ndk` will build the sysroots for the architectures in the current
	// lunch target. `build/soong/scripts/build-ndk-prebuilts.sh` will build the
	// sysroots for all the NDK architectures and package them so they can be
	// imported into the NDK's build.
	ctx.Build(pctx, android.BuildParams{
		Rule:      android.Touch,
		Output:    getNdkFullTimestampFile(ctx),
		Implicits: append(fullDepPaths, headerCCompatVerificationTimestampPaths...),
	})
}
