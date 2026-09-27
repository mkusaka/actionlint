//go:build wasm

package main

import (
	"io"
	"syscall/js"

	"github.com/mkusaka/yactionlint"
)

var (
	window = js.Global().Get("window")
)

func fail(err error, when string) {
	window.Call("showError", err.Error()+" on "+when)
}

func encodeErrorAsMap(err *actionlint.Error) map[string]interface{} {
	obj := make(map[string]interface{}, 4)
	obj["message"] = err.Message
	obj["line"] = err.Line
	obj["column"] = err.Column
	obj["kind"] = err.Kind
	return obj
}

func lint(source, format string) interface{} {
	opts := actionlint.LinterOptions{InputFormat: actionlint.FileWorkflow}
	path := "test.yaml"
	if format == "action" {
		opts.InputFormat = actionlint.FileAction
		path = "action.yml"
	}
	linter, err := actionlint.NewLinter(io.Discard, &opts)
	if err != nil {
		fail(err, "creating linter instance")
		return nil
	}

	errs, err := linter.Lint(path, []byte(source), nil)
	if err != nil {
		fail(err, "applying lint rules")
		return nil
	}

	ret := make([]interface{}, 0, len(errs))
	for _, err := range errs {
		ret = append(ret, encodeErrorAsMap(err))
	}

	window.Call("onCheckCompleted", js.ValueOf(ret))

	return nil
}

func runActionlint(_this js.Value, args []js.Value) interface{} {
	format := "workflow"
	if len(args) > 1 {
		format = args[1].String()
	}
	return lint(args[0].String(), format)
}

func main() {
	window.Set("runActionlint", js.FuncOf(runActionlint))
	window.Call("dismissLoading")
	lint(window.Call("getYamlSource").String(), window.Call("getInputFormat").String()) // Show the first result
	select {}
}
