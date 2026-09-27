package actionlint

import "testing"

func TestExplicitPermissionLevel(t *testing.T) {
	tests := []struct {
		name  string
		perms *ReusableWorkflowPermissions
		scope string
		level int
		known bool
	}{
		{
			name:  "write all grants id token",
			perms: &ReusableWorkflowPermissions{All: "write-all"},
			scope: "id-token",
			level: permissionWrite,
			known: true,
		},
		{
			name:  "read all does not grant id token",
			perms: &ReusableWorkflowPermissions{All: "read-all"},
			scope: "id-token",
			level: permissionNone,
			known: true,
		},
		{
			name:  "dynamic scope is unknown",
			perms: &ReusableWorkflowPermissions{Scopes: map[string]string{"contents": "${{ inputs.permission }}"}},
			scope: "contents",
			known: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			level, known := explicitPermissionLevel(tc.perms, tc.scope)
			if level != tc.level || known != tc.known {
				t.Fatalf("got level=%d, known=%t; want level=%d, known=%t", level, known, tc.level, tc.known)
			}
		})
	}
}

func TestDefaultPermissionLevel(t *testing.T) {
	if got := defaultPermissionLevel(AssumeDefaultPermissionsRestricted, "contents"); got != permissionRead {
		t.Fatalf("restricted contents permission = %d, want read", got)
	}
	if got := defaultPermissionLevel(AssumeDefaultPermissionsRestricted, "pull-requests"); got != permissionNone {
		t.Fatalf("restricted pull-requests permission = %d, want none", got)
	}
	if got := defaultPermissionLevel(AssumeDefaultPermissionsPermissive, "id-token"); got != permissionNone {
		t.Fatalf("permissive id-token permission = %d, want none", got)
	}
}
