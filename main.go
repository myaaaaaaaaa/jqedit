package main

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/myaaaaaaaaa/go-jqx"
)

const sampleJSON = `{"a":5,"b":"c","d":["e","f"],"g":{"h":"i","j":"k"}}`

var spaceRe = regexp.MustCompile(`\S+`)

func fmtScript(code string) string {
	words := spaceRe.FindAllString(code, -1)
	code = strings.Join(words, " ")
	if code == "" {
		code = "."
	}
	return code
}

type model struct {
	textarea textarea.Model
	viewport viewport.Model

	input string
	code  string

	compact bool
	err     error
	num     int
}

func newModel(text string) model {
	rt := model{
		textarea: textarea.New(),
		viewport: viewport.New(80, 15),
		input:    text,
		code:     "#placeholder",
	}

	rt.textarea.SetHeight(3)
	rt.textarea.Placeholder = "jq..."
	rt.textarea.Focus()

	return rt
}
func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return updateMsg(func(m *model) tea.Cmd {
			m.num++
			return tick()
		})
	})
}

var logged = map[string]bool{}

func logScript(code string) tea.Cmd {
	if logged[code] {
		return nil
	}
	logged[code] = true
	return tea.Printf("    '%s'", code)
}

type tabMsg struct{}
type updateMsg func(*model) tea.Cmd
type switchMsg struct {
	m tea.Model
	c tea.Cmd
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		textarea.Blink,
		tick(),
	)
}
func (m model) query() (string, error) {
	var output bytes.Buffer

	prog := jqx.Program{
		Args:   []string{" " + m.code},
		Stdin:  bytes.NewBufferString(m.input),
		Stdout: &output,

		StdoutIsTerminal: !m.compact,
	}
	err := prog.Main()
	rt := output.String()
	if rt == "null\n" {
		return rt, fmt.Errorf("error: query returned null")
	}
	return rt, err
}

const Margin = 8

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		width := msg.Width - Margin*2
		m.textarea.SetWidth(width * 3 / 4)
		m.viewport.Width = width
		m.viewport.Height = msg.Height / 2
		return m, nil
	case updateMsg:
		cmd := msg(&m)
		return m, cmd
	case switchMsg:
		return msg.m, msg.c
	case tabMsg:
		m.code = "#placeholder"
		m.compact = !m.compact
	case tea.MouseMsg:
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}

	var cmd tea.Cmd
	var text string
	m.textarea, cmd = m.textarea.Update(msg)
	code := fmtScript(m.textarea.Value())
	if m.code == code {
		goto abortUpdate
	}

	m.code = code
	text, m.err = m.query()
	if m.err != nil {
		goto abortUpdate
	}

	cmd = tea.Batch(cmd, logScript(code))
	m.viewport.SetContent(text)

abortUpdate:
	return m, cmd
}

var headerStyle = lipgloss.NewStyle().
	Bold(true).
	Foreground(lipgloss.Color("#4444cc"))
var errorStyle = lipgloss.NewStyle().
	Bold(true).
	Foreground(lipgloss.Color("#880000"))

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

	mainView := lipgloss.JoinVertical(lipgloss.Center,
		headerStyle.Render(tock),
		strings.Repeat("─", 5),
		viewport,
		strings.Repeat("─", 5),
		m.textarea.View(),
	)

	return lipgloss.JoinHorizontal(lipgloss.Center, strings.Repeat(" ", Margin), mainView) +
		"\n" + errorStyle.Render(err)
}

func msgFilter(_ tea.Model, msg tea.Msg) tea.Msg {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyEsc, tea.KeyCtrlC, tea.KeyCtrlQ:
			return switchMsg{
				emptyModel{},
				tea.Quit,
			}
		case tea.KeyTab:
			return tabMsg{}
		}
	}
	return msg
}

type emptyModel struct{}

func (_ emptyModel) Init() tea.Cmd                         { return nil }
func (e emptyModel) Update(_ tea.Msg) (tea.Model, tea.Cmd) { return e, nil }
func (_ emptyModel) View() string                          { return "" }

func isTerminal(f fs.File) bool {
	stat, err := f.Stat()
	if err != nil {
		return false
	}
	return stat.Mode()&fs.ModeCharDevice != 0
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
