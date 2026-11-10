//go:build !linux

package terminal

import (
	"reflect"
	"testing"
)

func TestParsePIDs(t *testing.T) {
	tests := []struct {
		name, out string
		want      []int
	}{
		{"two", "123\n456\n", []int{123, 456}},
		{"twice and noise", "7\n7\nx\n-3\n0\n8\n", []int{7, 8}},
		{"nothing", "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parsePIDs(tt.out); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parsePIDs = %v, want %v", got, tt.want)
			}
		})
	}
}
