package state

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/roboll/helmfile/pkg/environment"
	"github.com/roboll/helmfile/pkg/remote"

	"github.com/roboll/helmfile/pkg/testhelper"
	"go.uber.org/zap"

	"github.com/stretchr/testify/require"
)

func createFromYaml(content []byte, file string, env string, logger *zap.SugaredLogger) (*HelmState, error) {
	c := &StateCreator{
		logger:   logger,
		readFile: os.ReadFile,
		abs:      filepath.Abs,

		DeleteFile: os.Remove,
		Strict:     true,
	}
	return c.ParseAndLoad(content, filepath.Dir(file), file, env, true, nil)
}

func TestReadFromYaml(t *testing.T) {
	yamlFile := "example/path/to/yaml/file"
	yamlContent := []byte(`releases:
- name: myrelease
  namespace: mynamespace
  chart: mychart
`)
	state, err := createFromYaml(yamlContent, yamlFile, DefaultEnv, logger)
	if err != nil {
		t.Errorf("unxpected error: %v", err)
	}

	if state.Releases[0].Name != "myrelease" {
		t.Errorf("unexpected release name: expected=myrelease actual=%s", state.Releases[0].Name)
	}
	if state.Releases[0].Namespace != "mynamespace" {
		t.Errorf("unexpected chart namespace: expected=mynamespace actual=%s", state.Releases[0].Chart)
	}
	if state.Releases[0].Chart != "mychart" {
		t.Errorf("unexpected chart name: expected=mychart actual=%s", state.Releases[0].Chart)
	}
}

func TestReadFromYaml_NonexistentEnv(t *testing.T) {
	yamlFile := "example/path/to/yaml/file"
	yamlContent := []byte(`releases:
- name: myrelease
  namespace: mynamespace
  chart: mychart
`)
	_, err := createFromYaml(yamlContent, yamlFile, "production", logger)
	if err == nil {
		t.Error("expected error")
	}
}

type stateTestEnv struct {
	Files   map[string]string
	WorkDir string
}

func (testEnv stateTestEnv) MustLoadState(t *testing.T, file, envName string) *HelmState {
	t.Helper()

	testFs := testhelper.NewTestFs(testEnv.Files)

	if testFs.Cwd == "" {
		testFs.Cwd = "/"
	}

	yamlContent, ok := testEnv.Files[file]
	if !ok {
		t.Fatalf("no file named %q registered", file)
	}

	r := remote.NewRemote(logger, testFs.Cwd, testFs.ReadFile, testFs.DirectoryExistsAt, testFs.FileExistsAt)
	state, err := NewCreator(logger, testFs.ReadFile, testFs.FileExists, testFs.Abs, testFs.Glob, testFs.DirectoryExistsAt, nil, nil, "", r).
		ParseAndLoad([]byte(yamlContent), filepath.Dir(file), file, envName, true, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	return state
}

func TestReadFromYaml_NonDefaultEnv(t *testing.T) {
	yamlFile := "/example/path/to/helmfile.yaml"
	yamlContent := []byte(`environments:
  production:
    values:
    - foo.yaml
    - bar.yaml.gotmpl

releases:
- name: myrelease
  namespace: mynamespace
  chart: mychart
  values:
  - values.yaml.gotmpl
`)

	fooYamlFile := "/example/path/to/foo.yaml"
	fooYamlContent := []byte(`foo: foo
# As this file doesn't have an file extension ".gotmpl", this template expression should not be evaluated
baz: "{{ readFile \"baz.txt\" }}"`)

	barYamlFile := "/example/path/to/bar.yaml.gotmpl"
	barYamlContent := []byte(`foo: FOO
bar: {{ readFile "bar.txt" }}
env: {{ .Environment.Name }}
`)

	barTextFile := "/example/path/to/bar.txt"
	barTextContent := []byte("BAR")

	expected := map[string]interface{}{
		"foo": "FOO",
		"bar": "BAR",
		// As the file doesn't have an file extension ".gotmpl", this template expression should not be evaluated
		"baz": "{{ readFile \"baz.txt\" }}",
		"env": "production",
	}

	valuesFile := "/example/path/to/values.yaml.gotmpl"
	valuesContent := []byte(`env: {{ .Environment.Name }}
releaseName: {{ .Release.Name }}
releaseNamespace: {{ .Release.Namespace }}
`)

	expectedValues := `env: production
releaseName: myrelease
releaseNamespace: mynamespace
`

	testFs := testhelper.NewTestFs(map[string]string{
		fooYamlFile: string(fooYamlContent),
		barYamlFile: string(barYamlContent),
		barTextFile: string(barTextContent),
		valuesFile:  string(valuesContent),
	})
	testFs.Cwd = "/example/path/to"

	r := remote.NewRemote(logger, testFs.Cwd, testFs.ReadFile, testFs.DirectoryExistsAt, testFs.FileExistsAt)
	env := environment.Environment{
		Name: "production",
	}
	state, err := NewCreator(logger, testFs.ReadFile, testFs.FileExists, testFs.Abs, testFs.Glob, testFs.DirectoryExistsAt, nil, nil, "", r).
		ParseAndLoad(yamlContent, filepath.Dir(yamlFile), yamlFile, "production", true, &env)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	actual := state.Env.Values
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("unexpected environment values: expected=%v, actual=%v", expected, actual)
	}

	release := state.Releases[0]

	state.ApplyOverrides(&release)

	actualValuesData, err := state.RenderReleaseValuesFileToBytes(&release, valuesFile)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	actualValues := string(actualValuesData)

	if !reflect.DeepEqual(expectedValues, actualValues) {
		t.Errorf("unexpected values: expected=%v, actual=%v", expectedValues, actualValues)
	}
}

