//go:build darwin

// Command aneperf monitors Apple Neural Engine performance metrics.
//
// By default, it runs a continuous terminal dashboard showing ANE power,
// energy, and state residency. Use --json for a single JSON sample.
//
// Usage:
//
//	aneperf [flags]
//
// Flags:
//
//	--interval  sample interval (default 1s)
//	--json      single JSON sample then exit
//	-v          verbose JSON output (include all raw channels)
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strings"
	"time"

	"github.com/tmc/aneperf"
)

// ANSI escape helpers.
const (
	ansiReset    = "\033[0m"
	ansiBold     = "\033[1m"
	ansiDim      = "\033[2m"
	ansiGreen    = "\033[32m"
	ansiGreenBld = "\033[32;1m"
	ansiGreenDim = "\033[32;2m"
	ansiYellow   = "\033[33m"
	ansiBlue     = "\033[34m"
	ansiCyan     = "\033[36m"
	ansiCyanBold = "\033[36;1m"
	ansiWhite    = "\033[37m"
	ansiRed      = "\033[31m"
	ansiHome     = "\033[H"
	ansiClearEOD = "\033[J"
	ansiAltOn    = "\033[?1049h"
	ansiAltOff   = "\033[?1049l"
	ansiHideCur  = "\033[?25l"
	ansiShowCur  = "\033[?25h"
)

func main() {
	interval := flag.Duration("interval", 1*time.Second, "sample interval")
	jsonOut := flag.Bool("json", false, "single JSON sample then exit")
	verbose := flag.Bool("v", false, "verbose JSON output (include all raw channels)")
	flag.Parse()

	if err := run(*interval, *jsonOut, *verbose); err != nil {
		fmt.Fprintf(os.Stderr, "aneperf: %v\n", err)
		os.Exit(1)
	}
}

func run(interval time.Duration, jsonOut, verbose bool) error {
	sampler, err := aneperf.NewSampler()
	if err != nil {
		return err
	}
	defer sampler.Close()

	if jsonOut {
		return runOnce(sampler, interval, verbose)
	}
	return runLive(sampler, interval)
}

// sampleSummary is the condensed JSON output for --json (without -v).
type sampleSummary struct {
	Timestamp           time.Time `json:"timestamp"`
	Architecture        string    `json:"architecture"`
	NumCores            int64     `json:"num_cores"`
	ANEPowerW           float64   `json:"ane_power_watts"`
	ANEUtilizationPct   float64   `json:"ane_utilization_pct"`
	ANEClusterActivePct float64   `json:"ane_cluster_active_pct"`
	GPUPowerW           float64   `json:"gpu_power_watts,omitempty"`
	GPUActivePct        float64   `json:"gpu_active_pct,omitempty"`
	GPUTempC            float64   `json:"gpu_temp_c,omitempty"`
}

func runOnce(sampler *aneperf.Sampler, interval time.Duration, verbose bool) error {
	sample, err := sampler.Sample(interval)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if verbose {
		return enc.Encode(sample)
	}
	return enc.Encode(sampleSummary{
		Timestamp:           sample.Timestamp,
		Architecture:        sample.Device.Architecture,
		NumCores:            sample.Device.NumCores,
		ANEPowerW:           sample.ANEPowerW,
		ANEUtilizationPct:   sample.ANEUtilizationPct,
		ANEClusterActivePct: sample.ANEClusterActivePct,
		GPUPowerW:           sample.GPUPowerW,
		GPUActivePct:        sample.GPUActivePct,
		GPUTempC:            sample.GPUTempC,
	})
}

var powerHistory []float64
var gpuPowerHistory []float64
var gpuStateHistory = newChannelStateWindow(4)
var bandwidthHistory = newChannelStateWindow(4)

// channelHistory tracks per-channel scalar histories (bandwidth, rates, etc.)
var channelHistory = map[string][]float64{}

const historyLen = 38
const maxRenderedStates = 4

type statePct struct {
	name string
	pct  float64
}

