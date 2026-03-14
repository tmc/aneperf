package aneperf

import "testing"

func TestSanitizeMetricName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"lowercase", "ane-power", "ane-power"},
		{"uppercase", "ANE Power", "ane-power"},
		{"mixed", "SOC-NI4 ANE UP", "soc-ni4-ane-up"},
		{"special chars", "kANE_UKNOWN", "kane-uknown"},
		{"trailing special", "test!!!", "test"},
		{"empty", "", ""},
		{"numbers", "core16", "core16"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeMetricName(tt.in)
			if got != tt.want {
				t.Errorf("sanitizeMetricName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestEnergyToWatts(t *testing.T) {
	tests := []struct {
		name       string
		energy     int64
		unit       string
		durationMs float64
		want       float64
	}{
		{"mJ over 1s", 1000, "mJ", 1000, 1.0},
		{"uJ over 1s", 1000000, "uJ", 1000, 1.0},
		{"nJ over 1s", 1000000000, "nJ", 1000, 1.0},
		{"mJ over 500ms", 500, "mJ", 500, 1.0},
		{"zero duration clamps to 1ms", 1000, "mJ", 0, 1000.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := energyToWatts(tt.energy, tt.unit, tt.durationMs)
			if got != tt.want {
				t.Errorf("energyToWatts(%d, %q, %f) = %f, want %f", tt.energy, tt.unit, tt.durationMs, got, tt.want)
			}
		})
	}
}

func TestContainsANE(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"ANE", true},
		{"ane", true},
		{"SOC-NI4 ANE UP", true},
		{"GPU", false},
		{"an", false},
		{"", false},
		{"aNe", true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got := containsANE(tt.in)
			if got != tt.want {
				t.Errorf("containsANE(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
