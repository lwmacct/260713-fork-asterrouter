import { createServer } from 'node:http'

const port = Number(process.env.ASTER_E2E_UPSTREAM_PORT || 29000)
let mode = 'normal'

function json(response, status, body) {
  response.writeHead(status, { 'Content-Type': 'application/json' })
  response.end(JSON.stringify(body))
}

async function readJSON(request) {
  const chunks = []
  for await (const chunk of request) chunks.push(chunk)
  return JSON.parse(Buffer.concat(chunks).toString('utf8') || '{}')
}

const server = createServer(async (request, response) => {
  try {
    if (request.method === 'GET' && request.url === '/health') {
      json(response, 200, { status: 'ok', mode })
      return
    }
    if (request.method === 'POST' && request.url === '/__test/mode') {
      const payload = await readJSON(request)
      mode = String(payload.mode || 'normal')
      json(response, 200, { mode })
      return
    }
    if (request.method === 'GET' && request.url === '/v1/models') {
      json(response, 200, { object: 'list', data: [{ id: 'upstream-model', object: 'model' }] })
      return
    }
    if (request.method === 'POST' && request.url === '/v1/chat/completions') {
      const payload = await readJSON(request)
      if (payload.model === 'fail-model') {
        json(response, 500, { error: { type: 'upstream_error', message: 'synthetic route failure' } })
        return
      }
      if (mode === '429') {
        json(response, 429, { error: { type: 'rate_limit_error', message: 'synthetic rate limit' } })
        return
      }
      if (mode === '500') {
        json(response, 500, { error: { type: 'upstream_error', message: 'synthetic upstream failure' } })
        return
      }
      if (payload.stream) {
        response.writeHead(200, { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-cache' })
        response.write('data: {"id":"e2e-stream","choices":[{"delta":{"content":"hello"}}]}\n\n')
        response.end('data: [DONE]\n\n')
        return
      }
      json(response, 200, {
        id: 'e2e-completion',
        object: 'chat.completion',
        choices: [{ index: 0, message: { role: 'assistant', content: 'e2e-ok' }, finish_reason: 'stop' }],
        usage: { prompt_tokens: 7, completion_tokens: 11 }
      })
      return
    }
    json(response, 404, { error: { type: 'not_found', message: 'not found' } })
  } catch (error) {
    json(response, 400, { error: { type: 'invalid_request', message: error instanceof Error ? error.message : 'invalid request' } })
  }
})

server.listen(port, '127.0.0.1', () => {
  console.log(`Fake OpenAI: http://127.0.0.1:${port}/v1`)
})

for (const signal of ['SIGINT', 'SIGTERM']) {
  process.on(signal, () => server.close(() => process.exit(0)))
}
