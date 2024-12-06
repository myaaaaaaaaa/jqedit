package main

import (
	"bytes"
	"cmp"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/itchyny/gojq"
	"github.com/myaaaaaaaaa/go-jqx"
)

const sampleJSON = `{"a":5,"b":"c","d":["e","f"],"g":{"h":"i","j":"k"}}`

type data struct {
	input   string
	code    string
	compact bool
}

func (d data) query() (string, error) {
	var output bytes.Buffer

	prog := jqx.Program{
		Args:   []string{" " + d.code},
		Stdin:  bytes.NewBufferString(d.input),
		Stdout: &output,

		StdoutIsTerminal: !d.compact,
	}
	err := prog.Main()
	rt := output.String()
	if rt == "null\n" {
		return rt, fmt.Errorf("error: query returned null")
	}
	return rt, err
}

type (
	saveMsg struct{}
	tabMsg  struct{}
	tickMsg struct{}
)
type updateMsg struct {
	m tea.Model
	c tea.Cmd
}

type model struct {
	textarea textarea.Model
	viewport viewport.Model
	vcontent string

	d data

	err error
	num int
}

func newModel(text string) model {
	rt := model{
		textarea: textarea.New(),
		viewport: viewport.New(10, 10),
		d: data{
			input: text,
			code:  "#placeholder",
		}}

	rt.textarea.SetHeight(3)
	rt.textarea.Placeholder = "jq..."
	rt.textarea.Focus()

	return rt
}
func (m model) Init() tea.Cmd {
	return tea.Batch(
		textarea.Blink,
		tick(),
	)
}

const Margin = 8

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	oldData := m.d

	switch msg := msg.(type) {
	case updateMsg:
		if msg.m == nil {
			msg.m = m
		}
		return msg.m, msg.c
	case tea.WindowSizeMsg:
		width := msg.Width - Margin*2
		m.textarea.SetWidth(width * 3 / 4)
		m.viewport.Width = width
		m.viewport.Height = msg.Height / 2
		return m, nil
	case tea.MouseMsg:
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd

	case saveMsg:
		outFile := cmp.Or(os.Getenv("XDG_RUNTIME_DIR"), "/tmp")
		outFile = path.Join(outFile, "jq.out.txt")

		m.err = os.WriteFile(outFile, []byte(m.vcontent), 0666)
		if m.err == nil {
			return m, tea.Printf("    saved file://%s", subtleStyle.Render(outFile))
		}

		return m, nil
	case tickMsg:
		m.num++
		return m, nil
	case tabMsg:
		m.d.compact = !m.d.compact
	}

	var cmd tea.Cmd
	var text string
	m.textarea, cmd = m.textarea.Update(msg)
	m.d.code = fmtScript(m.textarea.Value())
	if m.d == oldData {
		goto abortUpdate
	}

	text, m.err = m.d.query()
	if m.err != nil {
		goto abortUpdate
	}

	cmd = tea.Batch(cmd, logScript(m.d.code))
	m.viewport.SetContent(text)
	m.vcontent = text

abortUpdate:
	return m, cmd
}

var (
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#4444cc"))
	errorStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#880000"))
	subtleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#cccccc"))
)

func (m model) View() string {
	viewport := m.viewport.View()

	err := ""
	if m.err != nil {
		err = m.err.Error()
	}

	tock := "Tick"
	if m.num%2 == 1 {
		tock = "Tock"
	}
	tock = fmt.Sprint(tock, " #", m.num)

	hr := subtleStyle.Render("────")
	vr := subtleStyle.Render("│\n│")

	mainView := lipgloss.JoinVertical(lipgloss.Center,
		headerStyle.Render(tock),

		hr,
		lipgloss.JoinHorizontal(lipgloss.Center,
			vr,
			viewport,
			vr,
		),
		hr,

		m.textarea.View(),
	)

	return lipgloss.JoinHorizontal(lipgloss.Center, strings.Repeat(" ", Margin-1), mainView) +
		"\n" + errorStyle.Render(err)
}

type emptyModel struct{}

func (_ emptyModel) Init() tea.Cmd                         { return nil }
func (e emptyModel) Update(_ tea.Msg) (tea.Model, tea.Cmd) { return e, nil }
func (_ emptyModel) View() string                          { return "" }

////////////////////////////////////////////////////////////////////////////////
// Helpers
////////////////////////////////////////////////////////////////////////////////

var spaceRe = regexp.MustCompile(`\S+`)

func fmtScript(code string) string {
	code = strings.TrimSpace(code)
	if code == "" {
		code = "."
	}
	return code
}
func isTerminal(f fs.File) bool {
	stat, err := f.Stat()
	if err != nil {
		return false
	}
	return stat.Mode()&fs.ModeCharDevice != 0
}
func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return updateMsg{c: tea.Batch(
			func() tea.Msg { return tickMsg{} },
			tick(),
		)}
	})
}

var logged = map[string]bool{}

func logScript(code string) tea.Cmd {
	{
		q, err := gojq.Parse(code)
		if err != nil {
			panic(err)
		}
		code = q.String()
	}

	if logged[code] {
		return nil
	}
	logged[code] = true
	return tea.Printf("    '%s'", code)
}
func msgFilter(_ tea.Model, msg tea.Msg) tea.Msg {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyEsc, tea.KeyCtrlC, tea.KeyCtrlQ:
			return updateMsg{
				emptyModel{},
				tea.Quit,
			}
		case tea.KeyCtrlS:
			return saveMsg{}
		case tea.KeyTab:
			return tabMsg{}
		}
	}
	return msg
}
func main() {
	text := sampleJSON
	if !isTerminal(os.Stdin) {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			panic(err)
		}
		text = string(b)
	}

	p := tea.NewProgram(newModel(text),
		tea.WithFilter(msgFilter),
		tea.WithMouseCellMotion(),
	)

	if _, err := p.Run(); err != nil {
		panic(err)
	}
}
