package overrides

import (
    "encoding/json"
    "fmt"
    "os"
    "time"
)

// Minimal structures mirroring config/policy_overrides.json (global part only)

type Confidence struct {
    Value *float64 `json:"value,omitempty"`
    Min   *float64 `json:"min,omitempty"`
    Max   *float64 `json:"max,omitempty"`
}

type Checklist struct {
    Default  *int    `json:"default,omitempty"`
    Allow3If *string `json:"allow_3_if,omitempty"`
}

type Cooldown struct {
    Base *float64 `json:"base,omitempty"`
    Min  *float64 `json:"min,omitempty"`
    Max  *float64 `json:"max,omitempty"`
}

type Trial struct {
    Enabled        bool     `json:"enabled"`
    ConfidenceBand *string  `json:"confidence_band,omitempty"`
    RiskBudgetPct  *float64 `json:"risk_budget_pct,omitempty"`
}

type Global struct {
    ConfidenceThresholdBase *Confidence `json:"confidence_threshold_base,omitempty"`
    ChecklistMinHits        *Checklist  `json:"checklist_min_hits,omitempty"`
    CooldownMinutes         *Cooldown   `json:"cooldown_minutes,omitempty"`
    TrialEntry              *Trial      `json:"trial_entry,omitempty"`
}

type Meta struct {
    GeneratedAt string   `json:"generated_at"`
    TTLHours    *float64 `json:"ttl_hours,omitempty"`
    Approver    *string  `json:"approver,omitempty"`
}

type Overrides struct {
    Meta   Meta   `json:"meta"`
    Global Global `json:"global"`
    Symbols map[string]Global `json:"symbols,omitempty"`
}

func clamp(val, lo, hi *float64) (float64, bool) {
    if val == nil {
        return 0, false
    }
    v := *val
    if lo != nil && v < *lo {
        v = *lo
    }
    if hi != nil && v > *hi {
        v = *hi
    }
    return v, true
}

// BuildHeaderFromFile reads overrides JSON, validates TTL, clamps values, and returns a header string.
func BuildHeaderFromFile(path string) (string, bool) {
    b, err := os.ReadFile(path)
    if err != nil {
        return "", false
    }
    var ov Overrides
    if err := json.Unmarshal(b, &ov); err != nil {
        return "", false
    }
    if ov.Meta.TTLHours == nil || ov.Meta.GeneratedAt == "" {
        return "", false
    }
    gen, err := time.Parse(time.RFC3339, ov.Meta.GeneratedAt)
    if err != nil {
        return "", false
    }
    if time.Since(gen.UTC()) > time.Duration(*ov.Meta.TTLHours*float64(time.Hour)) {
        return "", false
    }

    // Render header lines
    validUntil := gen.UTC().Add(time.Duration(*ov.Meta.TTLHours * float64(time.Hour)))
    header := fmt.Sprintf("运行时策略覆盖（有效期至 %s)\n", validUntil.Format(time.RFC3339))

    if ov.Global.ConfidenceThresholdBase != nil {
        c := ov.Global.ConfidenceThresholdBase
        if val, ok := clamp(c.Value, c.Min, c.Max); ok {
            header += fmt.Sprintf("- confidence_threshold_base: %.0f\n", val)
        }
    }
    if ov.Global.ChecklistMinHits != nil && ov.Global.ChecklistMinHits.Default != nil {
        d := *ov.Global.ChecklistMinHits.Default
        extra := ""
        if ov.Global.ChecklistMinHits.Allow3If != nil && *ov.Global.ChecklistMinHits.Allow3If == "key3_strong" {
            extra = "；关键三项齐可放宽为3/7"
        }
        header += fmt.Sprintf("- checklist_min_hits: 默认 %d/7%s\n", d, extra)
    }
    if ov.Global.CooldownMinutes != nil && ov.Global.CooldownMinutes.Base != nil {
        header += fmt.Sprintf("- cooldown_minutes: %.0f\n", *ov.Global.CooldownMinutes.Base)
    }
    if ov.Global.TrialEntry != nil && ov.Global.TrialEntry.Enabled {
        band := ""
        if ov.Global.TrialEntry.ConfidenceBand != nil {
            band = *ov.Global.TrialEntry.ConfidenceBand
        }
        risk := 0.5
        if ov.Global.TrialEntry.RiskBudgetPct != nil {
            risk = *ov.Global.TrialEntry.RiskBudgetPct
        }
        header += fmt.Sprintf("- trial_entry: 开启；band=%s；risk_budget_pct=%.2f%%\n", band, risk)
    }
    // Optional: symbol-specific overrides (short form)
    if ov.Symbols != nil && len(ov.Symbols) > 0 {
        header += "- symbol_overrides:" + "\n"
        count := 0
        for sym, g := range ov.Symbols {
            if count >= 10 { // avoid long prompt
                header += "  - ... (truncated)\n"
                break
            }
            line := fmt.Sprintf("  - %s:", sym)
            if g.ConfidenceThresholdBase != nil {
                if v, ok := clamp(g.ConfidenceThresholdBase.Value, g.ConfidenceThresholdBase.Min, g.ConfidenceThresholdBase.Max); ok {
                    line += fmt.Sprintf(" confidence=%0.0f", v)
                }
            }
            if g.ChecklistMinHits != nil && g.ChecklistMinHits.Default != nil {
                line += fmt.Sprintf(" checklist=%d/7", *g.ChecklistMinHits.Default)
                if g.ChecklistMinHits.Allow3If != nil && *g.ChecklistMinHits.Allow3If == "key3_strong" {
                    line += " allow_3_if=key3_strong"
                }
            }
            if g.TrialEntry != nil && g.TrialEntry.Enabled {
                band := ""
                if g.TrialEntry.ConfidenceBand != nil {
                    band = *g.TrialEntry.ConfidenceBand
                }
                risk := 0.5
                if g.TrialEntry.RiskBudgetPct != nil {
                    risk = *g.TrialEntry.RiskBudgetPct
                }
                line += fmt.Sprintf(" trial_entry band=%s risk=%.2f%%", band, risk)
            }
            header += line + "\n"
            count++
        }
    }

    header += "- 注：BTC 门槛/防假突破/SLTP/清算距离/冷却下限等硬规则不变\n"

    return header, true
}
