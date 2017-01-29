package eval

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"syscall"

	"github.com/elves/elvish/parse"
	"github.com/elves/elvish/util"
)

// Exception represents an elvish exception. It is both a Value accessible to
// elvishscript, and the type of error returned by public facing evaluation
// methods like (*Evaler)PEval.
type Exception struct {
	Cause     error
	Traceback *util.SourceContext
}

// OK is a pointer to the zero value of Exception, representing the absense of
// exception.
var OK = &Exception{}

func (exc *Exception) Error() string {
	return exc.Cause.Error()
}

func (exc *Exception) Pprint() string {
	buf := new(bytes.Buffer)
	// Error message
	var msg string
	if pprinter, ok := exc.Cause.(util.Pprinter); ok {
		msg = pprinter.Pprint()
	} else {
		msg = "\033[31;1m" + exc.Cause.Error() + "\033[m"
	}
	fmt.Fprintf(buf, "Exception: %s\n", msg)
	buf.WriteString("Traceback:")

	for tb := exc.Traceback; tb != nil; tb = tb.Next {
		buf.WriteString("\n  ")
		tb.Pprint(buf, "    ")
	}

	return buf.String()
}

func (exc *Exception) Kind() string {
	return "exception"
}

func (exc *Exception) Repr(indent int) string {
	if exc.Cause == nil {
		return "$ok"
	}
	if r, ok := exc.Cause.(Reprer); ok {
		return r.Repr(indent)
	}
	return "?(error " + parse.Quote(exc.Cause.Error()) + ")"
}

func (exc *Exception) Bool() bool {
	return exc.Cause == nil
}

// PipelineError represents the errors of pipelines, in which multiple commands
// may error.
type PipelineError struct {
	Errors []*Exception
}

func (pe PipelineError) Repr(indent int) string {
	// TODO Make a more generalized ListReprBuilder and use it here.
	b := new(bytes.Buffer)
	b.WriteString("?(multi-error")
	elemIndent := indent + len("?(multi-error ")
	for _, e := range pe.Errors {
		if indent > 0 {
			b.WriteString("\n" + strings.Repeat(" ", elemIndent))
		} else {
			b.WriteString(" ")
		}
		b.WriteString(e.Repr(elemIndent))
	}
	b.WriteString(")")
	return b.String()
}

func (pe PipelineError) Error() string {
	b := new(bytes.Buffer)
	b.WriteString("(")
	for i, e := range pe.Errors {
		if i > 0 {
			b.WriteString(" | ")
		}
		if e == nil || e.Cause == nil {
			b.WriteString("<nil>")
		} else {
			b.WriteString(e.Error())
		}
	}
	b.WriteString(")")
	return b.String()
}

// Flow is a special type of error used for control flows.
type Flow uint

// Control flows.
const (
	Return Flow = iota
	Break
	Continue
)

var flowNames = [...]string{
	"return", "break", "continue",
}

func (f Flow) Repr(int) string {
	return "?(" + f.Error() + ")"
}

func (f Flow) Error() string {
	if f >= Flow(len(flowNames)) {
		return fmt.Sprintf("!(BAD FLOW: %v)", f)
	}
	return flowNames[f]
}

func (f Flow) Pprint() string {
	return "\033[33;1m" + f.Error() + "\033[m"
}

// ExternalCmdExit contains the exit status of external commands. If the
// command was stopped rather than terminated, the Pid field contains the pid
// of the process.
type ExternalCmdExit struct {
	syscall.WaitStatus
	CmdName string
	Pid     int
}

func NewExternalCmdExit(name string, ws syscall.WaitStatus, pid int) error {
	if ws.Exited() && ws.ExitStatus() == 0 {
		return nil
	}
	if !ws.Stopped() {
		pid = 0
	}
	return ExternalCmdExit{ws, name, pid}
}

func FakeExternalCmdExit(name string, exit int, sig syscall.Signal) ExternalCmdExit {
	return ExternalCmdExit{syscall.WaitStatus(exit<<8 + int(sig)), name, 0}
}

func (exit ExternalCmdExit) Error() string {
	ws := exit.WaitStatus
	quotedName := parse.Quote(exit.CmdName)
	switch {
	case ws.Exited():
		return quotedName + " exited with " + strconv.Itoa(ws.ExitStatus())
	case ws.Signaled():
		msg := quotedName + " killed by signal " + ws.Signal().String()
		if ws.CoreDump() {
			msg += " (core dumped)"
		}
		return msg
	case ws.Stopped():
		msg := quotedName + " stopped by signal " + fmt.Sprintf("%s (pid=%d)", ws.StopSignal(), exit.Pid)
		trap := ws.TrapCause()
		if trap != -1 {
			msg += fmt.Sprintf(" (trapped %v)", trap)
		}
		return msg
	default:
		return fmt.Sprint(quotedName, " has unknown WaitStatus ", ws)
	}
}

func allok(es []*Exception) bool {
	for _, e := range es {
		if e != nil && e.Cause != nil {
			return false
		}
	}
	return true
}