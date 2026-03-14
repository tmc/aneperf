//go:build darwin

package aneperf

import (
	"fmt"

	"github.com/ebitengine/purego"
)

var (
	ioreportHandle uintptr

	ioReportCopyChannelsInGroup   func(group cfStringRef, subgroup cfStringRef, a uint64, b uint64, c uint64) cfDictionaryRef
	ioReportMergeChannels         func(into cfDictionaryRef, from cfDictionaryRef, unused uintptr)
	ioReportCreateSubscription    func(a uintptr, channels cfDictionaryRef, out *cfDictionaryRef, d uint64, e uintptr) uintptr
	ioReportCreateSamples         func(sub uintptr, channels cfDictionaryRef, unused uintptr) cfDictionaryRef
	ioReportCreateSamplesDelta    func(prev cfDictionaryRef, curr cfDictionaryRef, unused uintptr) cfDictionaryRef
	ioReportGetChannelCount       func(samples cfDictionaryRef) cfIndex
	ioReportChannelGetGroup       func(sample cfDictionaryRef) cfStringRef
	ioReportChannelGetSubGroup    func(sample cfDictionaryRef) cfStringRef
	ioReportChannelGetChannelName func(sample cfDictionaryRef) cfStringRef
	ioReportChannelGetUnitLabel   func(sample cfDictionaryRef) cfStringRef
	ioReportSimpleGetIntegerValue func(sample cfDictionaryRef, a int32) int64
	ioReportStateGetCount         func(sample cfDictionaryRef) int32
	ioReportStateGetNameForIndex  func(sample cfDictionaryRef, index int32) cfStringRef
	ioReportStateGetResidency     func(sample cfDictionaryRef, index int32) int64
)

func loadIOReport() error {
	if ioreportHandle != 0 {
		return nil
	}
	if err := loadCF(); err != nil {
		return err
	}
	var err error
	ioreportHandle, err = purego.Dlopen("/usr/lib/libIOReport.dylib", purego.RTLD_LAZY)
	if err != nil {
		return fmt.Errorf("load libIOReport: %w", err)
	}
	purego.RegisterLibFunc(&ioReportCopyChannelsInGroup, ioreportHandle, "IOReportCopyChannelsInGroup")
	purego.RegisterLibFunc(&ioReportMergeChannels, ioreportHandle, "IOReportMergeChannels")
	purego.RegisterLibFunc(&ioReportCreateSubscription, ioreportHandle, "IOReportCreateSubscription")
	purego.RegisterLibFunc(&ioReportCreateSamples, ioreportHandle, "IOReportCreateSamples")
	purego.RegisterLibFunc(&ioReportCreateSamplesDelta, ioreportHandle, "IOReportCreateSamplesDelta")
	purego.RegisterLibFunc(&ioReportGetChannelCount, ioreportHandle, "IOReportGetChannelCount")
	purego.RegisterLibFunc(&ioReportChannelGetGroup, ioreportHandle, "IOReportChannelGetGroup")
	purego.RegisterLibFunc(&ioReportChannelGetSubGroup, ioreportHandle, "IOReportChannelGetSubGroup")
	purego.RegisterLibFunc(&ioReportChannelGetChannelName, ioreportHandle, "IOReportChannelGetChannelName")
	purego.RegisterLibFunc(&ioReportChannelGetUnitLabel, ioreportHandle, "IOReportChannelGetUnitLabel")
	purego.RegisterLibFunc(&ioReportSimpleGetIntegerValue, ioreportHandle, "IOReportSimpleGetIntegerValue")
	purego.RegisterLibFunc(&ioReportStateGetCount, ioreportHandle, "IOReportStateGetCount")
	purego.RegisterLibFunc(&ioReportStateGetNameForIndex, ioreportHandle, "IOReportStateGetNameForIndex")
	purego.RegisterLibFunc(&ioReportStateGetResidency, ioreportHandle, "IOReportStateGetResidency")
	return nil
}

