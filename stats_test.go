package aneperf

import (
	"testing"
	"time"
)

func TestComputeStats(t *testing.T) {
	tests := []struct {
		name            string
		delta           Delta
		wantActivePct   float64
		wantPeakBucket  string
		wantPeakPct     float64
		wantInterrupts  int64
		wantRatePositve bool
		wantThrottles   int64
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
