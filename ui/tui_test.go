package ui

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"andromeda/internal/iface"
	"andromeda/internal/probe"
)

func dummyResults() []probe.ProbeResult {
	return []probe.ProbeResult{
		{
			Interface:     iface.Interface{Name: "wlp1s0", IP: net.ParseIP("10.56.75.30")},
			LatencyMs:     12,
			PacketLossPct: 0,
		},
		{
			Interface:     iface.Interface{Name: "tailscale0", IP: net.ParseIP("100.75.212.0")},
			LatencyMs:     48,
			PacketLossPct: 25,
		},
		{
			Interface:     iface.Interface{Name: "usb0", IP: net.ParseIP("192.168.42.2")},
			LatencyMs:     0,
			PacketLossPct: 100,
		},
	}
}

func readyModel() Model {
	m := New(Options{
		Target:   "8.8.8.8",
		Interval: time.Second,
		Source:   func(context.Context) ([]probe.ProbeResult, error) { return dummyResults(), nil },
	})
	m.loading = false
	m.results = dummyResults()
	return m
}

// trimRight drops trailing spaces so the golden literal does not depend on
// invisible whitespace at the end of each line.
func trimRight(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}
	return strings.Join(lines, "\n")
}

func TestViewRendersRankingInOrder(t *testing.T) {
	view := readyModel().View()

	last := -1
	for _, want := range []string{"wlp1s0", "tailscale0", "usb0"} {
		idx := strings.Index(view, want)
		if idx < 0 {
			t.Fatalf("view is missing %q:\n%s", want, view)
		}
		if idx < last {
			t.Fatalf("%q appears out of ranking order:\n%s", want, view)
		}
		last = idx
	}

	for _, want := range []string{"target 8.8.8.8", "12 ms", "25%", "100%", "[q] quit", "[p] pause", "[r] refresh"} {
		if !strings.Contains(strings.ToUpper(view), strings.ToUpper(want)) {
			t.Errorf("view is missing %q:\n%s", want, view)
		}
	}
}

func TestViewPrintsDashForDeadLink(t *testing.T) {
	view := readyModel().View()
	usb := view[strings.Index(view, "usb0"):]

	if strings.Contains(usb, "0 ms") {
		t.Errorf("dead link shows 0 ms instead of a dash:\n%s", view)
	}
}

func TestViewMarksBestRow(t *testing.T) {
	view := readyModel().View()

	var best string
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "wlp1s0") {
			best = line
		}
	}
	if best == "" {
		t.Fatalf("best row not found:\n%s", view)
	}
	if !strings.Contains(best, "▶") {
		t.Errorf("best row has no marker: %q", best)
	}
}

func TestViewLoadingState(t *testing.T) {
	view := New(Options{Interval: time.Second}).View()
	for _, want := range []string{"measuring every interface", "probing..."} {
		if !strings.Contains(view, want) {
			t.Errorf("loading view missing %q:\n%s", want, view)
		}
	}
}

func TestViewEmptyState(t *testing.T) {
	m := New(Options{Interval: time.Second})
	m.loading = false

	if view := m.View(); !strings.Contains(view, "no active interfaces") {
		t.Errorf("empty view missing explanation:\n%s", view)
	}
}

func TestViewErrorState(t *testing.T) {
	m := New(Options{Interval: time.Second})
	m.loading = false
	m.err = errors.New("raw socket not permitted")

	if view := m.View(); !strings.Contains(view, "raw socket not permitted") {
		t.Errorf("error view missing the cause:\n%s", view)
	}
}

func TestViewKeepsRankingWhenRefreshFails(t *testing.T) {
	m := readyModel()
	m.err = errors.New("timeout")

	view := m.View()
	if !strings.Contains(view, "timeout") {
		t.Error("error text missing")
	}
	if !strings.Contains(view, "wlp1s0") {
		t.Error("last ranking dropped after a failed refresh")
	}
}

func TestViewHidesHistoryUntilTwoSamples(t *testing.T) {
	m := readyModel()
	m = m.withHistory(dummyResults())

	if strings.Contains(m.View(), "history") {
		t.Error("history line rendered from a single sample")
	}

	m = m.withHistory(dummyResults())
	if !strings.Contains(m.View(), "history") {
		t.Error("history line missing once two samples exist")
	}
}

func TestUpdateStoresResultsAndHistory(t *testing.T) {
	m := New(Options{Interval: time.Second})
	next, _ := m.Update(resultMsg{results: dummyResults()})

	got := next.(Model)
	if got.loading {
		t.Error("loading should stop once a result arrives")
	}
	if len(got.results) != 3 {
		t.Errorf("stored %d results, want 3", len(got.results))
	}
	if len(got.history) != 3 {
		t.Errorf("recorded history for %d interfaces, want 3", len(got.history))
	}
}

func TestUpdateKeepsPreviousRankingOnError(t *testing.T) {
	m := readyModel()
	next, _ := m.Update(resultMsg{err: errors.New("timeout")})

	got := next.(Model)
	if got.err == nil {
		t.Error("error was not stored")
	}
	if len(got.results) != 3 {
		t.Errorf("previous ranking dropped on a failed refresh: %d results", len(got.results))
	}
}

