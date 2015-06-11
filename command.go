// Package command implements helper functions for multi-command programs.
//
// This package includes types and functions for easily defining multiple
// subcommands with different options. A "help" subcommand is also automatically
// generated, which might be used to list all the available subcommands or
// to view all the help about a specific subcommand.
//
// Clients should just define a list of the command they implement and just call
// Run or RunOpts directly from main().
//
//  package main
//
//  import (
//	"github.com/rainycape/command"
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
//  func awesomeCommand(args *command.Args, opts *AwesomeOptions) error {
//  ...
//  }
//
//  func main() {
//	command.Exit(command.Run(cmds))
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
	"runtime"
	"text/tabwriter"
)

var (
	argsType = reflect.TypeOf([]string(nil))
	errType  = reflect.TypeOf((*error)(nil)).Elem()
	cmdType  = reflect.TypeOf((*Cmd)(nil))
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
	// name are prepended to it when displaying it to the user,
	// as well as any arguments defined in the Args field.
	// (e.g. Usage = <something> shows "usage: myprog subcmd <something>")
	Usage string
	// Args accepted by the command. If nil, no argument validation
	// is performed. To define a command which accepts no arguments and
	// errors when arguments are passed, set this field to NoArgs.
	// See the Argument and Args types for more information.
	Args []*Argument
	// Func is the handler function for the command. The function must take either
	// one or two arguments. The first one must be an *Args, which is
	// used to access non-flag arguments.
	// If the function accepts a second argument it must
	// be of the exact same type than the value provided in the Options field.
	// Handler functions might optionally return an error value.
	Func interface{}
	// Options might be either nil or a pointer to a struct type. Command flags
	// will be generated from this struct, in the same order as the fields are
	// defined. The current value of the field will be used as the default value
	// for the flag. Each field might also include two struct tags:
	//
	//  - name: The name of the flag. If not present, it will default to the field name in lowercase.
	//  - help: The short help shown the flag package for the given field.
	Options interface{}
}

func (c *Cmd) hasArgs() bool {
	return len(c.Args) > 0 && !reflect.DeepEqual(c.Args, NoArgs)
}

// Exit exits with exit status zero when err is nil and with
// non-zero when err is non-nil.
func Exit(err error) {
	status := 0
	switch err {
	case ErrHelp:
		status = 2
	case ErrNoCommand:
		status = 3
	case ErrUnusedArguments:
		status = 4
	case nil:
		// keep 0 status
	default:
		status = 1
	}
	os.Exit(status)
}

// Run is a shorthand for RunOpts(nil, nil, commands).
func Run(commands []*Cmd) error {
	return RunOpts(nil, nil, commands)
}

// The CommandProvider interface might be implemented by the
// type used in the Options field of the Options type. If
// implemented, its Commands function is called after
// Options.BeforeFunc and before Options.Func
type CommandProvider interface {
	Commands() ([]*Cmd, error)
}

// Options are used to specify additional options when calling RunOpts
type Options struct {
	// Options represents global options which the application
	// needs to handle before running any commands. If this field
	// is non-nil and Handler is also non-nil, its first argument
	// must match the type of this field.
	//
	// Optionally, the value in this field might implement the
	// CommandProvider interface. In that case, its Commands function
	// is called after BeforeFunc and before Func.
	Options interface{}
	// Func is called after the command to execute is determined but before
	// executing it.
	Func func(*Cmd, *Options) error
	// BeforeFunc must follow the same characteristics of Func, except it
	// can't take an optional *Cmd parameter.
	//
	// BeforeFunc is called before the command to execute is determined, so
	// it can be used to conditionally set up additional commands.
	BeforeFunc func(*Options) error
}

func (opts *Options) additionalCommands() []*Cmd {
	if opts != nil && opts.Options != nil {
		if provider, ok := opts.Options.(CommandProvider); ok {
			cmds, err := provider.Commands()
			if err != nil {
				panic(fmt.Errorf("error obtaining additional commands: %v", err))
			}
			return cmds
		}
	}
	return nil
}

