// Package score ranks probe results by combined latency and packet loss.
package score

import (
	"math"
	"slices"

	"andromeda/internal/probe"
)

const (
	// LossWeight and LatencyWeight split the full 100 points. Loss carries
	// more weight: a link that drops packets is worse than a slower link that
	// still delivers every packet.
	LossWeight    = 0.6
	LatencyWeight = 0.4

	// LatencyCapMs is where the latency component hits zero. Anything slower
	// is treated the same.
	LatencyCapMs = 300.0
)

// Score rates one result from 0 (unusable) to 100 (ideal). Higher is better.
func Score(r probe.ProbeResult) float64 {
	loss := clamp(r.PacketLossPct, 0, 100)
	if loss >= 100 {
		// probe.One reports 0ms for a link that answered nothing. Without this
		// guard a dead interface would collect every latency point and rank as
		// the fastest link in the list.
		return 0
	}

	latency := clamp(r.LatencyMs, 0, LatencyCapMs)
	lossPoints := LossWeight * (1 - loss/100)
	latencyPoints := LatencyWeight * (1 - latency/LatencyCapMs)
	return 100 * (lossPoints + latencyPoints)
}

// Rank returns the results best first. The input is left untouched, and equal
// scores keep their original order.
func Rank(results []probe.ProbeResult) []probe.ProbeResult {
	ranked := slices.Clone(results)
	slices.SortStableFunc(ranked, func(a, b probe.ProbeResult) int {
		switch sa, sb := Score(a), Score(b); {
		case sa > sb:
			return -1
		case sa < sb:
			return 1
		default:
			return 0
		}
	})
	return ranked
}

func clamp(v, lo, hi float64) float64 {
	return math.Min(math.Max(v, lo), hi)
}
