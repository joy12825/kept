package api

import (
	"net/http"
	"testing"
	"time"
)

func waitConverted(t *testing.T, admin *client, name string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		for _, it := range admin.mustDo(http.MethodGet, "/api/v1/admin/storages", nil, http.StatusOK).body["items"].([]any) {
			s := it.(map[string]any)
			if s["name"] != name {
				continue
			}
			if enc, ok := s["encryption"].(map[string]any); ok && enc["converting"] == false {
				if enc["error"] != nil {
					t.Fatalf("conversion: %v", enc)
				}
				return enc
			}
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatal("the conversion did not finish")
	return nil
}
