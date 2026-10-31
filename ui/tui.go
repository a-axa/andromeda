// Package ui renders the live interface ranking in the terminal.
package ui

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"andromeda/internal/probe"
	"andromeda/internal/score"
)

// Palette. The panel paints its own dark background, so these foregrounds keep
// their contrast whatever theme the terminal underneath uses.
const (
	bgPanel = "#0d1117"
	bgBest  = "#111e1c" // green at 8% over bgPanel, the mockup's best-row tint

	fgRule = "#21262d"

	fgTitle  = "#e6edf3"
	fgBody   = "#c9d1d9"
	fgMuted  = "#8b949e"
	fgBorder = "#30363d"
	fgGreen  = "#3fb950"
	fgAmber  = "#d29922"
	fgRed    = "#f85149"
	fgBlue   = "#58a6ff"
)

const (
	columnGap  = 2
	scoreBarW  = 16
	panelPadX  = 2
	historyLen = 10
)

// Source measures and ranks every interface, best first. The UI calls it on
// every refresh; wiring lives in main so the UI never touches the network.
type Source func(ctx context.Context) ([]probe.ProbeResult, error)

// Options configure the dashboard.
type Options struct {
	Target   string
	Interval time.Duration
	Source   Source
}

// Model is the bubbletea state: the last ranking plus what the view needs to
// show while a refresh is in flight.
type Model struct {
	opts    Options
	results []probe.ProbeResult
	loading bool
	paused  bool
	err     error
	start   time.Time
	history map[string][]int
}

// New builds a dashboard that starts measuring immediately.
func New(opts Options) Model {
	if opts.Interval <= 0 {
		opts.Interval = 2 * time.Second
	}
	return Model{opts: opts, loading: true, start: time.Now()}
}

type resultMsg struct {
	results []probe.ProbeResult
	err     error
}

type tickMsg time.Time

// Init kicks off the first measurement and the refresh loop.
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.probe(), tick(m.opts.Interval))
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "p":
			m.paused = !m.paused
			// Resume measures right away; the tick chain never stopped, so it is
			// not restarted here or two chains would run side by side.
			if !m.paused && !m.loading {
				m.loading = true
				return m, m.probe()
			}
		case "r":
			if m.loading {
				return m, nil
			}
			m.loading = true
			return m, m.probe()
		}
	case resultMsg:
		m.loading = false
		m.err = msg.err
		if msg.err == nil {
			m.results = msg.results
			m = m.withHistory(msg.results)
		}
	case tickMsg:
		// A slow measurement, or a paused dashboard, keeps the previous ranking
		// on screen instead of stacking a second probe on top of it.
		if m.paused || m.loading {
			return m, tick(m.opts.Interval)
		}
		m.loading = true
		return m, tea.Batch(m.probe(), tick(m.opts.Interval))
	}
	return m, nil
}

// withHistory appends one score sample per interface and keeps the last
// historyLen, so the sparkline shows real measurements only.
func (m Model) withHistory(results []probe.ProbeResult) Model {
	next := make(map[string][]int, len(m.history)+len(results))
	for name, samples := range m.history {
		next[name] = samples
	}
	for _, r := range results {
		samples := append([]int(nil), next[r.Interface.Name]...)
		if len(samples) >= historyLen {
			samples = samples[len(samples)-historyLen+1:]
		}
		next[r.Interface.Name] = append(samples, scorePct(r))
	}
	m.history = next
	return m
}

func (m Model) View() string {
	var body []string
	switch {
	case len(m.results) == 0 && m.err != nil:
		body = m.messageBox("measurement failed: "+m.err.Error(), fgRed)
	case len(m.results) == 0 && m.loading:
		body = m.messageBox("measuring every interface...", fgMuted)
	case len(m.results) == 0:
		body = m.messageBox("no active interfaces", fgMuted)
	default:
		body = m.tableBox(m.cols())
	}

	lineW := lipgloss.Width(body[0])
	lines := []string{
		spaces(lineW, bgPanel),
		m.headLine(lineW),
		m.subtitleLine(lineW),
	}
	if m.err != nil && len(m.results) > 0 {
		lines = append(lines, m.plainLine(paint(fgRed, bgPanel, false, "measurement failed: "+m.err.Error()), lineW))
	}
	lines = append(lines, spaces(lineW, bgPanel))
	lines = append(lines, body...)
	lines = append(lines, spaces(lineW, bgPanel))
	if h := m.historyLine(lineW); h != "" {
		lines = append(lines, h)
	}
	lines = append(lines, m.footerLine(lineW))
	lines = append(lines, spaces(lineW, bgPanel))
	return strings.Join(lines, "\n")
}