func runLive(sampler *aneperf.Sampler, interval time.Duration) error {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	defer signal.Stop(sig)

	powerHistory = nil
	gpuPowerHistory = nil
	gpuStateHistory = newChannelStateWindow(4)
	bandwidthHistory = newChannelStateWindow(4)
	channelHistory = map[string][]float64{}

	enterLiveScreen()
	defer leaveLiveScreen()

	snap := sampler.Start()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-sig:
			return nil
		case <-ticker.C:
		}

		delta := sampler.Stop(snap)
		snap = sampler.Start()

		powerHistory = append(powerHistory, delta.PowerW)
		gpuPowerHistory = append(gpuPowerHistory, delta.GPUPowerW)
		if len(powerHistory) > historyLen {
			powerHistory = powerHistory[len(powerHistory)-historyLen:]
		}
		if len(gpuPowerHistory) > historyLen {
			gpuPowerHistory = gpuPowerHistory[len(gpuPowerHistory)-historyLen:]
		}

		printLive(delta, interval)
	}
}

func printLive(d aneperf.Delta, interval time.Duration) {
	cat := aneperf.ClassifyChannels(d.Channels)
	stats := aneperf.ComputeStats(d)
	gpuStateHistory.observe(cat.GPUStats)
	bandwidthHistory.observe(cat.Bandwidth)
	gpuStates := gpuStateHistory.smooth(cat.GPUStats)
	bandwidth := bandwidthHistory.smooth(cat.Bandwidth)

	fmt.Print(ansiHome, ansiClearEOD)

	// Header.
	fmt.Printf("%s aneperf %s%s— %s  (every %s, measured %.3fs)%s\n",
		ansiCyanBold, ansiReset, ansiDim, time.Now().Format("15:04:05"), interval, d.Duration.Seconds(), ansiReset)
	fmt.Printf("%s%s%s\n", ansiDim, strings.Repeat("━", 58), ansiReset)

	// Device info.
	fwIcon := ansiRed + "✗" + ansiReset
	if d.Device.FirmwareOK {
		fwIcon = ansiGreen + "✓" + ansiReset
	}
	fmt.Printf("  %s%s%s  %s%d cores%s  v%d.%d  fw:%s  pwr:%s%d%s/%d\n",
		ansiBold, d.Device.Architecture, ansiReset,
		ansiWhite, d.Device.NumCores, ansiReset,
		d.Device.Version, d.Device.MinorVersion,
		fwIcon,
		ansiDim, d.Device.PowerState, ansiReset, d.Device.MaxPowerSt)

	// Cluster power state — compact indicator right after device info.
	printClusterPower(cat.ClusterPower)
	fmt.Println()

	// Power section.
	pMin, pAvg, pMax := powerStats(powerHistory)
	fmt.Printf("%s╸ Power%s\n", ansiBold, ansiReset)
	fmt.Printf("  ANE Power:  %s%.3f W%s    %smin %.3f  avg %.3f  max %.3f%s\n",
		ansiGreenBld, d.PowerW, ansiReset, ansiDim, pMin, pAvg, pMax, ansiReset)
	if hasGPUInfo(d, cat.GPUStats) {
		gMin, gAvg, gMax := powerStats(gpuPowerHistory)
		fmt.Printf("  GPU Power:  %s%.3f W%s    %smin %.3f  avg %.3f  max %.3f%s\n",
			ansiBlue, d.GPUPowerW, ansiReset, ansiDim, gMin, gAvg, gMax, ansiReset)
		if d.GPUTempC > 0 {
			fmt.Printf("  GPU Temp:   %s%.1f C%s\n", ansiBlue, d.GPUTempC, ansiReset)
		}
	}

	// Active percentage — prefer Fast-Die CE histogram if available.
	fmt.Printf("  ANE Active: %s\n", activeBar(stats.ActivePct, 30))
	if hasGPUInfo(d, cat.GPUStats) {
		fmt.Printf("  GPU Active: %s\n", activeBar(computeStateActivePct(gpuStates, "OFF", "IDLE", "DOWN"), 30))
	}

	fmt.Printf("  %s  History:    %s%s\n", ansiDim, sparkline(powerHistory), ansiReset)
	if hasGPUInfo(d, cat.GPUStats) {
		fmt.Printf("  %s  GPU Hist:   %s%s\n", ansiDim, sparkline(gpuPowerHistory), ansiReset)
	}

	// Energy value.
	type energyRow struct {
		channel string
		unit    string
		value   int64
		watts   float64
	}
	var energyRows []energyRow
	for _, ch := range cat.Energy {
		if ch.Value == 0 {
			continue
		}
		watts := energyValueToWatts(ch.Value, ch.Unit, interval)
		energyRows = append(energyRows, energyRow{
			channel: ch.Channel,
			unit:    ch.Unit,
			value:   ch.Value,
			watts:   watts,
		})
	}
	sort.Slice(energyRows, func(i, j int) bool {
		if energyRows[i].watts == energyRows[j].watts {
			return energyRows[i].channel < energyRows[j].channel
		}
		return energyRows[i].watts > energyRows[j].watts
	})
	if len(energyRows) == 0 {
		fmt.Printf("  %sno non-zero energy deltas%s\n", ansiDim, ansiReset)
	} else {
		limit := min(len(energyRows), 4)
		for _, row := range energyRows[:limit] {
			fmt.Printf("  %s%-20s%s %s%5d %s%s %s(%.3fW)%s\n",
				ansiWhite, row.channel, ansiReset,
				ansiWhite, row.value, row.unit, ansiReset,
				ansiDim, row.watts, ansiReset)
		}
		if len(energyRows) > limit {
			fmt.Printf("  %s+%d more energy channels%s\n", ansiDim, len(energyRows)-limit, ansiReset)
		}
	}
	fmt.Println()

	// Compute utilization histogram.
	printComputeUtilization(cat.ComputeEn, stats)
	printGPUStats(gpuStates)

	// Voltage states.
	printStateSection("Voltage States", cat.Voltage, ansiBlue, ansiGreen)

	// DCS frequency states.
	printStateSection("DCS Frequency", cat.DCSFloor, ansiCyan, ansiGreen)

	// Bandwidth — compact summary.
	printBandwidth(bandwidth)

	// Throttle.
	printThrottle(cat.Throttle, stats.TotalThrottles, d.Duration)

	// Throttle detail — SoC Stats residency breakdown.
	printThrottleDetail(cat.ThrottleDetail)

	// Counters — split into count channels and time channels.
	printCounters(cat.Interrupt, d.Duration)

	fmt.Printf("\n%sPress Ctrl-C to stop.%s\n", ansiDim, ansiReset)
}

