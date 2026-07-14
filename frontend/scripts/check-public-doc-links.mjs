import { execFileSync } from 'node:child_process'
import { readFileSync } from 'node:fs'
import { dirname, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..', '..')
const publicDocuments = ['README.md', 'README.zh-CN.md']
const trackedFiles = new Set(
  execFileSync('git', ['ls-files'], { cwd: root, encoding: 'utf8' })
    .split(/\r?\n/)
    .filter(Boolean)
)
const findings = []

for (const document of publicDocuments) {
  const body = readFileSync(resolve(root, document), 'utf8')
  for (const match of body.matchAll(/(?<!!)(?:\[[^\]]*\])\(([^)]+)\)/g)) {
    let target = match[1].trim().replace(/^<|>$/g, '')
    if (!target || /^(?:https?:|mailto:|#)/.test(target)) continue
    target = target.split(/[?#]/, 1)[0]
    if (!target) continue

    const resolved = relative(root, resolve(dirname(resolve(root, document)), decodeURIComponent(target)))
    if (!trackedFiles.has(resolved)) {
      findings.push(`${document}: relative link ${match[1]} does not target a published file (${resolved})`)
    }
  }
}

if (findings.length > 0) {
  console.error(findings.join('\n'))
  process.exit(1)
}

console.log(`Public documentation links check passed (${publicDocuments.length} files).`)