// cols sizes each column to its widest cell so the table stays as narrow as the
// real data allows.
type cols struct {
	marker int
	iface  int
	ip     int
	lat    int
	loss   int
	score  int
}

func (c cols) content() int {
	return c.marker + columnGap + c.iface + columnGap + c.ip + columnGap +
		c.lat + columnGap + c.loss + columnGap + c.score
}

func (m Model) cols() cols {
	c := cols{
		marker: 1,
		iface:  len("interface"),
		ip:     len("ip"),
		lat:    len("latency"),
		loss:   len("loss"),
		score:  scoreBarW + 1 + 3,
	}
	for _, r := range m.results {
		c.iface = max(c.iface, lipgloss.Width(r.Interface.Name))
		c.ip = max(c.ip, lipgloss.Width(ipText(r)))
		c.lat = max(c.lat, lipgloss.Width(latencyText(r)))
		c.loss = max(c.loss, lipgloss.Width(lossText(r)))
	}
	// Caps keep a long interface name or a tunnel address from pushing the panel
	// past a standard 80-column terminal.
	c.iface = min(c.iface, 16)
	c.ip = min(c.ip, 15)
	return c
}

func (m Model) tableBox(c cols) []string {
	inner := c.content() + 2
	left := spaces(panelPadX, bgPanel)
	rule := func(l, r string) string {
		return left + paint(fgBorder, bgPanel, false, l+strings.Repeat("─", inner)+r) + left
	}

	out := []string{
		rule("╭", "╮"),
		m.cellsLine(m.headerCells(c), bgPanel, c),
		m.divider(c),
	}
	for i, r := range m.results {
		bg := bgPanel
		if i == 0 {
			bg = bgBest
		}
		out = append(out, m.cellsLine(m.rowCells(r, i == 0, c), bg, c))
		if i < len(m.results)-1 {
			out = append(out, m.divider(c))
		}
	}
	return append(out, rule("╰", "╯"))
}

func (m Model) cellsLine(cells, bg string, c cols) string {
	edge := paint(fgBorder, bgPanel, false, "│")
	fill := c.content() + 2 - 2 - lipgloss.Width(cells)
	if fill < 0 {
		fill = 0
	}
	body := spaces(1, bg) + cells + spaces(1+fill, bg)
	return spaces(panelPadX, bgPanel) + edge + body + edge + spaces(panelPadX, bgPanel)
}

func (m Model) divider(c cols) string {
	return spaces(panelPadX, bgPanel) +
		paint(fgRule, bgPanel, false, "├"+strings.Repeat("─", c.content()+2)+"┤") +
		spaces(panelPadX, bgPanel)
}

func (m Model) headerCells(c cols) string {
	cells := []string{
		spaces(c.marker, bgPanel),
		paint(fgMuted, bgPanel, false, pad("interface", c.iface)),
		paint(fgMuted, bgPanel, false, pad("ip", c.ip)),
		paint(fgMuted, bgPanel, false, padLeft("latency", c.lat)),
		paint(fgMuted, bgPanel, false, padLeft("loss", c.loss)),
		paint(fgMuted, bgPanel, false, pad("score", c.score)),
	}
	return strings.Join(cells, spaces(columnGap, bgPanel))
}

func (m Model) rowCells(r probe.ProbeResult, best bool, c cols) string {
	bg := bgPanel
	if best {
		bg = bgBest
	}
	dead := r.PacketLossPct >= 100

	marker, markerFg := " ", fgMuted
	nameFg := fgTitle
	if best {
		marker, markerFg = "▶", fgGreen
	}
	if dead {
		nameFg = fgMuted
	}
	valFg := statusColor(r)

	cells := []string{
		paint(markerFg, bg, false, pad(marker, c.marker)),
		paint(nameFg, bg, false, pad(truncate(r.Interface.Name, c.iface), c.iface)),
		paint(fgMuted, bg, false, pad(ipText(r), c.ip)),
		paint(valFg, bg, false, padLeft(latencyText(r), c.lat)),
		paint(valFg, bg, false, padLeft(lossText(r), c.loss)),
		m.scoreCell(r, bg),
	}
	return strings.Join(cells, spaces(columnGap, bg))
}

