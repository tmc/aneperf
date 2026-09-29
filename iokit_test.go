package aneperf

import "testing"

func TestCombineDeviceInfos(t *testing.T) {
	devices := []DeviceInfo{
		{Architecture: "h18g", NumCores: 16, BoardSubType: 0, FirmwareOK: true},
		{Architecture: "h18g", NumCores: 16, BoardSubType: 1, FirmwareOK: true},
	}
	got := combineDeviceInfos(devices)
	if got.NumCores != 32 || got.InstanceCount != 2 || len(got.Instances) != 2 {
		t.Fatalf("combined device info: %+v", got)
	}
	if got.Instances[0].BoardSubType != 0 || got.Instances[1].BoardSubType != 1 {
		t.Fatalf("lost individual device properties: %+v", got.Instances)
	}
}

func TestCombineSingleDeviceInfo(t *testing.T) {
	device := DeviceInfo{Architecture: "h17g", NumCores: 16, BoardSubType: 0}
	got := combineDeviceInfos([]DeviceInfo{device})
	if got.NumCores != 16 || got.InstanceCount != 1 || len(got.Instances) != 1 {
		t.Fatalf("single device info: %+v", got)
	}
}
