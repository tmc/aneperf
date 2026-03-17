package aneperf

import (
	"testing"
	"time"
)

func TestComputeStats(t *testing.T) {
	tests := []struct {
		name               string
		delta              Delta
		wantActivePct      float64
		wantPeakBucket     string
		wantPeakPct        float64
		wantInterrupts     int64
		wantRatePositve    bool
		wantThrottles      int64
		checkGPUActive     bool
		wantGPUActive      float64
		checkClusterActive bool
		wantClusterActive  float64
		wantThrottleReason map[string]float64
	}{
		{
			name: "ce histogram idle",
			delta: Delta{
				Duration: time.Second,
				Channels: []Channel{
					{Group: "PMP", SubGroup: "Fast-Die CE", Channel: "ANE0", States: []StateEntry{
						{Name: "0%", Residency: 1000},
						{Name: "50%", Residency: 0},
						{Name: "100%", Residency: 0},
					}},
				},
			},
			wantActivePct:  0,
			wantPeakBucket: "0%",
			wantPeakPct:    100,
		},
		{
			name: "ce histogram active",
			delta: Delta{
				Duration: time.Second,
				Channels: []Channel{
					{Group: "PMP", SubGroup: "Fast-Die CE", Channel: "ANE0", States: []StateEntry{
						{Name: "0%", Residency: 500},
						{Name: "50%", Residency: 300},
						{Name: "100%", Residency: 200},
					}},
				},
			},
			wantActivePct:  35, // (0*500 + 50*300 + 100*200) / 1000
			wantPeakBucket: "0%",
			wantPeakPct:    50,
		},
		{
			name: "voltage fallback",
			delta: Delta{
				Duration: time.Second,
				Channels: []Channel{
					{Group: "PMP", SubGroup: "SOC Floor", Channel: "ANE0", States: []StateEntry{
						{Name: "VMIN", Residency: 800},
						{Name: "VNOM", Residency: 200},
					}},
				},
			},
			wantActivePct:  20,
			wantPeakBucket: "",
			wantPeakPct:    0,
		},
		{
			name: "interrupts and throttles",
			delta: Delta{
				Duration: 2 * time.Second,
				Channels: []Channel{
					{Group: "Interrupt Statistics (by index)", Channel: "ANE Handler Count", Value: 100},
					{Group: "Interrupt Statistics (by index)", Channel: "ANE Timer Count", Value: 50},
					{Group: "PMP", SubGroup: "PWRS0", Channel: "ANE Throttle", Value: 3},
				},
			},
			wantInterrupts:  150,
			wantRatePositve: true,
			wantThrottles:   3,
		},
		{
			name: "cluster power active",
			delta: Delta{
				Duration: time.Second,
				Channels: []Channel{
					{Group: "SoC Stats", SubGroup: "Cluster Power States", Channel: "PACC1_ANE", States: []StateEntry{
						{Name: "ACT", Residency: 800},
						{Name: "INACT", Residency: 200},
					}},
				},
			},
			checkClusterActive: true,
			wantClusterActive:  80,
		},
		{
			name: "gpu active residency",
			delta: Delta{
				Duration: time.Second,
				Channels: []Channel{
					{Group: "GPU Stats", SubGroup: "GPU Performance States", Channel: "GPUPH", States: []StateEntry{
						{Name: "OFF", Residency: 200},
						{Name: "IDLE", Residency: 300},
						{Name: "P1", Residency: 500},
					}},
				},
			},
			checkGPUActive: true,
			wantGPUActive:  50,
		},
		{
			name: "throttle detail reasons",
			delta: Delta{
				Duration: time.Second,
				Channels: []Channel{
					{Group: "SoC Stats", SubGroup: "Events", Channel: "ANE_THROTTLE_PPT_TRIG", States: []StateEntry{
						{Name: "ACT", Residency: 32},
						{Name: "INACT", Residency: 968},
					}},
					{Group: "SoC Stats", SubGroup: "Events", Channel: "ANE_THROTTLE_ADCLK_TRIG", States: []StateEntry{
						{Name: "ACT", Residency: 0},
						{Name: "INACT", Residency: 1000},
					}},
				},
			},
			wantThrottleReason: map[string]float64{
				"ANE_THROTTLE_PPT_TRIG":   3.2,
				"ANE_THROTTLE_ADCLK_TRIG": 0,
			},
		},
		{
			name: "cluster power zero residency",
			delta: Delta{
				Duration: time.Second,
				Channels: []Channel{
					{Group: "SoC Stats", SubGroup: "Cluster Power States", Channel: "PACC1_ANE", States: []StateEntry{
						{Name: "ACT", Residency: 0},
						{Name: "INACT", Residency: 1000},
					}},
				},
			},
			checkClusterActive: true,
			wantClusterActive:  0,
		},
		{
			name: "cluster power no ACT state",
			delta: Delta{
				Duration: time.Second,
				Channels: []Channel{
					{Group: "SoC Stats", SubGroup: "Cluster Power States", Channel: "PACC1_ANE", States: []StateEntry{
						{Name: "INACT", Residency: 1000},
					}},
				},
			},
			checkClusterActive: true,
			wantClusterActive:  0,
		},
		{
			name:  "empty channels",
			delta: Delta{Duration: time.Second},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeStats(tt.delta)
			if got.ActivePct != tt.wantActivePct {
				t.Errorf("ActivePct = %v, want %v", got.ActivePct, tt.wantActivePct)
			}
			if got.PeakCEBucket != tt.wantPeakBucket {
				t.Errorf("PeakCEBucket = %q, want %q", got.PeakCEBucket, tt.wantPeakBucket)
			}
			if got.PeakCEPct != tt.wantPeakPct {
				t.Errorf("PeakCEPct = %v, want %v", got.PeakCEPct, tt.wantPeakPct)
			}
			if got.TotalInterrupts != tt.wantInterrupts {
				t.Errorf("TotalInterrupts = %v, want %v", got.TotalInterrupts, tt.wantInterrupts)
			}
			if tt.wantRatePositve && got.InterruptRate <= 0 {
				t.Errorf("InterruptRate = %v, want positive", got.InterruptRate)
			}
			if got.TotalThrottles != tt.wantThrottles {
				t.Errorf("TotalThrottles = %v, want %v", got.TotalThrottles, tt.wantThrottles)
			}
			if tt.checkClusterActive && got.ClusterActivePct != tt.wantClusterActive {
				t.Errorf("ClusterActivePct = %v, want %v", got.ClusterActivePct, tt.wantClusterActive)
			}
			if tt.checkGPUActive && got.GPUActivePct != tt.wantGPUActive {
				t.Errorf("GPUActivePct = %v, want %v", got.GPUActivePct, tt.wantGPUActive)
			}
			if tt.wantThrottleReason != nil {
				for name, wantPct := range tt.wantThrottleReason {
					gotPct, ok := got.ThrottleReasons[name]
					if !ok {
						t.Errorf("ThrottleReasons missing %q", name)
					} else if diff := gotPct - wantPct; diff > 0.01 || diff < -0.01 {
						t.Errorf("ThrottleReasons[%q] = %v, want %v", name, gotPct, wantPct)
					}
				}
			}
		})
	}
}

func TestParseFloat(t *testing.T) {
	tests := []struct {
		in   string
		want float64
	}{
		{"0", 0},
		{"42", 42},
		{"3.14", 3.14},
		{"100", 100},
		{"", 0},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got := parseFloat(tt.in)
			if got != tt.want {
				t.Errorf("parseFloat(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