// computeActivePctFromCE computes ANE utilization from the Fast-Die CE histogram.
// States are percentage buckets (0%, 5%, 10%, ... 100%). We compute the
// weighted average utilization.
func computeActivePctFromCE(channels []aneperf.Channel) float64 {
	for _, ch := range channels {
		if len(ch.States) == 0 {
			continue
		}
		var totalRes int64
		var weightedSum float64
		for _, s := range ch.States {
			totalRes += s.Residency
			// Parse percentage from state name like "0%", "5%", "100%".
			name := strings.TrimSpace(s.Name)
			name = strings.TrimSuffix(name, "%")
			pct := 0.0
			fmt.Sscanf(name, "%f", &pct)
			weightedSum += pct * float64(s.Residency)
		}
		if totalRes > 0 {
			return weightedSum / float64(totalRes)
		}
	}
	return 0
}

func computeActivePctFromVoltage(channels []aneperf.Channel) float64 {
	// Return the maximum active percentage across all voltage state channels.
	maxActive := 0.0
	for _, ch := range channels {
		if len(ch.States) == 0 {
			continue
		}
		var total, vminRes int64
		hasVMIN := false
		for _, s := range ch.States {
			total += s.Residency
			if strings.TrimSpace(s.Name) == "VMIN" {
				vminRes = s.Residency
				hasVMIN = true
			}
		}
		if hasVMIN && total > 0 {
			active := float64(total-vminRes) / float64(total) * 100
			if active > maxActive {
				maxActive = active
			}
		}
	}
	return maxActive
}

func energyValueToWatts(value int64, unit string, interval time.Duration) float64 {
	ms := float64(interval) / float64(time.Millisecond)
	if ms <= 0 {
		ms = 1000
	}
	rate := float64(value) / (ms / 1000.0)
	switch unit {
	case "mJ":
		return rate / 1e3
	case "uJ":
		return rate / 1e6
	case "nJ":
		return rate / 1e9
	default:
		return rate / 1e6
	}
}

func activeBar(pct float64, width int) string {
	filled := int(pct / 100 * float64(width))
	filled = min(filled, width)
	var b strings.Builder
	b.WriteString(ansiGreen + "[" + ansiReset)
	b.WriteString(ansiGreenDim)
	for i := range width {
		if i < filled {
			b.WriteString("█")
		} else {
			b.WriteString("░")
		}
	}
	b.WriteString(ansiReset)
	b.WriteString(ansiGreen + "]" + ansiReset)
	fmt.Fprintf(&b, " %5.1f%%", pct)
	return b.String()
}

