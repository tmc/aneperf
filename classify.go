//go:build darwin

package aneperf

import (
	"strings"
)

// ChannelsByCategory groups ANE channels by their IOReport category.
type ChannelsByCategory struct {
	Energy         []Channel // Energy Model
	Voltage        []Channel // SOC Floor (VMIN/VNOM/VMAX)
	DCSFloor       []Channel // DCS Floor (F1-F6)
	ComputeEn      []Channel // Fast-Die CE (0%-100%)
	GPUStats       []Channel // GPU Stats (performance states)
	Bandwidth      []Channel // AF BW + DCS BW + SOC-NI Util BW
	Throttle       []Channel // PWRS0 throttle counters
	Interrupt      []Channel // Interrupt Statistics
	ThrottleDetail []Channel // SoC Stats → Events (ANE_THROTTLE_* INACT/ACT)
	ClusterPower   []Channel // SoC Stats → Cluster Power States (PACC*_ANE)
}

// ClassifyChannels splits channels into categories by group and subgroup.
func ClassifyChannels(channels []Channel) ChannelsByCategory {
	var c ChannelsByCategory
	for _, ch := range channels {
		switch ch.Group {
		case "Energy Model":
			c.Energy = append(c.Energy, ch)
		case "Interrupt Statistics (by index)":
			c.Interrupt = append(c.Interrupt, ch)
		case "GPU Stats":
			c.GPUStats = append(c.GPUStats, ch)
		case "SoC Stats":
			switch ch.SubGroup {
			case "Events":
				c.ThrottleDetail = append(c.ThrottleDetail, ch)
			case "Cluster Power States":
				c.ClusterPower = append(c.ClusterPower, ch)
			}
		default:
			if !strings.HasPrefix(ch.Group, "PMP") {
				break
			}
			switch {
			case ch.SubGroup == "SOC Floor":
				c.Voltage = append(c.Voltage, ch)
			case ch.SubGroup == "DCS Floor":
				c.DCSFloor = append(c.DCSFloor, ch)
			case ch.SubGroup == "Fast-Die CE":
				c.ComputeEn = append(c.ComputeEn, ch)
			case strings.Contains(ch.SubGroup, "BW"):
				c.Bandwidth = append(c.Bandwidth, ch)
			case strings.HasPrefix(ch.SubGroup, "PWRS"):
				c.Throttle = append(c.Throttle, ch)
			default:
				c.Bandwidth = append(c.Bandwidth, ch)
			}
		}
	}
	return c
}
