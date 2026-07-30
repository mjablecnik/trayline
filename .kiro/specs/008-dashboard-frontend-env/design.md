# Design: 008 — Dashboard Frontend Environment Editor

## Overview

Environment tab in project detail: view and edit .env files with key-value UI, validation, and sensitive value masking.

## Component Structure

```
src/routes/[project]/env/
└── +page.svelte                  # Environment tab page

src/lib/components/
├── EnvEditor.svelte              # Full editor for one file
├── EnvRow.svelte                 # Single key-value row
├── EnvFileTabs.svelte            # Tab selector for .env files
├── EnvReference.svelte           # .env.example reference panel
└── MaskedInput.svelte            # Input with reveal toggle
```

## Page Layout

```
┌─────────────────────────────────────────────────┐
│ File: [.env] [.env.example] [.env.prod]         │
├─────────────────────────────────────────────────┤
│                                                 │
│ Key              │ Value                    │    │
│ ─────────────────┼──────────────────────────┼───│
│ DATABASE_URL     │ postgres://localhost/myapp│ 🗑 │
│ API_KEY          │ ●●●●●●●●●●●● [👁]       │ 🗑 │
│ PORT             │ 3000                     │ 🗑 │
│ DEBUG            │ true                     │ 🗑 │
│                                                 │
│ [+ Add variable]                    [Save]      │
│                                                 │
│ ─── Reference (.env.example) ───                │
│ DATABASE_URL = your-database-url-here           │
│ API_KEY = your-api-key-here                     │
│ PORT = 3000                                     │
└─────────────────────────────────────────────────┘
```

## State Management

```typescript
interface EnvEditorState {
  files: EnvFile[];
  activeFile: string;          // currently selected filename
  modified: Set<string>;       // filenames with unsaved changes
  errors: Map<string, string>; // row-level validation errors
}

interface EnvFile {
  filename: string;
  variables: EnvVariable[];
  original: EnvVariable[];    // snapshot for dirty detection
}

interface EnvVariable {
  key: string;
  value: string;
  id: string;                 // unique ID for list keying
}
```

## Sensitive Key Detection

```typescript
const SENSITIVE_PATTERNS = ['KEY', 'SECRET', 'TOKEN', 'PASSWORD', 'PRIVATE'];

function isSensitive(key: string): boolean {
  const upper = key.toUpperCase();
  return SENSITIVE_PATTERNS.some(p => upper.includes(p));
}
```

## MaskedInput Component

```svelte
<script>
  let revealed = false;
  export let value: string;
  export let sensitive: boolean;
</script>

{#if sensitive && !revealed}
  <input type="password" bind:value disabled={false} />
  <button on:click={() => revealed = true}>👁</button>
{:else}
  <input type="text" bind:value />
  {#if sensitive}
    <button on:click={() => revealed = false}>🙈</button>
  {/if}
{/if}
```

## Validation

Real-time on input change:

```typescript
const KEY_REGEX = /^[A-Za-z_][A-Za-z0-9_]*$/;

function validateRow(key: string, allKeys: string[]): string | null {
  if (!key) return t('env.error.empty_key');
  if (!KEY_REGEX.test(key)) return t('env.error.invalid_key');
  if (allKeys.filter(k => k === key).length > 1) return t('env.error.duplicate');
  return null;
}
```

Errors displayed inline below the key input in red text.
Save button disabled when any error exists.

## Save Flow

```typescript
async function save() {
  const vars = currentFile.variables.map(v => ({ key: v.key, value: v.value }));
  await api.putEnv(project, { filename: activeFile, variables: vars });
  // Success → brief green checkmark/toast
  // Error → inline error message below save button
  markClean(activeFile);
}
```

## Unsaved Changes Detection

- Compare current variables against `original` snapshot
- If different → show dot on file tab, highlight save button
- On tab switch or navigation away with unsaved changes → confirm dialog

## Reference Panel

When editing `.env`, if `.env.example` exists in the file list:
- Show below the editor as a read-only reference
- Muted text styling
- Helps user see expected keys and placeholder values

## Responsive Behavior

- Mobile: key and value stack vertically (key above, value below)
- Desktop: key and value side by side in table row
- File tabs scroll horizontally on mobile
- Action buttons (delete, reveal) always accessible