func TestReadFromYaml_OverrideNamespace(t *testing.T) {
	yamlFile := "/example/path/to/helmfile.yaml"
	yamlContent := []byte(`environments:
  production:
    values:
    - foo.yaml
    - bar.yaml.gotmpl

# A.k.a helmfile apply --namespace myns
namespace: myns

releases:
- name: myrelease
  namespace: mynamespace
  chart: mychart
  values:
  - values.yaml.gotmpl
`)

	fooYamlFile := "/example/path/to/foo.yaml"
	fooYamlContent := []byte(`foo: foo
# As this file doesn't have an file extension ".gotmpl", this template expression should not be evaluated
baz: "{{ readFile \"baz.txt\" }}"`)

	barYamlFile := "/example/path/to/bar.yaml.gotmpl"
	barYamlContent := []byte(`foo: FOO
bar: {{ readFile "bar.txt" }}
`)

	barTextFile := "/example/path/to/bar.txt"
	barTextContent := []byte("BAR")

	expected := map[string]interface{}{
		"foo": "FOO",
		"bar": "BAR",
		// As the file doesn't have an file extension ".gotmpl", this template expression should not be evaluated
		"baz": "{{ readFile \"baz.txt\" }}",
	}

	valuesFile := "/example/path/to/values.yaml.gotmpl"
	valuesContent := []byte(`env: {{ .Environment.Name }}
releaseName: {{ .Release.Name }}
releaseNamespace: {{ .Release.Namespace }}
overrideNamespace: {{ .Namespace }}
`)

	expectedValues := `env: production
releaseName: myrelease
releaseNamespace: myns
overrideNamespace: myns
`

	testFs := testhelper.NewTestFs(map[string]string{
		fooYamlFile: string(fooYamlContent),
		barYamlFile: string(barYamlContent),
		barTextFile: string(barTextContent),
		valuesFile:  string(valuesContent),
	})
	testFs.Cwd = "/example/path/to"

	r := remote.NewRemote(logger, testFs.Cwd, testFs.ReadFile, testFs.DirectoryExistsAt, testFs.FileExistsAt)
	state, err := NewCreator(logger, testFs.ReadFile, testFs.FileExists, testFs.Abs, testFs.Glob, testFs.DirectoryExistsAt, nil, nil, "", r).
		ParseAndLoad(yamlContent, filepath.Dir(yamlFile), yamlFile, "production", true, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	actual := state.Env.Values
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("unexpected environment values: expected=%v, actual=%v", expected, actual)
	}

	release := state.Releases[0]

	state.ApplyOverrides(&release)

	actualValuesData, err := state.RenderReleaseValuesFileToBytes(&release, valuesFile)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	actualValues := string(actualValuesData)

	if !reflect.DeepEqual(expectedValues, actualValues) {
		t.Errorf("unexpected values: expected=%v, actual=%v", expectedValues, actualValues)
	}
}

