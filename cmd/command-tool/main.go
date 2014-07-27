// command-tool implements a command line application which
// generates documentation for tools using gopkgs.com/command.
//
// Install this tool by running:
//
//  go install gopkgs.com/command.v1/cmd/command-tool
//
// Then view its help:
//
//  command-tool help
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"
	"text/template"
	"unicode/utf8"

	"gopkgs.com/command.v1"
)

var (
	tmpl = `{{ $name := .Name }}{{ with .Flags }}## Global flags

{{ range . }}{{ template "flag" . }}
{{ end }}
{{ end }}
{{ with .Commands }}## Commands

{{ range . }}
- ## {{ .Name|e }}
{{ with .Help }}
    {{ .|e }}
{{ end }}
{{ if .Usage }}    Usage: ` + "```" + `{{ $name }} {{ .Name }} {{ .Usage }}` + "```" + `{{ end }}
{{ with .LongHelp }}
{{ mle . 4 }}
{{ end }}
{{ if .Flags }}    Flags:

{{ range .Flags }}    {{ template "flag" . }}
{{ end }}
{{ end }}
{{ end }}
{{ end }}
{{ define "flag" }} - **{{ .Name|e }}**{{ with .Type|e }} *\({{ . }}\)*{{ end }}{{ if or .Help .Default }}:{{ with .Help }} {{ .|e }}{{ end }}{{ with .Default }} *default: {{ .|e }}*{{ end }}{{ end }}{{ end }}`
)

var (
	commands = []*command.Cmd{
		{
			Name:    "help-md",
			Usage:   "<cmd>",
			Help:    "Generates a markdown document with the help for the given command",
			Func:    helpCommand,
			Options: &helpOptions{},
		},
	}

	templateFuncs = template.FuncMap{
		"e":   markdownEscape,
		"mle": mardownEscapeMultiline,
	}

	mainTemplate = template.Must(template.New("main").Funcs(templateFuncs).Parse(tmpl))
)

func markdownEscape(s string) string {
	start := 0
	whitespace := 0
	nonWhitespace := false
	var buf bytes.Buffer
	for ii := 0; ii < len(s); {
		c, size := utf8.DecodeRuneInString(s[ii:])
		if c == ' ' && !nonWhitespace {
			whitespace++
		} else {
			nonWhitespace = true
			if c == '\n' {
				whitespace = 0
				nonWhitespace = false
			}
		}
		if whitespace < 4 {
			switch c {
			// characters that need to be escaped
			case '\\', '`', '*', '_', '{', '}', '[', ']', '(', ')', '#', '+', '-', '.', '!', '<', '>':
				buf.WriteString(s[start:ii])
				buf.WriteByte('\\')
				buf.WriteRune(c)
				start = ii + size
			default:
			}
		}
		ii += size
	}
	if start == 0 {
		// No escaping required
		return s
	}
	buf.WriteString(s[start:len(s)])
	return buf.String()
}

func mardownEscapeMultiline(s string, spaces int) string {
	escaped := markdownEscape(s)
	if spaces > 0 {
		pad := strings.Repeat(" ", spaces)
		repl := "\n" + pad
		escaped = pad + strings.Replace(escaped, "\n", repl, -1)
	}
	return escaped
}

func appendFile(buf *bytes.Buffer, filename string) error {
	f, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(buf, f)
	return err
}

type helpOptions struct {
	Header string `help:"Header to prepend to the document"`
	Footer string `help:"Footer to append to the document"`
	Output string `name:"o" help:"Output file. If empty, output is printed to stdout"`
}

func helpCommand(args []string, opts *helpOptions) error {
	if len(args) != 1 {
		return fmt.Errorf("help only accepts one argument")
	}
	cmd := exec.Command(args[0])
	cmd.Env = []string{
		command.CommandDumpHelpEnvVar + "=1",
	}
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	var help *command.Help
	if err := json.Unmarshal(buf.Bytes(), &help); err != nil {
		return err
	}
	var out bytes.Buffer
	if opts.Header != "" {
		if err := appendFile(&out, opts.Header); err != nil {
			return fmt.Errorf("error reading header file %s: %s", opts.Header, err)
		}
	} else {
		escaped := markdownEscape(help.Name)
		out.WriteString(escaped)
		out.WriteByte('\n')
		out.WriteString(strings.Repeat("=", len(escaped)))
		out.WriteByte('\n')
		out.WriteByte('\n')
	}
	if err := mainTemplate.Execute(&out, help); err != nil {
		return fmt.Errorf("error executing template: %s", err)
	}
	if opts.Footer != "" {
		if err := appendFile(&out, opts.Footer); err != nil {
			return fmt.Errorf("error reading header file %s: %s", opts.Footer, err)
		}
	}
	if opts.Output != "" {
		if err := ioutil.WriteFile(opts.Output, out.Bytes(), 0644); err != nil {
			return fmt.Errorf("error writing outfile file %s: %s", opts.Output, err)
		}
	} else {
		fmt.Print(out.String())
	}
	return nil
}

func main() {
	command.Run(commands)
}
