//go:build darwin

package aneperf

import (
	"fmt"
	"sort"

	"github.com/ebitengine/purego"
)

var (
	iokitHandle uintptr

	ioMainPort                        func(unused uintptr, port *uint32) int32
	ioServiceMatching                 func(name *byte) cfDictionaryRef
	ioServiceGetMatchingServices      func(mainPort uint32, matching cfDictionaryRef, iterator *uint32) int32
	ioIteratorNext                    func(iterator uint32) uint32
	ioRegistryEntryCreateCFProperties func(entry uint32, properties *cfDictionaryRef, allocator uintptr, options uint32) int32
	ioObjectRelease                   func(object uint32) int32
)

func loadIOKit() error {
	if iokitHandle != 0 {
		return nil
	}
	if err := loadCF(); err != nil {
		return err
	}
	var err error
	iokitHandle, err = purego.Dlopen("/System/Library/Frameworks/IOKit.framework/IOKit", purego.RTLD_LAZY)
	if err != nil {
		return fmt.Errorf("load IOKit: %w", err)
	}
	purego.RegisterLibFunc(&ioMainPort, iokitHandle, "IOMainPort")
	purego.RegisterLibFunc(&ioServiceMatching, iokitHandle, "IOServiceMatching")
	purego.RegisterLibFunc(&ioServiceGetMatchingServices, iokitHandle, "IOServiceGetMatchingServices")
	purego.RegisterLibFunc(&ioIteratorNext, iokitHandle, "IOIteratorNext")
	purego.RegisterLibFunc(&ioRegistryEntryCreateCFProperties, iokitHandle, "IORegistryEntryCreateCFProperties")
	purego.RegisterLibFunc(&ioObjectRelease, iokitHandle, "IOObjectRelease")
	return nil
}

// DeviceInfo contains H11ANE device properties from the IORegistry.
type DeviceInfo struct {
	Architecture  string       `json:"architecture"`
	NumCores      int64        `json:"num_cores"`
	InstanceCount int          `json:"instance_count,omitempty"`
	Instances     []DeviceInfo `json:"instances,omitempty"`
	BoardType     int64        `json:"board_type"`
	BoardSubType  int64        `json:"board_sub_type"`
	Version       int64        `json:"version"`
	MinorVersion  int64        `json:"minor_version"`
	FirmwareOK    bool         `json:"firmware_loaded"`
	PowerState    int64        `json:"power_state"`
	MaxPowerSt    int64        `json:"max_power_state"`
}

// ReadDeviceInfo reads every H11ANE device and returns their combined core count.
// Instances retains the properties of each physical ANE device.
func ReadDeviceInfo() (DeviceInfo, error) {
	devices, err := ReadDeviceInfos()
	if err != nil {
		return DeviceInfo{}, err
	}
	return combineDeviceInfos(devices), nil
}

func combineDeviceInfos(devices []DeviceInfo) DeviceInfo {
	combined := devices[0]
	combined.InstanceCount = len(devices)
	combined.Instances = devices
	combined.NumCores = 0
	for _, device := range devices {
		combined.NumCores += device.NumCores
		combined.FirmwareOK = combined.FirmwareOK && device.FirmwareOK
	}
	return combined
}

// ReadDeviceInfos reads properties for each H11ANE service in the IORegistry.
func ReadDeviceInfos() ([]DeviceInfo, error) {
	if err := loadIOKit(); err != nil {
		return nil, err
	}

	var port uint32
	if ioMainPort(0, &port) != 0 {
		return nil, fmt.Errorf("read device info: IOMainPort failed")
	}

	matching := ioServiceMatching(cstring("H11ANEIn"))
	if matching == 0 {
		return nil, fmt.Errorf("read device info: no H11ANEIn matching dict")
	}

	var iter uint32
	if ioServiceGetMatchingServices(port, matching, &iter) != 0 {
		return nil, fmt.Errorf("read device info: no H11ANE service found")
	}
	defer ioObjectRelease(iter)

	var devices []DeviceInfo
	for service := ioIteratorNext(iter); service != 0; service = ioIteratorNext(iter) {
		device, err := readANEDevice(service)
		ioObjectRelease(service)
		if err != nil {
			return nil, err
		}
		devices = append(devices, device)
	}
	if len(devices) == 0 {
		return nil, fmt.Errorf("read device info: no H11ANE device")
	}
	sort.Slice(devices, func(i, j int) bool {
		return devices[i].BoardSubType < devices[j].BoardSubType
	})
	return devices, nil
}

func readANEDevice(service uint32) (DeviceInfo, error) {
	var props cfDictionaryRef
	if ioRegistryEntryCreateCFProperties(service, &props, 0, 0) != 0 {
		return DeviceInfo{}, fmt.Errorf("read device info: could not read properties")
	}
	defer cfRelease(cfTypeRef(props))

	var di DeviceInfo

	devicePropertiesKey := makeCFString("DeviceProperties")
	defer cfRelease(cfTypeRef(devicePropertiesKey))
	devPropsRef := cfDictionaryGetValue(props, devicePropertiesKey)
	if devPropsRef != 0 {
		devProps := cfDictionaryRef(devPropsRef)
		di.Architecture = dictGetString(devProps, "ANEDevicePropertyTypeANEArchitectureTypeStr")
		di.NumCores = dictGetInt64(devProps, "ANEDevicePropertyNumANECores")
		di.BoardType = dictGetInt64(devProps, "ANEDevicePropertyANEHWBoardType")
		di.BoardSubType = dictGetInt64(devProps, "ANEDevicePropertyANEHWBoardSubType")
		di.Version = dictGetInt64(devProps, "ANEDevicePropertyANEVersion")
		di.MinorVersion = dictGetInt64(devProps, "ANEDevicePropertyANEMinorVersion")
	}

	if v, ok := dictGetBool(props, "FirmwareLoaded"); ok {
		di.FirmwareOK = v
	}

	ioPowerManagementKey := makeCFString("IOPowerManagement")
	defer cfRelease(cfTypeRef(ioPowerManagementKey))
	pmRef := cfDictionaryGetValue(props, ioPowerManagementKey)
	if pmRef != 0 {
		pm := cfDictionaryRef(pmRef)
		di.PowerState = dictGetInt64(pm, "CurrentPowerState")
		di.MaxPowerSt = dictGetInt64(pm, "MaxPowerState")
	}

	return di, nil
}