// scoreCell is an 18-char meter (bar plus number) so the eye can rank rows
// without reading digits.
func (m Model) scoreCell(r probe.ProbeResult, bg string) string {
	pct := scorePct(r)
	barFg := statusColor(r)
	numFg := fgTitle
	if r.PacketLossPct >= 100 {
		barFg = fgBorder
		numFg = fgMuted
	}
	return paint(barFg, bg, false, scoreBar(pct)) +
		spaces(1, bg) +
		paint(numFg, bg, false, fmt.Sprintf("%3d", pct))
}

func (m Model) headLine(lineW int) string {
	left := paint(fgTitle, bgPanel, true, "ANDROMEDA")
	right := m.headerStatus()
	n := lineW - 2*panelPadX - lipgloss.Width(left) - lipgloss.Width(right)
	if n < 1 {
		n = 1
	}
	return spaces(panelPadX, bgPanel) + left + spaces(n, bgPanel) + right + spaces(panelPadX, bgPanel)
}

func (m Model) headerStatus() string {
	dot, label, dotFg := "●", "live", fgGreen
	switch {
	case m.err != nil:
		dot, label, dotFg = "●", "error", fgRed
	case m.paused:
		dot, label, dotFg = "◼", "paused", fgMuted
	case m.loading:
		dot, label, dotFg = "◐", "probing...", fgBlue
	}

	var b strings.Builder
	if m.opts.Target != "" {
		b.WriteString(paint(fgMuted, bgPanel, false, "target "+m.opts.Target))
		b.WriteString(paint(fgMuted, bgPanel, false, "  •  "))
	}
	b.WriteString(paint(dotFg, bgPanel, false, dot+" "))
	b.WriteString(paint(fgMuted, bgPanel, false, label))
	return b.String()
}

func (m Model) subtitleLine(lineW int) string {
	noun := "interfaces"
	if len(m.results) == 1 {
		noun = "interface"
	}
	text := fmt.Sprintf("%d %s  •  uptime %s", len(m.results), noun, formatUptime(time.Since(m.start)))
	return m.plainLine(paint(fgMuted, bgPanel, false, text), lineW)
}

// historyLine only renders when an interface has at least two samples, so the
// sparkline never shows a made-up trend.
func (m Model) historyLine(lineW int) string {
	const prefix = "history  "
	avail := lineW - 2*panelPadX
	parts := []string{paint(fgMuted, bgPanel, false, prefix)}
	used := lipgloss.Width(prefix)

	for _, r := range m.results {
		samples := m.history[r.Interface.Name]
		if len(samples) < 2 {
			continue
		}
		sepW := 0
		if len(parts) > 1 {
			sepW = 4
		}
		seg := paint(fgMuted, bgPanel, false, r.Interface.Name) +
			spaces(2, bgPanel) +
			paint(statusColor(r), bgPanel, false, sparkline(samples))
		segW := lipgloss.Width(r.Interface.Name) + 2 + len(samples)
		if used+sepW+segW > avail {
			break
		}
		if sepW > 0 {
			parts = append(parts, spaces(sepW, bgPanel))
		}
		parts = append(parts, seg)
		used += sepW + segW
	}
	if len(parts) == 1 {
		return ""
	}
	return m.plainLine(strings.Join(parts, ""), lineW)
}

func (m Model) footerLine(lineW int) string {
	pause := "pause"
	if m.paused {
		pause = "resume"
	}
	content := strings.Join([]string{
		helpKey("q", "quit"),
		helpKey("p", pause),
		helpKey("r", "refresh"),
	}, spaces(3, bgPanel))
	return m.plainLine(content, lineW)
}

func (m Model) plainLine(content string, lineW int) string {
	tail := lineW - panelPadX - lipgloss.Width(content)
	if tail < 0 {
		tail = 0
	}
	return spaces(panelPadX, bgPanel) + content + spaces(tail, bgPanel)
}

