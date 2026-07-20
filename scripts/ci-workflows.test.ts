import { readFile } from 'node:fs/promises'
import { describe, expect, test } from 'bun:test'

function extractOnBlock(workflow: string): string {
  const match = workflow.match(/^on:\r?\n([\s\S]*?)(?=^permissions:)/m)
  if (!match) throw new Error('workflow does not contain an on block before permissions')
  return match[1]
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

    expect(workflow).toContain('go test -race ./cmd/... ./internal/...')
    expect(workflow).toContain('go test ./test/contract/...')
    expect(workflow).toContain('go test ./test/ops/...')
    expect(workflow).not.toContain('go test -race ./...')
    expect(workflow).not.toContain('go test -race ./test/contract/...')
    expect(workflow).not.toContain('go test -race ./test/ops/...')
  })
})
