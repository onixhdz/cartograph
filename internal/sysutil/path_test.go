package sysutil

import "testing"

func TestIsPathSegment(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{name: "plugin", want: true},
		{name: "plugin.exe", want: true},
		{name: "model-Q8_0.gguf", want: true},
		{name: "", want: false},
		{name: ".", want: false},
		{name: "..", want: false},
		{name: "dir/plugin", want: false},
		{name: `dir\plugin`, want: false},
	}

	for _, tt := range tests {
		if got := IsPathSegment(tt.name); got != tt.want {
			t.Fatalf("IsPathSegment(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}
