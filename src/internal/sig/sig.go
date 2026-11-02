// Package sig maps the named signals of the alignment baseline onto
// Sparkplug B metrics. A signal "Batter ready" with fields batchId and pans is
// carried as metrics "Batter ready/batchId" and "Batter ready/pans".
package sig

import (
	"strconv"
	"strings"

	"cakefactory/internal/sparkplug"
)

// Signal names, identical on both ends of every interface (alignment baseline).
const (
	OrderAndRecipe  = "Production order and recipe targets"
	IngredientReq   = "Ingredient request and reservation result"
	BatterReady     = "Batter ready"
	DepositingState = "Depositing stopped / working again"
	CooledCake      = "Cooled cake"
	PanRequest      = "Pan request"
	PanForWashing   = "Pan for washing"
	CleanPanSupply  = "Clean pan supply"
	PanAdmission    = "Stop / resume pan admission"
	BandState       = "Band state change"
	PalletReady     = "Pallet ready for shipment"
	Events          = "Log, warning, error and discard events"
	Heartbeat       = "Machine status (heartbeat)"
	RepairConfirmed = "Repair confirmed"
	SectionCommand  = "Section command"
	TargetSpeed     = "Target speed"
	FaultInjection  = "Fault injection"
	NodeControl     = "Node Control"
)

// Signal is a named set of fields.
type Signal struct {
	Name string
	F    map[string]any
}

// New builds a signal from alternating key, value arguments.
func New(name string, kv ...any) Signal {
	s := Signal{Name: name, F: map[string]any{}}
	for i := 0; i+1 < len(kv); i += 2 {
		s.F[kv[i].(string)] = kv[i+1]
	}
	return s
}

// Metrics encodes the signal as Sparkplug metrics.
func (s Signal) Metrics() []sparkplug.Metric {
	out := make([]sparkplug.Metric, 0, len(s.F))
	for k, v := range s.F {
		out = append(out, sparkplug.NewMetric(s.Name+"/"+k, v))
	}
	return out
}

// FromMetrics groups metrics back into signals by their name prefix.
func FromMetrics(ms []sparkplug.Metric) []Signal {
	idx := map[string]int{}
	var out []Signal
	for _, m := range ms {
		name, field, ok := strings.Cut(m.Name, "/")
		if !ok {
			continue
		}
		i, seen := idx[name]
		if !seen {
			i = len(out)
			idx[name] = i
			out = append(out, Signal{Name: name, F: map[string]any{}})
		}
		out[i].F[field] = m.Value
	}
	return out
}

// Str returns a string field.
func (s Signal) Str(k string) string {
	switch v := s.F[k].(type) {
	case string:
		return v
	case nil:
		return ""
	default:
		return anyString(v)
	}
}

func anyString(v any) string {
	switch x := v.(type) {
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	}
	return ""
}

// Int returns an integer field.
func (s Signal) Int(k string) int {
	switch v := s.F[k].(type) {
	case int64:
		return int(v)
	case float64:
		return int(v)
	}
	return 0
}

// Float returns a float field.
func (s Signal) Float(k string) float64 {
	switch v := s.F[k].(type) {
	case float64:
		return v
	case int64:
		return float64(v)
	}
	return 0
}

// Bool returns a boolean field.
func (s Signal) Bool(k string) bool {
	b, _ := s.F[k].(bool)
	return b
}
