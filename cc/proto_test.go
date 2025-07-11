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

import (
	"strings"
	"testing"

	"android/soong/android"
)

func TestProto(t *testing.T) {
	t.Run("simple", func(t *testing.T) {
		ctx := testCc(t, `
		cc_library_shared {
			name: "libfoo",
			srcs: ["a.proto"],
		}`)

		proto := ctx.ModuleForTests(t, "libfoo", "android_arm_armv7-a-neon_shared").Output("proto/a.pb.cc")

		if cmd := proto.RuleParams.Command; !strings.Contains(cmd, "--cpp_out=") {
			t.Errorf("expected '--cpp_out' in %q", cmd)
		}
	})

	t.Run("plugin", func(t *testing.T) {
		ctx := testCc(t, `
		cc_binary_host {
			name: "protoc-gen-foobar",
			stl: "none",
		}

		cc_library_shared {
			name: "libfoo",
			srcs: ["a.proto"],
			proto: {
				plugin: "foobar",
			},
		}`)

		buildOS := ctx.Config().BuildOS.String()

		proto := ctx.ModuleForTests(t, "libfoo", "android_arm_armv7-a-neon_shared").Output("proto/a.pb.cc")
		foobar := ctx.ModuleForTests(t, "protoc-gen-foobar", buildOS+"_x86_64")

		cmd := proto.RuleParams.Command
		if w := "--foobar_out="; !strings.Contains(cmd, w) {
			t.Errorf("expected %q in %q", w, cmd)
		}

		foobarPath := foobar.Module().(android.HostToolProvider).HostToolPath().RelativeToTop().String()

		if w := "--plugin=protoc-gen-foobar=" + foobarPath; !strings.Contains(cmd, w) {
			t.Errorf("expected %q in %q", w, cmd)
		}
	})

	t.Run("grpc-cpp-plugin", func(t *testing.T) {
		ctx := testCc(t, `
                cc_binary_host {
                        name: "protoc-gen-grpc-cpp-plugin",
                        stl: "none",
                }

                cc_library_shared {
                        name: "libgrpc",
                        srcs: ["a.proto"],
                        proto: {
                                plugin: "grpc-cpp-plugin",
                        },
                }`)

		buildOS := ctx.Config().BuildOS.String()

		proto := ctx.ModuleForTests(t, "libgrpc", "android_arm_armv7-a-neon_shared").Output("proto/a.grpc.pb.cc")
		grpcCppPlugin := ctx.ModuleForTests(t, "protoc-gen-grpc-cpp-plugin", buildOS+"_x86_64")

		cmd := proto.RuleParams.Command
		if w := "--grpc-cpp-plugin_out="; !strings.Contains(cmd, w) {
			t.Errorf("expected %q in %q", w, cmd)
		}

		grpcCppPluginPath := grpcCppPlugin.Module().(android.HostToolProvider).HostToolPath().RelativeToTop().String()

		if w := "--plugin=protoc-gen-grpc-cpp-plugin=" + grpcCppPluginPath; !strings.Contains(cmd, w) {
			t.Errorf("expected %q in %q", w, cmd)
		}
	})

}
