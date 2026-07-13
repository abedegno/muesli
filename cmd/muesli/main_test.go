package main

import (
	"testing"

	"github.com/abedegno/muesli/internal/config"
)

func TestIsEmbeddedMode(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.Config
		args []string
		want bool
	}{
		{name: "default false", cfg: config.Config{}, args: []string{"muesli"}, want: false},
		{name: "env true", cfg: config.Config{Embedded: true}, args: []string{"muesli"}, want: true},
		{name: "flag true", cfg: config.Config{}, args: []string{"muesli", "--embedded"}, want: true},
		{name: "both true", cfg: config.Config{Embedded: true}, args: []string{"muesli", "--embedded"}, want: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isEmbeddedMode(tc.cfg, tc.args); got != tc.want {
				t.Fatalf("isEmbeddedMode() = %v, want %v", got, tc.want)
			}
		})
	}
}
