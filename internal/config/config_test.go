package config

import "testing"

func TestSystemPublicAddressesAreLiteralDeclarations(t *testing.T) {
	t.Setenv("TML_ADMIN_PASSWORD", "test-password-123456789")
	for _, value := range []string{"10.0.0.1", "169.254.169.254", "localhost", "https://example.com", "::1", "2001:db8::1%eth0"} {
		t.Setenv("TML_SYSTEM_PUBLIC_IPS", value)
		if _, err := Load(); err == nil {
			t.Fatal("accepted invalid public declaration", value)
		}
	}
	t.Setenv("TML_SYSTEM_PUBLIC_IPS", "35.198.225.153, 2001:db8::1")
	c, err := Load()
	if err != nil || len(c.SystemPublicIPs) != 2 {
		t.Fatal(c.SystemPublicIPs, err)
	}
}

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

func TestTrustedProxiesAreExplicitAndValidated(t *testing.T) {
	t.Setenv("TML_ADMIN_PASSWORD", "test-password-123456789")
	for _, value := range []string{"0.0.0.0/0", "::/0", "nginx", "invalid", "fe80::1%eth0"} {
		t.Setenv("TML_TRUSTED_PROXIES", value)
		if _, err := Load(); err == nil {
			t.Fatal("accepted unbounded or invalid proxy", value)
		}
	}
	t.Setenv("TML_TRUSTED_PROXIES", "172.18.0.3/32, 127.0.0.1, ::1")
	c, err := Load()
	if err != nil || len(c.TrustedProxies) != 3 {
		t.Fatal(c.TrustedProxies, err)
	}
}
