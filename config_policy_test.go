package actionlint

import (
	"strings"
	"testing"
)

func TestConfigPermissionAssumptionAndTimeout(t *testing.T) {
	cfg, err := ParseConfig([]byte("assume-default-permissions: permissive\ntimeout-minutes:\n  required: true\n  max: 30\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AssumeDefaultPermissions == nil || *cfg.AssumeDefaultPermissions != AssumeDefaultPermissionsPermissive || !cfg.TimeoutMinutes.Required || cfg.TimeoutMinutes.MaxMinutes != 30 {
		t.Fatalf("incorrect parsed policy: %+v", cfg)
	}
	for _, bad := range []struct{ input, message string }{
		{"assume-default-permissions: unlimited\n", "assume-default-permissions"},
		{"timeout-minutes:\n  max: -1\n", "timeout-minutes.max"},
	} {
		if _, err := ParseConfig([]byte(bad.input)); err == nil || !strings.Contains(err.Error(), bad.message) {
			t.Fatalf("config %q: expected error mentioning %q, got %v", bad.input, bad.message, err)
		}
	}
}
