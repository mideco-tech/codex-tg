package control

import (
	"reflect"
	"testing"
)

func TestCapabilitiesFromMethods(t *testing.T) {
	capabilities := CapabilitiesFromMethods([]string{
		"thread/fork",
		"skills/list",
		"skills/list",
		"",
	})

	if !capabilities.Supports(CapabilityThreadFork) {
		t.Fatal("thread fork capability should be supported")
	}
	if !capabilities.Supports(CapabilitySkillsList) {
		t.Fatal("skills list capability should be supported")
	}
	if capabilities.Supports(CapabilityHooksList) {
		t.Fatal("hooks list capability should be unsupported")
	}
	if got, want := capabilities.Methods(), []string{"skills/list", "thread/fork"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Methods = %#v, want %#v", got, want)
	}
}

func TestCapabilitiesFromClientRequestSchema(t *testing.T) {
	schema := []byte(`{
	  "oneOf": [
	    {"properties": {"method": {"enum": ["thread/fork"]}}},
	    {"properties": {"method": {"enum": ["skills/list"]}}},
	    {"properties": {"method": {"enum": ["mcpServerStatus/list"]}}},
	    {"properties": {"method": {"enum": ["review/start"]}}},
	    {"properties": {"method": {"enum": ["command/exec"]}}}
	  ]
	}`)

	capabilities, err := CapabilitiesFromClientRequestSchema(schema)
	if err != nil {
		t.Fatalf("CapabilitiesFromClientRequestSchema failed: %v", err)
	}
	for _, capability := range []Capability{
		CapabilityThreadFork,
		CapabilitySkillsList,
		CapabilityMCPServerStatusList,
		CapabilityReviewStart,
		CapabilityCommandExec,
	} {
		if !capabilities.Supports(capability) {
			t.Fatalf("%s capability should be supported", capability)
		}
	}
	if capabilities.Supports(CapabilityThreadGoalSet) {
		t.Fatal("future goal set capability should be unsupported when schema omits thread/goal/set")
	}
}

func TestCapabilitiesFromClientRequestSchemaRejectsInvalidJSON(t *testing.T) {
	if _, err := CapabilitiesFromClientRequestSchema([]byte(`{`)); err == nil {
		t.Fatal("invalid JSON succeeded")
	}
}
