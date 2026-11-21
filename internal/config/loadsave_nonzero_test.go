package config

import (
	"reflect"
	"testing"
)

func TestNonZeroAgentsFiltersEmptyDefinitions(t *testing.T) {
	off := false
	tests := []struct {
		name string
		in   map[string]AgentDefinition
		want map[string]AgentDefinition
	}{
		{name: "nil map", in: nil, want: map[string]AgentDefinition{}},
		{name: "empty map", in: map[string]AgentDefinition{}, want: map[string]AgentDefinition{}},
		{
			name: "only zero entries",
			in:   map[string]AgentDefinition{"a": {}, "b": {}},
			want: map[string]AgentDefinition{},
		},
		{
			name: "mixed zero and non-zero entries",
			in: map[string]AgentDefinition{
				"empty":  {},
				"path":   {Path: "sh"},
				"policy": {InstallRules: &off},
			},
			want: map[string]AgentDefinition{
				"path":   {Path: "sh"},
				"policy": {InstallRules: &off},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var before map[string]AgentDefinition
			if tt.in != nil {
				before = make(map[string]AgentDefinition, len(tt.in))
				for k, v := range tt.in {
					before[k] = v
				}
			}
			got := nonZeroAgents(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("nonZeroAgents() = %#v, want %#v", got, tt.want)
			}
			if !reflect.DeepEqual(tt.in, before) {
				t.Fatalf("input was modified: %#v, want %#v", tt.in, before)
			}
			if tt.in != nil {
				got["new"] = AgentDefinition{Path: "x"}
				if _, ok := tt.in["new"]; ok {
					t.Fatal("result aliases the input map")
				}
			}
		})
	}
}
