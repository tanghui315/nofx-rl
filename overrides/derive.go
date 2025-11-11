package overrides

import (
    "encoding/json"
    "os"
    "time"
)

// Template structs for reading analytics/opportunity_templates.json
type TemplateIf struct {
    TFAgreeMin    *int     `json:"tf_agree_min,omitempty"`
    VolZMin       *float64 `json:"vol_z_min,omitempty"`
    OIChg15mMin   *float64 `json:"oi_chg_15m_min,omitempty"`
    FundingAbsMax *float64 `json:"funding_abs_max,omitempty"`
}

type TemplateMetrics struct {
    Support            int      `json:"support"`
    Winrate            *float64 `json:"winrate,omitempty"`
    RRMean             *float64 `json:"rr_mean,omitempty"`
    Sharpe             *float64 `json:"sharpe,omitempty"`
    UpliftVsBaseline   *float64 `json:"uplift_vs_baseline,omitempty"`
}

type Template struct {
    Regime     string           `json:"regime"`
    Symbol     string           `json:"symbol,omitempty"`
    TemplateID string           `json:"template_id"`
    If         TemplateIf       `json:"if"`
    Metrics    TemplateMetrics  `json:"metrics"`
    TTLHours   *float64         `json:"ttl_hours,omitempty"`
}

// DeriveOverridesFromTemplates applies a simple rule:
// If a key3-like template exists with sufficient support, create a soft override
// lowering threshold by ~1 point and allowing 3/7 when key3 holds, and enabling trial-entry.
func DeriveOverridesFromTemplates(templates []Template) *Overrides {
    // Collect global and symbol-level templates that meet minimal support
    var hasGlobal bool
    symSet := map[string]bool{}
    for _, t := range templates {
        if t.TemplateID != "key3_basic_v1" || t.Metrics.Support < 80 {
            continue
        }
        if t.Symbol == "" {
            hasGlobal = true
        } else {
            symSet[t.Symbol] = true
        }
    }
    if !hasGlobal && len(symSet) == 0 {
        return nil
    }
    // Defaults derived here; values are intentionally conservative and match docs
    v := 79.0 // assuming baseline 80
    mn := 78.0
    mx := 85.0
    d := 4
    allow := "key3_strong"
    band := "78..80"
    risk := 0.5
    ttl := 4.0
    now := time.Now().UTC().Format(time.RFC3339)

    ov := &Overrides{
        Meta: Meta{GeneratedAt: now, TTLHours: &ttl},
        Global: Global{},
    }
    if hasGlobal {
        ov.Global.ConfidenceThresholdBase = &Confidence{Value: &v, Min: &mn, Max: &mx}
        ov.Global.ChecklistMinHits = &Checklist{Default: &d, Allow3If: &allow}
        ov.Global.TrialEntry = &Trial{Enabled: true, ConfidenceBand: &band, RiskBudgetPct: &risk}
    }
    if len(symSet) > 0 {
        ov.Symbols = map[string]Global{}
        for sym := range symSet {
            ov.Symbols[sym] = Global{
                ConfidenceThresholdBase: &Confidence{Value: &v, Min: &mn, Max: &mx},
                ChecklistMinHits:        &Checklist{Default: &d, Allow3If: &allow},
                TrialEntry:              &Trial{Enabled: true, ConfidenceBand: &band, RiskBudgetPct: &risk},
            }
        }
    }
    return ov
}

func SaveOverrides(path string, ov *Overrides) error {
    b, err := json.MarshalIndent(ov, "", "  ")
    if err != nil { return err }
    return os.WriteFile(path, b, 0644)
}
