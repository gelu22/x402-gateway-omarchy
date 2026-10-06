// qml-structure.test.mjs — static ratchets for QML structure (Etap 53).
// Class A (53.1): signal handlers with params must use function(...) or (...) =>
// Classes B–D added by 53.2–53.4.
import assert from "node:assert/strict"
import fs from "node:fs"
import path from "node:path"
import test from "node:test"
import { fileURLToPath } from "node:url"

const here = path.dirname(fileURLToPath(import.meta.url))
const qmlDir = path.join(here, "..")

function listProductQml() {
  return fs
    .readdirSync(qmlDir)
    .filter((f) => f.endsWith(".qml"))
    .map((f) => path.join(qmlDir, f))
}

function stripLineComment(line) {
  const t = line.trimStart()
  if (t.startsWith("//")) return ""
  return line
}

/** signal foo(a, b) → { name: "foo", arity: 2 }; signal bar() ignored */
function collectSignalsWithParams(files) {
  const names = new Set(["exited"]) // Quickshell Process.exited(int, ExitStatus)
  const re = /^\s*signal\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(([^)]*)\)/
  for (const file of files) {
    const text = fs.readFileSync(file, "utf8")
    for (const raw of text.split("\n")) {
      const line = stripLineComment(raw)
      const m = line.match(re)
      if (!m) continue
      const params = m[2].trim()
      if (params.length === 0) continue
      names.add(m[1])
    }
  }
  return names
}

function handlerName(signalName) {
  return "on" + signalName.charAt(0).toUpperCase() + signalName.slice(1)
}

/**
 * Find onX: handlers for signals that require formal params.
 * Returns [{file, line, signal, snippet}] for illegal forms.
 */
function findInjectionHandlers(files, signalsWithParams) {
  const findings = []
  const onNames = new Map()
  for (const s of signalsWithParams) {
    onNames.set(handlerName(s), s)
  }
  // onFoo: <expr>  — capture rest of line after colon
  const re = /^(\s*)on([A-Z][A-Za-z0-9_]*)\s*:\s*(.*)$/
  for (const file of files) {
    const lines = fs.readFileSync(file, "utf8").split("\n")
    for (let i = 0; i < lines.length; i++) {
      const raw = lines[i]
      if (stripLineComment(raw) === "" && raw.trimStart().startsWith("//")) continue
      const m = raw.match(re)
      if (!m) continue
      const onKey = "on" + m[2]
      if (!onNames.has(onKey)) continue
      let expr = m[3].trim()
      // Multi-line block starting with { is still injection for parameterized signals
      const ok =
        expr.startsWith("function") ||
        /^\([^)]*\)\s*=>/.test(expr) ||
        /^[A-Za-z_][A-Za-z0-9_]*\s*=>/.test(expr)
      if (!ok) {
        findings.push({
          file: path.basename(file),
          line: i + 1,
          signal: onNames.get(onKey),
          snippet: raw.trim(),
        })
      }
    }
  }
  return findings
}

test("class A: parameterized signal handlers declare formal params", () => {
  const files = listProductQml()
  const signals = collectSignalsWithParams(files)
  assert.ok(signals.has("submitEmail"), "submitEmail must be discovered")
  assert.ok(signals.has("exited"), "exited is always parameterized")
  const bad = findInjectionHandlers(files, signals)
  assert.deepEqual(
    bad,
    [],
    bad.map((f) => `${f.file}:${f.line} ${f.snippet}`).join("\n")
  )
})

test("class A ratchet: bare onSubmitEmail is rejected", () => {
  const files = listProductQml()
  const signals = collectSignalsWithParams(files)
  const tmp = path.join(here, "_tmp-panel-injection.qml")
  const body = `Item {
    signal submitEmail(string email)
    onSubmitEmail: root.submitEmail(email)
}
`
  fs.writeFileSync(tmp, body)
  try {
    const bad = findInjectionHandlers([tmp], signals)
    assert.equal(bad.length, 1)
    assert.equal(bad[0].line, 3)
  } finally {
    fs.unlinkSync(tmp)
  }
})

const NON_ITEM_ROOTS = new Set([
  "PanelWindow",
  "FloatingWindow",
  "PopupWindow",
  "QtObject",
  "Scope",
  "ShellRoot",
])

const ATTACHED_RE = /^\s*(Keys|Accessible|KeyNavigation)\./

