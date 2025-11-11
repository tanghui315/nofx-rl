package telemetry

import (
    "encoding/json"
    "fmt"
    "os"
    "path/filepath"
    "time"
)

// TickEntry is a minimal schema for scout logging.
// Fields align with scripts/analyze_scout.py expectations but keep optional details empty for now.
type TickEntry struct {
    TS       string                 `json:"ts"`
    Symbol   string                 `json:"symbol"`
    Reason   string                 `json:"reason"` // e.g., "decision_tick"
    Features map[string]interface{} `json:"features"`
    Risk     map[string]interface{} `json:"risk"`
    Decision map[string]interface{} `json:"decision"`
}

// AppendJSONL appends a JSON object as single line to logs/trading/YYYY-MM-DD-trade-log.jsonl (UTC day).
func AppendJSONL(obj interface{}) error {
    now := time.Now().UTC()
    dir := filepath.Join("logs", "trading")
    if err := os.MkdirAll(dir, 0755); err != nil {
        return fmt.Errorf("mkdir logs/trading: %w", err)
    }
    file := filepath.Join(dir, fmt.Sprintf("%s-trade-log.jsonl", now.Format("2006-01-02")))
    f, err := os.OpenFile(file, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
    if err != nil {
        return fmt.Errorf("open jsonl: %w", err)
    }
    defer f.Close()
    b, err := json.Marshal(obj)
    if err != nil {
        return fmt.Errorf("marshal: %w", err)
    }
    if _, err := f.Write(append(b, '\n')); err != nil {
        return fmt.Errorf("write: %w", err)
    }
    return nil
}

