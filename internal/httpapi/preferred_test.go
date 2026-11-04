package httpapi

import "testing"

func TestPreferredRoutesRequireAdministratorAndRejectPrivateSource(t *testing.T) {
	_, h := testAPI(t)
	for _, path := range []string{"preferred/import", "preferred/random", "preferred/preview", "preferred/generate"} {
		r := call(t, h, "POST", "/api/v1/catalog/"+path, map[string]any{}, false)
		if r.Code != 401 {
			t.Fatal(path, r.Code)
		}
	}
	r := call(t, h, "POST", "/api/v1/catalog/preferred/import", map[string]any{"url": "https://169.254.169.254/latest/meta-data", "mode": "feed", "port": 443, "limit": 10}, true)
	if r.Code == 200 {
		t.Fatal("metadata fetch accepted")
	}
	r = call(t, h, "POST", "/api/v1/catalog/preferred/random", map[string]any{"count": 5, "port": 443}, true)
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
}
