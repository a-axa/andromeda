package score

import (
	"net"
	"testing"

	"andromeda/internal/iface"
	"andromeda/internal/probe"
)

func result(name string, latencyMs, lossPct float64) probe.ProbeResult {
	return probe.ProbeResult{
		Interface:     iface.Interface{Name: name, IP: net.ParseIP("10.0.0.2")},
		LatencyMs:     latencyMs,
		PacketLossPct: lossPct,
	}
}

func names(results []probe.ProbeResult) []string {
	out := make([]string, len(results))
	for i, r := range results {
		out[i] = r.Interface.Name
	}
	return out
}

func TestScore(t *testing.T) {
	tests := []struct {
		name  string
		input probe.ProbeResult
		want  float64
	}{
		{"ideal link", result("a", 0, 0), 100},
		{"half latency cap", result("a", 150, 0), 80},
		{"beyond latency cap", result("a", 900, 0), 60},
		{"zero latency but total loss", result("a", 0, 100), 0},
		{"half packets lost", result("a", 0, 50), 70},
		{"negative values clamp", result("a", -20, -5), 100},
		{"loss above 100 clamps", result("a", 10, 150), 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Score(tc.input); got != tc.want {
				t.Errorf("Score(%+v) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestRank(t *testing.T) {
	results := []probe.ProbeResult{
		result("dead", 0, 100),  // 0 points
		result("slow", 250, 0),  // 66.67 points
		result("fast", 10, 0),   // 98.67 points
		result("flaky", 10, 50), // 68.67 points
	}

	got := names(Rank(results))
	want := []string{"fast", "flaky", "slow", "dead"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ranking = %v, want %v", got, want)
		}
	}
}

func TestRankKeepsInputOrderForTies(t *testing.T) {
	results := []probe.ProbeResult{
		result("first", 20, 0),
		result("second", 20, 0),
		result("third", 20, 0),
	}

	got := names(Rank(results))
	want := []string{"first", "second", "third"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ranking = %v, want %v", got, want)
		}
	}
}

func TestRankDoesNotMutateInput(t *testing.T) {
	results := []probe.ProbeResult{result("slow", 250, 0), result("fast", 10, 0)}

	Rank(results)

	if results[0].Interface.Name != "slow" {
		t.Errorf("input was reordered: %v", names(results))
	}
}

func TestRankEmpty(t *testing.T) {
	if got := Rank(nil); len(got) != 0 {
		t.Errorf("Rank(nil) = %v, want empty", got)
	}
}
