package api

import (
	"encoding/json"
	"kept/server/internal/i18n"
	"net/http"
)

// The web app manifest (v1.5.9, after paperless-ngx): “add to home screen”
// on Android gives an app-like icon that opens without the browser's
// address bar. Built from the instance's name and logo; the default icons
// ship with the web app. Installing only: no offline service worker.
func (s *Server) manifest(w http.ResponseWriter, r *http.Request) {
	b, err := s.branding.Get(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "internal", i18n.N("读取名称失败"), nil)
		return
	}
	icons := []map[string]string{}
	if b.LogoVersion != "" {
		// a custom logo first; its size is not known, the browser scales it
		icons = append(icons, map[string]string{"src": "/api/v1/branding/logo?v=" + b.LogoVersion, "sizes": "any", "purpose": "any"})
	}
	icons = append(icons,
		map[string]string{"src": "/icon-192.png", "sizes": "192x192", "type": "image/png", "purpose": "any"},
		map[string]string{"src": "/icon-512.png", "sizes": "512x512", "type": "image/png", "purpose": "any"},
		map[string]string{"src": "/icon-maskable-512.png", "sizes": "512x512", "type": "image/png", "purpose": "maskable"},
	)
	m := map[string]any{
		"name": b.Name, "short_name": b.Name, "lang": "zh-CN",
		"start_url": "/", "scope": "/", "display": "standalone",
		"background_color": "#FFFFFF", "theme_color": "#2563EB", "icons": icons,
	}
	w.Header().Set("Content-Type", "application/manifest+json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_ = json.NewEncoder(w).Encode(m)
}