var sparkBlocks = []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

func sparkline(data []float64) string {
	if len(data) == 0 {
		return ""
	}
	maxVal := 0.0
	for _, v := range data {
		if v > maxVal {
			maxVal = v
		}
	}
	var b strings.Builder
	for _, v := range data {
		idx := 0
		if maxVal > 0 {
			idx = int(v / maxVal * float64(len(sparkBlocks)-1))
			idx = min(idx, len(sparkBlocks)-1)
		}
		b.WriteRune(sparkBlocks[idx])
	}
	return b.String()
}

// printComputeUtilization renders the Fast-Die CE histogram as a compact bar.
func printComputeUtilization(channels []aneperf.Channel, stats aneperf.DeltaStats) {
	for _, ch := range channels {
		if len(ch.States) == 0 {
			continue
		}
		var total int64
		for _, s := range ch.States {
			total += s.Residency
		}
		if total == 0 {
			continue
		}

		peakInfo := ""
		if stats.PeakCEBucket != "" {
			peakInfo = fmt.Sprintf("  peak:%s  avg:%.1f%%", stats.PeakCEBucket, stats.ActivePct)
		}
		fmt.Printf("%s╸ Compute Utilization%s %s(%s)%s%s\n", ansiBold, ansiReset, ansiDim, ch.Channel, peakInfo, ansiReset)

		// Show histogram of percentage buckets.
		maxRes := int64(0)
		for _, s := range ch.States {
			if s.Residency > maxRes {
				maxRes = s.Residency
			}
		}
		for _, s := range ch.States {
			pct := float64(s.Residency) / float64(total) * 100
			if pct < 0.5 {
				continue
			}
			name := strings.TrimSpace(s.Name)
			barLen := 0
			if maxRes > 0 {
				barLen = int(float64(s.Residency) / float64(maxRes) * 20)
			}
			bar := strings.Repeat("▓", barLen) + strings.Repeat("░", 20-barLen)
			fmt.Printf("  %s%4s%s %s%s%s %4.1f%%\n", ansiCyan, name, ansiReset, ansiGreenDim, bar, ansiReset, pct)
		}
		fmt.Println()
	}
}

func printGPUStats(channels []aneperf.Channel) {
	if len(channels) == 0 {
		return
	}
	printStateSection("GPU States", channels, ansiBlue, ansiCyan)
}

// printStateSection prints voltage or DCS floor state channels.
// Always records voltage history so past activity remains visible.
func printStateSection(title string, channels []aneperf.Channel, dominantColor, otherColor string) {
	if len(channels) == 0 {
		return
	}

	// Record voltage history for all channels upfront.
	if title == "Voltage States" {
		for _, ch := range channels {
			activePct := 100.0 - vminPct(ch)
			recordHistory("volt:"+ch.Channel, activePct)
		}
	}

	var lines []string
	shown := make(map[string]bool)
	for _, ch := range channels {
		var total int64
		for _, s := range ch.States {
			total += s.Residency
		}
		if total == 0 {
			continue
		}

		// Collect states >0.5%.
		var active []statePct
		dominantName := ""
		dominantPct := 0.0
		for _, s := range ch.States {
			pct := float64(s.Residency) / float64(total) * 100
			name := strings.TrimSpace(s.Name)
			if pct >= 0.5 {
				active = append(active, statePct{name, pct})
			}
			if pct > dominantPct {
				dominantPct = pct
				dominantName = name
			}
		}
		sort.Slice(active, func(i, j int) bool {
			if active[i].pct == active[j].pct {
				return active[i].name < active[j].name
			}
			return active[i].pct > active[j].pct
		})

		if !stateSectionInteresting(title, active, dominantName, dominantPct) {
			continue
		}
		shown[ch.Channel] = true

		var voltStats string
		if title == "Voltage States" {
			voltStats = historyStats("volt:" + ch.Channel)
		}

		var b strings.Builder
		fmt.Fprintf(&b, "  %-22s", ch.Channel)
		limit := min(len(active), maxRenderedStates)
		for _, s := range active[:limit] {
			color := otherColor
			if s.name == dominantName {
				color = dominantColor
			}
			fmt.Fprintf(&b, "  %s%s%s %.0f%%", color, s.name, ansiReset, s.pct)
		}
		if len(active) > limit {
			fmt.Fprintf(&b, "  %s+%d%s", ansiDim, len(active)-limit, ansiReset)
		}
		b.WriteString(voltStats)
		lines = append(lines, b.String())
	}

	// Show channels that have historical signal even if currently idle/uninteresting.
	if title == "Voltage States" {
		for _, ch := range channels {
			if shown[ch.Channel] {
				continue
			}
			hkey := "volt:" + ch.Channel
			if hasHistorySignal(hkey) {
				lines = append(lines, fmt.Sprintf("  %-22s  %sidle%s%s", ch.Channel, ansiDim, ansiReset, historyStats(hkey)))
			}
		}
	}

	if len(lines) == 0 {
		return
	}
	fmt.Printf("%s╸ %s%s\n", ansiBold, title, ansiReset)
	for _, line := range lines {
		fmt.Println(line)
	}
	fmt.Println()
}

