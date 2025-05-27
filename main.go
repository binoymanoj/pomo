package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	flag "github.com/spf13/pflag"
)

const (
	StateInput = iota
	StateTimer
	StateBreak
	StateComplete
)

type tickMsg time.Time

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

type model struct {
	state          int
	timerDuration  time.Duration
	breakDuration  time.Duration
	sessions       int
	currentSession int

	// Input fields
	timerInput   textinput.Model
	breakInput   textinput.Model
	sessionInput textinput.Model
	activeInput  int

	// Timer
	progress      progress.Model
	startTime     time.Time
	totalTime     time.Duration
	remaining     time.Duration
	isBreak       bool
	isPaused      bool
	pausedTime    time.Duration
	lastPauseTime time.Time

	// Styling
	width  int
	height int
}

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FF6B6B")).
			MarginTop(1).
			MarginBottom(1)

	inputStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#4ECDC4")).
			Padding(0, 1).
			MarginBottom(1)

	activeInputStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("#FF6B6B")).
				Padding(0, 1).
				MarginBottom(1)

	timerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFE66D")).
			Align(lipgloss.Center).
			MarginTop(2).
			MarginBottom(2)

	pausedStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFA07A")).
			Align(lipgloss.Center).
			MarginTop(2).
			MarginBottom(2)

	sessionStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#95E1D3")).
			Align(lipgloss.Center).
			MarginBottom(1)

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#A8E6CF")).
			MarginTop(2)
)

func initialModel() model {
	// Initialize input fields
	timerInput := textinput.New()
	timerInput.Placeholder = "25m"
	timerInput.Focus()
	timerInput.CharLimit = 10
	timerInput.Width = 20

	breakInput := textinput.New()
	breakInput.Placeholder = "5m"
	breakInput.CharLimit = 10
	breakInput.Width = 20

	sessionInput := textinput.New()
	sessionInput.Placeholder = "4"
	sessionInput.CharLimit = 2
	sessionInput.Width = 20

	// Initialize progress bar
	prog := progress.New(progress.WithDefaultGradient())
	prog.Width = 50

	return model{
		state:        StateInput,
		timerInput:   timerInput,
		breakInput:   breakInput,
		sessionInput: sessionInput,
		progress:     prog,
		activeInput:  0,
	}
}

func (m model) Init() tea.Cmd {
	if m.state == StateTimer {
		return tick()
	}
	return textinput.Blink
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch m.state {
		case StateInput:
			return m.updateInput(msg)
		case StateTimer, StateBreak:
			return m.updateTimer(msg)
		case StateComplete:
			if msg.String() == "q" || msg.String() == "ctrl+c" {
				return m, tea.Quit
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.progress.Width = min(50, m.width-10)
		return m, nil

	case tickMsg:
		if (m.state == StateTimer || m.state == StateBreak) && !m.isPaused {
			return m.updateTick()
		}
	}

	return m, cmd
}

func (m model) updateInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "tab", "down":
		m.activeInput = (m.activeInput + 1) % 3
		m.updateInputFocus()
	case "shift+tab", "up":
		m.activeInput = (m.activeInput - 1 + 3) % 3
		m.updateInputFocus()
	case "enter":
		if m.validateAndSetValues() {
			m.state = StateTimer
			m.startTimer()
			return m, tick()
		}
	}

	// Update the active input
	switch m.activeInput {
	case 0:
		m.timerInput, cmd = m.timerInput.Update(msg)
	case 1:
		m.breakInput, cmd = m.breakInput.Update(msg)
	case 2:
		m.sessionInput, cmd = m.sessionInput.Update(msg)
	}

	return m, cmd
}

func (m model) updateTimer(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "p", " ":
		// Toggle pause/resume
		if m.isPaused {
			// Resume
			m.isPaused = false
			// Adjust start time to account for paused duration
			pauseDuration := time.Since(m.lastPauseTime)
			m.pausedTime += pauseDuration
			return m, tick()
		} else {
			// Pause
			m.isPaused = true
			m.lastPauseTime = time.Now()
			return m, nil
		}
	}
	return m, nil
}

func (m model) updateTick() (tea.Model, tea.Cmd) {
	elapsed := time.Since(m.startTime) - m.pausedTime
	m.remaining = m.totalTime - elapsed

	if m.remaining <= 0 {
		// Timer completed
		m.sendNotification()
		if m.isBreak {
			// Break completed, start next session or finish
			m.currentSession++
			if m.currentSession >= m.sessions {
				m.state = StateComplete
				return m, nil
			} else {
				m.state = StateTimer
				m.startTimer()
			}
		} else {
			// Work session completed, start break
			m.state = StateBreak
			m.startBreak()
		}
		return m, tick()
	}

	return m, tick()
}

