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
// # Device Info
//
// Read ANE hardware properties directly:
//
//	info, err := aneperf.ReadDeviceInfo()
//
// # Requirements
//
// macOS only. Requires Apple Silicon with ANE (M1+).
// Uses private APIs: libIOReport.dylib, IOKit H11ANEIn service.
package aneperf
