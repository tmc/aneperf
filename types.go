//go:build darwin

package aneperf

import (
	"strings"
	"time"
)

// Sample represents a point-in-time snapshot of ANE performance counters.
type Sample struct {
	Timestamp time.Time  `json:"timestamp"`
	Device    DeviceInfo `json:"device"`
	ANEPowerW float64    `json:"ane_power_watts,omitempty"`
	Channels  []Channel  `json:"channels,omitempty"`
}

// Channel represents a single IOReport channel with its current value or states.
type Channel struct {
	Group    string       `json:"group"`
	SubGroup string       `json:"subgroup"`
	Channel  string       `json:"channel"`
	Unit     string       `json:"unit,omitempty"`
	Value    int64        `json:"value,omitempty"`
	States   []StateEntry `json:"states,omitempty"`
}

// StateEntry represents a named power management state with residency time.
type StateEntry struct {
	Name      string `json:"name"`
	Residency int64  `json:"residency_ns"`
}

// Snapshot is an opaque handle to a raw IOReport sample.
// Pass it to Sampler.Stop to compute a Delta.
type Snapshot struct {
	raw cfDictionaryRef
	t   time.Time
}

// Delta contains the difference between two snapshots.
type Delta struct {
	Duration time.Duration `json:"duration"`
	Device   DeviceInfo    `json:"device"`
	PowerW   float64       `json:"ane_power_watts"`
	Channels []Channel     `json:"channels,omitempty"`
}

// Metric selects which categories of metrics ReportMetrics emits.
// Combine with bitwise OR to select multiple categories.
type Metric int

const (
	MetricPower     Metric = 1 << iota // ane-watts
	MetricEnergy                       // ane-energy-{unit}/op
	MetricCompute                      // ane-compute-%
	MetricVoltage                      // {channel}-active-%
	MetricCluster                      // ane-cluster-active-%
	MetricThrottle                     // ane-throttle-* (events + detail)
	MetricInterrupt                    // ane-interrupts/op, handler counts
	MetricDuration                     // sample-ns/op

	// MetricDefault includes the most commonly useful categories:
	// power, energy, compute utilization, and sample duration.
	MetricDefault = MetricPower | MetricEnergy | MetricCompute | MetricDuration

	MetricAll = MetricPower | MetricEnergy | MetricCompute |
		MetricVoltage | MetricCluster | MetricThrottle |
		MetricInterrupt | MetricDuration
)

func (m Metric) has(flag Metric) bool { return m&flag != 0 }