func (m *model) updateInputFocus() {
	m.timerInput.Blur()
	m.breakInput.Blur()
	m.sessionInput.Blur()

	switch m.activeInput {
	case 0:
		m.timerInput.Focus()
	case 1:
		m.breakInput.Focus()
	case 2:
		m.sessionInput.Focus()
	}
}

func (m *model) validateAndSetValues() bool {
	// Parse timer duration
	timerStr := m.timerInput.Value()
	if timerStr == "" {
		timerStr = "25m"
	}
	timer, err := parseDuration(timerStr)
	if err != nil {
		return false
	}

	// Parse break duration
	breakStr := m.breakInput.Value()
	if breakStr == "" {
		breakStr = "5m"
	}
	breakDur, err := parseDuration(breakStr)
	if err != nil {
		return false
	}

	// Parse sessions
	sessionStr := m.sessionInput.Value()
	if sessionStr == "" {
		sessionStr = "4"
	}
	sessions, err := strconv.Atoi(sessionStr)
	if err != nil || sessions <= 0 {
		return false
	}

	m.timerDuration = timer
	m.breakDuration = breakDur
	m.sessions = sessions
	m.currentSession = 0

	return true
}

func (m *model) startTimer() {
	m.isBreak = false
	m.isPaused = false
	m.pausedTime = 0
	m.totalTime = m.timerDuration
	m.startTime = time.Now()
	m.remaining = m.totalTime
}

func (m *model) startBreak() {
	m.isBreak = true
	m.isPaused = false
	m.pausedTime = 0
	m.totalTime = m.breakDuration
	m.startTime = time.Now()
	m.remaining = m.totalTime
}

func (m *model) sendNotification() {
	var title, message string
	if m.isBreak {
		title = "Break Complete!"
		if m.currentSession+1 >= m.sessions {
			message = "All sessions completed! Great work! 🎉"
		} else {
			message = fmt.Sprintf("Break over! Starting session %d/%d", m.currentSession+2, m.sessions)
		}
	} else {
		title = "Work Session Complete!"
		message = fmt.Sprintf("Session %d/%d done! Time for a break! ☕", m.currentSession+1, m.sessions)
	}

	// Send notification with sound
	cmd := exec.Command("notify-send", "-u", "normal", "-t", "5000", title, message)
	cmd.Run()

	// Play notification sound (using paplay if available)
	soundCmd := exec.Command("paplay", "/usr/share/sounds/alsa/Front_Left.wav")
	soundCmd.Run()
}

func (m model) View() string {
	switch m.state {
	case StateInput:
		return m.viewInput()
	case StateTimer, StateBreak:
		return m.viewTimer()
	case StateComplete:
		return m.viewComplete()
	}
	return ""
}

func (m model) viewInput() string {
	title := titleStyle.Render("🍅 Pomodoro Timer Setup")

	var inputs []string

	// Timer input
	style := inputStyle
	if m.activeInput == 0 {
		style = activeInputStyle
	}
	inputs = append(inputs, style.Render(fmt.Sprintf("Work Duration: %s", m.timerInput.View())))

	// Break input
	style = inputStyle
	if m.activeInput == 1 {
		style = activeInputStyle
	}
	inputs = append(inputs, style.Render(fmt.Sprintf("Break Duration: %s", m.breakInput.View())))

	// Session input
	style = inputStyle
	if m.activeInput == 2 {
		style = activeInputStyle
	}
	inputs = append(inputs, style.Render(fmt.Sprintf("Sessions: %s", m.sessionInput.View())))

	help := helpStyle.Render("Tab/↑↓: Navigate • Enter: Start • Ctrl+C: Quit\nFormat: 25m, 1h30m, 90s")

	return lipgloss.JoinVertical(lipgloss.Left,
		title,
		"",
		strings.Join(inputs, "\n"),
		"",
		help,
	)
}