// RunOpts tries to run a command from the specified list using the
// given arguments, interpreting the first argument as the command name.
// If the returned error is non-nil, it will be one of:
//
//  - ErrNoCommand when no arguments are provided
//  - ErrHelp when the user has requested any help to be shown
//  - ErrUnusedArguments when the command doesn't accept any arguments, but the user has provided some
//  - An UnknownCommandError when the command (the first argument) does not exist
//  - Any error returned by Options.BeforeFunc or Options.Func
//  - Any error returned by the command handler
//
// If args is nil, it will be set to os.Args[1:].
//
// Any user error will be printed to os.Stderr by RunOpts, so callers don't need to print any
// error messages themselves.
//
// Note that RunOpts will panic in case of a programming error. This usually happens
// when Func or Options don't match the required constraints. See the documentation on
// those fields in the Cmd type for more information.
func RunOpts(args []string, opts *Options, commands []*Cmd) (err error) {
	if os.Getenv(CommandDumpHelpEnvVar) != "" {
		if err := dumpHelp(os.Stdout, opts, commands); err != nil {
			panic(err)
		}
		return nil
	}
	if args == nil {
		args = os.Args[1:]
	}
	rem, err := parseGlobalOptions(args, opts)
	if err != nil {
		return err
	}
	if opts != nil && opts.BeforeFunc != nil {
		if err := opts.BeforeFunc(opts); err != nil {
			return err
		}
	}
	commands = append(commands, opts.additionalCommands()...)
	if len(rem) == 0 || rem[0] == "help" {
		return printHelp(os.Stderr, rem, commands)
	}
	name := rem[0]
	cmdArgs := rem[1:]
	cmd := commandByName(commands, name)
	if cmd == nil {
		return printHelp(os.Stderr, args, commands)
	}
	defer recoverRun(cmd, &err)
	fn := reflect.ValueOf(cmd.Func)
	if fn.Kind() != reflect.Func {
		panic(fmt.Errorf("command handler %s is not a function, it's %T", name, cmd.Func))
	}
	if err := validateCmdFuncReturn(fn); err != nil {
		panic(fmt.Errorf("invalid handler for command %s: %s", name, err))
	}
	var optsVal reflect.Value
	if cmd.Options != nil {
		optsVal = reflect.ValueOf(cmd.Options)
		flags, err := setupOptionsFlags(name, optsVal)
		if err != nil {
			panic(err)
		}
		if err := flags.Parse(cmdArgs); err != nil {
			return err
		}
		cmdArgs = flags.Args()
	}
	cmdArguments := newArgs(cmdArgs, cmd)
	if err := validateCmdFuncInput(fn, optsVal); err != nil {
		panic(fmt.Errorf("invalid handler for command %s: %s", name, err))
	}
	if opts != nil && opts.Func != nil {
		if err := opts.Func(cmd, opts); err != nil {
			return err
		}
	}
	if err := cmdArguments.validate(); err != nil {
		if err == ErrUnusedArguments {
			fmt.Fprintf(os.Stderr, "command %s does not accept any arguments\n", name)
		}
		return err
	}
	fnArgs := []reflect.Value{reflect.ValueOf(cmdArguments)}
	if optsVal.IsValid() {
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

func parseGlobalOptions(args []string, opts *Options) ([]string, error) {
	if opts != nil && opts.Options != nil {
		globalOptsVal := reflect.ValueOf(opts.Options)
		flags, err := setupOptionsFlags("", globalOptsVal)
		if err != nil {
			panic(err)
		}
		if err := flags.Parse(args); err != nil {
			return nil, err
		}
		args = flags.Args()
	}
	return args, nil
}

func validateCmdFuncInput(fn reflect.Value, optsVal reflect.Value) error {
	argsType := reflect.TypeOf((*Args)(nil))
	fnTyp := fn.Type()
	numIn := fnTyp.NumIn()
	if numIn < 1 || fnTyp.In(0) != argsType {
		return fmt.Errorf("function %s must accept %s as its first argument", funcName(fn), argsType)
	}
	if optsVal.IsValid() {
		if numIn < 2 || fnTyp.In(1) != optsVal.Type() {
			return fmt.Errorf("function %s must accept %s as its second argument", funcName(fn), optsVal.Type())
		}
	}
	return nil
}

func validateCmdFuncReturn(val reflect.Value) error {
	typ := val.Type()
	numOut := typ.NumOut()
	if numOut == 0 {
		return nil
	}
	if numOut > 1 {
		return fmt.Errorf("function %s must return 0 or 1 arguments, not %d", funcName(val), numOut)
	}
	if typ.Out(0) != errType {
		return fmt.Errorf("function %s must return a value of type %s, not %s", funcName(val), errType, typ.Out(0))
	}
	return nil
}

func funcName(val reflect.Value) string {
	ptr := val.Pointer()
	fn := runtime.FuncForPC(ptr)
	if fn == nil {
		return fmt.Sprintf("unknown function at %p", ptr)
	}
	return fn.Name()
}

func setupOptionsFlags(name string, sval reflect.Value) (*flag.FlagSet, error) {
	arg0 := filepath.Base(os.Args[0])
	var flagsName string
	if name != "" {
		flagsName = fmt.Sprintf("%s %s subcommand", arg0, name)
	} else {
		flagsName = arg0
	}
	flags := flag.NewFlagSet(flagsName, flag.ContinueOnError)
	err := visitStruct(sval, func(name string, help string, field *reflect.StructField, val reflect.Value, ptr interface{}) error {
		if value, ok := ptr.(flag.Value); ok {
			flags.Var(value, name, help)
			return nil
		}
		switch val.Type().Kind() {
		case reflect.Bool:
			flags.BoolVar(ptr.(*bool), name, val.Bool(), help)
		case reflect.Float64:
			flags.Float64Var(ptr.(*float64), name, val.Float(), help)
		case reflect.Int:
			flags.IntVar(ptr.(*int), name, int(val.Int()), help)
		case reflect.Uint:
			flags.UintVar(ptr.(*uint), name, uint(val.Uint()), help)
		case reflect.Int64:
			flags.Int64Var(ptr.(*int64), name, val.Int(), help)
		case reflect.Uint64:
			flags.Uint64Var(ptr.(*uint64), name, val.Uint(), help)
		case reflect.String:
			flags.StringVar(ptr.(*string), name, val.String(), help)
		default:
			return fmt.Errorf("field %s has invalid option type %s", field.Name, field.Type)
		}
		return nil
	})
	switch err {
	case errNoPointer:
		if name != "" {
			return nil, fmt.Errorf("invalid command %s options %s, must be a pointer", name, sval.Type())
		}
		return nil, fmt.Errorf("invalid options %s, must be a pointer", sval.Type())
	case errNoStruct:
		if name != "" {
			return nil, fmt.Errorf("command %s options field is not a struct, it's %T", name, sval.Type())
		}
		return nil, fmt.Errorf("options field is not a struct, it's %T", sval.Type())
	}
	return flags, err
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
	if cmd.Usage != "" || cmd.hasArgs() {
		fmt.Fprintf(w, "usage: %s %s", filepath.Base(os.Args[0]), cmd.Name)
		if cmd.Usage != "" {
			fmt.Fprintf(w, " %s", cmd.Usage)
		}
		for _, v := range cmd.Args {
			if v.Optional {
				fmt.Fprintf(w, " [%s]", v.Name)
			} else {
				fmt.Fprintf(w, " %s", v.Name)
			}
		}
		fmt.Fprint(w, "\n")
	}
	if cmd.LongHelp != "" {
		fmt.Fprintf(w, "\n%s\n", cmd.LongHelp)
	}
	if cmd.Options != nil {
		fmt.Fprint(w, "\nFlags:\n")
		opts := reflect.ValueOf(cmd.Options)
		if fs, err := setupOptionsFlags(cmd.Name, opts); err == nil {
			fs.PrintDefaults()
		}
	}
	if cmd.hasArgs() {
		fmt.Fprint(w, "\nArguments:\n")
		for _, v := range cmd.Args {
			fmt.Fprintf(w, "  %s: %s\n", v.Name, v.Help)
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
	tw := tabwriter.NewWriter(w, 0, 8, 2, ' ', 0)
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
