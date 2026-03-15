//go:build darwin

package aneperf

import (
	"fmt"
	"unsafe"

	"github.com/ebitengine/purego"
)

// CoreFoundation types for use with purego.
type cfStringRef uintptr
type cfDictionaryRef uintptr
type cfArrayRef uintptr
type cfTypeRef uintptr
type cfIndex int64

const kCFStringEncodingUTF8 = 0x08000100

const kCFNumberSInt64Type = 4

var (
	cfHandle uintptr

	cfArrayGetCount            func(arr cfArrayRef) cfIndex
	cfArrayGetValueAtIndex     func(arr cfArrayRef, idx cfIndex) cfTypeRef
	cfDictionaryGetValue       func(dict cfDictionaryRef, key cfStringRef) cfTypeRef
	cfDictionaryCreateMutableCopy func(allocator uintptr, capacity cfIndex, dict cfDictionaryRef) cfDictionaryRef
	cfStringCreateWithCString  func(alloc uintptr, cstr *byte, encoding uint32) cfStringRef
	cfGetTypeID                func(ref cfTypeRef) uint64
	cfStringGetTypeID          func() uint64
	cfNumberGetTypeID          func() uint64
	cfBooleanGetTypeID         func() uint64
	cfStringGetCString         func(ref cfStringRef, buf *byte, bufLen cfIndex, encoding uint32) bool
	cfNumberGetValue           func(ref cfTypeRef, numType int32, valuePtr unsafe.Pointer) bool
	cfBooleanGetValue          func(ref cfTypeRef) bool
	cfRelease                  func(ref cfTypeRef)
)

func loadCF() error {
	if cfHandle != 0 {
		return nil
	}
	var err error
	cfHandle, err = purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_LAZY)
	if err != nil {
		return fmt.Errorf("load CoreFoundation: %w", err)
	}
	purego.RegisterLibFunc(&cfArrayGetCount, cfHandle, "CFArrayGetCount")
	purego.RegisterLibFunc(&cfArrayGetValueAtIndex, cfHandle, "CFArrayGetValueAtIndex")
	purego.RegisterLibFunc(&cfDictionaryGetValue, cfHandle, "CFDictionaryGetValue")
	purego.RegisterLibFunc(&cfDictionaryCreateMutableCopy, cfHandle, "CFDictionaryCreateMutableCopy")
	purego.RegisterLibFunc(&cfStringCreateWithCString, cfHandle, "CFStringCreateWithCString")
	purego.RegisterLibFunc(&cfGetTypeID, cfHandle, "CFGetTypeID")
	purego.RegisterLibFunc(&cfStringGetTypeID, cfHandle, "CFStringGetTypeID")
	purego.RegisterLibFunc(&cfNumberGetTypeID, cfHandle, "CFNumberGetTypeID")
	purego.RegisterLibFunc(&cfBooleanGetTypeID, cfHandle, "CFBooleanGetTypeID")
	purego.RegisterLibFunc(&cfStringGetCString, cfHandle, "CFStringGetCString")
	purego.RegisterLibFunc(&cfNumberGetValue, cfHandle, "CFNumberGetValue")
	purego.RegisterLibFunc(&cfBooleanGetValue, cfHandle, "CFBooleanGetValue")
	purego.RegisterLibFunc(&cfRelease, cfHandle, "CFRelease")
	return nil
}

func makeCFString(s string) cfStringRef {
	cs := cstring(s)
	return cfStringCreateWithCString(0, cs, kCFStringEncodingUTF8)
}

func cfStringToGo(ref cfStringRef) string {
	if ref == 0 {
		return ""
	}
	buf := make([]byte, 256)
	if cfStringGetCString(ref, &buf[0], cfIndex(len(buf)), kCFStringEncodingUTF8) {
		for i, b := range buf {
			if b == 0 {
				return string(buf[:i])
			}
		}
	}
	return ""
}

func cstring(s string) *byte {
	b := append([]byte(s), 0)
	return &b[0]
}

func dictGetString(dict cfDictionaryRef, key string) string {
	k := makeCFString(key)
	defer cfRelease(cfTypeRef(k))
	v := cfDictionaryGetValue(dict, cfStringRef(k))
	if v == 0 {
		return ""
	}
	if cfGetTypeID(v) == cfStringGetTypeID() {
		return cfStringToGo(cfStringRef(v))
	}
	return ""
}

func dictGetInt64(dict cfDictionaryRef, key string) int64 {
	k := makeCFString(key)
	defer cfRelease(cfTypeRef(k))
	v := cfDictionaryGetValue(dict, cfStringRef(k))
	if v == 0 {
		return 0
	}
	if cfGetTypeID(v) == cfNumberGetTypeID() {
		var n int64
		cfNumberGetValue(v, kCFNumberSInt64Type, unsafe.Pointer(&n))
		return n
	}
	return 0
}

func dictGetBool(dict cfDictionaryRef, key string) (bool, bool) {
	k := makeCFString(key)
	defer cfRelease(cfTypeRef(k))
	v := cfDictionaryGetValue(dict, cfStringRef(k))
	if v == 0 {
		return false, false
	}
	return cfBooleanGetValue(v), true
}
