package command

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
)

const (
	// Setting CommandDumpHelpEnvVar to a non-empty
	// value causes any tool using command to dump
	// its help as JSON to the standard output when
	// it's run. It's intended to be used by 3rd party
	// tools to automatically generate documentation
	// for any tool using this package.
	CommandDumpHelpEnvVar = "COMMAND_DUMP_HELP"
)

// Flag represents a global or a command flag.
type Flag struct {
	Name    string `json:"name"`
	Help    string `json:"help"`
	Type    string `json:"type"`
	Default string `json:"default"`
}

// Help represents the help for a tool using this package.
// This structure is used when dumping the
// help in JSON, so other packages can use it to
// parse the help output from a command.
type Help struct {
	Name     string         `json:"name"`
	Flags    []*Flag        `json:"flags"`
	Commands []*CommandHelp `json:"commands"`
}

// CommandHelp is the help for a given command.
type CommandHelp struct {
	Name     string  `json:"name"`
	Help     string  `json:"help"`
	LongHelp string  `json:"long_help"`
	Usage    string  `json:"usage"`
	Flags    []*Flag `json:"flags"`
}

func flagsHelp(opts interface{}) ([]*Flag, error) {
	sval := reflect.ValueOf(opts)
	var flags []*Flag
	err := visitStruct(sval, func(name string, help string, field *reflect.StructField, val reflect.Value, ptr interface{}) error {
		fl := &Flag{
			Name: name,
			Help: help,
		}
		if value, ok := ptr.(flag.Value); ok {
			fl.Default = value.String()
			fl.Type = "string"
			flags = append(flags, fl)
			return nil
		}
		switch val.Type().Kind() {
		case reflect.Bool, reflect.Float64, reflect.Int, reflect.Uint, reflect.Int64, reflect.Uint64, reflect.String:
			fl.Default = fmt.Sprintf("%v", val.Interface())
			fl.Type = fmt.Sprintf("%s", val.Type())
		default:
			return fmt.Errorf("field %s has invalid option type %s", field.Name, field.Type)
		}
		flags = append(flags, fl)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return flags, nil
}

func commandHelp(cmd *Cmd) (*CommandHelp, error) {
	h := &CommandHelp{
		Name:     cmd.Name,
		Help:     cmd.Help,
		LongHelp: cmd.LongHelp,
		Usage:    cmd.Usage,
	}
	if cmd.Options != nil {
		flags, err := flagsHelp(cmd.Options)
		if err != nil {
			return nil, err
		}
		h.Flags = flags
	}
	return h, nil
}

func dumpHelp(w io.Writer, opts *Options, commands []*Cmd) error {
	help := &Help{
		Name: filepath.Base(os.Args[0]),
	}
	if opts != nil && opts.Options != nil {
		flags, err := flagsHelp(opts.Options)
		if err != nil {
			return err
		}
		help.Flags = flags
	}
	for _, v := range commands {
		cmd, err := commandHelp(v)
		if err != nil {
			return err
		}
		help.Commands = append(help.Commands, cmd)
	}
	return json.NewEncoder(w).Encode(help)
}
