//go:build darwin

package testworkload

import (
	"os"
	"testing"
	"time"

	"github.com/tmc/aneperf"
	"github.com/tmc/apple/x/ane"
)

func TestSmoke(t *testing.T) {
	if os.Getenv("ANE_BENCH") != "1" {
		t.Skip("set ANE_BENCH=1 to run ANE workload tests")
	}

	c, err := ane.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	m, err := OpenIdentity(c, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	input := []float32{1, 2, 3, 4}
	if err := m.WriteInputF32(0, input); err != nil {
		t.Fatal(err)
	}
	if err := m.Eval(); err != nil {
		t.Fatalf("Eval: %v", err)
	}

	output := make([]float32, 4)
	if err := m.ReadOutputF32(0, output); err != nil {
		t.Fatal(err)
	}
	t.Logf("output: %v", output)
}

func BenchmarkConv(b *testing.B) {
	if os.Getenv("ANE_BENCH") != "1" {
		b.Skip("set ANE_BENCH=1 to run ANE workload benchmarks")
	}

	c, err := ane.Open()
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()

	m, err := OpenConv(c, 64, 64)
	if err != nil {
		b.Fatal(err)
	}
	defer m.Close()

	input := make([]float32, 64)
	for i := range input {
		input[i] = float32(i) / 64
	}
	if err := m.WriteInputF32(0, input); err != nil {
		b.Fatal(err)
	}

	// Warmup.
	for range 10 {
		m.Eval()
	}

	b.ResetTimer()
	for b.Loop() {
		if err := m.Eval(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkConvWithANEPerf(b *testing.B) {
	if os.Getenv("ANE_BENCH") != "1" {
		b.Skip("set ANE_BENCH=1 to run ANE workload benchmarks")
	}

	c, err := ane.Open()
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()

	m, err := OpenConv(c, 64, 64)
	if err != nil {
		b.Fatal(err)
	}
	defer m.Close()

	sampler, err := aneperf.NewSampler()
	if err != nil {
		b.Fatal(err)
	}
	defer sampler.Close()

	input := make([]float32, 64)
	for i := range input {
		input[i] = float32(i) / 64
	}
	if err := m.WriteInputF32(0, input); err != nil {
		b.Fatal(err)
	}

	// Warmup.
	for range 10 {
		m.Eval()
	}

	b.ResetTimer()
	snap := sampler.Start()
	for b.Loop() {
		if err := m.Eval(); err != nil {
			b.Fatal(err)
		}
	}
	delta := sampler.Stop(snap)
	delta.ReportMetrics(b)
}

// BenchmarkConvDetailedMetrics demonstrates the full set of ANE metrics
// including cluster power, compute utilization, and throttle detail.
func BenchmarkConvDetailedMetrics(b *testing.B) {
	if os.Getenv("ANE_BENCH") != "1" {
		b.Skip("set ANE_BENCH=1 to run ANE workload benchmarks")
	}

	c, err := ane.Open()
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()

	m, err := OpenConv(c, 64, 64)
	if err != nil {
		b.Fatal(err)
	}
	defer m.Close()

	sampler, err := aneperf.NewSampler()
	if err != nil {
		b.Fatal(err)
	}
	defer sampler.Close()

	input := make([]float32, 64)
	for i := range input {
		input[i] = float32(i) / 64
	}
	if err := m.WriteInputF32(0, input); err != nil {
		b.Fatal(err)
	}

	// Warmup.
	for range 10 {
		m.Eval()
	}

	// Run a sustained burst to capture meaningful residency data.
	// ReportMetrics now includes: ane-compute-%, ane-cluster-active-%,
	// and ane-throttle-{reason}-act-% alongside the original metrics.
	snap := sampler.Start()
	deadline := time.Now().Add(2 * time.Second)
	n := 0
	for time.Now().Before(deadline) {
		if err := m.Eval(); err != nil {
			b.Fatal(err)
		}
		n++
	}
	delta := sampler.Stop(snap)

	b.ReportMetric(float64(n), "evals")
	b.ReportMetric(float64(n)/delta.Duration.Seconds(), "evals/s")
	delta.ReportMetrics(b, aneperf.MetricAll)
}

// BenchmarkConvMinimalMetrics demonstrates selective metric reporting,
// emitting only power and compute utilization.
func BenchmarkConvMinimalMetrics(b *testing.B) {
	if os.Getenv("ANE_BENCH") != "1" {
		b.Skip("set ANE_BENCH=1 to run ANE workload benchmarks")
	}

	c, err := ane.Open()
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()

	m, err := OpenConv(c, 64, 64)
	if err != nil {
		b.Fatal(err)
	}
	defer m.Close()

	sampler, err := aneperf.NewSampler()
	if err != nil {
		b.Fatal(err)
	}
	defer sampler.Close()

	input := make([]float32, 64)
	for i := range input {
		input[i] = float32(i) / 64
	}
	if err := m.WriteInputF32(0, input); err != nil {
		b.Fatal(err)
	}

	// Warmup.
	for range 10 {
		m.Eval()
	}

	snap := sampler.Start()
	deadline := time.Now().Add(time.Second)
	n := 0
	for time.Now().Before(deadline) {
		if err := m.Eval(); err != nil {
			b.Fatal(err)
		}
		n++
	}
	delta := sampler.Stop(snap)

	b.ReportMetric(float64(n), "evals")
	delta.ReportMetrics(b, aneperf.MetricPower|aneperf.MetricCompute)
}

func BenchmarkConvBurstWithANEPerf(b *testing.B) {
	if os.Getenv("ANE_BENCH") != "1" {
		b.Skip("set ANE_BENCH=1 to run ANE workload benchmarks")
	}

	c, err := ane.Open()
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()

	m, err := OpenConv(c, 64, 64)
	if err != nil {
		b.Fatal(err)
	}
	defer m.Close()

	sampler, err := aneperf.NewSampler()
	if err != nil {
		b.Fatal(err)
	}
	defer sampler.Close()

	input := make([]float32, 64)
	for i := range input {
		input[i] = float32(i) / 64
	}
	if err := m.WriteInputF32(0, input); err != nil {
		b.Fatal(err)
	}

	// Run a 1-second burst and measure ANE metrics.
	snap := sampler.Start()
	deadline := time.Now().Add(time.Second)
	n := 0
	for time.Now().Before(deadline) {
		if err := m.Eval(); err != nil {
			b.Fatal(err)
		}
		n++
	}
	delta := sampler.Stop(snap)

	b.ReportMetric(float64(n), "evals")
	b.ReportMetric(float64(n)/delta.Duration.Seconds(), "evals/s")
	delta.ReportMetrics(b)
}
