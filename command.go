// Package command implements helper functions for multi-command programs.
//
// This package includes types and functions for easily defining multiple
// subcommands with different options. A "help" subcommand is also automatically
// generated, which might be used to list all the available subcommands or
// to view all the help about a specific subcommand.
//
// Clients should just define a list of the command they implement and the call
// Run or RunArgs directly from main().
//
//  package main
//
//  import (
//	"gopkgs.com/command.v1"
//  )
//
//  var (
//	cmds = []*command.Cmd{
//	    {
//		Name: "awesome",
//		Help: "do something awesome"
//		Options: &awesomeOptions{Value:42},
//	    },
//	}
//  )
//
//  type awesomeOptions struct {
//	Value int `name:"v" help:"Some arbitrary value"`
//  }
//
//  func awesomeCommand(args []string, opts *AwesomeOptions) error {
//  ...
//  }
//
//  func main() {
//	commands.Run(cmds)
//  }
package command

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"text/tabwriter"
)

var (
	argsType = reflect.TypeOf([]string(nil))
	// ErrNoCommand is returned from Run when no command
	// has been specified (i.e. there are no arguments).
	ErrNoCommand = errors.New("no command provided")
	// ErrHelp is returned from Run when the help is shown,
	// either the brief help or the detailed help for a
	// command.
	ErrHelp = errors.New("help has been shown")
	// ErrUnusedArguments is returned form Run when the command
	// does not accept any arguments, but the user has provided
	// some.
	ErrUnusedArguments = errors.New("arguments provided but not used")
)

// UnknownCommandError is returned from Run when the specified
// command does not exist.
type UnknownCommandError string

func (e UnknownCommandError) Error() string {
	return fmt.Sprintf("unknown command %s", string(e))
}

// Cmd represents an available command.
type Cmd struct {
	// Name is the name of the command, case sensitive.
	Name string
	// Help is a short, one line help string displayed by
	// the help command when listing all the commands.
	Help string
	// LongHelp is a long, potentially multi-line, help message
	// displayed when using the help command for a specific
	// command (e.g. myprogram help somecommand)
	LongHelp string
	// Usage is displayed when showing the help for a specific
	// command. The program name (os.Args[0]) and the command
	// name are prepended to it when displaying it to the user.
	// (e.g. Usage = <some-argument> shows "usage: myprog subcmd <some-argument>")
	Usage string
	// Func is the handler function for the command. The function must take either
	// one or two arguments. The first one must be a []string, which will contain
	// any non-flag arguments. If the function accepts a second argument it must
	// be of the exact same type than the value provided in the Options field.
	// Handler functions might optionally return an error value.
	Func interface{}
	// Options might be either nil or a pointer to a struct type. Command flags
	// will be generated from this struct, in the same order as the fields are
	// defined. The current value of the field will be used as the default value
	// for the flag. Each field might also include two struct tags:
	//
	//  - name: The name of the flag. If not present, it will default to the field name.
	//  - help: The short help shown the flag package for the given field.
	Options interface{}
}

// Run is a shorthand for RunArgs(os.Args[1:], commands).
func Run(commands []*Cmd) error {
	return RunArgs(os.Args[1:], commands)
}

// RunArgs tries to run a command from the specified list using the
// given arguments, interpreting the first argument as the command name.
// If the returned error is non-nil, it will be one of:
//
//  - ErrNoCommand when no arguments are provided
//  - ErrHelp when the user has requested any help to be shown
//  - ErrUnusedArguments when the command doesn't accept any arguments, but the user has provided some
//  - An UnknownCommandError when the command (the first argument) does not exist
//  - Any error returned by the command handler
//
// Any user error will be printed to Stderr by RunArgs, so callers don't need to print any
// error messages themselves.
//
// Note that RunArgs will panic in case of a programming error. This usually happens
// when Func or Options don't match the required constraints. See the documentation on
// those fields in the Cmd type for more information.
func RunArgs(args []string, commands []*Cmd) error {
	if len(args) == 0 || args[0] == "help" {
		return printHelp(os.Stderr, args, commands)
	}
	name := args[0]
	rem := args[1:]
	cmd := commandByName(commands, name)
	if cmd == nil {
		return printHelp(os.Stderr, args, commands)
	}
	fn := reflect.ValueOf(cmd.Func)
	if fn.Kind() != reflect.Func {
		panic(fmt.Errorf("command handler %s is not a function, it's %T", name, cmd.Func))
	}
	var optsVal reflect.Value
	var fnArgs []reflect.Value
	typ := fn.Type()
	numIn := typ.NumIn()
	if cmd.Options != nil {
		optsVal = reflect.ValueOf(cmd.Options)
		optsType := optsVal.Type()
		if numIn == 0 || (typ.In(0) != optsType && (numIn < 2 || typ.In(1) != optsType)) {
			panic(fmt.Errorf("command %s (%s) declares options of type %T but does not accept them", name, typ, cmd.Options))
		}
		flags, err := setupOptionsFlags(name, optsVal)
		if err != nil {
			panic(err)
		}
		if err := flags.Parse(rem); err != nil {
			return err
		}
		rem = flags.Args()
	}
	if numIn > 0 && typ.In(0) == argsType {
		fnArgs = append(fnArgs, reflect.ValueOf(rem))
	} else if len(rem) > 0 {
		fmt.Fprintf(os.Stderr, "command %s does not accept any arguments\n", name)
		return ErrUnusedArguments
	}
	if cmd.Options != nil {
		fnArgs = append(fnArgs, optsVal)
	}
	res := fn.Call(fnArgs)
	if len(res) > 0 {
		if err, ok := res[0].Interface().(error); ok {
			fmt.Fprintf(os.Stderr, "error running command %s: %s\n", name, err)
			return err
		}
	}
	return nil
}

