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
	"strings"
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
	//  - name: The name of the flag. If not present, it will default to the field name in lowercase.
	//  - help: The short help shown the flag package for the given field.
	Options interface{}
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
	// Func must be a function which accepts a value of the same
	// type of Options as the first parameter and, optionally, a
	// *Cmd as the second argument, which will be the command which is
	// going the be run after calling Func.  Note that setting a non-nil
	// Func with nil Options is supported, but in that case Func must
	// take zero or one argument of type *Cmd. In any case, Func might return
	// either zero or one value of type error.
	//
	// Func is called after the command to execute is determined but before
	// executing it.
	Func interface{}
	// BeforeFunc must follow the same characteristics of Func, except it
	// can't take an optional *Cmd parameter.
	//
	// BeforeFunc is called before the command to execute is determined, so
	// it can be used to conditionally set up additional commands.
	BeforeFunc interface{}
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
	optsFn, optsBeforeFn, optsArgs, args, err := parseOptions(args, opts)
	if err != nil {
		return err
	}
	if err := callOptionsFunc(optsBeforeFn, optsArgs, nil); err != nil {
		return err
	}
	name := args[0]
	rem := args[1:]
	commands = append(commands, opts.additionalCommands()...)
	if len(args) == 0 || args[0] == "help" {
		return printHelp(os.Stderr, args, commands)
	}
	cmd := commandByName(commands, name)
	if cmd == nil {
		return printHelp(os.Stderr, args, commands)
	}
	defer recoverRun(cmd, &err)
	if err := callOptionsFunc(optsFn, optsArgs, cmd); err != nil {
		return err
	}
	fn := reflect.ValueOf(cmd.Func)
	if fn.Kind() != reflect.Func {
		panic(fmt.Errorf("command handler %s is not a function, it's %T", name, cmd.Func))
	}
	if err := validateFuncReturn(fn); err != nil {
		panic(fmt.Errorf("invalid handler for command %s: %s", name, err))
	}
	var optsVal reflect.Value
	var fnArgs []reflect.Value
	var mandatory []reflect.Type
	if cmd.Options != nil {
		optsVal = reflect.ValueOf(cmd.Options)
		mandatory = append(mandatory, optsVal.Type())
		flags, err := setupOptionsFlags(name, optsVal)
		if err != nil {
			panic(err)
		}
		if err := flags.Parse(rem); err != nil {
			return err
		}
		rem = flags.Args()
	}
	if err := validateFuncInput(fn, []reflect.Type{argsType}, mandatory, true); err != nil {
		panic(fmt.Errorf("invalid handler for command %s: %s", name, err))
	}
	typ := fn.Type()
	numIn := typ.NumIn()
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

func callOptionsFunc(fn reflect.Value, args []reflect.Value, cmd *Cmd) error {
	if fn.IsValid() {
		if fn.Type().NumIn() > len(args) {
			args = append(args, reflect.ValueOf(cmd))
		}
		res := fn.Call(args)
		if len(res) > 0 {
			if err, ok := res[0].Interface().(error); ok {
				fmt.Fprintf(os.Stderr, "%s\n", err)
				return err
			}
		}
	}
	return nil
}

func parseOptions(args []string, opts *Options) (reflect.Value, reflect.Value, []reflect.Value, []string, error) {
	var optsFn reflect.Value
	var optsBeforeFn reflect.Value
	var optsArgs []reflect.Value
	if opts != nil {
		var globalOptsVal reflect.Value
		var mandatoryArgs []reflect.Type
		if opts.Options != nil {
			globalOptsVal = reflect.ValueOf(opts.Options)
			optsArgs = append(optsArgs, globalOptsVal)
			mandatoryArgs = append(mandatoryArgs, globalOptsVal.Type())
			flags, err := setupOptionsFlags("", globalOptsVal)
			if err != nil {
				panic(err)
			}
			if err := flags.Parse(args); err != nil {
				return optsFn, optsBeforeFn, optsArgs, args, err
			}
			args = flags.Args()
		}
		optsFn = validateOptionsFunc(opts.Func, "Func", []reflect.Type{cmdType}, mandatoryArgs)
		optsBeforeFn = validateOptionsFunc(opts.BeforeFunc, "BeforeFunc", nil, mandatoryArgs)
	}
	return optsFn, optsBeforeFn, optsArgs, args, nil
}

func validateOptionsFunc(fn interface{}, name string, optionalArgs []reflect.Type, mandatoryArgs []reflect.Type) reflect.Value {
	if fn == nil {
		return reflect.Value{}
	}
	val := reflect.ValueOf(fn)
	if val.Kind() != reflect.Func {
		panic(fmt.Errorf("Options.%s is not a function, it's %T", name, fn))
	}
	if err := validateFuncReturn(val); err != nil {
		panic(fmt.Errorf("invalid Options.%s: %s", name, err))
	}
	if err := validateFuncInput(val, optionalArgs, mandatoryArgs, false); err != nil {
		panic(fmt.Errorf("invalid Options.%s: %s", name, err))
	}
	return val
}

func validateArguments(val reflect.Value, n int, args []reflect.Type, mandatory bool) (int, error) {
	typ := val.Type()
	numIn := typ.NumIn()
	for _, v := range args {
		if n >= numIn {
			if !mandatory {
				break
			}
			return 0, fmt.Errorf("function %s must accept an argument of type %s as the #%d parameter, but in only has %d arguments", funcName(val), v, n+1, numIn)
		}
		in := typ.In(n)
		if in != v {
			if !mandatory {
				continue
			}
			return 0, fmt.Errorf("function %s must accept an argument of type %s as the #%d parameter, not %s", funcName(val), v, n+1, in)
		}
		n++
	}
	return n, nil
}

func validateFuncInput(val reflect.Value, optional []reflect.Type, mandatory []reflect.Type, optionalFirst bool) error {
	n := 0
	var toCheck [][]reflect.Type
	var mandatories []bool
	if optionalFirst {
		toCheck = [][]reflect.Type{optional, mandatory}
		mandatories = []bool{false, true}
	} else {
		toCheck = [][]reflect.Type{mandatory, optional}
		mandatories = []bool{true, false}
	}
	var err error
	for ii, v := range toCheck {
		n, err = validateArguments(val, n, v, mandatories[ii])
		if err != nil {
			return err
		}
	}
	typ := val.Type()
	numIn := typ.NumIn()
	if n != numIn {
		var remaining []string
		for ii := n; ii < numIn; ii++ {
			remaining = append(remaining, typ.In(ii).String())
		}
		return fmt.Errorf("function %s has %d unused arguments of types %s", funcName(val), numIn-n, strings.Join(remaining, ", "))
	}
	return nil
}

func validateFuncReturn(val reflect.Value) error {
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
