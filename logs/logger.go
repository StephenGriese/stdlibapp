package logs

import (
	"context"
	"fmt"
	"github.com/StephenGriese/stdlibapp/dictionary"
)

func NewLogger() dictionary.Logger {
	return logger{}
}

type logger struct{}

var _ dictionary.Logger = logger{}

func (l logger) Info(_ context.Context, msg string, keyvals ...any) {
	txt := []interface{}{msg}
	txt = append(txt, keyvals)
	fmt.Println(txt)
}
