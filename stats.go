//go:build darwin

package aneperf

import "strings"

// DeltaStats contains derived metrics computed from a Delta.
type DeltaStats struct {
	ActivePct        float64 // weighted CE utilization or voltage fallback
	PeakCEBucket     string  // CE bucket with highest residency ("0%", "45%", etc.)
	PeakCEPct        float64 // residency % in that peak bucket
	TotalInterrupts  int64
	InterruptRate    float64 // interrupts/sec (using Delta.Duration)
	TotalThrottles   int64
	GPUActivePct     float64            // GPU active residency from GPU performance states
	ClusterActivePct float64            // first PACC*_ANE channel ACT / (ACT+INACT) * 100
	ThrottleReasons  map[string]float64 // throttle name → ACT residency %
}

// ComputeStats derives summary metrics from a Delta.
func ComputeStats(d Delta) DeltaStats {
	cat := ClassifyChannels(d.Channels)
	var s DeltaStats

	// Active percentage — prefer Fast-Die CE histogram, fall back to voltage.
	s.ActivePct = computeActivePct(cat.ComputeEn)
	if s.ActivePct == 0 {
		s.ActivePct = computeVoltageActivePct(cat.Voltage)
	}

	// Peak CE bucket.
	s.PeakCEBucket, s.PeakCEPct = peakCEBucket(cat.ComputeEn)

	// Interrupts.
	for _, ch := range cat.Interrupt {
		if ch.Value > 0 {
			s.TotalInterrupts += ch.Value
		}
	}
	durSec := d.Duration.Seconds()
	if durSec > 0 && s.TotalInterrupts > 0 {
		s.InterruptRate = float64(s.TotalInterrupts) / durSec
	}

	// Throttles.
	for _, ch := range cat.Throttle {
		if ch.Value > 0 {
			s.TotalThrottles += ch.Value
		}
	}

	s.GPUActivePct = computeGPUActivePct(cat.GPUStats)

	// Cluster power — compute ACT residency from PACC*_ANE channels.
	s.ClusterActivePct = computeClusterActivePct(cat.ClusterPower)

	// Throttle detail — compute ACT residency % per throttle reason.
	s.ThrottleReasons = computeThrottleReasons(cat.ThrottleDetail)

	return s
}

// computeActivePct computes weighted average CE utilization from Fast-Die CE channels.
func computeActivePct(channels []Channel) float64 {
	for _, ch := range channels {
		if len(ch.States) == 0 {
			continue
		}
		var totalRes int64
		var weightedSum float64
		for _, s := range ch.States {
			totalRes += s.Residency
			name := strings.TrimSpace(s.Name)
			name = strings.TrimSuffix(name, "%")
			pct := parseFloat(name)
			weightedSum += pct * float64(s.Residency)
		}
		if totalRes > 0 {
			return weightedSum / float64(totalRes)
		}
	}
	return 0
}

// computeVoltageActivePct computes active percentage from voltage state channels.
func computeVoltageActivePct(channels []Channel) float64 {
	maxActive := 0.0
	for _, ch := range channels {
		if len(ch.States) == 0 {
			continue
		}
		var total, vminRes int64
		hasVMIN := false
		for _, s := range ch.States {
			total += s.Residency
			if strings.TrimSpace(s.Name) == "VMIN" {
				vminRes = s.Residency
				hasVMIN = true
			}
		}
		if hasVMIN && total > 0 {
			active := float64(total-vminRes) / float64(total) * 100
			if active > maxActive {
				maxActive = active
			}
		}
	}
	return maxActive
}

// peakCEBucket returns the CE bucket name and its residency percentage.
func peakCEBucket(channels []Channel) (string, float64) {
	for _, ch := range channels {
		if len(ch.States) == 0 {
			continue
		}
		var total int64
		for _, s := range ch.States {
			total += s.Residency
		}
		if total == 0 {
			continue
		}
		peakName := ""
		peakPct := 0.0
		for _, s := range ch.States {
			pct := float64(s.Residency) / float64(total) * 100
			if pct > peakPct {
				peakPct = pct
				peakName = strings.TrimSpace(s.Name)
			}
		}
		return peakName, peakPct
	}
	return "", 0
}

// computeClusterActivePct returns the ACT residency percentage across cluster power channels.
func computeClusterActivePct(channels []Channel) float64 {
	for _, ch := range channels {
		if len(ch.States) == 0 {
			continue
		}
		var actRes, total int64
		for _, s := range ch.States {
			total += s.Residency
			if strings.TrimSpace(s.Name) == "ACT" {
				actRes = s.Residency
			}
		}
		if total > 0 {
			return float64(actRes) / float64(total) * 100
		}
	}
	return 0
}

func computeGPUActivePct(channels []Channel) float64 {
	for _, ch := range channels {
		if len(ch.States) == 0 {
			continue
		}
		var activeRes, total int64
		for _, s := range ch.States {
			total += s.Residency
			name := strings.TrimSpace(s.Name)
			if name != "OFF" && name != "IDLE" && name != "DOWN" {
				activeRes += s.Residency
			}
		}
		if total > 0 {
			return float64(activeRes) / float64(total) * 100
		}
	}
	return 0
}

// computeThrottleReasons returns per-throttle ACT residency percentages.
func computeThrottleReasons(channels []Channel) map[string]float64 {
	if len(channels) == 0 {
		return nil
	}
	reasons := make(map[string]float64, len(channels))
	for _, ch := range channels {
		if len(ch.States) == 0 {
			continue
		}
		var actRes, total int64
		for _, s := range ch.States {
			total += s.Residency
			if strings.TrimSpace(s.Name) == "ACT" {
				actRes = s.Residency
			}
		}
		if total > 0 {
			reasons[ch.Channel] = float64(actRes) / float64(total) * 100
		}
	}
	return reasons
}

// parseFloat parses a simple non-negative float from a string.
func parseFloat(s string) float64 {
	var result float64
	var frac float64
	decimal := false
	divisor := 1.0
	for i := range len(s) {
		c := s[i]
		if c >= '0' && c <= '9' {
			if decimal {
				divisor *= 10
				frac += float64(c-'0') / divisor
			} else {
				result = result*10 + float64(c-'0')
			}
		} else if c == '.' && !decimal {
			decimal = true
		}
	}
	return result + frac
}
