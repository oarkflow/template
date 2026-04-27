//go:build js && wasm

package main

import (
	"encoding/json"
	"sync"
	"syscall/js"

	"github.com/oarkflow/template"
)

var (
	rendererMu sync.Mutex
	renderer   *template.Engine
	entryFile  string
	lastError  string
)

func main() {
	core := map[string]any{
		"init":         js.FuncOf(jsInit),
		"render":       js.FuncOf(jsRender),
		"setSignal":    js.FuncOf(jsSetSignal),
		"getSignal":    js.FuncOf(jsGetSignal),
		"getSignals":   js.FuncOf(jsGetSignals),
		"getHandlers":  js.FuncOf(jsGetHandlers),
		"getLastError": js.FuncOf(jsGetLastError),
	}
	js.Global().Set("SPLWASMCore", js.ValueOf(core))
	select {}
}

func jsInit(this js.Value, args []js.Value) any {
	rendererMu.Lock()
	defer rendererMu.Unlock()
	clearLastError()

	var bundle template.Bundle
	if err := json.Unmarshal([]byte(args[0].String()), &bundle); err != nil {
		setLastError(err)
		return ""
	}
	data := map[string]any{}
	if len(args) > 1 && args[1].String() != "" {
		if err := json.Unmarshal([]byte(args[1].String()), &data); err != nil {
			setLastError(err)
			return ""
		}
	}
	globals := map[string]any{}
	if len(args) > 3 && args[3].String() != "" {
		if err := json.Unmarshal([]byte(args[3].String()), &globals); err != nil {
			setLastError(err)
			return ""
		}
	}
	engine := template.New()
	if err := engine.LoadBundle(bundle); err != nil {
		setLastError(err)
		return ""
	}
	for k, v := range globals {
		engine.Globals[k] = v
	}
	entry := bundle.Entry
	if len(args) > 2 && args[2].String() != "" {
		entry = args[2].String()
	}
	out, err := engine.RenderBrowserFile(entry, data)
	if err != nil {
		setLastError(err)
		return ""
	}
	renderer = engine
	entryFile = entry
	return out
}

func jsRender(this js.Value, args []js.Value) any {
	rendererMu.Lock()
	defer rendererMu.Unlock()
	clearLastError()
	if renderer == nil {
		return ""
	}
	out, err := renderer.RenderBrowserFile(entryFile, nil)
	if err != nil {
		setLastError(err)
		return ""
	}
	return out
}

func jsSetSignal(this js.Value, args []js.Value) any {
	rendererMu.Lock()
	defer rendererMu.Unlock()
	clearLastError()
	if renderer == nil || len(args) < 2 {
		return nil
	}
	var value any
	if err := json.Unmarshal([]byte(args[1].String()), &value); err != nil {
		setLastError(err)
		return nil
	}
	if err := renderer.SetSignal(args[0].String(), value); err != nil {
		setLastError(err)
	}
	return nil
}

func jsGetSignal(this js.Value, args []js.Value) any {
	rendererMu.Lock()
	defer rendererMu.Unlock()
	clearLastError()
	if renderer == nil || len(args) == 0 {
		return "null"
	}
	value, ok := renderer.GetSignal(args[0].String())
	if !ok {
		return "null"
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		setLastError(err)
		return "null"
	}
	return string(encoded)
}

func jsGetSignals(this js.Value, args []js.Value) any {
	rendererMu.Lock()
	defer rendererMu.Unlock()
	clearLastError()
	if renderer == nil {
		return "{}"
	}
	encoded, err := json.Marshal(renderer.BrowserSignals())
	if err != nil {
		setLastError(err)
		return "{}"
	}
	return string(encoded)
}

func jsGetHandlers(this js.Value, args []js.Value) any {
	rendererMu.Lock()
	defer rendererMu.Unlock()
	clearLastError()
	if renderer == nil {
		return "{}"
	}
	encoded, err := json.Marshal(renderer.BrowserHandlers())
	if err != nil {
		setLastError(err)
		return "{}"
	}
	return string(encoded)
}

func jsGetLastError(this js.Value, args []js.Value) any {
	rendererMu.Lock()
	defer rendererMu.Unlock()
	return lastError
}

func clearLastError() {
	lastError = ""
}

func setLastError(err error) {
	if err == nil {
		lastError = ""
		return
	}
	lastError = err.Error()
}
