package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"
)

var appVersion = envOr("APP_VERSION", "unknown")

func main() {
	port := envOr("PORT", "8080")
	mux := http.NewServeMux()
	mux.HandleFunc("/", handleHome)
	mux.HandleFunc("/api/whoami", handleWhoami)
	mux.HandleFunc("/admin/crash", handleCrash)

	log.Printf("canary-demo-app listening on :%s version=%s", port, appVersion)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatal(err)
	}
}

func handleHome(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Connection", "close")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	color, badge := versionStyle(appVersion)
	hostname, _ := os.Hostname()
	fmt.Fprintf(w, `<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>%s</title>
<style>
body{font-family:system-ui,sans-serif;display:flex;align-items:center;justify-content:center;min-height:100vh;margin:0;background:#0f172a;color:#e2e8f0}
.card{text-align:center;padding:2.5rem 3rem;border-radius:16px;background:#1e293b;box-shadow:0 8px 32px rgba(0,0,0,.35)}
.badge{font-size:2rem;font-weight:700;color:%s;margin-bottom:.5rem}
.sub{color:#94a3b8;font-size:.95rem}
</style></head><body>
<div class="card"><div class="badge">%s</div><div class="sub">Pod: %s</div></div>
</body></html>`, badge, color, badge, hostname)
}

func handleWhoami(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Connection", "close")
	w.Header().Set("Content-Type", "application/json")
	hostname, _ := os.Hostname()
	_ = json.NewEncoder(w).Encode(map[string]string{
		"version":  appVersion,
		"pod":      hostname,
		"hostname": hostname,
	})
}

func handleCrash(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	log.Printf("crash requested, will exit shortly")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"crashing"}`))
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	// 응답 전송 후 종료 (클라이언트 EOF 방지)
	time.AfterFunc(200*time.Millisecond, func() { os.Exit(1) })
}

func versionStyle(version string) (color, badge string) {
	switch version {
	case "Canary v2":
		return "#4ade80", "Canary v2"
	case "Stable v1":
		return "#60a5fa", "Stable v1"
	default:
		return "#fbbf24", version
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
