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
)

func main() {
	interval := flag.Duration("interval", 1*time.Second, "sample interval")
	jsonOut := flag.Bool("json", false, "single JSON sample then exit")
	flag.Parse()

	if err := run(*interval, *jsonOut); err != nil {
		fmt.Fprintf(os.Stderr, "aneperf: %v\n", err)
		os.Exit(1)
	}
}

func run(interval time.Duration, jsonOut bool) error {
	sampler, err := aneperf.NewSampler()
	if err != nil {
		return err
	}
	defer sampler.Close()

	if jsonOut {
		return runOnce(sampler, interval)
	}
	return runLive(sampler, interval)
}

func runOnce(sampler *aneperf.Sampler, interval time.Duration) error {
	sample, err := sampler.Sample(interval)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(sample)
}

var powerHistory []float64

const historyLen = 38

func runLive(sampler *aneperf.Sampler, interval time.Duration) error {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)

	snap := sampler.Start()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-sig:
			fmt.Fprintf(os.Stderr, "\n")
			return nil
		case <-ticker.C:
		}

		delta := sampler.Stop(snap)
		snap = sampler.Start()

		powerHistory = append(powerHistory, delta.PowerW)
		if len(powerHistory) > historyLen {
			powerHistory = powerHistory[len(powerHistory)-historyLen:]
		}

		printLive(delta, interval)
	}
}