func TestHistoryKeepsOnlyRecentSamples(t *testing.T) {
	m := New(Options{Interval: time.Second})
	for range historyLen + 5 {
		m = m.withHistory(dummyResults())
	}

	if got := len(m.history["wlp1s0"]); got != historyLen {
		t.Errorf("kept %d samples, want %d", got, historyLen)
	}
	if got := m.history["wlp1s0"][historyLen-1]; got != scorePct(dummyResults()[0]) {
		t.Errorf("newest sample is %d, want the latest score", got)
	}
}

func TestTickWhileLoadingDoesNotStartSecondProbe(t *testing.T) {
	m := New(Options{Interval: time.Millisecond})
	m.loading = true

	_, cmd := m.Update(tickMsg(time.Now()))
	if cmd == nil {
		t.Fatal("tick should schedule the next tick")
	}
	if _, isResult := cmd().(resultMsg); isResult {
		t.Error("tick started a probe while one was still running")
	}
}

func TestTickWhilePausedDoesNotProbe(t *testing.T) {
	m := readyModel()
	m.paused = true

	_, cmd := m.Update(tickMsg(time.Now()))
	if cmd == nil {
		t.Fatal("paused tick should still schedule the next tick")
	}
	if _, isResult := cmd().(resultMsg); isResult {
		t.Error("paused dashboard started a probe")
	}
}

func TestPauseKeyPausesAndResumes(t *testing.T) {
	m := readyModel()
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})

	paused := next.(Model)
	if !paused.paused {
		t.Fatal("p did not pause")
	}
	if !strings.Contains(paused.View(), "[p] resume") {
		t.Error("footer does not offer resume while paused")
	}
	if paused.loading {
		t.Error("pausing should not start a probe")
	}

	next, cmd := paused.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	resumed := next.(Model)
	if resumed.paused {
		t.Fatal("p did not resume")
	}
	if cmd == nil {
		t.Fatal("resuming should probe immediately")
	}
	if _, ok := cmd().(resultMsg); !ok {
		t.Error("resuming did not start a probe")
	}
}

func TestRefreshKeyProbesImmediately(t *testing.T) {
	m := readyModel()
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})

	if !next.(Model).loading {
		t.Error("r should mark the dashboard as loading")
	}
	if cmd == nil {
		t.Fatal("r should probe")
	}
	if _, ok := cmd().(resultMsg); !ok {
		t.Error("r did not start a probe")
	}

	loading := next.(Model)
	if _, cmd := loading.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")}); cmd != nil {
		t.Error("r stacked a second probe while one was running")
	}
}

func TestScoreBarFill(t *testing.T) {
	for _, tc := range []struct {
		pct    int
		filled int
	}{
		{pct: -10, filled: 0},
		{pct: 0, filled: 0},
		{pct: 50, filled: 8},
		{pct: 100, filled: scoreBarW},
		{pct: 140, filled: scoreBarW},
	} {
		bar := scoreBar(tc.pct)
		if got := strings.Count(bar, "█"); got != tc.filled {
			t.Errorf("scoreBar(%d) filled %d cells, want %d", tc.pct, got, tc.filled)
		}
		if got := len([]rune(bar)); got != scoreBarW {
			t.Errorf("scoreBar(%d) is %d wide, want %d", tc.pct, got, scoreBarW)
		}
	}
}

func TestSparkline(t *testing.T) {
	if got, want := sparkline([]int{0, 50, 100}), "▁▅█"; got != want {
		t.Errorf("sparkline = %q, want %q", got, want)
	}
}

func TestFormatUptime(t *testing.T) {
	for _, tc := range []struct {
		in   time.Duration
		want string
	}{
		{in: -time.Second, want: "00:00"},
		{in: 134 * time.Second, want: "02:14"},
		{in: time.Hour + time.Minute + time.Second, want: "1:01:01"},
	} {
		if got := formatUptime(tc.in); got != tc.want {
			t.Errorf("formatUptime(%s) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestViewGolden(t *testing.T) {
	// Locks the layout so alignment or column changes show up as a failing
	// diff. Update the literal when the design changes on purpose.
	m := readyModel()
	m.start = time.Now().Add(-134 * time.Second)
	m.history = map[string][]int{
		"wlp1s0":     {96, 98, 97, 95, 98, 99, 98, 97, 98, 98},
		"tailscale0": {70, 82, 76, 90, 84, 80, 86, 82, 74, 79},
	}

	want := `
  ANDROMEDA                                  target 8.8.8.8  •  ● live
  3 interfaces  •  uptime 02:14

  ╭──────────────────────────────────────────────────────────────────╮
  │    interface   ip            latency  loss  score                │
  ├──────────────────────────────────────────────────────────────────┤
  │ ▶  wlp1s0      10.56.75.30     12 ms    0%  ████████████████  98 │
  ├──────────────────────────────────────────────────────────────────┤
  │    tailscale0  100.75.212.0    48 ms   25%  █████████████░░░  79 │
  ├──────────────────────────────────────────────────────────────────┤
  │    usb0        192.168.42.2        -  100%  ░░░░░░░░░░░░░░░░   0 │
  ╰──────────────────────────────────────────────────────────────────╯

  history  wlp1s0  ██████████    tailscale0  ▆▇▆▇▇▇▇▇▆▇
  [q] quit   [p] pause   [r] refresh
`

	if got := trimRight(m.View()); got != want {
		t.Errorf("view changed:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestQuitKey(t *testing.T) {
	m := readyModel()

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Fatal("q should return a command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("q did not quit")
	}
}
