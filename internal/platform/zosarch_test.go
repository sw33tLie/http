// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package platform_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"os/exec"
	"testing"
	"text/template"

	"github.com/sw33tLie/http/internal/diff"
	"github.com/sw33tLie/http/internal/testenv"
)

var flagFix = flag.Bool("fix", false, "if true, fix out-of-date generated files")

// TestGenerated verifies that zosarch.go is up to date,
// or regenerates it if the -fix flag is set.
func TestGenerated(t *testing.T) {
	testenv.MustHaveGoRun(t)

	// Here we use 'go run cmd/dist' instead of 'go tool dist' in case the
	// installed cmd/dist is stale or missing. We don't want to miss a
	// skew in the data due to a stale binary.
	cmd := testenv.Command(t, "go", "run", "cmd/dist", "list", "-json", "-broken")

	// cmd/dist requires GOROOT to be set explicitly in the environment.
	cmd.Env = append(cmd.Environ(), "GOROOT="+testenv.GOROOT(t))

	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			t.Logf("stderr:\n%s", ee.Stderr)
		}
		t.Fatalf("%v: %v", cmd, err)
	}

	type listEntry struct {
		GOOS, GOARCH string
		CgoSupported bool
		FirstClass   bool
		Broken       bool
	}
	var entries []listEntry
	if err := json.Unmarshal(out, &entries); err != nil {
		t.Fatal(err)
	}

	tmplOut := new(bytes.Buffer)
	tmpl := template.Must(template.New("zosarch").Parse(zosarchTmpl))
	err = tmpl.Execute(tmplOut, entries)
	if err != nil {
		t.Fatal(err)
	}

	cmd = testenv.Command(t, "gofmt")
	cmd.Stdin = bytes.NewReader(tmplOut.Bytes())
	want, err := cmd.Output()
	if err != nil {
		t.Logf("stdin:\n%s", tmplOut.Bytes())
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			t.Logf("stderr:\n%s", ee.Stderr)
		}
		t.Fatalf("%v: %v", cmd, err)
	}

	got, err := os.ReadFile("zosarch.go")
	if err == nil && bytes.Equal(got, want) {
		return
	}

	if !*flagFix {
		if err != nil {
			t.Log(err)
		} else {
			t.Logf("diff:\n%s", diff.Diff("zosarch.go", got, "want", want))
		}
		t.Fatalf("zosarch.go is missing or out of date; to regenerate, run\ngo generate internal/platform")
	}

	if err := os.WriteFile("zosarch.go", want, 0666); err != nil {
		t.Fatal(err)
	}
}

const zosarchTmpl = `// Code generated by go test internal/platform -fix. DO NOT EDIT.

// To change the information in this file, edit the cgoEnabled and/or firstClass
// maps in cmd/dist/build.go, then run 'go generate internal/platform'.

package platform

// List is the list of all valid GOOS/GOARCH combinations,
// including known-broken ports.
var List = []OSArch{
{{range .}}	{ {{ printf "%q" .GOOS }}, {{ printf "%q" .GOARCH }} },
{{end}}
}

var distInfo = map[OSArch]osArchInfo {
{{range .}}	{ {{ printf "%q" .GOOS }}, {{ printf "%q" .GOARCH }} }:
{ {{if .CgoSupported}}CgoSupported: true, {{end}}{{if .FirstClass}}FirstClass: true, {{end}}{{if .Broken}} Broken: true, {{end}} },
{{end}}
}
`
