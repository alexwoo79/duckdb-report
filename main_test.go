package main

import (
	"testing"

	"github.com/wailsapp/wails/v2/pkg/options/linux"
)

func TestLinuxWebviewGpuPolicy(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  linux.WebviewGpuPolicy
	}{
		{name: "default on demand", want: linux.WebviewGpuPolicyOnDemand},
		{name: "never", value: "never", want: linux.WebviewGpuPolicyNever},
		{name: "disabled", value: "off", want: linux.WebviewGpuPolicyNever},
		{name: "always", value: "always", want: linux.WebviewGpuPolicyAlways},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("DUCKDB_REPORT_WEBVIEW_GPU", test.value)
			if got := linuxWebviewGpuPolicy(); got != test.want {
				t.Fatalf("linuxWebviewGpuPolicy() = %v, want %v", got, test.want)
			}
		})
	}
}
