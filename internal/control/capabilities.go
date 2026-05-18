package control

import (
	"encoding/json"
	"sort"
)

type Capability string

const (
	CapabilityThreadFork          Capability = "thread_fork"
	CapabilitySkillsList          Capability = "skills_list"
	CapabilityHooksList           Capability = "hooks_list"
	CapabilityMCPServerStatusList Capability = "mcp_server_status_list"
	CapabilityAppList             Capability = "app_list"
	CapabilityReviewStart         Capability = "review_start"
	CapabilityCommandExec         Capability = "command_exec"
	CapabilityThreadGoalSet       Capability = "thread_goal_set"
	CapabilityThreadGoalClear     Capability = "thread_goal_clear"
)

var capabilityMethods = map[Capability]string{
	CapabilityThreadFork:          "thread/fork",
	CapabilitySkillsList:          "skills/list",
	CapabilityHooksList:           "hooks/list",
	CapabilityMCPServerStatusList: "mcpServerStatus/list",
	CapabilityAppList:             "app/list",
	CapabilityReviewStart:         "review/start",
	CapabilityCommandExec:         "command/exec",
	CapabilityThreadGoalSet:       "thread/goal/set",
	CapabilityThreadGoalClear:     "thread/goal/clear",
}

type CapabilityMap struct {
	methods map[string]struct{}
}

func CapabilitiesFromMethods(methods []string) CapabilityMap {
	out := CapabilityMap{methods: map[string]struct{}{}}
	for _, method := range methods {
		if method == "" {
			continue
		}
		out.methods[method] = struct{}{}
	}
	return out
}

func CapabilitiesFromClientRequestSchema(data []byte) (CapabilityMap, error) {
	var schema struct {
		OneOf []struct {
			Properties struct {
				Method struct {
					Enum []string `json:"enum"`
				} `json:"method"`
			} `json:"properties"`
		} `json:"oneOf"`
	}
	if err := json.Unmarshal(data, &schema); err != nil {
		return CapabilityMap{}, err
	}
	methods := make([]string, 0, len(schema.OneOf))
	for _, option := range schema.OneOf {
		methods = append(methods, option.Properties.Method.Enum...)
	}
	return CapabilitiesFromMethods(methods), nil
}

func (m CapabilityMap) SupportsMethod(method string) bool {
	if m.methods == nil || method == "" {
		return false
	}
	_, ok := m.methods[method]
	return ok
}

func (m CapabilityMap) Supports(capability Capability) bool {
	method := capabilityMethods[capability]
	return m.SupportsMethod(method)
}

func (m CapabilityMap) Methods() []string {
	out := make([]string, 0, len(m.methods))
	for method := range m.methods {
		out = append(out, method)
	}
	sort.Strings(out)
	return out
}
