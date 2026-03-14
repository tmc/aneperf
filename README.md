# aneperf

Apple Neural Engine performance monitoring for macOS.

aneperf samples ANE energy, power management state residency, and interrupt
statistics using Apple's private IOReport and IOKit APIs via
[purego](https://github.com/ebitengine/purego) (no cgo required).

## Install

```
go install github.com/tmc/aneperf/cmd/aneperf@latest
go install github.com/tmc/aneperf/cmd/aneperfweb@latest
```

## CLI Usage

Live terminal dashboard (default):

```
aneperf
```

Single JSON sample:

```
aneperf --json
```

Custom sample interval:

```
aneperf --interval 500ms
```

## Web Dashboard

Browser-based dashboard streaming ANE telemetry over Server-Sent Events:

```
aneperfweb
aneperfweb --addr :9092 --interval 500ms
```

Open `http://localhost:9092` in a browser. The dashboard shows the same
metrics as the TUI: power, active %, compute utilization histogram, voltage
states, DCS frequency, bandwidth, throttle events, and interrupt counters.

## Library API

```go
sampler, err := aneperf.NewSampler()
if err != nil {
    log.Fatal(err)
}
defer sampler.Close()

// One-shot sample.
sample, err := sampler.Sample(time.Second)
fmt.Printf("ANE Power: %.3f W\n", sample.ANEPowerW)

// Start/Stop for precise measurement windows.
snap := sampler.Start()
// ... do work ...
delta := sampler.Stop(snap)
fmt.Printf("%.3f W over %v\n", delta.PowerW, delta.Duration)
```

### Benchmark Integration

Use `Start`/`Stop` with `ReportMetrics` to add ANE metrics to Go benchmarks:

```go
func BenchmarkMyWorkload(b *testing.B) {
    sampler, _ := aneperf.NewSampler()
    defer sampler.Close()

    snap := sampler.Start()
    for b.Loop() {
        // ... ANE work ...
    }
    delta := sampler.Stop(snap)
    delta.ReportMetrics(b)
}
```

`ReportMetrics` reports:

| Metric | Description |
|--------|-------------|
| `ane-watts` | ANE power consumption |
| `ane-energy-{unit}/op` | Total ANE energy (mJ, uJ, or nJ) |
| `ane-active-%` | Percentage of time not at VMIN |
| `ane-interrupts/op` | Total interrupt handler count |
| `ane-throttle-events/op` | Throttle event count (only if >0) |
| `sample-ns/op` | Measurement duration |

### Device Info

```go
info, err := aneperf.ReadDeviceInfo()
// info.Architecture, info.NumCores, info.FirmwareOK, info.PowerState, ...
```

## Channel Groups

ANE metrics are organized into IOReport channel groups:

- **Energy Model** — energy consumption values (mJ/uJ/nJ)
- **PMP** — power management states:
  - **SOC Floor** — voltage states (VMIN, VNOM, VMAX)
  - **DCS Floor** — frequency states (F1–F6)
  - **Fast-Die CE** — compute utilization histogram (0%–100% buckets)
  - **AF BW / DCS BW / SOC-NI Util BW** — bandwidth tiers
  - **PWRS** — throttle event counters
- **Interrupt Statistics** — per-index interrupt handler counts

## Architecture

aneperf uses `purego` to dynamically load Apple's private frameworks at
runtime without cgo:

- `libIOReport.dylib` — IOReport subscription and channel sampling
- `IOKit.framework` — H11ANEIn service discovery and device properties
- `CoreFoundation.framework` — CF type wrappers

The sampler creates IOReport subscriptions for Energy Model, PMP, and
Interrupt Statistics channel groups, filtering to ANE-related channels.
Two snapshots are taken and differenced to produce a `Delta` with power
estimates and state residencies.

## testworkload

The `testworkload` package generates real ANE activity for testing:

```
ANE_BENCH=1 go test -bench=. -benchtime=5s ./testworkload/
```

It compiles and evaluates models directly on the ANE using
`github.com/tmc/apple/x/ane`, reporting aneperf metrics alongside
standard benchmark output.

## Requirements

- macOS with Apple Silicon (M1+)
- Go 1.25+
- Uses private Apple APIs (`libIOReport.dylib`, IOKit `H11ANEIn` service) — these are undocumented and may change between macOS versions
