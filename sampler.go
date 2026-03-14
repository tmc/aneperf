//go:build darwin

package aneperf

import (
	"fmt"
	"time"
)

// Sampler creates IOReport subscriptions for ANE performance monitoring.
// Use NewSampler to create one, then Start/Stop to measure intervals.
type Sampler struct {
	sub *subscription
}

// NewSampler creates a new ANE performance sampler.
// It loads the required IOReport and IOKit libraries and creates a subscription
// for Energy Model, PMP, and Interrupt Statistics channels.
func NewSampler() (*Sampler, error) {
	if err := loadIOKit(); err != nil {
		return nil, fmt.Errorf("new sampler: %w", err)
	}
	if err := loadIOReport(); err != nil {
		return nil, fmt.Errorf("new sampler: %w", err)
	}
	sub, err := createSubscription()
	if err != nil {
		return nil, fmt.Errorf("new sampler: %w", err)
	}
	return &Sampler{sub: sub}, nil
}

// Close releases the sampler's IOReport subscription resources.
func (s *Sampler) Close() {
	if s.sub != nil {
		cfRelease(cfTypeRef(s.sub.channels))
		s.sub = nil
	}
}

// Start takes an initial sample and returns a Snapshot.
// Pass the returned Snapshot to Stop after the workload completes.
func (s *Sampler) Start() Snapshot {
	raw := s.sub.sample()
	return Snapshot{raw: raw, t: time.Now()}
}

// Stop takes a second sample and computes the delta from snap.
// The returned Delta contains ANE-filtered channels and power estimate.
func (s *Sampler) Stop(snap Snapshot) Delta {
	curr := s.sub.sample()
	now := time.Now()
	duration := now.Sub(snap.t)

	dev, _ := ReadDeviceInfo()

	if snap.raw == 0 || curr == 0 {
		return Delta{Duration: duration, Device: dev}
	}

	delta := ioReportCreateSamplesDelta(snap.raw, curr, 0)
	cfRelease(cfTypeRef(snap.raw))
	cfRelease(cfTypeRef(curr))

	if delta == 0 {
		return Delta{Duration: duration, Device: dev}
	}
	defer cfRelease(cfTypeRef(delta))

	channels := filterANEChannels(extractChannels(delta))
	power := computeANEPower(channels, float64(duration.Milliseconds()))

	return Delta{
		Duration: duration,
		Device:   dev,
		PowerW:   power,
		Channels: channels,
	}
}

// Sample takes two snapshots separated by the given interval and returns
// a complete Sample with device info, ANE power, and channel data.
// This is the simple one-shot API for non-benchmark use.
func (s *Sampler) Sample(interval time.Duration) (Sample, error) {
	dev, err := ReadDeviceInfo()
	if err != nil {
		return Sample{}, fmt.Errorf("sample: %w", err)
	}

	s1 := s.sub.sample()
	if s1 == 0 {
		return Sample{Timestamp: time.Now(), Device: dev}, nil
	}

	time.Sleep(interval)

	s2 := s.sub.sample()
	if s2 == 0 {
		cfRelease(cfTypeRef(s1))
		return Sample{Timestamp: time.Now(), Device: dev}, nil
	}

	delta := ioReportCreateSamplesDelta(s1, s2, 0)
	cfRelease(cfTypeRef(s1))
	cfRelease(cfTypeRef(s2))

	if delta == 0 {
		return Sample{Timestamp: time.Now(), Device: dev}, nil
	}
	defer cfRelease(cfTypeRef(delta))

	channels := filterANEChannels(extractChannels(delta))
	power := computeANEPower(channels, float64(interval.Milliseconds()))

	return Sample{
		Timestamp: time.Now(),
		Device:    dev,
		ANEPowerW: power,
		Channels:  channels,
	}, nil
}