// printBandwidth prints bandwidth channels grouped by SubGroup with headers.
// Always records history (even zero) so past activity remains visible.
func printBandwidth(channels []aneperf.Channel) {
	if len(channels) == 0 {
		return
	}

	type bandwidthRow struct {
		group   string
		channel string
		summary bandwidthSummary
		ok      bool // has current-sample data
	}

	// Record history for all channels, even idle ones.
	var rows []bandwidthRow
	for _, ch := range channels {
		summary, ok := summarizeBandwidth(ch)
		hkey := "bw:" + ch.Channel
		if ok {
			recordHistory(hkey, summary.AvgGBps)
		} else {
			recordHistory(hkey, 0)
		}
		if ok && summary.isInteresting() {
			rows = append(rows, bandwidthRow{group: ch.SubGroup, channel: ch.Channel, summary: summary, ok: true})
		}
	}

	// Also show channels that have historical signal even if current sample is idle.
	shown := make(map[string]bool)
	for _, r := range rows {
		shown[r.channel] = true
	}
	for _, ch := range channels {
		if shown[ch.Channel] {
			continue
		}
		hkey := "bw:" + ch.Channel
		if hasHistorySignal(hkey) {
			rows = append(rows, bandwidthRow{group: ch.SubGroup, channel: ch.Channel})
		}
	}

	if len(rows) == 0 {
		return
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].summary.AvgGBps == rows[j].summary.AvgGBps {
			if rows[i].group == rows[j].group {
				return rows[i].channel < rows[j].channel
			}
			return rows[i].group < rows[j].group
		}
		return rows[i].summary.AvgGBps > rows[j].summary.AvgGBps
	})

	fmt.Printf("%s╸ Bandwidth%s\n", ansiBold, ansiReset)
	limit := min(len(rows), 8)
	for _, row := range rows[:limit] {
		hkey := "bw:" + row.channel
		if row.ok {
			fmt.Printf("  %s%-14s%s  %-18s  %s%5.1fGB/s%s avg  %s%-6s%s %3.0f%%  %speak %4.0fGB/s%s%s\n",
				ansiDim, row.group, ansiReset,
				row.channel,
				ansiCyan, row.summary.AvgGBps, ansiReset,
				ansiWhite, row.summary.DominantName, ansiReset, row.summary.DominantPct,
				ansiDim, row.summary.MaxGBps, ansiReset,
				historyStats(hkey))
		} else {
			// Idle now but has history.
			fmt.Printf("  %s%-14s  %-18s  idle%s%s\n",
				ansiDim, row.group, row.channel, ansiReset,
				historyStats(hkey))
		}
	}
	if len(rows) > limit {
		fmt.Printf("  %s+%d more bandwidth channels%s\n", ansiDim, len(rows)-limit, ansiReset)
	}
	fmt.Println()
}

type bandwidthSummary struct {
	MinGBps      float64
	DominantName string
	DominantPct  float64
	MaxGBps      float64
	AvgGBps      float64
}

func (s bandwidthSummary) isInteresting() bool {
	return s.AvgGBps >= s.MinGBps+1 || s.DominantPct < 90
}

