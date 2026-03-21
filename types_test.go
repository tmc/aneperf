package aneperf

import (
	"encoding/json"
	"testing"
	"time"
)

type reportedMetric struct {
	value float64
	unit  string
}

type metricReporter struct {
	metrics []reportedMetric
}

func (r *metricReporter) ReportMetric(value float64, unit string) {
	r.metrics = append(r.metrics, reportedMetric{value: value, unit: unit})
}

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

func TestComputeGPUPower(t *testing.T) {
	channels := []Channel{
		{Group: "Energy Model", Channel: "GPU Energy", Unit: "mJ", Value: 500},
		{Group: "Energy Model", Channel: "GPU SRAM", Unit: "mJ", Value: 250},
		{Group: "Energy Model", Channel: "ANE Energy", Unit: "mJ", Value: 1000},
	}
	got := computeGPUPower(channels, 500)
	if got != 1.5 {
		t.Errorf("computeGPUPower(...) = %v, want 1.5", got)
	}
}

func TestComputeGPUTemp(t *testing.T) {
	channels := []Channel{
		{Group: "GPU Stats", SubGroup: "Temperature", Channel: "Tg1a Latest", Value: 58},
		{Group: "GPU Stats", SubGroup: "Temperature", Channel: "Tg5a Latest", Value: 6200},
		{Group: "GPU Stats", SubGroup: "Temperature", Channel: "Tg9a Sum", Value: 12345},
	}
	got := computeGPUTemp(channels)
	if got != 60 {
		t.Errorf("computeGPUTemp(...) = %v, want 60", got)
	}
}

func TestNormalizeTemperature(t *testing.T) {
	tests := []struct {
		in   int64
		want float64
	}{
		{58, 58},
		{6200, 62},
		{58123, 58.123},
	}
	for _, tt := range tests {
		if got := normalizeTemperature(tt.in); got != tt.want {
			t.Errorf("normalizeTemperature(%d) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestReportMetricsIgnoresGPUEnergy(t *testing.T) {
	r := &metricReporter{}
	Delta{
		Channels: []Channel{
			{Group: "Energy Model", Channel: "ANE Energy", Unit: "mJ", Value: 10},
			{Group: "Energy Model", Channel: "GPU Energy", Unit: "mJ", Value: 20},
		},
	}.ReportMetrics(r, MetricEnergy)

	if len(r.metrics) != 1 {
		t.Fatalf("reported %d metrics, want 1", len(r.metrics))
	}
	if got := r.metrics[0].unit; got != "ane-energy-mJ/op" {
		t.Fatalf("metric unit = %q, want %q", got, "ane-energy-mJ/op")
	}
	if got := r.metrics[0].value; got != 10 {
		t.Fatalf("metric value = %v, want 10", got)
	}
}

func TestDurationMilliseconds(t *testing.T) {
	tests := []struct {
		name string
		in   time.Duration
		want float64
	}{
		{"one second", time.Second, 1000},
		{"half millisecond", 500 * time.Microsecond, 0.5},
		{"one nanosecond", time.Nanosecond, 0.000001},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := durationMilliseconds(tt.in)
			if got != tt.want {
				t.Errorf("durationMilliseconds(%v) = %v, want %v", tt.in, got, tt.want)
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
		{"dart-ane0", true},
		{"ANE0", true},
		{"THROT-ANE-SUM", true},
		{"ANE_THROTTLE_HW_TRIG", true},
		{"ANEXL", true},
		{"VLane", false},
		{"Miscellaneous", false},
		{"LanesEng", false},
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

func TestSampleJSONIncludesUtilizationFields(t *testing.T) {
	sample := Sample{}

	data, err := json.Marshal(sample)
	if err != nil {
		t.Fatalf("json.Marshal(Sample{}): %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal(sample): %v", err)
	}

	if _, ok := got["ane_utilization_pct"]; !ok {
		t.Fatal("sample json missing ane_utilization_pct")
	}
	if _, ok := got["ane_cluster_active_pct"]; !ok {
		t.Fatal("sample json missing ane_cluster_active_pct")
	}
}

func TestDeltaJSONIncludesUtilizationFields(t *testing.T) {
	delta := Delta{}

	data, err := json.Marshal(delta)
	if err != nil {
		t.Fatalf("json.Marshal(Delta{}): %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal(delta): %v", err)
	}

	if _, ok := got["ane_utilization_pct"]; !ok {
		t.Fatal("delta json missing ane_utilization_pct")
	}
	if _, ok := got["ane_cluster_active_pct"]; !ok {
		t.Fatal("delta json missing ane_cluster_active_pct")
	}
}
