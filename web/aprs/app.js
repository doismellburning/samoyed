// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

// Glue between the page and samoyed.wasm, which registers samoyedDecode and
// samoyedEncode on globalThis once it is running.
//
// The decoder prints, as samoyed-decode_aprs does, rather than returning a
// string.  Go on js/wasm writes standard output and error through
// globalThis.fs.writeSync (see wasm_exec.js), so withCapture swaps that out
// for the length of a call and collects what was written.

"use strict";

const $ = (id) => document.getElementById(id);

const statusEl = $("status");

// --- Capturing Go's output ---

const utf8 = new TextDecoder("utf-8");
const originalWriteSync = globalThis.fs.writeSync.bind(globalThis.fs);
let captured = null; // [{fd, text}] while a call is being captured

globalThis.fs.writeSync = (fd, buf) => {
	if (captured === null) {
		return originalWriteSync(fd, buf);
	}

	const text = utf8.decode(buf);
	const last = captured[captured.length - 1];
	if (last && last.fd === fd) {
		last.text += text;
	} else {
		captured.push({ fd, text });
	}

	return buf.length;
};

function withCapture(f) {
	captured = [];
	try {
		const result = f();
		return { result, output: captured };
	} finally {
		captured = null;
	}
}

// Put captured output into a <pre>: standard error (logrus's complaints) is
// styled apart from standard output.
function render(pre, output) {
	pre.replaceChildren(...output.map(({ fd, text }) => {
		const span = document.createElement("span");
		span.textContent = text;
		if (fd === 2) {
			span.className = "stderr";
		}
		return span;
	}));
}

// --- Decode ---

const decodeForm = $("decode-form");
const decodeInput = $("decode-input");
const decodeOutput = $("decode-output");

function decode() {
	const { output } = withCapture(() => globalThis.samoyedDecode(decodeInput.value));
	render(decodeOutput, output);
}

decodeForm.addEventListener("submit", (e) => {
	e.preventDefault();
	decode();
});

decodeInput.addEventListener("keydown", (e) => {
	if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) {
		e.preventDefault();
		decodeForm.requestSubmit();
	}
});

$("decode-clear").addEventListener("click", () => {
	decodeInput.value = "";
	decodeOutput.replaceChildren();
	decodeInput.focus();
});

// --- Encode ---

const encodeForm = $("encode-form");
const encodeResult = $("encode-result");
const encodeOutput = $("encode-output");
const encodeLog = $("encode-log");
const symbolPreset = $("symbol-preset");

const numberFields = [
	"lat", "lon", "ambiguity", "altitudeFt", "course", "speedKnots",
	"power", "height", "gain", "freq", "tone", "offset",
];
const checkboxFields = ["compressed", "messaging"];

function packetType() {
	return encodeForm.elements.type.value;
}

// Show only the fieldsets the chosen packet type uses.
function showFieldsForType() {
	const type = packetType();
	const show = {
		"only-position": type === "position",
		"only-object": type === "object",
		"only-message": type === "message",
		"only-located": type !== "message",
	};
	for (const [cls, visible] of Object.entries(show)) {
		for (const el of encodeForm.getElementsByClassName(cls)) {
			el.hidden = !visible;
			// Hidden required fields would otherwise block submission.
			for (const input of el.querySelectorAll("input, select")) {
				input.disabled = !visible;
			}
		}
	}
}

function encodeRequest() {
	const req = {};
	for (const el of encodeForm.elements) {
		if (!el.name || el.disabled || el.type === "radio") {
			continue;
		}
		if (checkboxFields.includes(el.name)) {
			req[el.name] = el.checked;
		} else if (numberFields.includes(el.name)) {
			// An empty optional number is left out, which Go sees as absent.
			if (el.value !== "") {
				req[el.name] = Number(el.value);
			}
		} else if (el.name === "timestamp") {
			if (el.value !== "") {
				req.timestamp = new Date(el.value).toISOString();
			}
		} else {
			req[el.name] = el.value;
		}
	}
	req.type = packetType();
	return req;
}

encodeForm.addEventListener("change", (e) => {
	if (e.target.name === "type") {
		showFieldsForType();
	}
});

symbolPreset.addEventListener("change", () => {
	const value = symbolPreset.value;
	if (value.length === 2) {
		encodeForm.elements.symbolTable.value = value[0];
		encodeForm.elements.symbol.value = value[1];
	} else {
		encodeForm.elements.symbolTable.focus();
	}
});

for (const name of ["symbolTable", "symbol"]) {
	encodeForm.elements[name].addEventListener("input", () => {
		const value = encodeForm.elements.symbolTable.value + encodeForm.elements.symbol.value;
		const match = [...symbolPreset.options].find((o) => o.value === value);
		symbolPreset.value = match ? match.value : "";
	});
}

encodeForm.addEventListener("submit", (e) => {
	e.preventDefault();

	const { result, output } = withCapture(() => globalThis.samoyedEncode(JSON.stringify(encodeRequest())));

	if (result.error) {
		encodeOutput.textContent = "Error: " + result.error;
		encodeOutput.classList.add("error");
	} else {
		encodeOutput.textContent = result.line;
		encodeOutput.classList.remove("error");
	}
	$("encode-copy").disabled = $("encode-decode").disabled = Boolean(result.error);
	encodeResult.hidden = false;

	// Warnings, e.g. about an unusual symbol, that didn't stop it encoding.
	render(encodeLog, output);
	encodeLog.hidden = output.length === 0;
});

$("encode-copy").addEventListener("click", async () => {
	try {
		await navigator.clipboard.writeText(encodeOutput.textContent);
		$("encode-copy").textContent = "Copied";
		setTimeout(() => { $("encode-copy").textContent = "Copy"; }, 1500);
	} catch {
		window.getSelection().selectAllChildren(encodeOutput);
	}
});

$("encode-decode").addEventListener("click", () => {
	decodeInput.value = encodeOutput.textContent;
	decode();
	$("decode-section").scrollIntoView({ behavior: "smooth" });
});

showFieldsForType();

// --- Start up ---

async function start() {
	const go = new Go();
	const wasm = fetch("samoyed.wasm");
	let instance;
	try {
		({ instance } = await WebAssembly.instantiateStreaming(wasm, go.importObject));
	} catch {
		// A server that doesn't send application/wasm defeats instantiateStreaming.
		({ instance } = await WebAssembly.instantiate(await (await fetch("samoyed.wasm")).arrayBuffer(), go.importObject));
	}

	const ready = new Promise((resolve) => globalThis.addEventListener("samoyedready", resolve, { once: true }));
	go.run(instance).then(() => {
		statusEl.textContent = "The decoder stopped unexpectedly; reload the page to start it again.";
		statusEl.className = "error";
	});
	await ready;

	statusEl.textContent = "";
	statusEl.hidden = true;
	for (const button of document.querySelectorAll("button[type=submit]")) {
		button.disabled = false;
	}
	decode();
}

start().catch((err) => {
	statusEl.textContent = "Could not start the decoder: " + err;
	statusEl.className = "error";
});