/** Depth-1 lines with Keys./Accessible./KeyNavigation. when root is non-Item. */
function findAttachedOnNonItemRoot(file) {
  const lines = fs.readFileSync(file, "utf8").split("\n")
  let root = null
  let depth = 0
  const findings = []
  for (let i = 0; i < lines.length; i++) {
    const raw = lines[i]
    const trimmed = stripLineComment(raw)
    if (!root) {
      const m = trimmed.match(/^([A-Za-z_][A-Za-z0-9_.]*)\s*\{/)
      if (m) {
        root = m[1]
        depth = 1
        continue
      }
    }
    if (!root) continue
    // Count braces on the physical line (comments already blanked for leading //)
    const code = stripLineComment(raw)
    if (depth === 1 && ATTACHED_RE.test(code) && NON_ITEM_ROOTS.has(root)) {
      findings.push({ file: path.basename(file), line: i + 1, snippet: raw.trim() })
    }
    for (const ch of code) {
      if (ch === "{") depth++
      else if (ch === "}") depth--
    }
  }
  return findings
}

test("class C: Keys/Accessible not on non-Item root (depth 1)", () => {
  const bad = []
  for (const file of listProductQml()) {
    bad.push(...findAttachedOnNonItemRoot(file))
  }
  assert.deepEqual(
    bad,
    [],
    bad.map((f) => `${f.file}:${f.line} ${f.snippet}`).join("\n")
  )
})

test("class C ratchet: Keys on PanelWindow root is rejected", () => {
  const tmp = path.join(here, "_tmp-keys-root.qml")
  fs.writeFileSync(
    tmp,
    `PanelWindow {
    Keys.onEscapePressed: root.close()
    Item {}
}
`
  )
  try {
    const bad = findAttachedOnNonItemRoot(tmp)
    assert.equal(bad.length, 1)
    assert.equal(bad[0].line, 2)
  } finally {
    fs.unlinkSync(tmp)
  }
})

/** Object-child lines at depth 1 under QtObject without `property ` / `component `. */
function findQtObjectObjectChildren(file) {
  const lines = fs.readFileSync(file, "utf8").split("\n")
  let root = null
  let depth = 0
  const findings = []
  const childRe = /^\s*(?:component\s+)?([A-Z][A-Za-z0-9_.]*)\s*\{/
  for (let i = 0; i < lines.length; i++) {
    const raw = lines[i]
    const code = stripLineComment(raw)
    if (!root) {
      const m = code.match(/^QtObject\s*\{/)
      if (m) {
        root = "QtObject"
        depth = 1
        continue
      }
      continue
    }
    if (depth === 1) {
      const trimmed = code.trimStart()
      if (
        trimmed &&
        !trimmed.startsWith("property ") &&
        !trimmed.startsWith("component ") &&
        !trimmed.startsWith("id:") &&
        !trimmed.startsWith("signal ") &&
        !trimmed.startsWith("function ") &&
        !trimmed.startsWith("readonly ") &&
        childRe.test(trimmed)
      ) {
        findings.push({ file: path.basename(file), line: i + 1, snippet: raw.trim() })
      }
    }
    for (const ch of code) {
      if (ch === "{") depth++
      else if (ch === "}") depth--
    }
  }
  return findings
}

test("class B: QtObject roots have no bare object children", () => {
  const bad = []
  for (const file of listProductQml()) {
    bad.push(...findQtObjectObjectChildren(file))
  }
  assert.deepEqual(
    bad,
    [],
    bad.map((f) => `${f.file}:${f.line} ${f.snippet}`).join("\n")
  )
})

test("class B ratchet: fixture QtObject { Timer {} } is rejected", () => {
  const fixture = path.join(here, "fixtures", "qtobject-child.qml")
  const bad = findQtObjectObjectChildren(fixture)
  assert.equal(bad.length, 1, JSON.stringify(bad))
  assert.equal(bad[0].line, 3)
})

const ITEMISH_ROOTS = new Set(["Item", "Rectangle", "MouseArea", "FocusScope"])
const FORBIDDEN_PROP_NAMES = new Set([
  "state",
  "states",
  "transitions",
  "data",
  "children",
  "resources",
  "parent",
  "visible",
  "enabled",
  "opacity",
  "clip",
  "focus",
  "x",
  "y",
  "z",
  "width",
  "height",
  "scale",
  "rotation",
])

function findShadowingProperties(file) {
  const lines = fs.readFileSync(file, "utf8").split("\n")
  let root = null
  const findings = []
  const propRe =
    /^\s*(?:readonly\s+)?property\s+[A-Za-z0-9_<>,\s*]+\s+([A-Za-z_][A-Za-z0-9_]*)\b/
  for (let i = 0; i < lines.length; i++) {
    const code = stripLineComment(lines[i])
    if (!root) {
      const m = code.match(/^([A-Za-z_][A-Za-z0-9_.]*)\s*\{/)
      if (m) root = m[1]
      continue
    }
    if (!ITEMISH_ROOTS.has(root)) continue
    const m = code.match(propRe)
    if (m && FORBIDDEN_PROP_NAMES.has(m[1])) {
      findings.push({
        file: path.basename(file),
        line: i + 1,
        name: m[1],
        snippet: lines[i].trim(),
      })
    }
  }
  return findings
}

test("class D: no Item built-in name shadows", () => {
  const bad = []
  for (const file of listProductQml()) {
    bad.push(...findShadowingProperties(file))
  }
  assert.deepEqual(
    bad,
    [],
    bad.map((f) => `${f.file}:${f.line} ${f.snippet}`).join("\n")
  )
})

test("class D ratchet: property string state on Item is rejected", () => {
  const tmp = path.join(here, "_tmp-state-shadow.qml")
  fs.writeFileSync(tmp, `Item {\n    property string state: "offline"\n}\n`)
  try {
    const bad = findShadowingProperties(tmp)
    assert.equal(bad.length, 1)
    assert.equal(bad[0].name, "state")
  } finally {
    fs.unlinkSync(tmp)
  }
})

// Class E (59.1 / 60.1): Text showing untrusted/URL-shaped data must set PlainText.
// AutoText parses HTML and can fetch loopback before daemon SSRF (#10216, #10220).
const UNTRUSTED_TEXT_RE =
  /\b(targetUrl|MFA_RESET_URL|accountNetworkLine|walletAddress|modelData\.(host|reason|name)|\bmodelData\b|\.secret\b|root\.text\b|errorMessage|alertText)\b/

/**
 * Find Text { ... text: <untrusted> ... } blocks missing textFormat: Text.PlainText.
 * Only the text: binding line is matched (visible:/other props may mention targetUrl).
 */
function findUntrustedTextWithoutPlain(file) {
  const lines = fs.readFileSync(file, "utf8").split("\n")
  const findings = []
  for (let i = 0; i < lines.length; i++) {
    const code = stripLineComment(lines[i])
    if (!/^\s*Text\s*\{/.test(code)) continue
    let depth = 0
    let textBinding = ""
    let hasPlain = false
    let end = i
    for (let j = i; j < lines.length; j++) {
      const line = stripLineComment(lines[j])
      for (const ch of line) {
        if (ch === "{") depth++
        else if (ch === "}") depth--
      }
      end = j
      const textM = line.match(/^\s*text\s*:\s*(.*)$/)
      if (textM) {
        textBinding = textM[1]
        // Multi-line text: continuations (indented, no new property key)
        for (let k = j + 1; k < lines.length; k++) {
          const cont = stripLineComment(lines[k])
          if (/^\s*(?:\/\/|$)/.test(cont)) continue
          if (/^\s*[A-Za-z_]/.test(cont) && !/^\s*[+\-]/.test(cont.trimStart())) break
          if (/^\s*[+\-]/.test(cont) || cont.trimStart().startsWith('"') || cont.trimStart().startsWith("'")) {
            textBinding += " " + cont.trim()
            continue
          }
          break
        }
      }
      if (/textFormat\s*:\s*Text\.PlainText/.test(line)) hasPlain = true
      if (depth === 0 && j > i) break
      if (depth === 0 && j === i && line.includes("}")) break
    }
    if (!textBinding) continue
    if (!UNTRUSTED_TEXT_RE.test(textBinding)) continue
    // Skip ternary that only uses modelData for a boolean gate to constant glyphs
    if (/modelData\.integrated\s*\?\s*["']/.test(textBinding)) continue
    if (!hasPlain) {
      findings.push({
        file: path.basename(file),
        line: i + 1,
        endLine: end + 1,
        snippet: lines[i].trim(),
      })
    }
  }
  return findings
}

test("class E: untrusted Text bindings use Text.PlainText", () => {
  const bad = []
  for (const file of listProductQml()) {
    bad.push(...findUntrustedTextWithoutPlain(file))
  }
  assert.deepEqual(
    bad,
    [],
    bad.map((f) => `${f.file}:${f.line} ${f.snippet}`).join("\n")
  )
})

test("class E ratchet: Text { text: root.targetUrl } without PlainText is rejected", () => {
  const tmp = path.join(here, "_tmp-plaintext.qml")
  fs.writeFileSync(
    tmp,
    `Item {
    Text {
        text: root.targetUrl
        color: "white"
    }
}
`
  )
  try {
    const bad = findUntrustedTextWithoutPlain(tmp)
    assert.equal(bad.length, 1)
    assert.equal(bad[0].line, 2)
  } finally {
    fs.unlinkSync(tmp)
  }
})

test("class E ratchet: Text { text: root.text } without PlainText is rejected", () => {
  const tmp = path.join(here, "_tmp-plaintext-banner.qml")
  fs.writeFileSync(
    tmp,
    `Item {
    Text {
        text: root.text
        color: "red"
    }
}
`
  )
  try {
    const bad = findUntrustedTextWithoutPlain(tmp)
    assert.equal(bad.length, 1)
    assert.equal(bad[0].line, 2)
  } finally {
    fs.unlinkSync(tmp)
  }
})
