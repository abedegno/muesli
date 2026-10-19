// Extract explicit UI literals; never translate arbitrary note/transcript data.
// --write wraps new JSX/copy and refreshes the English source catalogue.
import fs from 'node:fs'
import path from 'node:path'
import ts from 'typescript'
import { JSDOM } from 'jsdom'
const write = process.argv.includes('--write')
const decoder = new JSDOM('').window.document.createElement('textarea')
const decode = (text) => {
  decoder.innerHTML = text
  return decoder.value
}
const messages = new Set()
const attributes = new Set([
  'title',
  'label',
  'hint',
  'placeholder',
  'aria-label',
  'ariaLabel',
  'alt',
  'description',
])
const files = []
function walk(dir) {
  for (const item of fs.readdirSync(dir, { withFileTypes: true })) {
    const p = path.join(dir, item.name)
    if (item.isDirectory()) {
      if (item.name !== 'i18n') walk(p)
    } else if (/\.tsx?$/.test(p) && !p.includes('.test.') && !p.includes('test-utils'))
      files.push(p)
  }
}
walk('src/renderer')
walk('web/admin/src')
function cleanJsx(text) {
  const lines = text.split(/\r\n|\n|\r/)
  let last = 0
  for (let i = 0; i < lines.length; i++) if (/[^ \t]/.test(lines[i])) last = i
  let result = ''
  lines.forEach((line, i) => {
    line = line.replace(/\t/g, ' ')
    if (i !== 0) line = line.replace(/^ +/, '')
    if (i !== lines.length - 1) line = line.replace(/ +$/, '')
    if (line) result += line + (i !== last ? ' ' : '')
  })
  return decode(result)
}
for (const file of files) {
  const input = fs.readFileSync(file, 'utf8'),
    source = ts.createSourceFile(
      file,
      input,
      ts.ScriptTarget.Latest,
      true,
      file.endsWith('.tsx') ? ts.ScriptKind.TSX : ts.ScriptKind.TS
    )
  const edits = new Map()
  const add = (node, text, replacement) => {
    if (!text.trim()) return
    messages.add(text)
    edits.set(node.getStart(source), { end: node.end, text: replacement })
  }
  function display(node) {
    if (!node) return
    if (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) {
      const text = node.text
      add(node, text, `uiText(${JSON.stringify(text)})`)
      return
    }
    if (ts.isTemplateExpression(node)) {
      let text = node.head.text
      const values = []
      node.templateSpans.forEach((span, index) => {
        const name = `value${index + 1}`
        text += `{${name}}` + span.literal.text
        values.push(`${name}: ${span.expression.getText(source)}`)
      })
      add(node, text, `uiText(${JSON.stringify(text)}, {${values.join(', ')}})`)
      return
    }
    if (ts.isConditionalExpression(node)) {
      display(node.whenTrue)
      display(node.whenFalse)
      return
    }
    if (
      ts.isBinaryExpression(node) &&
      [
        ts.SyntaxKind.QuestionQuestionToken,
        ts.SyntaxKind.BarBarToken,
        ts.SyntaxKind.AmpersandAmpersandToken,
      ].includes(node.operatorToken.kind)
    ) {
      display(node.right)
      return
    }
    if (ts.isParenthesizedExpression(node)) display(node.expression)
  }
  function visit(node) {
    if (ts.isJsxText(node)) {
      const parent = node.parent
      const tag = ts.isJsxElement(parent) ? parent.openingElement.tagName.getText(source) : ''
      if (!['style', 'script', 'code', 'pre'].includes(tag)) {
        const cleaned = cleanJsx(node.text)
        if (cleaned.trim()) {
          const key = cleaned.trim()
          messages.add(key)
          edits.set(node.pos, {
            end: node.end,
            text: `${cleaned.startsWith(' ') ? "{' '}" : ''}{uiText(${JSON.stringify(key)})}${cleaned.endsWith(' ') ? "{' '}" : ''}`,
          })
        }
      }
    }
    if (
      ts.isJsxAttribute(node) &&
      attributes.has(node.name.getText(source)) &&
      node.initializer &&
      ts.isStringLiteral(node.initializer)
    ) {
      const text = decode(node.initializer.text)
      add(node.initializer, text, `{uiText(${JSON.stringify(text)})}`)
    }
    if (ts.isJsxExpression(node) && node.expression) {
      const parent = node.parent
      if (ts.isJsxAttribute(parent)) {
        if (attributes.has(parent.name.getText(source))) display(node.expression)
      } else {
        const tag = ts.isJsxElement(parent) ? parent.openingElement.tagName.getText(source) : ''
        if (!['style', 'script', 'code', 'pre'].includes(tag)) display(node.expression)
      }
    }
    if (
      ts.isPropertyAssignment(node) &&
      attributes.has(node.name.getText(source).replace(/^['"]|['"]$/g, ''))
    )
      display(node.initializer)
    if (ts.isCallExpression(node)) {
      const name = node.expression.getText(source)
      if (
        name === 'uiText' &&
        node.arguments[0] &&
        (ts.isStringLiteral(node.arguments[0]) ||
          ts.isNoSubstitutionTemplateLiteral(node.arguments[0]))
      )
        messages.add(node.arguments[0].text)
      if (['setError', 'setMessage'].includes(name)) {
        const message = node.arguments[0]
        if (message && (ts.isStringLiteral(message) || ts.isNoSubstitutionTemplateLiteral(message)))
          if (message.text.trim()) messages.add(message.text)
      }
      if (name === 'notify' || name.endsWith('.notify') || name === 'announce')
        display(node.arguments[0])
    }
    if (ts.isNewExpression(node) && node.expression.getText(source) === 'Error') {
      const message = node.arguments?.[0]
      if (message && (ts.isStringLiteral(message) || ts.isNoSubstitutionTemplateLiteral(message)))
        if (message.text.trim()) messages.add(message.text)
    }
    ts.forEachChild(node, visit)
  }
  visit(source)
  if (edits.size) {
    let output = input
    const sorted = [...edits].sort(([a], [b]) => b - a)
    let previous = input.length
    for (const [start, edit] of sorted) {
      if (edit.end > previous) throw new Error(`Overlapping UI edits in ${file}`)
      output = output.slice(0, start) + edit.text + output.slice(edit.end)
      previous = start
    }
    if (!/import\s*\{[^}]*\buiText\b[^}]*\}\s*from\s*['"]@\/i18n['"]/.test(input))
      output = "import { uiText } from '@/i18n'\n" + output
    if (write) fs.writeFileSync(file, output)
    else {
      console.error('Unlocalized UI copy:', file)
      process.exitCode = 1
    }
  }
}
const enSource = ts.createSourceFile(
  'en.ts',
  fs.readFileSync('src/renderer/i18n/en.ts', 'utf8'),
  ts.ScriptTarget.Latest,
  true
)
function collect(node) {
  if (
    ts.isPropertyAssignment(node) &&
    (ts.isStringLiteral(node.initializer) || ts.isNoSubstitutionTemplateLiteral(node.initializer))
  )
    messages.add(node.initializer.text)
  ts.forEachChild(node, collect)
}
collect(enSource)
for (const text of ['Open Muesli', 'Quit Muesli', 'AI summaries and chat', 'Semantic search'])
  messages.add(text)
const list = [...messages].sort(),
  target = 'src/renderer/i18n/messages.json'
if (write)
  fs.writeFileSync(
    target,
    JSON.stringify(Object.fromEntries(list.map((text) => [text, text])), null, 2) + '\n'
  )
else {
  const saved = JSON.parse(fs.readFileSync(target, 'utf8'))
  const missing = list.filter((key) => !(key in saved))
  if (missing.length) {
    console.error('Uncatalogued UI messages:', missing)
    process.exitCode = 1
  }
}
console.log(`${list.length} UI messages across ${files.length} renderer files`)