func (m Model) messageBox(text, fg string) []string {
	inner := max(56, lipgloss.Width(text)+4)
	left := spaces(panelPadX, bgPanel)
	rule := func(l, r string) string {
		return left + paint(fgBorder, bgPanel, false, l+strings.Repeat("─", inner)+r) + left
	}
	edge := paint(fgBorder, bgPanel, false, "│")
	body := spaces(2, bgPanel) + paint(fg, bgPanel, false, text) +
		spaces(inner-2-lipgloss.Width(text), bgPanel)
	return []string{
		rule("╭", "╮"),
		left + edge + body + edge + left,
		rule("╰", "╯"),
	}
}

// Run starts the dashboard and blocks until the user quits.
func Run(opts Options) error {
	_, err := tea.NewProgram(New(opts), tea.WithAltScreen()).Run()
	return err
}

func tick(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg { return tickMsg(t) })
}

var errNoSource = errors.New("ui: no source configured")

func (m Model) probe() tea.Cmd {
	src := m.opts.Source
	return func() tea.Msg {
		if src == nil {
			return resultMsg{err: errNoSource}
		}
		results, err := src(context.Background())
		return resultMsg{results: results, err: err}
	}
}

func scorePct(r probe.ProbeResult) int {
	return int(math.Round(score.Score(r)))
}

func statusColor(r probe.ProbeResult) string {
	if r.PacketLossPct >= 100 {
		return fgRed
	}
	switch p := scorePct(r); {
	case p >= 80:
		return fgGreen
	case p >= 50:
		return fgAmber
	default:
		return fgRed
	}
}

func scoreBar(pct int) string {
	pct = clampInt(pct, 0, 100)
	filled := int(math.Round(float64(pct) / 100 * scoreBarW))
	return strings.Repeat("█", filled) + strings.Repeat("░", scoreBarW-filled)
}

var sparkRunes = []rune("▁▂▃▄▅▆▇█")

func sparkline(samples []int) string {
	var b strings.Builder
	for _, v := range samples {
		v = clampInt(v, 0, 100)
		idx := int(math.Round(float64(v) / 100 * float64(len(sparkRunes)-1)))
		b.WriteRune(sparkRunes[idx])
	}
	return b.String()
}

// ipText prints a dash for an interface with no usable address, matching the
// latency dash so a dead link never looks measured.
func ipText(r probe.ProbeResult) string {
	if r.Interface.IP == nil {
		return "-"
	}
	return r.Interface.IP.String()
}

// latencyText prints a dash instead of 0ms for a link that never answered: zero
// latency with 100% loss would read as the fastest link on screen.
func latencyText(r probe.ProbeResult) string {
	if r.PacketLossPct >= 100 {
		return "-"
	}
	return fmt.Sprintf("%.0f ms", r.LatencyMs)
}

func lossText(r probe.ProbeResult) string {
	return fmt.Sprintf("%.0f%%", r.PacketLossPct)
}

func formatUptime(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Truncate(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}

// paint styles plain text. Every segment carries the background it sits on, so
// a row keeps one continuous tint after the ANSI reset closes the previous cell.
func paint(fg, bg string, bold bool, s string) string {
	st := lipgloss.NewStyle().Background(lipgloss.Color(bg))
	if fg != "" {
		st = st.Foreground(lipgloss.Color(fg))
	}
	if bold {
		st = st.Bold(true)
	}
	return st.Render(s)
}

func spaces(n int, bg string) string {
	if n <= 0 {
		return ""
	}
	return paint("", bg, false, strings.Repeat(" ", n))
}

func helpKey(key, label string) string {
	return paint(fgBody, bgPanel, false, "["+key+"]") + paint(fgMuted, bgPanel, false, " "+label)
}

func pad(s string, w int) string {
	if n := w - lipgloss.Width(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}

func padLeft(s string, w int) string {
	if n := w - lipgloss.Width(s); n > 0 {
		return strings.Repeat(" ", n) + s
	}
	return s
}

func truncate(s string, w int) string {
	if lipgloss.Width(s) <= w {
		return s
	}
	r := []rune(s)
	for len(r) > 0 && lipgloss.Width(string(r)) > w {
		r = r[:len(r)-1]
	}
	return string(r)
}

func clampInt(v, lo, hi int) int {
	return min(max(v, lo), hi)
}
