// Package aneperf provides Apple Neural Engine performance monitoring.
//
// It uses Apple's private IOReport and IOKit APIs via purego (no cgo) to
// sample ANE energy, power management state residency, and interrupt
// statistics.
//
// # Quick Start
//
// Take a one-shot sample:
//
//	sampler, err := aneperf.NewSampler()
//	if err != nil {
//		log.Fatal(err)
//	}
//	defer sampler.Close()
//
//	sample, err := sampler.Sample(time.Second)
//	if err != nil {
//		log.Fatal(err)
//	}
//	fmt.Printf("ANE Power: %.3f W\n", sample.ANEPowerW)
//
// # Benchmark Integration
//
// Use Start/Stop for Go benchmark metric reporting:
//
//	func BenchmarkMyANEWorkload(b *testing.B) {
//		sampler, _ := aneperf.NewSampler()
//		defer sampler.Close()
//		for b.Loop() {
//			snap := sampler.Start()
//			// ... do ANE work ...
//			delta := sampler.Stop(snap)
//			delta.ReportMetrics(b)
//		}
//	}
//
// Pass [Metric] flags to select specific categories:
//
//	delta.ReportMetrics(b, aneperf.MetricPower|aneperf.MetricCompute)
//
// # Device Info
//
// Read ANE hardware properties directly:
//
//	info, err := aneperf.ReadDeviceInfo()
//
// # Metrics Reference
//
// Channels are grouped by IOReport category. The key groups are:
//
//   - Energy Model: ANE energy consumption in millijoules (mJ) or microjoules (uJ)
//     per sample interval, converted to watts for display.
//
//   - PMP-prefixed groups (Power Management):
//   - SOC Floor (Voltage States): VMIN/VNOM/VMAX residency. VMIN means the ANE
//     is idle or in its lowest voltage rail. Higher states (VNOM, VMAX) indicate
//     active compute at higher performance points.
//   - DCS Floor: DCS frequency states. Higher frequency states indicate more
//     memory bandwidth demand from the ANE.
//   - Fast-Die CE: Histogram of ANE utilization percentage buckets (0% through
//     100%). The weighted average gives overall utilization.
//   - PWRS0 (Throttle Counters): Count of throttle events in the sample interval.
//   - AF BW / DCS BW / SOC-NI Util BW (Bandwidth): Memory bus utilization tiers.
//     Channels are per-link and per-direction: RD=read, WR=write, RD+WR=combined.
//
//   - Interrupt Statistics (by index): Hardware interrupt counts and handler time.
//   - Handler Count: Number of interrupt handler invocations.
//   - Handler Time (MATUs): Cumulative time in handlers, measured in Mach
//     absolute time units.
//
//   - SoC Stats:
//   - Cluster Power States (PACC*_ANE): Whether the ANE cluster is powered on
//     (ACT) or off (INACT). High ACT residency means the ANE hardware is
//     available and has been active.
//   - Events (ANE_THROTTLE_*): Per-reason throttle residency. Each channel
//     tracks a specific throttle trigger. ACT residency > 0 means the ANE was
//     actively throttled for that reason. Known triggers include PPT, ADCLK,
//     DITHR, and EXT.
//
// # Requirements
//
// macOS only. Requires Apple Silicon with ANE (M1+).
// Uses private APIs: libIOReport.dylib, IOKit H11ANEIn service.
package aneperf
