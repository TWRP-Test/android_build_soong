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

package find_input_delta_lib

import (
	"io"
	"text/template"

	fid_exp "android/soong/cmd/find_input_delta/find_input_delta_proto"
	"google.golang.org/protobuf/proto"
)

var DefaultTemplate = `
	{{- define "contents"}}
		{{- range .Deletions}}-{{.}} {{end}}
		{{- range .Additions}}+{{.}} {{end}}
		{{- range .Changes}}+{{- .Name}} {{end}}
		{{- range .Changes}}
		  {{- if or .Additions .Deletions .Changes}}--file {{.Name}} {{template "contents" .}}--endfile {{end}}
		{{- end}}
	{{- end}}
	{{- template "contents" .}}`

type FileList struct {
	// The name of the parent for the list of file differences.
	// For the outermost FileList, this is the name of the ninja target.
	// Under `Changes`, it is the name of the changed file.
	Name string

	// The added files
	Additions []string

	// The deleted files
	Deletions []string

	// The modified files
	Changes []FileList
}

func (fl FileList) Marshal() (*fid_exp.FileList, error) {
	ret := &fid_exp.FileList{
		Name: proto.String(fl.Name),
	}
	if len(fl.Additions) > 0 {
		ret.Additions = fl.Additions
	}
	for _, ch := range fl.Changes {
		change, err := ch.Marshal()
		if err != nil {
			return nil, err
		}
		ret.Changes = append(ret.Changes, change)
	}
	if len(fl.Deletions) > 0 {
		ret.Deletions = fl.Deletions
	}
	return ret, nil
}

func (fl FileList) Format(wr io.Writer, format string) error {
	tmpl, err := template.New("filelist").Parse(format)
	if err != nil {
		return err
	}
	return tmpl.Execute(wr, fl)
}
