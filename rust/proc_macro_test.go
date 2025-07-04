// Copyright 2021 The Android Open Source Project
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

package rust

import (
	"strings"
	"testing"
)

func TestRustProcMacro(t *testing.T) {
	ctx := testRust(t, `
          rust_proc_macro {
	    name: "libprocmacro",
	    srcs: ["foo.rs"],
	    crate_name: "procmacro",
	  }
	`)

	libprocmacro := ctx.ModuleForTests(t, "libprocmacro", "linux_glibc_x86_64").Rule("rustc")

	if !strings.Contains(libprocmacro.Args["rustcFlags"], "--extern proc_macro") {
		t.Errorf("--extern proc_macro flag not being passed to rustc for proc macro %#v", libprocmacro.Args["rustcFlags"])
	}
}
