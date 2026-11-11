package auth

import "testing"

func TestPermissions(t *testing.T) {
	if Allowed("viewer", "camera") || Allowed("operator", "settings") || Allowed("developer", "boot-write") {
		t.Fatal("privilege boundary violated")
	}
	if !Allowed("viewer", "read") || !Allowed("operator", "camera") || !Allowed("admin", "settings") {
		t.Fatal("valid permission denied")
	}
}