func printLive(d aneperf.Delta, interval time.Duration) {
	fmt.Print("\033[2J\033[H")

	cat := aneperf.ClassifyChannels(d.Channels)

	// Header.
	fmt.Printf("%s aneperf %s%s— %s  (every %s)%s\n",
		ansiCyanBold, ansiReset, ansiDim, time.Now().Format("15:04:05"), interval, ansiReset)
	fmt.Printf("%s%s%s\n", ansiDim, strings.Repeat("━", 58), ansiReset)

	// Device info.
	fwIcon := ansiRed + "✗" + ansiReset
	if d.Device.FirmwareOK {
		fwIcon = ansiGreen + "✓" + ansiReset
	}
	fmt.Printf("  %s%s%s  %s%d cores%s  fw:%s  pwr:%s%d%s/%d\n",
		ansiBold, d.Device.Architecture, ansiReset,
		ansiWhite, d.Device.NumCores, ansiReset,
		fwIcon,
		ansiDim, d.Device.PowerState, ansiReset, d.Device.MaxPowerSt)
	fmt.Println()

	// Power section.
	fmt.Printf("%s╸ Power%s\n", ansiBold, ansiReset)
	fmt.Printf("  ANE Power:  %s%.3f W%s\n", ansiGreenBld, d.PowerW, ansiReset)

	// Active percentage — prefer Fast-Die CE histogram if available.
	activePct := computeActivePctFromCE(cat.ComputeEn)
	if activePct == 0 {
		activePct = computeActivePctFromVoltage(cat.Voltage)
	}
	fmt.Printf("  ANE Active: %s\n", activeBar(activePct, 30))

	fmt.Printf("  %s  History:    %s%s\n", ansiDim, sparkline(powerHistory), ansiReset)

	// Energy value.
	for _, ch := range cat.Energy {
		if ch.Value > 0 {
			watts := energyValueToWatts(ch.Value, ch.Unit, interval)
			fmt.Printf("  %s%-20s%s %s%5d %s%s %s(%.3fW)%s\n",
				ansiWhite, ch.Channel, ansiReset,
				ansiWhite, ch.Value, ch.Unit, ansiReset,
				ansiDim, watts, ansiReset)
		} else {
			fmt.Printf("  %s%-20s%s %s%5d %s%s\n",
				ansiDim, ch.Channel, ansiReset,
				ansiDim, ch.Value, ch.Unit, ansiReset)
		}
	}
	fmt.Println()

	// Compute utilization histogram.
	printComputeUtilization(cat.ComputeEn)

	// Voltage states.
	printStateSection("Voltage States", cat.Voltage, ansiBlue, ansiGreen)

	// DCS frequency states.
	printStateSection("DCS Frequency", cat.DCSFloor, ansiCyan, ansiGreen)

	// Bandwidth — compact summary.
	printBandwidth(cat.Bandwidth)

	// Throttle.
	printThrottle(cat.Throttle)

	// Counters.
	printCounters(cat.Interrupt)

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
	ms := float64(interval.Milliseconds())
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
func printComputeUtilization(channels []aneperf.Channel) {
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

		fmt.Printf("%s╸ Compute Utilization%s %s(%s)%s\n", ansiBold, ansiReset, ansiDim, ch.Channel, ansiReset)

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

// printStateSection prints voltage or DCS floor state channels.
func printStateSection(title string, channels []aneperf.Channel, dominantColor, otherColor string) {
	if len(channels) == 0 {
		return
	}

	// Check if any channel has activity.
	hasActivity := false
	for _, ch := range channels {
		var total int64
		for _, s := range ch.States {
			total += s.Residency
		}
		if total > 0 {
			hasActivity = true
			break
		}
	}
	if !hasActivity {
		return
	}

	fmt.Printf("%s╸ %s%s\n", ansiBold, title, ansiReset)
	for _, ch := range channels {
		var total int64
		for _, s := range ch.States {
			total += s.Residency
		}
		if total == 0 {
			continue
		}

		// Collect states >0.5%.
		type sp struct {
			name string
			pct  float64
		}
		var active []sp
		dominantName := ""
		dominantPct := 0.0
		for _, s := range ch.States {
			pct := float64(s.Residency) / float64(total) * 100
			name := strings.TrimSpace(s.Name)
			if pct >= 0.5 {
				active = append(active, sp{name, pct})
			}
			if pct > dominantPct {
				dominantPct = pct
				dominantName = name
			}
		}

		fmt.Printf("  %-22s", ch.Channel)
		for _, s := range active {
			color := otherColor
			if s.name == dominantName {
				color = dominantColor
			}
			fmt.Printf("  %s%s%s %0.f%%", color, s.name, ansiReset, s.pct)
		}
		fmt.Println()
	}
	fmt.Println()
}

// printBandwidth prints bandwidth channels with only the top tiers.
func printBandwidth(channels []aneperf.Channel) {
	if len(channels) == 0 {
		return
	}

	hasActivity := false
	for _, ch := range channels {
		var total int64
		for _, s := range ch.States {
			total += s.Residency
		}
		if total > 0 {
			hasActivity = true
			break
		}
	}
	if !hasActivity {
		return
	}

	fmt.Printf("%s╸ Bandwidth%s\n", ansiBold, ansiReset)
	for _, ch := range channels {
		var total int64
		for _, s := range ch.States {
			total += s.Residency
		}
		if total == 0 {
			continue
		}

		type sp struct {
			name string
			pct  float64
		}
		var active []sp
		for _, s := range ch.States {
			pct := float64(s.Residency) / float64(total) * 100
			if pct >= 0.5 {
				active = append(active, sp{strings.TrimSpace(s.Name), pct})
			}
		}
		if len(active) == 0 {
			continue
		}

		sort.Slice(active, func(i, j int) bool { return active[i].pct > active[j].pct })

		shown := min(len(active), 5)
		fmt.Printf("  %-22s", ch.Channel)
		for _, s := range active[:shown] {
			fmt.Printf("  %s%s%s %0.f%%", ansiCyan, s.name, ansiReset, s.pct)
		}
		if len(active) > shown {
			fmt.Printf("  %s+%d more%s", ansiDim, len(active)-shown, ansiReset)
		}
		fmt.Println()
	}
	fmt.Println()
}

// printThrottle prints throttle event counters.
func printThrottle(channels []aneperf.Channel) {
	var hasThrottle bool
	for _, ch := range channels {
		if ch.Value > 0 {
			hasThrottle = true
			break
		}
	}
	if !hasThrottle {
		return
	}

	fmt.Printf("%s╸ Throttle%s\n", ansiBold, ansiReset)
	for _, ch := range channels {
		if ch.Value > 0 {
			fmt.Printf("  %-40s %s%d%s %s\n", ch.Channel, ansiYellow, ch.Value, ansiReset, ch.Unit)
		}
	}
	fmt.Println()
}

// printCounters prints interrupt/counter channels.
func printCounters(channels []aneperf.Channel) {
	var hasCounters bool
	for _, ch := range channels {
		if ch.Value != 0 {
			hasCounters = true
			break
		}
	}
	if !hasCounters {
		return
	}

	fmt.Printf("%s╸ Counters%s\n", ansiBold, ansiReset)
	for _, ch := range channels {
		if ch.Value != 0 {
			fmt.Printf("  %50s %s%d%s\n", ch.Channel, ansiYellow, ch.Value, ansiReset)
		}
	}
}
