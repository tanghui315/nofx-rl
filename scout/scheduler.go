package scout

import (
    "encoding/json"
    "log"
    "nofx/overrides"
    "os"
    "os/exec"
    "time"
)

// StartScheduler launches a background goroutine that runs the Python analyzer
// every interval (default 45m) and derives policy_overrides.json from templates.
// It is safe to call multiple times; only one goroutine runs.

var started bool

func StartScheduler() {
    if started {
        return
    }
    started = true
    interval := 45 * time.Minute
    go func() {
        for {
            runOnce()
            time.Sleep(interval)
        }
    }()
}

func runOnce() {
    // 1) Run analyzer (best-effort)
    cmd := exec.Command("python3", "scripts/analyze_scout.py", "--hours", "48", "--horizon", "30")
    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr
    if err := cmd.Run(); err != nil {
        log.Printf("[scout] analyze_scout.py failed: %v", err)
        // continue; we may still have previous templates
    }

    // 2) Read templates
    b, err := os.ReadFile("analytics/opportunity_templates.json")
    if err != nil {
        log.Printf("[scout] read templates failed: %v", err)
        return
    }
    var templates []overrides.Template
    if err := json.Unmarshal(b, &templates); err != nil {
        log.Printf("[scout] parse templates failed: %v", err)
        return
    }
    // 3) Derive overrides
    ov := overrides.DeriveOverridesFromTemplates(templates)
    if ov == nil {
        // No strong template → remove overrides to fallback to defaults
        _ = os.Remove("config/policy_overrides.json")
        log.Printf("[scout] no significant template; overrides cleared")
        return
    }
    if err := overrides.SaveOverrides("config/policy_overrides.json", ov); err != nil {
        log.Printf("[scout] save overrides failed: %v", err)
        return
    }
    log.Printf("[scout] overrides updated; ttl=%.0fh", *ov.Meta.TTLHours)
}

