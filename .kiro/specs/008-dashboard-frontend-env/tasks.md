# Tasks: 008 — Dashboard Frontend Environment Editor

## Task 1: Implement EnvFileTabs component
- [x] Create `src/lib/components/EnvFileTabs.svelte`
- [x] Accept list of filenames, active filename
- [x] Render horizontal tab bar with filename buttons
- [x] Active tab highlighted
- [x] Unsaved changes indicator (dot) on modified tabs
- [x] Scrollable on mobile (overflow-x: auto)

## Task 2: Implement MaskedInput component
- [x] Create `src/lib/components/MaskedInput.svelte`
- [x] Props: value (bindable), sensitive (boolean)
- [x] If sensitive: show dots (type=password) with eye toggle
- [x] Toggle reveals actual value (switches to type=text)
- [x] If not sensitive: always show as text input
- [x] Consistent sizing with other form inputs

## Task 3: Implement EnvRow component
- [x] Create `src/lib/components/EnvRow.svelte`
- [x] Display: key input (or label), value (MaskedInput), delete button
- [x] Key input for new rows (editable), label for existing rows
- [x] Inline validation error below key field (red text)
- [x] Delete button (trash icon) with confirm on click
- [x] Responsive: stack key/value vertically on mobile

## Task 4: Implement EnvEditor component
- [x] Create `src/lib/components/EnvEditor.svelte`
- [x] Accept variables array for current file
- [x] Render list of EnvRow components
- [x] "Add variable" button at bottom (appends empty row)
- [x] Real-time validation on all rows
- [x] Save button (disabled when validation errors exist)
- [x] Track dirty state (compare against original snapshot)

## Task 5: Implement validation logic
- [x] Create validation helper in `src/lib/utils/env.ts`
- [x] Key regex: `^[A-Za-z_][A-Za-z0-9_]*$`
- [x] Check empty key, invalid format, duplicates
- [x] Return localized error messages
- [x] Run on every input change (reactive)

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
