//go:build darwin

package aneperf

import "strings"

// DeltaStats contains derived metrics computed from a Delta.
type DeltaStats struct {
	ActivePct        float64 // weighted CE utilization or voltage fallback
	ActiveSource     string  // "compute", "voltage", or "none"
	PeakCEBucket     string  // CE bucket with highest residency ("0%", "45%", etc.)
	PeakCEPct        float64 // residency % in that peak bucket
	TotalInterrupts  int64
	InterruptRate    float64 // interrupts/sec (using Delta.Duration)
	TotalThrottles   int64
	GPUActivePct     float64            // GPU active residency from GPU performance states
	ClusterActivePct float64            // average ACT residency across PACC*_ANE clusters
	ThrottleReasons  map[string]float64 // throttle name → ACT residency %
}

// ComputeStats derives summary metrics from a Delta.
func ComputeStats(d Delta) DeltaStats {
	cat := ClassifyChannels(d.Channels)
	var s DeltaStats

	// Active percentage — prefer Fast-Die CE histogram, fall back to voltage.
	var hasComputeSamples bool
	s.ActivePct, hasComputeSamples = computeActivePct(cat.ComputeEn)
	if hasComputeSamples {
		s.ActiveSource = "compute"
	} else if voltagePct, hasVoltageSamples := computeVoltageActivePct(cat.Voltage); hasVoltageSamples {
		s.ActivePct = voltagePct
		s.ActiveSource = "voltage"
	} else {
		s.ActiveSource = "none"
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

// computeActivePct averages the utilization of all Fast-Die compute clusters.
// A cluster with no residency contributes zero when another cluster sampled.
func computeActivePct(channels []Channel) (float64, bool) {
	var clusterSum float64
	var clusterCount int
	var hasSamples bool
	for _, ch := range channels {
		if len(ch.States) == 0 {
			continue
		}
		clusterCount++
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
			clusterSum += weightedSum / float64(totalRes)
			hasSamples = true
		}
	}
	if !hasSamples {
		return 0, false
	}
	return clusterSum / float64(clusterCount), true
}

// computeVoltageActivePct averages non-VMIN residency across ANE voltage channels.
func computeVoltageActivePct(channels []Channel) (float64, bool) {
	var activeSum float64
	var channelCount int
	var hasSamples bool
	for _, ch := range channels {
		if len(ch.States) == 0 {
			continue
		}
		channelCount++
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
			activeSum += float64(total-vminRes) / float64(total) * 100
			hasSamples = true
		}
	}
	if !hasSamples {
		return 0, false
	}
	return activeSum / float64(channelCount), true
}

// peakCEBucket combines the CE histograms of every compute cluster.
func peakCEBucket(channels []Channel) (string, float64) {
	residencyByBucket := make(map[string]int64)
	var bucketOrder []string
	var total int64
	for _, ch := range channels {
		for _, s := range ch.States {
			if s.Residency <= 0 {
				continue
			}
			name := strings.TrimSpace(s.Name)
			if _, ok := residencyByBucket[name]; !ok {
				bucketOrder = append(bucketOrder, name)
			}
			residencyByBucket[name] += s.Residency
			total += s.Residency
		}
	}
	if total == 0 {
		return "", 0
	}
	peakName := ""
	var peakResidency int64
	for _, name := range bucketOrder {
		if residencyByBucket[name] > peakResidency {
			peakName = name
			peakResidency = residencyByBucket[name]
		}
	}
	return peakName, float64(peakResidency) / float64(total) * 100
}

// computeClusterActivePct averages ACT residency across cluster power channels.
func computeClusterActivePct(channels []Channel) float64 {
	var activeSum float64
	var channelCount int
	for _, ch := range channels {
		if len(ch.States) == 0 {
			continue
		}
		channelCount++
		var actRes, total int64
		for _, s := range ch.States {
			total += s.Residency
			if strings.TrimSpace(s.Name) == "ACT" {
				actRes = s.Residency
			}
		}
		if total > 0 {
			activeSum += float64(actRes) / float64(total) * 100
		}
	}
	if channelCount == 0 {
		return 0
	}
	return activeSum / float64(channelCount)
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