// subscription holds the IOReport subscription and channels dict
// needed for subsequent sampling calls.
type subscription struct {
	sub      uintptr
	channels cfDictionaryRef
}

func createSubscription() (*subscription, error) {
	energyChannels := ioReportCopyChannelsInGroup(makeCFString("Energy Model"), 0, 0, 0, 0)
	if energyChannels == 0 {
		return nil, fmt.Errorf("create subscription: no energy model channels")
	}

	pmpChannels := ioReportCopyChannelsInGroup(makeCFString("PMP"), 0, 0, 0, 0)
	if pmpChannels != 0 {
		ioReportMergeChannels(energyChannels, pmpChannels, 0)
		cfRelease(cfTypeRef(pmpChannels))
	}

	intChannels := ioReportCopyChannelsInGroup(makeCFString("Interrupt Statistics (by index)"), 0, 0, 0, 0)
	if intChannels != 0 {
		ioReportMergeChannels(energyChannels, intChannels, 0)
		cfRelease(cfTypeRef(intChannels))
	}

	count := ioReportGetChannelCount(energyChannels)
	channelsCopy := cfDictionaryCreateMutableCopy(0, count, energyChannels)
	cfRelease(cfTypeRef(energyChannels))

	if channelsCopy == 0 {
		return nil, fmt.Errorf("create subscription: failed to copy channels")
	}

	var subsystem cfDictionaryRef
	sub := ioReportCreateSubscription(0, channelsCopy, &subsystem, 0, 0)
	if sub == 0 {
		cfRelease(cfTypeRef(channelsCopy))
		return nil, fmt.Errorf("create subscription: IOReportCreateSubscription failed")
	}

	return &subscription{sub: sub, channels: channelsCopy}, nil
}

func (s *subscription) sample() cfDictionaryRef {
	return ioReportCreateSamples(s.sub, s.channels, 0)
}

func extractChannels(samples cfDictionaryRef) []Channel {
	arrRef := cfDictionaryGetValue(samples, makeCFString("IOReportChannels"))
	if arrRef == 0 {
		return nil
	}
	arr := cfArrayRef(arrRef)
	count := cfArrayGetCount(arr)
	if count <= 0 {
		return nil
	}

	var channels []Channel
	for i := range count {
		item := cfDictionaryRef(cfArrayGetValueAtIndex(arr, i))
		if item == 0 {
			continue
		}

		group := cfStringToGo(ioReportChannelGetGroup(item))
		subgroup := cfStringToGo(ioReportChannelGetSubGroup(item))
		name := cfStringToGo(ioReportChannelGetChannelName(item))
		unit := cfStringToGo(ioReportChannelGetUnitLabel(item))

		ch := Channel{
			Group:    group,
			SubGroup: subgroup,
			Channel:  name,
			Unit:     unit,
		}

		nStates := ioReportStateGetCount(item)
		if nStates > 0 {
			for j := range nStates {
				sn := cfStringToGo(ioReportStateGetNameForIndex(item, j))
				res := ioReportStateGetResidency(item, j)
				ch.States = append(ch.States, StateEntry{
					Name:      sn,
					Residency: res,
				})
			}
		} else {
			ch.Value = ioReportSimpleGetIntegerValue(item, 0)
		}

		channels = append(channels, ch)
	}

	return channels
}

func filterANEChannels(channels []Channel) []Channel {
	var out []Channel
	for _, ch := range channels {
		if containsANE(ch.SubGroup) || containsANE(ch.Channel) {
			out = append(out, ch)
		}
	}
	return out
}

func containsANE(s string) bool {
	for i := 0; i+3 <= len(s); i++ {
		c0, c1, c2 := s[i], s[i+1], s[i+2]
		if (c0 == 'a' || c0 == 'A') && (c1 == 'n' || c1 == 'N') && (c2 == 'e' || c2 == 'E') {
			return true
		}
	}
	return false
}
