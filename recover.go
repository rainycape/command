package command

import (
	"fmt"
	"os"
	"runtime"
	"strings"
)

func recoverRun(cmd *Cmd, err *error) {
	if r := recover(); r != nil {
		var file string
		var line int
		skip, _, _, ok := getPanic()
		if ok {
			_, file, line, ok = runtime.Caller(skip)
		}
		if ok {
			*err = fmt.Errorf("panic running command %s at %s:%d: %v", cmd.Name, file, line, r)
		} else {
			*err = fmt.Errorf("panic running command %s: %v", cmd.Name, r)
		}
		if err != nil && *err != nil {
			fmt.Fprintf(os.Stderr, "%s\n", *err)
		}
	}
}

// getPanic returns the number of frames to skip and the PC
// for the uppermost panic in the call stack (there might be
// multiple panics when a recover() catches a panic and then
// panics again). The second value indicates how many stack frames
// should be skipped in the stacktrace (they might not always match).
// The last return value indicates a frame could be found.
func getPanic() (int, int, uintptr, bool) {
	skip := 0
	callers := make([]uintptr, 10)
	for {
		calls := callers[:runtime.Callers(skip, callers)]
		c := len(calls)
		if c == 0 {
			break
		}
		for ii := c - 1; ii >= 0; ii-- {
			f := runtime.FuncForPC(calls[ii])
			if f != nil {
				name := f.Name()
				if strings.HasPrefix(name, "runtime.") && strings.Contains(name, "panic") {
					pcSkip := skip + ii - 1
					stackSkip := pcSkip
					switch name {
					case "runtime.panic":
					case "runtime.sigpanic":
						stackSkip -= 2
					default:
						stackSkip--
					}
					return pcSkip, stackSkip, calls[ii], true
				}
			}
		}
		skip += c
	}
	return 0, 0, 0, false
}