func summarizeBandwidth(ch aneperf.Channel) (bandwidthSummary, bool) {
	var total int64
	var weighted float64
	dominantName := ""
	dominantRes := int64(-1)
	maxTier := 0.0
	minTier := 0.0
	haveTier := false

	for _, s := range ch.States {
		res := s.Residency
		tier := parseBandwidthGBps(strings.TrimSpace(s.Name))
		total += res
		weighted += tier * float64(res)
		if res > dominantRes {
			dominantRes = res
			dominantName = strings.TrimSpace(s.Name)
		}
		if tier > 0 {
			if !haveTier || tier < minTier {
				minTier = tier
			}
			if tier > maxTier {
				maxTier = tier
			}
			haveTier = true
		}
	}
	if total == 0 || !haveTier {
		return bandwidthSummary{}, false
	}
	return bandwidthSummary{
		MinGBps:      minTier,
		DominantName: dominantName,
		DominantPct:  float64(dominantRes) / float64(total) * 100,
		MaxGBps:      maxTier,
		AvgGBps:      weighted / float64(total),
	}, true
}

func parseBandwidthGBps(name string) float64 {
	var gbps float64
	if _, err := fmt.Sscanf(name, "%fGB/s", &gbps); err == nil {
		return gbps
	}
	if _, err := fmt.Sscanf(name, "%f GB/s", &gbps); err == nil {
		return gbps
	}
	return 0
}

// printThrottle prints throttle event counters.
// Always records history so past throttle events remain visible.
func printThrottle(channels []aneperf.Channel, totalThrottles int64, dur time.Duration) {
	rate := 0.0
	if dur.Seconds() > 0 && totalThrottles > 0 {
		rate = float64(totalThrottles) / dur.Seconds()
	}
	recordHistory("throttle:total", rate)

	hasThrottle := totalThrottles > 0
	hasHistory := hasHistorySignal("throttle:total")
	if !hasThrottle && !hasHistory {
		return
	}

	fmt.Printf("%s╸ Throttle%s %s%d total  %.0f/s%s%s\n",
		ansiBold, ansiReset, ansiDim, totalThrottles, rate, ansiReset,
		historyStats("throttle:total"))
	for _, ch := range channels {
		if ch.Value > 0 {
			fmt.Printf("  %-40s %s%d%s %s\n", ch.Channel, ansiYellow, ch.Value, ansiReset, ch.Unit)
		}
	}
	fmt.Println()
}

// printCounters prints interrupt/counter channels with rate, split into count vs time.
// Always records history so past activity remains visible when idle.
func printCounters(channels []aneperf.Channel, dur time.Duration) {
	var countChs, timeChs []aneperf.Channel
	for _, ch := range channels {
		isTime := strings.Contains(ch.Channel, "(MATUs)") || strings.Contains(ch.Unit, "MATU")
		if isTime {
			timeChs = append(timeChs, ch)
		} else {
			countChs = append(countChs, ch)
		}
	}

	durSec := dur.Seconds()

	// Record history for all channels (even zero-value).
	for _, ch := range countChs {
		rate := 0.0
		if durSec > 0 && ch.Value > 0 {
			rate = float64(ch.Value) / durSec
		}
		recordHistory("cnt:"+ch.Channel, rate)
	}
	for _, ch := range timeChs {
		rate := 0.0
		if durSec > 0 && ch.Value > 0 {
			rate = float64(ch.Value) / durSec
		}
		recordHistory("time:"+ch.Channel, rate)
	}

	// Show counters that have current activity or historical signal.
	var showCount []aneperf.Channel
	for _, ch := range countChs {
		if ch.Value > 0 || hasHistorySignal("cnt:"+ch.Channel) {
			showCount = append(showCount, ch)
		}
	}
	if len(showCount) > 0 {
		fmt.Printf("%s╸ Counters%s\n", ansiBold, ansiReset)
		for _, ch := range showCount {
			rate := 0.0
			if durSec > 0 && ch.Value > 0 {
				rate = float64(ch.Value) / durSec
			}
			hkey := "cnt:" + ch.Channel
			fmt.Printf("  %-24s %s%8d%s  %s%7.1f/s%s%s\n",
				shortCounterName(ch.Channel), ansiYellow, ch.Value, ansiReset, ansiDim, rate, ansiReset,
				historyStats(hkey))
		}
		fmt.Println()
	}

	var showTime []aneperf.Channel
	for _, ch := range timeChs {
		if ch.Value > 0 || hasHistorySignal("time:"+ch.Channel) {
			showTime = append(showTime, ch)
		}
	}
	if len(showTime) > 0 {
		fmt.Printf("%s╸ Handler Time (MATUs)%s\n", ansiBold, ansiReset)
		for _, ch := range showTime {
			rate := 0.0
			if durSec > 0 && ch.Value > 0 {
				rate = float64(ch.Value) / durSec
			}
			hkey := "time:" + ch.Channel
			fmt.Printf("  %-24s %s%8d%s  %s%7.1f/s%s%s\n",
				shortCounterName(ch.Channel), ansiDim, ch.Value, ansiReset, ansiDim, rate, ansiReset,
				historyStats(hkey))
		}
		fmt.Println()
	}
}

