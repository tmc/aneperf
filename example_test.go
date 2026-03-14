//go:build darwin

package aneperf_test

import (
	"fmt"
	"log"
	"time"

	"github.com/tmc/aneperf"
)

func Example() {
	sampler, err := aneperf.NewSampler()
	if err != nil {
		log.Fatal(err)
	}
	defer sampler.Close()

	sample, err := sampler.Sample(100 * time.Millisecond)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("ANE Power: %.3f W\n", sample.ANEPowerW)
}

func ExampleReadDeviceInfo() {
	info, err := aneperf.ReadDeviceInfo()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Architecture: %s, Cores: %d\n", info.Architecture, info.NumCores)
}

func ExampleSampler_startStop() {
	sampler, err := aneperf.NewSampler()
	if err != nil {
		log.Fatal(err)
	}
	defer sampler.Close()

	snap := sampler.Start()
	time.Sleep(100 * time.Millisecond)
	delta := sampler.Stop(snap)

	fmt.Printf("ANE Power: %.3f W over %v\n", delta.PowerW, delta.Duration.Round(time.Millisecond))
}
