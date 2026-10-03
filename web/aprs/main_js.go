// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build js

// Command aprs is the in-browser APRS encoder/decoder: built with
// GOOS=js GOARCH=wasm, it registers samoyedEncode and samoyedDecode on the
// page's global object for app.js to call.
//
// The decoder prints, as samoyed-decode_aprs does, rather than returning a
// string; app.js collects what it writes to standard output and error.
package main

import (
	"encoding/json"
	"syscall/js"

	"github.com/sirupsen/logrus"
)

func main() {
	// The time of each complaint is just noise on the page.
	logrus.SetFormatter(&logrus.TextFormatter{DisableTimestamp: true}) //nolint:exhaustruct // the zero values are logrus's defaults

	var decoder = newDecoder()

	js.Global().Set("samoyedDecode", js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) != 1 {
			return nil
		}

		Decode(decoder, args[0].String())

		return nil
	}))

	js.Global().Set("samoyedEncode", js.FuncOf(func(_ js.Value, args []js.Value) any {
		var result = map[string]any{"line": "", "error": ""}

		var req = new(EncodeRequest)

		if len(args) != 1 {
			result["error"] = "samoyedEncode takes one argument"
		} else if err := json.Unmarshal([]byte(args[0].String()), req); err != nil {
			result["error"] = err.Error()
		} else if line, err := Encode(req); err != nil {
			result["error"] = err.Error()
		} else {
			result["line"] = line
		}

		return js.ValueOf(result)
	}))

	js.Global().Call("dispatchEvent", js.Global().Get("Event").New("samoyedready"))

	select {} // Keep the functions above callable.
}
