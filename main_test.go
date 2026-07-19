package main

import (
	"reflect"
	"testing"
)

func TestArgsWithDefaultCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want []string
	}{
		{name: "bare binary opens standalone", args: []string{"renart"}, want: []string{"renart", "standalone"}},
		{name: "help stays at root", args: []string{"renart", "--help"}, want: []string{"renart", "--help"}},
		{name: "explicit command is unchanged", args: []string{"renart", "web", "--no-open"}, want: []string{"renart", "web", "--no-open"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := argsWithDefaultCommand(tt.args); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("argsWithDefaultCommand(%v) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}