func TestReadFromYaml_StrictUnmarshalling(t *testing.T) {
	yamlFile := "example/path/to/yaml/file"
	yamlContent := []byte(`releases:
- name: myrelease
  namespace: mynamespace
  releases: mychart
`)
	_, err := createFromYaml(yamlContent, yamlFile, DefaultEnv, logger)
	if err == nil {
		t.Error("expected an error for wrong key 'releases' which is not in struct")
	}
}

func TestReadFromYaml_DeprecatedReleaseReferences(t *testing.T) {
	yamlFile := "example/path/to/yaml/file"
	yamlContent := []byte(`charts:
- name: myrelease
  chart: mychart
`)
	state, err := createFromYaml(yamlContent, yamlFile, DefaultEnv, logger)
	if err != nil {
		t.Errorf("unxpected error: %v", err)
	}

	if state.Releases[0].Name != "myrelease" {
		t.Errorf("unexpected release name: expected=myrelease actual=%s", state.Releases[0].Name)
	}
	if state.Releases[0].Chart != "mychart" {
		t.Errorf("unexpected chart name: expected=mychart actual=%s", state.Releases[0].Chart)
	}
}

func TestReadFromYaml_ConflictingReleasesConfig(t *testing.T) {
	yamlFile := "example/path/to/yaml/file"
	yamlContent := []byte(`charts:
- name: myrelease1
  chart: mychart1
releases:
- name: myrelease2
  chart: mychart2
`)
	_, err := createFromYaml(yamlContent, yamlFile, DefaultEnv, logger)
	if err == nil {
		t.Error("expected error")
	}
}

func TestReadFromYaml_FilterReleasesOnLabels(t *testing.T) {
	yamlFile := "example/path/to/yaml/file"
	yamlContent := []byte(`releases:
- name: myrelease1
  chart: mychart1
  labels:
    tier: frontend
    foo: bar
- name: myrelease2
  chart: mychart2
  labels:
    tier: frontend
- name: myrelease3
  chart: mychart3
  labels:
    tier: backend
`)
	cases := []struct {
		filter  LabelFilter
		results []bool
	}{
		{LabelFilter{positiveLabels: [][]string{{"tier", "frontend"}}},
			[]bool{true, true, false}},
		{LabelFilter{positiveLabels: [][]string{{"tier", "frontend"}, {"foo", "bar"}}},
			[]bool{true, false, false}},
		{LabelFilter{negativeLabels: [][]string{{"tier", "frontend"}}},
			[]bool{false, false, true}},
		{LabelFilter{positiveLabels: [][]string{{"tier", "frontend"}}, negativeLabels: [][]string{{"foo", "bar"}}},
			[]bool{false, true, false}},
	}
	state, err := createFromYaml(yamlContent, yamlFile, DefaultEnv, logger)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	for idx, c := range cases {
		for idx2, expected := range c.results {
			if f := c.filter.Match(state.Releases[idx2]); f != expected {
				t.Errorf("[case: %d][outcome: %d] Unexpected outcome wanted %t, got %t", idx, idx2, expected, f)
			}
		}
	}
}

