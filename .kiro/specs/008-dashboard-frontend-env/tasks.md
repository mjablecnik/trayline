# Tasks: 008 — Dashboard Frontend Environment Editor

## Task 1: Implement EnvFileTabs component
- [ ] Create `src/lib/components/EnvFileTabs.svelte`
- [ ] Accept list of filenames, active filename
- [ ] Render horizontal tab bar with filename buttons
- [ ] Active tab highlighted
- [ ] Unsaved changes indicator (dot) on modified tabs
- [ ] Scrollable on mobile (overflow-x: auto)

## Task 2: Implement MaskedInput component
- [ ] Create `src/lib/components/MaskedInput.svelte`
- [ ] Props: value (bindable), sensitive (boolean)
- [ ] If sensitive: show dots (type=password) with eye toggle
- [ ] Toggle reveals actual value (switches to type=text)
- [ ] If not sensitive: always show as text input
- [ ] Consistent sizing with other form inputs

## Task 3: Implement EnvRow component
- [ ] Create `src/lib/components/EnvRow.svelte`
- [ ] Display: key input (or label), value (MaskedInput), delete button
- [ ] Key input for new rows (editable), label for existing rows
- [ ] Inline validation error below key field (red text)
- [ ] Delete button (trash icon) with confirm on click
- [ ] Responsive: stack key/value vertically on mobile

## Task 4: Implement EnvEditor component
- [ ] Create `src/lib/components/EnvEditor.svelte`
- [ ] Accept variables array for current file
- [ ] Render list of EnvRow components
- [ ] "Add variable" button at bottom (appends empty row)
- [ ] Real-time validation on all rows
- [ ] Save button (disabled when validation errors exist)
- [ ] Track dirty state (compare against original snapshot)

## Task 5: Implement validation logic
- [ ] Create validation helper in `src/lib/utils/env.ts`
- [ ] Key regex: `^[A-Za-z_][A-Za-z0-9_]*$`
- [ ] Check empty key, invalid format, duplicates
- [ ] Return localized error messages
- [ ] Run on every input change (reactive)

## Task 6: Implement environment page
- [ ] Update `src/routes/[project]/env/+page.svelte`
- [ ] Fetch env files from API on mount
- [ ] Render EnvFileTabs + EnvEditor for active file
- [ ] Switching tabs loads that file's variables into editor
- [ ] Loading state: skeleton table
- [ ] Empty state: "No .env files found" message
- [ ] Error state: inline error banner

## Task 7: Implement save functionality
- [ ] On save click: call `PUT /projects/{name}/env`
- [ ] Success → brief green success message, mark file as clean
- [ ] Error → show API error message inline below save button
- [ ] Confirm dialog on tab switch / navigation with unsaved changes

## Task 8: Implement reference view
- [ ] Show `.env.example` variables below editor (when editing other .env files)
- [ ] Muted text styling, read-only
- [ ] Only shown when .env.example exists in the file list
- [ ] Helps user see expected keys and placeholder values