// printClusterPower renders a one-line cluster power state indicator.
func printClusterPower(channels []aneperf.Channel) {
	if len(channels) == 0 {
		return
	}
	for _, ch := range channels {
		if len(ch.States) == 0 {
			continue
		}
		var total int64
		for _, s := range ch.States {
			total += s.Residency
		}
		if total == 0 {
			continue
		}
		// Show ACT residency.
		for _, s := range ch.States {
			if strings.TrimSpace(s.Name) == "ACT" {
				pct := float64(s.Residency) / float64(total) * 100
				color := ansiGreen
				if pct < 50 {
					color = ansiYellow
				}
				fmt.Printf("  %s╸ Cluster Power%s   %s: %s%sACT %.1f%%%s\n",
					ansiDim, ansiReset, ch.Channel, color, ansiBold, pct, ansiReset)
			}
		}
	}
}

// printThrottleDetail renders SoC Stats throttle reason residency.
func printThrottleDetail(channels []aneperf.Channel) {
	if len(channels) == 0 {
		return
	}

	// Check if any throttle reason has ACT > 0.
	anyActive := false
	for _, ch := range channels {
		for _, s := range ch.States {
			if strings.TrimSpace(s.Name) == "ACT" && s.Residency > 0 {
				anyActive = true
				break
			}
		}
		if anyActive {
			break
		}
	}

	if !anyActive {
		return
	}
	fmt.Printf("%s╸ Throttle Detail%s\n", ansiBold, ansiReset)

	for _, ch := range channels {
		if len(ch.States) == 0 {
			continue
		}
		var total int64
		for _, s := range ch.States {
			total += s.Residency
		}
		if total == 0 {
			continue
		}

		var active []statePct
		for _, s := range ch.States {
			pct := float64(s.Residency) / float64(total) * 100
			name := strings.TrimSpace(s.Name)
			if pct >= 0.5 {
				active = append(active, statePct{name, pct})
			}
		}
		// Show channels where ACT > 0, or all if none active.
		hasACT := false
		for _, s := range active {
			if s.name == "ACT" && s.pct > 0 {
				hasACT = true
			}
		}
		color := ansiDim
		if hasACT {
			color = ansiYellow
		}
		fmt.Printf("  %s%-30s%s", color, ch.Channel, ansiReset)
		for _, s := range active {
			c := ansiDim
			if s.name == "ACT" && s.pct > 0 {
				c = ansiYellow
			}
			fmt.Printf("  %s%s%s %.0f%%", c, s.name, ansiReset, s.pct)
		}
		fmt.Println()
	}
	fmt.Println()
}

// vminPct returns the VMIN residency percentage for a channel.
func vminPct(ch aneperf.Channel) float64 {
	var total, vmin int64
	for _, s := range ch.States {
		total += s.Residency
		if strings.TrimSpace(s.Name) == "VMIN" {
			vmin = s.Residency
		}
	}
	if total == 0 {
		return 100
	}
	return float64(vmin) / float64(total) * 100
}

// recordHistory appends a value to the named channel's history, capping at historyLen.
func recordHistory(key string, val float64) {
	h := channelHistory[key]
	h = append(h, val)
	if len(h) > historyLen {
		h = h[len(h)-historyLen:]
	}
	channelHistory[key] = h
}

// historyStats returns formatted dim min/avg/max string for a channel history key.
func historyStats(key string) string {
	h := channelHistory[key]
	if len(h) < 2 {
		return ""
	}
	mn, av, mx := powerStats(h)
	return fmt.Sprintf("  %smin %.1f  avg %.1f  max %.1f%s", ansiDim, mn, av, mx, ansiReset)
}

