Fix issues found during code review of the taskline multi-project implementation (spec 012):

1. BUG: In `tools/taskline/server/worker.go`, `executeTask()` never calls `SetCurrentTask(task.Name)` on the ProjectLog before starting the command. This means all log lines get an empty task prefix. Add a type assertion or interface call before `w.runner.Start(...)` to set the current task name on the output writer.

2. Bump CLI version in `tools/taskline/cli/main.go` from 1.1.0 to 2.0.0 (breaking API change — paths moved from `/tasks` to `/projects/{project}/tasks`).

3. In `runtime/trayline`, the `schedule logs` case hardcodes `--follow` and doesn't forward any extra arguments (like `--tail N`) from the user. Forward remaining `$@` after the sub-action.

4. In `runtime/trayline`, the `schedule cancel` case parses human-readable output of `taskline status` via sed, which is fragile. Consider adding a machine-readable output mode in the future.

5. In `runtime/trayline`, the wrapper doesn't normalize the project name derived from `basename "$(pwd)"`. Directory names with uppercase or spaces will be rejected by the server's validation. Normalize to lowercase and replace invalid characters.
