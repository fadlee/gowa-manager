import { readFile } from 'node:fs/promises'
import { describe, expect, test } from 'bun:test'

function extractOnBlock(workflow: string): string {
  const match = workflow.match(/^on:\r?\n([\s\S]*?)(?=^permissions:)/m)
  if (!match) throw new Error('workflow does not contain an on block before permissions')
  return match[1]
}

function extractSection(workflow: string, start: string, end: string): string {
  const startIndex = workflow.indexOf(start)
  const endIndex = workflow.indexOf(end, startIndex + start.length)
  if (startIndex === -1 || endIndex === -1) throw new Error(`workflow section ${start} not found`)
  return workflow.slice(startIndex, endIndex)
}

describe('CI workflow triggers', () => {
  test('go-test is reusable or manual only to avoid duplicate PR and push runs', async () => {
    const workflow = await readFile('.github/workflows/go-test.yml', 'utf8')
    const onBlock = extractOnBlock(workflow)

    expect(onBlock).toContain('workflow_call:')
    expect(onBlock).toContain('workflow_dispatch:')
    expect(onBlock).not.toContain('push:')
    expect(onBlock).not.toContain('pull_request:')
  })

  test('linux race job leaves contract and ops packages to explicit non-race steps', async () => {
    const workflow = await readFile('.github/workflows/go-test.yml', 'utf8')

    expect(workflow).toContain('go test -race -p 1 ./cmd/... ./internal/...')
    expect(workflow).toContain('go test ./test/contract/...')
    expect(workflow).toContain('go test ./test/ops/...')
    expect(workflow).toContain('go test ./internal/supervisor/... ./internal/instances/...')
    expect(workflow).toContain('go test ./internal/monitoring/... ./internal/scheduler/...')
    const arm64Section = extractSection(workflow, '  go-linux-arm64:', '  go-windows-amd64:')
    expect(arm64Section).not.toContain('go test ./test/contract/...')
    expect(workflow).not.toContain('go test -race ./...')
    expect(workflow).not.toContain('go test -race ./test/contract/...')
    expect(workflow).not.toContain('go test -race ./test/ops/...')
  })
})
