# flowlite

A tiny, dependency-free, embeddable **BPMN 2.0 subset engine** for Go services that
need approvals without running a JVM or a workflow server.

```go
res, _ := svc.Start(ctx, runtime.StartInput{DefKey: "leave", Title: "Alice / 3 days"}, "alice")
res, _ = svc.Act(ctx, res.Inst.ID, "manager", runtime.ActionApprove, runtime.ActionInput{Comment: "ok"})
```

## Why

Most mature BPMN engines (Flowable, Activiti, Camunda 7) are JVM services. Teams that
ship a single Go binary either bolt on a Java service or hand-roll approvals. flowlite
is the middle ground: a small, correct execution core you can `import` and persist in
your own schema.

## What it supports

| Node | Semantics |
| --- | --- |
| `start` | exactly one per process |
| `approval` (BPMN `userTask`) | any / all countersign, ad-hoc approvers, timeout escalation |
| `exclusive` | condition expressions with one mandatory default branch |
| `end` | at least one per process |

Actions: approve, reject, countersign, rollback, escalate, withdraw.
Definitions round-trip with standard modelers through BPMN 2.0 XML
(`bpmn-js`, `Camunda Modeler`) using the `flowlite` extension namespace.

Not supported (on purpose): parallel/inclusive gateways, sub-processes, timers as
events, service tasks, form engines, BPMN DI generation. Unsupported BPMN elements are
rejected on import instead of being silently dropped.

## Packages

```
engine/        pure execution core (zero imports outside the stdlib)
store/         persistence ports (Definition / Instance / Task / Action)
store/memory/  in-memory implementation for tests and demos
runtime/       orchestration: definition snapshots, idempotent start,
               optimistic locking with replay, materialized tasks, audit actions
```

`runtime.Service` is the usual entry point. It guarantees:

- **Snapshot pinning** – an instance executes the definition captured at start time;
  editing a process never changes an in-flight instance.
- **Optimistic concurrency** – state carries a `Revision`; conflicting writes are
  re-read and replayed (default 3 attempts), so two approvers clicking at once cannot
  lose an update.
- **Materialized tasks** – "my todo" is an indexed query, not a full scan.
- **Append-only action log** – powers "handled by me" and audit.

## Wiring it up

Implement the four ports in `store` with your database. The GORM adapter used by
NexAdmin is a good reference. Multi-tenancy is the host's concern: put your tenant in
`context.Context` and let your store apply it. `Service` additionally exposes
`ContextFor` so scheduled sweeps can re-enter the right tenant for each instance.

```go
b := memory.New()
svc := runtime.New(b.Defs, b.Insts, b.Tasks, b.Actions)
svc.OnEvent = func(ctx context.Context, ev runtime.Event) { /* notify approvers */ }
```

Run `go run ./examples/basic` for a complete in-memory walkthrough.

## Definition format

Definitions can be authored as JSON (`engine.Definition`) or imported from BPMN XML:

```go
def, err := engine.ImportXML(xmlBytes)
raw, err := json.Marshal(def)
xmlOut, err := engine.ExportXML(def)
```

`ImportXML` also reads `camunda:assignee` / `camunda:candidateUsers` so existing models
import without edits. On export, approver metadata is written to the `flowlite`
namespace; register [`moddle/flowlite.json`](moddle/flowlite.json) with bpmn-js to keep
those attributes through visual editing.

## Status

Pre-1.0. The engine and runtime are covered by unit tests, including concurrent
approvals, snapshot pinning, idempotent start, timeout escalation and XML round-trips.
APIs may still change; semantic changes will be documented in release notes.

## Publishing notes

The module path `github.com/nexadmin/flowlite` is a placeholder for the in-repo copy.
Change it (and the `replace` directive in the host `go.mod`) when you move this
directory into its own repository.

## License

Apache-2.0. See [LICENSE](LICENSE) and [NOTICE](NOTICE).
