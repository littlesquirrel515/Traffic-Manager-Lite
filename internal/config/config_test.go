package config

import "testing"

func TestTargetPolicyDefaultsAndLegacyCompatibility(t *testing.T) {
	t.Setenv("TML_ADMIN_PASSWORD", "test-password-123456789")
	for _, targets := range []string{"", " , , "} {
		t.Setenv("TML_ALLOWED_TARGETS", targets)
		c, e := Load()
		if e != nil || len(c.AllowedTargets) != 0 {
			t.Fatalf("default policy: %v %v", c.AllowedTargets, e)
		}
	}
	t.Setenv("TML_ALLOWED_TARGETS", " xray, xray-multi, 172.18.0.0/16 ")
	c, e := Load()
	if e != nil || len(c.AllowedTargets) != 3 || c.AllowedTargets[1] != "xray-multi" {
		t.Fatalf("legacy strict mode: %v %v", c.AllowedTargets, e)
	}
}
