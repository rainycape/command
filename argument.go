package command

// Type Argument holds a required argument for a command. Command
// arguments are processed in the same order they're defined.
// Optional arguments must always be the last ones.
type Argument struct {
	// The name of the argument
	Name string
	// Help associated with the argument. Will be
	// displayed by the help command.
	Help string
	// Wheter the argument is optional
	Optional bool
}
