package log_drivers

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"go.uber.org/zap/zapcore"
)

type Message struct {
	Text string
}

type Log struct {
	Text  string
	Level string
	Lock  bool
}

type CliLogDriver struct {
	view
	program tea.Program
}

type view struct {
	Viewport  viewport.Model
	Spinner   spinner.Model
	lines     []string
	logs      []string
	lockedLog string
}

var maxLines = 15

//This is an ugly hack. I am a bad programmer for this, but bubbletea is weird
var Exit chan os.Signal

func newView() *view {

	Exit = make(chan os.Signal, 1)
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("62"))

	const width = 150
	vp := viewport.New(width, 17)
	vp.Style = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		PaddingRight(2)

	return &view{
		Viewport: vp,
		Spinner:  s,
		lines:    make([]string, maxLines),
	}
}

func (e view) Init() tea.Cmd {
	return e.Spinner.Tick
}

func (e view) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			Exit <- os.Interrupt
			return e, tea.Quit
		}
		var cmd tea.Cmd
		e.Viewport, cmd = e.Viewport.Update(msg)
		return e, cmd
	case Message:

		e.lines = append(e.lines, msg.Text)
		if len(e.lines) > maxLines {
			e.lines = e.lines[1:]
		}

		e.Viewport.SetContent(strings.Join(e.lines, "\n"))
		return e, nil
	case Log:
		e.logs = append(e.logs, msg.Text)
		if len(e.logs) > maxLines {
			e.logs = e.logs[1:]
		}
		if msg.Lock {
			e.lockedLog = msg.Text
		}
		return e, nil
	default:
		var cmd tea.Cmd
		e.Spinner, cmd = e.Spinner.Update(msg)
		return e, cmd
	}
}

func (e view) View() string {
	if len(e.lines) > 0 {
		var logLine string

		if e.lockedLog != "" {
			logLine = e.lockedLog
		} else if len(e.logs) > 0 {
			logLine = e.logs[len(e.logs)-1]
		}

		return fmt.Sprintf(
			"%s %s\n\n%s",
			e.Spinner.View(),
			logLine,
			e.Viewport.View(),
		) + "\n\n"
	} else {
		return fmt.Sprintf(
			"%s\n\n%s",
			strings.Join(e.logs, "\n"),
			e.Viewport.View(),
		) + "\n\n"

	}
}

func NewCliLogDriver() *CliLogDriver {
	model := newView()

	p := tea.NewProgram(model, tea.WithoutSignals())

	go p.Run()

	return &CliLogDriver{
		view:    *model,
		program: *p,
	}
}

func (s *CliLogDriver) Info(msg string, fields ...zapcore.Field) {
	s.program.Send(Log{Text: msg})
}

func (s *CliLogDriver) Error(msg string, fields ...zapcore.Field) {
	s.program.Send(Log{Text: msg})
}

func (s *CliLogDriver) Debug(msg string, fields ...zapcore.Field) {
	s.program.Send(Log{Text: msg})
}

func (s *CliLogDriver) Warn(msg string, fields ...zapcore.Field) {
	s.program.Send(Log{Text: msg})
}

func (s *CliLogDriver) LogRunCommand(processId string, cmd string) {
	s.program.Send(Log{Text: "Running new command: " + fmt.Sprintf("%s.%s", processId, cmd), Lock: true})
}

func (s *CliLogDriver) LogRunProcedure(processId string, cmd string, i int) {
	s.program.Send(Log{Text: "Running procedure: " + fmt.Sprintf("%s.%s[%d]", processId, cmd, i), Lock: true})
}

func (s *CliLogDriver) LogStdout(porcess string, cmd string, data string) {
	s.program.Send(Message{Text: data})
}