// hasHistorySignal returns true if the named channel has any non-zero history.
func hasHistorySignal(key string) bool {
	for _, v := range channelHistory[key] {
		if v > 0.001 {
			return true
		}
	}
	return false
}

func powerStats(history []float64) (min, avg, max float64) {
	if len(history) == 0 {
		return 0, 0, 0
	}
	min = history[0]
	max = history[0]
	sum := 0.0
	for _, v := range history {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
		sum += v
	}
	avg = sum / float64(len(history))
	return min, avg, max
}

func hasGPUInfo(d aneperf.Delta, channels []aneperf.Channel) bool {
	return d.GPUPowerW > 0 || d.GPUActivePct > 0 || len(channels) > 0
}

func stateSectionInteresting(title string, active []statePct, dominantName string, dominantPct float64) bool {
	switch title {
	case "Voltage States":
		return !(dominantName == "VMIN" && dominantPct >= 99)
	case "DCS Frequency":
		return !(dominantPct >= 99 && len(active) == 1)
	default:
		return true
	}
}

func shortCounterName(name string) string {
	switch {
	case strings.Contains(name, "First Level Interrupt Handler Count"):
		return "first-level count"
	case strings.Contains(name, "Second Level Interrupt Handler Count"):
		return "second-level count"
	case strings.Contains(name, "First Level Interrupt Handler Time"):
		return "first-level time"
	case strings.Contains(name, "Second Level Interrupt Handler CPU Time"):
		return "second-level cpu"
	case strings.Contains(name, "Second Level Interrupt Handler System Time"):
		return "second-level sys"
	default:
		return name
	}
}

func enterLiveScreen() {
	fmt.Print(ansiAltOn, ansiHideCur, ansiHome, ansiClearEOD)
}

func leaveLiveScreen() {
	fmt.Print(ansiShowCur, ansiReset, ansiAltOff)
}

type channelStateWindow struct {
	limit   int
	samples map[string][]map[string]int64
}

func newChannelStateWindow(limit int) *channelStateWindow {
	return &channelStateWindow{
		limit:   limit,
		samples: make(map[string][]map[string]int64),
	}
}

func (w *channelStateWindow) observe(channels []aneperf.Channel) {
	for _, ch := range channels {
		if len(ch.States) == 0 {
			continue
		}
		snapshot := make(map[string]int64, len(ch.States))
		for _, s := range ch.States {
			name := strings.TrimSpace(s.Name)
			if name == "" {
				continue
			}
			snapshot[name] += s.Residency
		}
		key := channelStateKey(ch)
		w.samples[key] = append(w.samples[key], snapshot)
		if len(w.samples[key]) > w.limit {
			w.samples[key] = w.samples[key][len(w.samples[key])-w.limit:]
		}
	}
}

func (w *channelStateWindow) smooth(channels []aneperf.Channel) []aneperf.Channel {
	out := make([]aneperf.Channel, 0, len(channels))
	for _, ch := range channels {
		key := channelStateKey(ch)
		snapshots := w.samples[key]
		if len(snapshots) == 0 {
			out = append(out, ch)
			continue
		}
		totals := make(map[string]int64)
		for _, sample := range snapshots {
			for name, residency := range sample {
				totals[name] += residency
			}
		}
		smoothed := ch
		smoothed.States = smoothed.States[:0]
		for name, residency := range totals {
			smoothed.States = append(smoothed.States, aneperf.StateEntry{
				Name:      name,
				Residency: residency,
			})
		}
		out = append(out, smoothed)
	}
	return out
}

func channelStateKey(ch aneperf.Channel) string {
	return ch.Group + "\x00" + ch.SubGroup + "\x00" + ch.Channel
}

func computeStateActivePct(channels []aneperf.Channel, idleStates ...string) float64 {
	idle := make(map[string]bool, len(idleStates))
	for _, state := range idleStates {
		idle[state] = true
	}
	for _, ch := range channels {
		if len(ch.States) == 0 {
			continue
		}
		var activeRes, total int64
		for _, s := range ch.States {
			total += s.Residency
			if !idle[strings.TrimSpace(s.Name)] {
				activeRes += s.Residency
			}
		}
		if total > 0 {
			return float64(activeRes) / float64(total) * 100
		}
	}
	return 0
}
