package command

import (
	"fmt"
	"reflect"
	"strconv"
)

var (
	// NoArgs is used to indicate that a command
	// receives no arguments. If the user provides any
	// additional arguments, it will return an error.
	NoArgs = []*Argument{nil}
)

// Type Args is used by command functions to
// receive their arguments.
type Args struct {
	args []string
	cmd  *Cmd
}

func newArgs(values []string, cmd *Cmd) *Args {
	return &Args{
		args: values,
		cmd:  cmd,
	}
}

func (a *Args) argumentPos(name string) (int, error) {
	for ii, v := range a.cmd.Args {
		if v.Name == name {
			return ii, nil
		}
	}
	return 0, fmt.Errorf("argument %q not found", name)
}

func (a *Args) validate() error {
	if reflect.DeepEqual(a.cmd.Args, NoArgs) && len(a.args) > 0 {
		return ErrUnusedArguments
	}
	hasOptional := false
	prov := len(a.args)
	var req int
	for _, v := range a.cmd.Args {
		if v.Optional {
			hasOptional = true
			continue
		}
		if hasOptional {
			return fmt.Errorf("required argument %q comes after optional arguments", v.Name)
		}
		req++
	}
	if req > prov {
		return fmt.Errorf("%d arguments required, but only %d provided", req, prov)
	}
	return nil
}

// Returns the argument with the given name as an int.
// If the argument does not exist or it can't be parsed
// as an int, it panics.
func (a *Args) Int(name string) int {
	s := a.String(name)
	val, err := strconv.Atoi(s)
	if err != nil {
		panic(fmt.Errorf("error parsing int argument %s: %v", name, err))
	}
	return val
}

// Returns the argument with the given name as a string.
// If the argument does not exist, it panics.
func (a *Args) String(name string) string {
	p, err := a.argumentPos(name)
	if err != nil {
		panic(err)
	}
	return a.StringAt(p)
}

// Returns the argument at the given position as a string.
// If the argument was not provided, it returns an empty
// string.
func (a *Args) StringAt(pos int) string {
	if pos >= len(a.args) {
		return ""
	}
	return a.args[pos]
}

// Returns the arguments as they were specified in the
// command line.
func (a *Args) Args() []string {
	return a.args
}
