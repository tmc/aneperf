//go:build darwin

// Package testworkload provides ANE workload generation for testing aneperf.
//
// It uses github.com/tmc/apple/x/ane to compile and evaluate models
// directly on the ANE, generating real hardware activity that aneperf
// can measure.
//
// # Quick Start
//
// Run the benchmarks:
//
//	ANE_BENCH=1 go test -bench=. -benchtime=5s ./testworkload/
//
// The FFN benchmark compiles a small convolution model from MIL text
// and runs it repeatedly on the ANE, reporting aneperf metrics alongside.
//
// # Requirements
//
// Requires Apple Silicon with ANE hardware.
package testworkload
