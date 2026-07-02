package job

import (
	"reflect"
	"testing"
)

func TestParseParametersFromScript(t *testing.T) {
	script := `pipeline {
	parameters {
		string(name: 'BRANCH', defaultValue: 'main', description: 'Branch to build')
		booleanParam(name: 'DEPLOY', defaultValue: false)
		choice(name: 'ENV', choices: ['dev', 'staging', 'prod'])
	}
	agent any
}`
	got := parseParametersFromScript(script)
	want := []string{"BRANCH=main", "DEPLOY=", "ENV=dev"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestMergeParamDefs_CLIOverridesByName(t *testing.T) {
	auto := []string{"BRANCH=main", "DEPLOY="}
	cli := []string{"BRANCH=develop", "EXTRA=1"}
	got := mergeParamDefs(auto, cli)
	want := []string{"BRANCH=develop", "DEPLOY", "EXTRA=1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestInjectAgent_ReplacesAgentBlock(t *testing.T) {
	script := `pipeline { agent { docker { image 'node' } } stages {} }`
	got := injectAgent(script, "my-node")
	want := `pipeline { agent { label 'my-node' } stages {} }`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestInjectAgent_ReplacesAgentAny(t *testing.T) {
	script := `pipeline { agent any stages {} }`
	got := injectAgent(script, "my-node")
	want := `pipeline { agent { label 'my-node' } stages {} }`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestInjectAgent_InsertsWhenMissing(t *testing.T) {
	script := `pipeline { stages {} }`
	got := injectAgent(script, "my-node")
	want := "pipeline {\n    agent { label 'my-node' } stages {} }"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestInjectAgent_NoopWhenNodeEmpty(t *testing.T) {
	script := `pipeline { agent any stages {} }`
	if got := injectAgent(script, ""); got != script {
		t.Errorf("expected no change, got %q", got)
	}
}
