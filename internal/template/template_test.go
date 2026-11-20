package template

import (
	"errors"
	"testing"
)

func TestResolve(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		params  map[string]string
		want    string
		wantErr bool
	}{
		{
			name:   "replace variables",
			input:  "SELECT * FROM {{ table }} WHERE d >= '{{start}}'",
			params: map[string]string{"table": "sales", "start": "2026-01-01"},
			want:   "SELECT * FROM sales WHERE d >= '2026-01-01'",
		},
		{
			name:    "missing variable",
			input:   "SELECT {{missing}}",
			params:  map[string]string{},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := Resolve(test.input, test.params)
			if test.wantErr {
				if !errors.Is(err, ErrVariableMissing) {
					t.Fatalf("Resolve() error = %v, want ErrVariableMissing", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("Resolve() = %q, want %q", got, test.want)
			}
		})
	}
}