// ReportMetrics reports delta metrics to a testing.B-compatible reporter.
//
// The optional metrics arguments select which categories to report.
// If none are provided, MetricDefault is used (power, energy, compute, duration).
// Pass MetricAll explicitly for the full set.
//
// Reported metrics:
//   - ane-watts: ANE power consumption (MetricPower)
//   - sample-ns/op: measurement duration (MetricDuration)
//   - ane-energy-{unit}/op: total ANE energy (MetricEnergy)
//   - {channel}-active-%: percentage of time not at VMIN (MetricVoltage)
//   - ane-throttle-events/op: throttle event count (MetricThrottle)
//   - ane-interrupts/op: total interrupt handler count (MetricInterrupt)
//   - ane-compute-%: weighted CE utilization (MetricCompute)
//   - ane-cluster-active-%: ANE cluster power-on residency (MetricCluster)
//   - ane-throttle-{reason}-act-%: per-reason throttle ACT residency (MetricThrottle)
func (d Delta) ReportMetrics(b interface{ ReportMetric(float64, string) }, metrics ...Metric) {
	mask := MetricDefault
	if len(metrics) > 0 {
		mask = 0
		for _, m := range metrics {
			mask |= m
		}
	}

	if mask.has(MetricPower) {
		b.ReportMetric(d.PowerW, "ane-watts")
	}
	if mask.has(MetricDuration) {
		b.ReportMetric(float64(d.Duration.Nanoseconds()), "sample-ns/op")
	}

	for _, ch := range d.Channels {
		switch ch.Group {
		case "Energy Model":
			if mask.has(MetricEnergy) && ch.Value != 0 {
				b.ReportMetric(float64(ch.Value), "ane-energy-"+ch.Unit+"/op")
			}
		default:
			if !strings.HasPrefix(ch.Group, "PMP") {
				break
			}
			if mask.has(MetricVoltage) && ch.SubGroup == "SOC Floor" && len(ch.States) > 0 {
				// Report active percentage for voltage state channels.
				var total, vminRes int64
				for _, s := range ch.States {
					total += s.Residency
					if len(s.Name) >= 4 {
						trimmed := s.Name
						for len(trimmed) > 0 && trimmed[0] == ' ' {
							trimmed = trimmed[1:]
						}
						if trimmed == "VMIN" {
							vminRes = s.Residency
						}
					}
				}
				if total > 0 {
					active := float64(total-vminRes) / float64(total) * 100
					name := sanitizeMetricName(ch.Channel)
					b.ReportMetric(active, name+"-active-%")
				}
			}
			// Report throttle events.
			if mask.has(MetricThrottle) && ch.Value > 0 && containsANE(ch.Channel) {
				b.ReportMetric(float64(ch.Value), "ane-throttle-events/op")
			}
		case "Interrupt Statistics (by index)":
			if mask.has(MetricInterrupt) && ch.Value > 0 && (containsANE(ch.Channel) || len(ch.Channel) > 20) {
				name := sanitizeMetricName(ch.Channel)
				b.ReportMetric(float64(ch.Value), name+"/op")
			}
		}
	}

	// Report interrupt totals.
	if mask.has(MetricInterrupt) {
		var totalInterrupts int64
		for _, ch := range d.Channels {
			if ch.Group == "Interrupt Statistics (by index)" && ch.Value > 0 {
				if len(ch.Channel) > 10 && ch.Channel[len(ch.Channel)-5:] == "Count" {
					totalInterrupts += ch.Value
				}
			}
		}
		if totalInterrupts > 0 {
			b.ReportMetric(float64(totalInterrupts), "ane-interrupts/op")
		}
	}

	// Report derived stats from newly classified channels.
	if mask.has(MetricCompute) || mask.has(MetricCluster) || mask.has(MetricThrottle) {
		stats := ComputeStats(d)
		if mask.has(MetricCompute) && stats.ActivePct > 0 {
			b.ReportMetric(stats.ActivePct, "ane-compute-%")
		}
		if mask.has(MetricCluster) && stats.ClusterActivePct > 0 {
			b.ReportMetric(stats.ClusterActivePct, "ane-cluster-active-%")
		}
		if mask.has(MetricThrottle) {
			for reason, actPct := range stats.ThrottleReasons {
				if actPct > 0 {
					b.ReportMetric(actPct, "ane-throttle-"+sanitizeMetricName(reason)+"-act-%")
				}
			}
		}
	}
}

// energyToWatts converts an energy delta (in the given unit) over durationMs to watts.
func energyToWatts(energy int64, unit string, durationMs float64) float64 {
	if durationMs <= 0 {
		durationMs = 1
	}
	rate := float64(energy) / (durationMs / 1000.0)
	switch unit {
	case "mJ":
		return rate / 1e3
	case "uJ":
		return rate / 1e6
	case "nJ":
		return rate / 1e9
	default:
		return rate / 1e6
	}
}

// computeANEPower extracts ANE power in watts from the energy model channels.
func computeANEPower(channels []Channel, durationMs float64) float64 {
	var total float64
	for _, ch := range channels {
		if ch.Group == "Energy Model" && containsANE(ch.Channel) && ch.Value != 0 {
			total += energyToWatts(ch.Value, ch.Unit, durationMs)
		}
	}
	return total
}

// sanitizeMetricName lowercases and replaces non-alphanumeric chars with hyphens.
func sanitizeMetricName(name string) string {
	var b []byte
	prevHyphen := true
	for i := range len(name) {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			b = append(b, c)
			prevHyphen = false
		case c >= 'A' && c <= 'Z':
			b = append(b, c+32) // toLower
			prevHyphen = false
		default:
			if !prevHyphen {
				b = append(b, '-')
				prevHyphen = true
			}
		}
	}
	// Trim trailing hyphen.
	if len(b) > 0 && b[len(b)-1] == '-' {
		b = b[:len(b)-1]
	}
	return string(b)
}
