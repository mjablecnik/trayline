# Design: 004 — Dashboard Frontend Setup

## Overview

SvelteKit static SPA scaffold with TypeScript, TailwindCSS, i18n, authentication, and deployment infrastructure.

## Project Structure

```
dashboard/
├── src/
│   ├── lib/
│   │   ├── api.ts              # Typed API client
│   │   ├── auth.ts             # Token management (localStorage)
│   │   ├── i18n/
│   │   │   ├── index.ts        # i18n setup + locale detection
│   │   │   ├── cs.ts           # Czech translations
│   │   │   └── en.ts           # English translations
│   │   └── stores/
│   │       ├── auth.ts         # Auth store (token state)
│   │       └── locale.ts       # Locale store
│   ├── routes/
│   │   ├── +layout.svelte      # Root layout (header, error boundary)
│   │   ├── +page.svelte        # Project list (or token entry)
│   │   ├── [project]/
│   │   │   ├── +layout.svelte  # Project detail shell (tabs)
│   │   │   ├── +page.svelte    # Default tab redirect → files
│   │   │   ├── tree/[...path]/+page.svelte  # File browser
│   │   │   └── commits/
│   │   │       └── [hash]/+page.svelte      # Commit detail
│   │   └── ...
│   ├── app.html
│   ├── app.css                 # Tailwind directives + global styles
│   └── hooks.client.ts         # Client-side error handling
├── static/
│   └── favicon.svg
├── scripts/
│   ├── build.sh
│   ├── start-docker.sh
│   ├── stop-docker.sh
│   └── deploy.sh
├── Dockerfile
├── fly.toml
├── .env.example
├── package.json
├── svelte.config.js
├── vite.config.ts
├── tailwind.config.js
├── tsconfig.json
└── .dockerignore
```

## Key Design Decisions

### SvelteKit Configuration

```js
// svelte.config.js
import adapter from '@sveltejs/adapter-static';

export default {
  kit: {
    adapter: adapter({ fallback: 'index.html' }), // SPA mode
    paths: { base: '' }
  }
};
```

`fallback: 'index.html'` ensures all routes are handled client-side (SPA behavior).

### API Client (`src/lib/api.ts`)

```typescript
const BASE_URL = import.meta.env.PUBLIC_API_URL;

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const token = getToken();
  const res = await fetch(`${BASE_URL}${path}`, {
    method,
    headers: {
      'Authorization': `Bearer ${token}`,
      'Content-Type': 'application/json',
    },
    body: body ? JSON.stringify(body) : undefined,
  });

  if (res.status === 401) {
    clearToken();
    goto('/');
    throw new AuthError();
  }

  if (!res.ok) {
    const err = await res.json();
    throw new ApiError(res.status, err.error, err.message);
  }

  return res.json();
}

// Typed endpoint functions
export const api = {
  getProjects: () => request<Project[]>('GET', '/projects'),
  getProject: (name: string) => request<ProjectDetail>('GET', `/projects/${name}`),
  getTree: (name: string, ref: string, path: string) => ...,
  getBlob: (name: string, ref: string, path: string) => ...,
  getCommits: (name: string, ref: string, limit: number, offset: number) => ...,
  getCommit: (name: string, hash: string) => ...,
  getStatus: (name: string) => ...,
  getEnv: (name: string) => ...,
  putEnv: (name: string, data: PutEnvRequest) => ...,
};
```

### Authentication Flow

```
User visits / → check localStorage for token
  ├─ No token → show TokenEntry component
  └─ Has token → fetch /projects
       ├─ 401 → clear token, show TokenEntry
       └─ Success → show ProjectList
```

### i18n Approach

Svelte store-based (no heavy library needed for 2 languages):

```typescript
// src/lib/i18n/index.ts
import { writable, derived } from 'svelte/store';
import cs from './cs';
import en from './en';

const translations = { cs, en };
export const locale = writable(detectLocale());
export const t = derived(locale, ($locale) => translations[$locale]);

function detectLocale(): 'cs' | 'en' {
  const stored = localStorage.getItem('locale');
  if (stored && stored in translations) return stored;
  const browser = navigator.language.slice(0, 2);
  return browser in translations ? browser : 'en';
}
```

Translation files are flat key-value objects:
```typescript
// src/lib/i18n/cs.ts
export default {
  'nav.projects': 'Projekty',
  'nav.logout': 'Odhlásit',
  'auth.title': 'Trayline Dashboard',
  'auth.placeholder': 'Zadejte API token...',
  'auth.connect': 'Připojit',
  'projects.empty': 'Žádné synchronizované projekty',
  // ...
};
```

### Layout Shell (`+layout.svelte`)

```
┌─────────────────────────────────────────────────┐
│  Header (sticky)                                │
│  [Trayline]              [CS/EN] [Logout]       │
├─────────────────────────────────────────────────┤
│                                                 │
│  <slot /> (page content)                        │
│                                                 │
└─────────────────────────────────────────────────┘
```

- Header is always visible (sticky top)
- Logout and language switcher in header
- On mobile: hamburger menu (if more nav items added later)

### Error Boundary

```svelte
<!-- +layout.svelte -->
<svelte:boundary>
  <slot />
  {#snippet failed(error)}
    <ErrorFallback {error} />
  {/snippet}
</svelte:boundary>
```

### Dockerfile (Multi-stage)

```dockerfile
FROM node:22.12.0-slim AS build
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM nginx:1.27.3-alpine
COPY --from=build /app/build /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 8080
```

Nginx config handles SPA routing (all paths → index.html).

### Environment Variables

```env
# .env.example
PUBLIC_API_URL=http://localhost:8080
```

Build-time variable (baked into static bundle via Vite's `import.meta.env`).

## Dependencies (package.json)

```json
{
  "devDependencies": {
    "@sveltejs/adapter-static": "3.x.x",
    "@sveltejs/kit": "2.x.x",
    "svelte": "5.x.x",
    "typescript": "5.x.x",
    "tailwindcss": "4.x.x",
    "vite": "6.x.x"
  }
}
```

Exact versions pinned at implementation time.
