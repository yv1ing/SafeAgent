import { defineConfig } from 'orval';

export default defineConfig({
  safeagent: {
    input: './openapi.json',
    output: {
      target: './src/api/generated/client.ts',
      schemas: './src/api/generated/models',
      clean: true,
      formatter: 'prettier',
      client: 'react-query',
      httpClient: 'fetch',
      mode: 'tags-split',
      override: {
        mutator: { path: './src/api/client.ts', name: 'request' },
        fetch: { includeHttpResponseReturnType: false },
      },
    },
  },
});
