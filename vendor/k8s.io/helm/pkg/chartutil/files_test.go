/*
Copyright The Helm Authors.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package chartutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

var cases = []struct {
	path, data string
}{
	{"ship/captain.txt", "The Captain"},
	{"ship/stowaway.txt", "Legatt"},
	{"story/name.txt", "The Secret Sharer"},
	{"story/author.txt", "Joseph Conrad"},
	{"multiline/test.txt", "bar\nfoo"},
}

func getTestFiles() Files {
	a := make(Files, len(cases))
	for _, c := range cases {
		a[c.path] = []byte(c.data)
	}
	return a
}

func TestNewFiles(t *testing.T) {
	files := getTestFiles()
	if len(files) != len(cases) {
		t.Errorf("Expected len() = %d, got %d", len(cases), len(files))
	}

	for i, f := range cases {
		if got := string(files.GetBytes(f.path)); got != f.data {
			t.Errorf("%d: expected %q, got %q", i, f.data, got)
		}
		if got := files.Get(f.path); got != f.data {
			t.Errorf("%d: expected %q, got %q", i, f.data, got)
		}
	}
}

func TestFileGlob(t *testing.T) {
	as := assert.New(t)

	f := getTestFiles()

	matched := f.Glob("story/**")

	as.Len(matched, 2, "Should be two files in glob story/**")
	as.Equal("Joseph Conrad", matched.Get("story/author.txt"))
}

func TestToConfig(t *testing.T) {
	as := assert.New(t)

	f := getTestFiles()
	out := f.Glob("**/captain.txt").AsConfig()
	as.Equal("captain.txt: The Captain", out)

	out = f.Glob("ship/**").AsConfig()
	as.Equal("captain.txt: The Captain\nstowaway.txt: Legatt", out)
}

func TestToSecret(t *testing.T) {
	as := assert.New(t)

	f := getTestFiles()

	out := f.Glob("ship/**").AsSecrets()
	as.Equal("captain.txt: VGhlIENhcHRhaW4=\nstowaway.txt: TGVnYXR0", out)
}

func TestLines(t *testing.T) {
	as := assert.New(t)

	f := getTestFiles()

	out := f.Lines("multiline/test.txt")
	as.Len(out, 2)

	as.Equal("bar", out[0])
}

func TestToYAML(t *testing.T) {
	expect := "foo: bar"
	v := struct {
		Foo string `json:"foo"`
	}{
		Foo: "bar",
	}

	if got := ToYAML(v); got != expect {
		t.Errorf("Expected %q, got %q", expect, got)
	}
}

func TestToTOML(t *testing.T) {
	expect := "foo = \"bar\"\n"
	v := struct {
		Foo string `toml:"foo"`
	}{
		Foo: "bar",
	}

	if got := ToTOML(v); got != expect {
		t.Errorf("Expected %q, got %q", expect, got)
	}

	// Regression for https://github.com/helm/helm/issues/2271
	dict := map[string]map[string]string{
		"mast": {
			"sail": "white",
		},
	}
	got := ToTOML(dict)
	expect = "[mast]\n  sail = \"white\"\n"
	if got != expect {
		t.Errorf("Expected:\n%s\nGot\n%s\n", expect, got)
	}
}

func TestFromYAML(t *testing.T) {
	doc := `hello: world
one:
  two: three
`
	dict := FromYAML(doc)
	if err, ok := dict["Error"]; ok {
		t.Fatalf("Parse error: %s", err)
	}

	if len(dict) != 2 {
		t.Fatal("expected two elements.")
	}

	world := dict["hello"]
	if world.(string) != "world" {
		t.Fatal("Expected the world. Is that too much to ask?")
	}

	// This should fail because we don't currently support lists:
	doc2 := `
- one
- two
- three
`
	dict = FromYAML(doc2)
	if _, ok := dict["Error"]; !ok {
		t.Fatal("Expected parser error")
	}
}

func TestToJSON(t *testing.T) {
	expect := `{"foo":"bar"}`
	v := struct {
		Foo string `json:"foo"`
	}{
		Foo: "bar",
	}

	if got := ToJSON(v); got != expect {
		t.Errorf("Expected %q, got %q", expect, got)
	}
}

func TestFromJSON(t *testing.T) {
	doc := `{
  "hello": "world",
  "one": {
      "two": "three"
  }
}
`
	dict := FromJSON(doc)
	if err, ok := dict["Error"]; ok {
		t.Fatalf("Parse error: %s", err)
	}

	if len(dict) != 2 {
		t.Fatal("expected two elements.")
	}

	world := dict["hello"]
	if world.(string) != "world" {
		t.Fatal("Expected the world. Is that too much to ask?")
	}

	// This should fail because we don't currently support lists:
	doc2 := `
[
 "one",
 "two",
 "three"
]
`
	dict = FromJSON(doc2)
	if _, ok := dict["Error"]; !ok {
		t.Fatal("Expected parser error")
	}
}
