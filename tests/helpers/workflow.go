// Package helpers - workflows used in tests.
package helpers

func SimpleWorkflow() string {
	return `
name = "simple"

[tasks.a]
cmd = "echo hello"
`
}

func RetryWorkflow() string {
	return `
name = "retry"

[tasks.a]
cmd = "false"
retries = 2
`
}

func ComplexWorkflow() string {
	return `
name = "complex"

[tasks.a]
cmd = "echo Task A"
[tasks.b]
cmd = "echo Task B"
depends_on = ["a"]
[tasks.c]
cmd = "echo Task C"
depends_on = ["a"]
[tasks.d]
cmd = "echo Task D"
depends_on = ["b", "c"]
`
}

func FailingWorkflow() string {
	return `
name = "failing"

[tasks.fail]
cmd = "exit 1"
retries = 0
`
}

func LongRunningWorkflow() string {
	return `
name = "long-running"

[tasks.sleep]
cmd = "sleep 30"
retries = 0
`
}

func MultiTaskWorkflow() string {
	return `
name = "multi"

[tasks.build]
cmd = "echo Building"
retries = 1

[tasks.test]
cmd = "echo Testing"
depends_on = ["build"]
retries = 1

[tasks.deploy]
cmd = "echo Deploying"
depends_on = ["test"]
retries = 0
`
}

func InvalidWorkflow() string {
	return `
name = "invalid"

[tasks.task1]
cmd = "echo test"
depends_on = ["nonexistent"]
`
}

func CycleWorkflow() string {
	return `
name = "cycle"

[tasks.task1]
cmd = "echo test"
depends_on = ["task2"]

[tasks.task2]
cmd = "echo test"
depends_on = ["task1"]
`
}

func ResumeWorkflow() string {
	return `
name = "resume"

[tasks.task1]
cmd = "exit 1"

[tasks.task2]
cmd = "echo success"
depends_on = ["task1"]
`
}

func ResumeWorkflowFixed() string {
	return `
name = "resume"

[tasks.task1]
cmd = "exit 0"

[tasks.task2]
cmd = "echo success"
depends_on = ["task1"]
`
}

// VarInterpolationWorkflow uses a Go template reference to a runtime variable.
func VarInterpolationWorkflow() string {
	return `
name = "var-interp"

[tasks.greet]
cmd = 'echo {{.MY_VAR}}'
`
}

// RegisterWorkflow stores task output into a context variable via register.
func RegisterWorkflow() string {
	return `
name = "register"

[tasks.produce]
cmd = "echo hello-from-register"
register = "task_output"
`
}

// PartialFailWorkflow has task1 succeed and task2 fail — suitable for testing
// resume skip behaviour (task1 should be skipped on resume).
func PartialFailWorkflow() string {
	return `
name = "partial-fail"

[tasks.task1]
cmd = "echo task1-ok"

[tasks.task2]
cmd = "exit 1"
depends_on = ["task1"]
`
}

// PartialFailWorkflowFixed is the fixed version where task2 now succeeds.
func PartialFailWorkflowFixed() string {
	return `
name = "partial-fail"

[tasks.task1]
cmd = "echo task1-ok"

[tasks.task2]
cmd = "echo task2-fixed"
depends_on = ["task1"]
`
}

// DiamondWorkflow is the classic diamond dependency pattern:
//
//	A → B → D
//	A → C → D
//
// D can only start after both B and C complete.  This is the canonical
// test-case for verifying that a work-stealing scheduler outperforms a
// level-based one: B can start immediately when A finishes, without
// waiting for C (which is at the same level as B but independent of A→B).
func DiamondWorkflow() string {
	return `
name = "diamond"

[tasks.a]
cmd = "echo A"

[tasks.b]
cmd = "echo B"
depends_on = ["a"]

[tasks.c]
cmd = "echo C"
depends_on = ["a"]

[tasks.d]
cmd = "echo D"
depends_on = ["b", "c"]
`
}

// FanOutWorkflow has a single root that fans out to N independent tasks
// which all converge back into a single final task.
//
//	root → t1 → final
//	root → t2 → final
//	root → t3 → final
//
// t1/t2/t3 should run concurrently.
func FanOutWorkflow() string {
	return `
name = "fanout"

[tasks.root]
cmd = "echo root"

[tasks.t1]
cmd = "echo t1"
depends_on = ["root"]

[tasks.t2]
cmd = "echo t2"
depends_on = ["root"]

[tasks.t3]
cmd = "echo t3"
depends_on = ["root"]

[tasks.final]
cmd = "echo final"
depends_on = ["t1", "t2", "t3"]
`
}

// WideParallelWorkflow has N independent root tasks (no dependencies at all).
// Used to verify maximum concurrency with numWorkers limiting throughput.
func WideParallelWorkflow() string {
	return `
name = "wide-parallel"

[tasks.p1]
cmd = "echo p1"

[tasks.p2]
cmd = "echo p2"

[tasks.p3]
cmd = "echo p3"

[tasks.p4]
cmd = "echo p4"
`
}

// IgnoreFailureWorkflow has a task that fails but is marked ignore_failure,
// allowing its dependent to still run.
func IgnoreFailureWorkflow() string {
	return `
name = "ignore-failure"

[tasks.failing]
cmd = "exit 1"
ignore_failure = true

[tasks.after]
cmd = "echo after-failing"
depends_on = ["failing"]
`
}