func setupOptionsFlags(name string, val reflect.Value) (*flag.FlagSet, error) {
	if val.Kind() != reflect.Ptr {
		return nil, fmt.Errorf("invalid options %s, must be a pointer", val.Type())
	}
	val = reflect.Indirect(val)
	typ := val.Type()
	if typ.Kind() != reflect.Struct {
		return nil, fmt.Errorf("invalid Options type %s, must be an struct", typ)
	}
	flagsName := fmt.Sprintf("%s %s subcommand", filepath.Base(os.Args[0]), name)
	flags := flag.NewFlagSet(flagsName, flag.ContinueOnError)
	for ii := 0; ii < typ.NumField(); ii++ {
		field := typ.Field(ii)
		fieldVal := val.Field(ii)
		ptr := fieldVal.Addr().Interface()
		var name, help string
		if n := field.Tag.Get("name"); n != "" {
			name = n
		}
		if h := field.Tag.Get("help"); h != "" {
			help = h
		}
		switch field.Type.Kind() {
		case reflect.Bool:
			flags.BoolVar(ptr.(*bool), name, fieldVal.Bool(), help)
		case reflect.Float64:
			flags.Float64Var(ptr.(*float64), name, fieldVal.Float(), help)
		case reflect.Int:
			flags.IntVar(ptr.(*int), name, int(fieldVal.Int()), help)
		case reflect.Uint:
			flags.UintVar(ptr.(*uint), name, uint(fieldVal.Uint()), help)
		case reflect.Int64:
			flags.Int64Var(ptr.(*int64), name, fieldVal.Int(), help)
		case reflect.Uint64:
			flags.Uint64Var(ptr.(*uint64), name, fieldVal.Uint(), help)
		case reflect.String:
			flags.StringVar(ptr.(*string), name, fieldVal.String(), help)
		default:
			if value, ok := ptr.(flag.Value); ok {
				flags.Var(value, name, help)
				continue
			}
			return nil, fmt.Errorf("field %s has invalid option type %s", field.Name, field.Type)
		}
	}
	return flags, nil
}

func commandByName(commands []*Cmd, name string) *Cmd {
	for _, v := range commands {
		if v.Name == name {
			return v
		}
	}
	return nil
}

func printCommandHelp(w io.Writer, cmd *Cmd) {
	fmt.Fprintf(w, "%s: %s\n", cmd.Name, cmd.Help)
	if cmd.Usage != "" {
		fmt.Fprintf(w, "\nusage: %s %s %s\n", filepath.Base(os.Args[0]), cmd.Name, cmd.Usage)
	}
	if cmd.LongHelp != "" {
		fmt.Fprintf(w, "\n%s\n\n", cmd.LongHelp)
	}
	if cmd.Options != nil {
		opts := reflect.ValueOf(cmd.Options)
		if fs, err := setupOptionsFlags(cmd.Name, opts); err == nil {
			fs.PrintDefaults()
		}
	}
}

func printHelp(w io.Writer, args []string, commands []*Cmd) error {
	var err error
	if len(args) == 0 {
		fmt.Fprintln(w, "missing command, available ones are:\n")
		err = ErrNoCommand
	} else {
		var unknown string
		if args[0] != "help" && commandByName(commands, args[0]) == nil {
			unknown = args[0]
		}
		if len(args) > 1 && args[0] == "help" {
			if cmd := commandByName(commands, args[1]); cmd != nil {
				printCommandHelp(w, cmd)
				return ErrHelp
			}
			unknown = args[1]
		}
		if unknown != "" {
			fmt.Fprintf(w, "unknown command %s, available ones are:\n\n", unknown)
			err = UnknownCommandError(unknown)
		}
	}
	tw := tabwriter.NewWriter(w, 0, 8, 0, '\t', 0)
	for _, v := range commands {
		fmt.Fprintf(tw, "%s\t%s\n", v.Name, v.Help)
	}
	fmt.Fprint(tw, "help\tPrint this help\n")
	tw.Flush()
	fmt.Fprint(w, "\nTo view additional help for each command use help <command_name>\n")
	if err == nil {
		err = ErrHelp
	}
	return err
}
