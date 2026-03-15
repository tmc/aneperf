//go:build darwin

package aneperf

import (
	"fmt"

	"github.com/ebitengine/purego"
)

var (
	ioreportHandle uintptr

	ioReportCopyAllChannels       func(a uint64, b uint64) cfDictionaryRef
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
	purego.RegisterLibFunc(&ioReportCopyAllChannels, ioreportHandle, "IOReportCopyAllChannels")
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

// discoverANEGroups scans all IOReport channels and returns the set of group
// names that contain at least one ANE-related channel. This avoids hardcoding
// chip-specific group names like "PMP0" vs "PMP".
func discoverANEGroups() map[string]bool {
	groups := map[string]bool{
		"Energy Model":                  true,
		"Interrupt Statistics (by index)": true,
	}
	all := ioReportCopyAllChannels(0, 0)
	if all == 0 {
		return groups
	}
	defer cfRelease(cfTypeRef(all))

	key := makeCFString("IOReportChannels")
	defer cfRelease(cfTypeRef(key))
	arrRef := cfDictionaryGetValue(all, key)
	if arrRef == 0 {
		return groups
	}
	arr := cfArrayRef(arrRef)
	n := cfArrayGetCount(arr)
	for i := range n {
		item := cfDictionaryRef(cfArrayGetValueAtIndex(arr, cfIndex(i)))
		if item == 0 {
			continue
		}
		group := cfStringToGo(ioReportChannelGetGroup(item))
		if groups[group] {
			continue
		}
		subgroup := cfStringToGo(ioReportChannelGetSubGroup(item))
		name := cfStringToGo(ioReportChannelGetChannelName(item))
		if containsANE(group) || containsANE(subgroup) || containsANE(name) {
			groups[group] = true
		}
	}
	return groups
}

func createSubscription() (*subscription, error) {
	groups := discoverANEGroups()

	var base cfDictionaryRef
	for group := range groups {
		gRef := makeCFString(group)
		ch := ioReportCopyChannelsInGroup(gRef, 0, 0, 0, 0)
		cfRelease(cfTypeRef(gRef))
		if ch == 0 {
			continue
		}
		if base == 0 {
			base = ch
		} else {
			ioReportMergeChannels(base, ch, 0)
			cfRelease(cfTypeRef(ch))
		}
	}
	if base == 0 {
		return nil, fmt.Errorf("create subscription: no channels found")
	}

	count := ioReportGetChannelCount(base)
	channelsCopy := cfDictionaryCreateMutableCopy(0, count, base)
	cfRelease(cfTypeRef(base))

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
	key := makeCFString("IOReportChannels")
	defer cfRelease(cfTypeRef(key))
	arrRef := cfDictionaryGetValue(samples, key)
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

// containsANE reports whether s contains "ANE" (case-insensitive) at a
// word boundary: the character before the match (if any) must be a
// non-letter. This avoids false positives like "VLane", "Miscellaneous",
// or "LanesEng" while still matching "ANEXL", "ANE0", "dart-ane0", etc.
func containsANE(s string) bool {
	for i := 0; i+3 <= len(s); i++ {
		c0, c1, c2 := s[i], s[i+1], s[i+2]
		if (c0 == 'a' || c0 == 'A') && (c1 == 'n' || c1 == 'N') && (c2 == 'e' || c2 == 'E') {
			if i == 0 || !isLetter(s[i-1]) {
				return true
			}
		}
	}
	return false
}

func isLetter(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}