func TestReadFromYaml_FilterNegatives(t *testing.T) {
	yamlFile := "example/path/to/yaml/file"
	yamlContent := []byte(`releases:
- name: myrelease1
  chart: mychart1
  labels:
    stage: pre
    foo: bar
- name: myrelease2
  chart: mychart2
  labels:
    stage: post
- name: myrelease3
  chart: mychart3
`)
	cases := []struct {
		filter  LabelFilter
		results []bool
	}{
		{LabelFilter{positiveLabels: [][]string{{"stage", "pre"}}},
			[]bool{true, false, false}},
		{LabelFilter{positiveLabels: [][]string{{"stage", "post"}}},
			[]bool{false, true, false}},
		{LabelFilter{negativeLabels: [][]string{{"stage", "pre"}, {"stage", "post"}}},
			[]bool{false, false, true}},
		{LabelFilter{negativeLabels: [][]string{{"foo", "bar"}}},
			[]bool{false, true, true}},
	}
	state, err := createFromYaml(yamlContent, yamlFile, DefaultEnv, logger)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	for idx, c := range cases {
		for idx2, expected := range c.results {
			if f := c.filter.Match(state.Releases[idx2]); f != expected {
				t.Errorf("[case: %d][outcome: %d] Unexpected outcome wanted %t, got %t", idx, idx2, expected, f)
			}
		}
	}
}

func TestReadFromYaml_Helmfiles_Selectors(t *testing.T) {
	tests := []struct {
		path      string
		content   []byte
		wantErr   bool
		helmfiles []SubHelmfileSpec
	}{
		{
			path: "working/selector",
			content: []byte(`helmfiles:
- simple/helmfile.yaml
- path: path/prefix/selector.yaml
  selectors:
    - name=zorba
    - foo=bar
- path: path/prefix/empty/selector.yaml
  selectors: []
- path: path/prefix/inherits/selector.yaml
  selectorsInherited: true
`),
			wantErr: false,
			helmfiles: []SubHelmfileSpec{{Path: "simple/helmfile.yaml", Selectors: nil, SelectorsInherited: false},
				{Path: "path/prefix/selector.yaml", Selectors: []string{"name=zorba", "foo=bar"}, SelectorsInherited: false},
				{Path: "path/prefix/empty/selector.yaml", Selectors: []string{}, SelectorsInherited: false},
				{Path: "path/prefix/inherits/selector.yaml", Selectors: nil, SelectorsInherited: true},
			},
		},
		{
			path: "failing2/selector",
			content: []byte(`helmfiles:
- path: failing2/helmfile.yaml
    wrongkey:
`),
			wantErr: true,
		},
		{
			path: "failing3/selector",
			content: []byte(`helmfiles:
- path: failing3/helmfile.yaml
    selectors: foo
`),
			wantErr: true,
		},
		{
			path: "failing4/selector",
			content: []byte(`helmfiles:
- path: failing4/helmfile.yaml
    selectors:
`),
			wantErr: true,
		},
		{
			path: "failing4/selector",
			content: []byte(`helmfiles:
- path: failing4/helmfile.yaml
    selectors:
      - colon: not-authorized
`),
			wantErr: true,
		},
		{
			path: "failing6/selector",
			content: []byte(`helmfiles:
- selectors:
    - whatever
`),
			wantErr: true,
		},
		{
			path: "failing7/selector",
			content: []byte(`helmfiles:
- path: foo/bar
  selectors:
  - foo=bar
  selectorsInherited: true
`),
			wantErr: true,
		},
	}
	for _, test := range tests {
		st, err := createFromYaml(test.content, test.path, DefaultEnv, logger)
		if err != nil {
			if test.wantErr {
				continue
			} else {
				t.Error("unexpected error:", err)
			}
		}
		require.Equalf(t, test.helmfiles, st.Helmfiles, "for path %s", test.path)
	}
}