func (m model) viewTimer() string {
	var title string
	var emoji string
	if m.isBreak {
		title = "☕ Break Time"
		emoji = "🛋️"
	} else {
		title = "🍅 Focus Time"
		emoji = "💪"
	}

	titleText := titleStyle.Render(title)

	// Session counter
	sessionText := sessionStyle.Render(fmt.Sprintf("Session %d of %d", m.currentSession+1, m.sessions))

	// Progress calculation
	elapsed := m.totalTime - m.remaining
	progressPercent := float64(elapsed) / float64(m.totalTime)
	if progressPercent > 1 {
		progressPercent = 1
	}

	// Progress bar
	progressBar := m.progress.ViewAs(progressPercent)

	// Time display with pause status
	var timeText string
	if m.isPaused {
		timeText = pausedStyle.Render(fmt.Sprintf("⏸️ PAUSED - %s remaining", formatDuration(m.remaining)))
	} else {
		timeText = timerStyle.Render(fmt.Sprintf("%s %s remaining", emoji, formatDuration(m.remaining)))
	}

	// Help text
	help := helpStyle.Render("Space/P: Pause/Resume • Q: Quit")

	return lipgloss.JoinVertical(lipgloss.Center,
		titleText,
		sessionText,
		"",
		progressBar,
		timeText,
		"",
		help,
	)
}

func (m model) viewComplete() string {
	title := titleStyle.Render("🎉 Pomodoro Session Complete!")

	stats := fmt.Sprintf("Completed %d sessions\nTotal focus time: %s\nTotal break time: %s",
		m.sessions,
		formatDuration(m.timerDuration*time.Duration(m.sessions)),
		formatDuration(m.breakDuration*time.Duration(m.sessions-1)),
	)

	statsText := timerStyle.Render(stats)
	help := helpStyle.Render("Q: Quit")

	return lipgloss.JoinVertical(lipgloss.Center,
		title,
		"",
		statsText,
		"",
		help,
	)
}

func parseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty duration")
	}

	// Handle just numbers (assume minutes)
	if num, err := strconv.Atoi(s); err == nil {
		return time.Duration(num) * time.Minute, nil
	}

	return time.ParseDuration(s)
}

func formatDuration(d time.Duration) string {
	if d < 0 {
		return "0s"
	}

	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60

	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, seconds)
	}
	return fmt.Sprintf("%02d:%02d", minutes, seconds)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func showHelp() {
	help := `🍅 Pomodoro Timer

Usage:
  pomo                           Interactive mode
  pomo -t <duration> -b <duration> [-s <sessions>]

Flags:
  -t, --timer <duration>     Work session duration (e.g., 25m, 1h30m, 45s)
  -b, --break <duration>     Break duration (e.g., 5m, 10m)
  -s, --sessions <number>    Number of sessions (default: 4)
  -h, --help                Show this help

Examples:
  pomo                       # Interactive mode
  pomo -t 25m -b 5m         # 25min work, 5min break, 4 sessions
  pomo -t 1h -b 10m -s 2    # 1hour work, 10min break, 2 sessions
  pomo -t 45m -b 15m -s 6   # 45min work, 15min break, 6 sessions

Duration formats:
  - Minutes: 25m, 30m
  - Hours: 1h, 1h30m
  - Seconds: 90s, 300s
  - Just numbers default to minutes: 25 = 25m

Controls:
  - Tab/Arrow keys: Navigate inputs
  - Enter: Start timer
  - Space/P: Pause/Resume (during timer)
  - Q/Ctrl+C: Quit
`
	fmt.Print(help)
}

func main() {
	var timerFlag, breakFlag string
	var sessionsFlag int
	var helpFlag bool

	flag.StringVarP(&timerFlag, "timer", "t", "", "Work session duration")
	flag.StringVarP(&breakFlag, "break", "b", "", "Break duration")
	flag.IntVarP(&sessionsFlag, "sessions", "s", 4, "Number of sessions")
	flag.BoolVarP(&helpFlag, "help", "h", false, "Show help")

	flag.Parse()

	if helpFlag {
		showHelp()
		return
	}

	m := initialModel()

	if timerFlag != "" && breakFlag != "" {
		timer, err := parseDuration(timerFlag)
		if err != nil {
			fmt.Printf("Error parsing timer duration: %v\n", err)
			os.Exit(1)
		}

		breakDur, err := parseDuration(breakFlag)
		if err != nil {
			fmt.Printf("Error parsing break duration: %v\n", err)
			os.Exit(1)
		}

		m.timerDuration = timer
		m.breakDuration = breakDur
		m.sessions = sessionsFlag
		m.currentSession = 0
		m.state = StateTimer
		m.startTimer()
	}

	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error running program: %v", err)
		os.Exit(1)
	}
}
