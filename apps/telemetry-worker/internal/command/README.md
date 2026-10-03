# Command channel

Machine command dispatch, acknowledgment handling and deadlines for the
telemetry worker. The `task_commands` table (the command ledger) is the
system of record for every command; Redis and MQTT are transport.

## Flow

```
pravara-api                         telemetry-worker                        machine
-----------                         ----------------                        -------
task_commands row (pending)
XADD pravara:commands  ─────────▶  Dispatcher (XREADGROUP / XAUTOCLAIM)
                                    ├─ ledger: still pending? same tenant/machine?
                                    ├─ MQTT publish {mqtt_topic}/cmd, QoS 1 ───▶ executes
                                    └─ ledger: sent + deadline, then XACK
                                    AckHandler  ◀──────────── {mqtt_topic}/ack
                                    ├─ resolve machine from the ack topic
                                    ├─ command must belong to that machine
                                    └─ ledger: acknowledged / completed / failed
                                       (+ outbox events, order roll-up)
                                    DeadlineSweeper (per tenant)
                                    └─ pending/sent past deadline → timeout (+ outbox)
```

## Files

| File | Responsibility |
|------|----------------|
| `dispatcher.go` | Stream consumer: consumer group, reclaim of idle entries, bounded retries, write-back |
| `mqtt_publisher.go` | Paho-backed publisher (QoS 1, waits for PUBACK) |
| `ack_handler.go` | Ack subscription, topic → machine binding, real-time ack event |
| `deadline_sweeper.go` | Expires commands that were never sent or never acknowledged |
| `ledger.go` | Ledger interfaces and statuses (implemented in `internal/db/command_ledger.go`) |
| `completion.go` | `JobCompletion` and the `JobCompletionHook` extension point |
| `types.go` | Command / ack payloads, stream entry fields, topic suffixes |

## Stream entry (written by pravara-api)

| Field | Content |
|-------|---------|
| `tenant_id` | issuing tenant UUID |
| `command_id` | `task_commands.command_id` |
| `payload` | JSON `MachineCommand` |

The worker publishes to the machine's `mqtt_topic` as stored in the ledger,
not the topic carried in the entry.

## Delivery guarantees

- **At least once.** An entry is acknowledged in the stream only after its
  outcome is in the ledger. A worker that dies mid-entry leaves it pending;
  another consumer reclaims it after `retry_idle_seconds`. Machines must treat
  `command_id` as an idempotency key.
- **No resend after an outcome.** A command that is no longer `pending` in the
  ledger is never published again.
- **Bounded retries.** Each failed MQTT publish increments `attempts` and
  stores `error_message`; at `max_attempts` the command is `failed` and
  `machine.command_failed` (plus `task.job_failed` for a task's `start_job`) is
  written to the outbox.
- **Deadlines.** A `sent` command without an ack by `deadline_at`
  (`ack_timeout_seconds` after sending), or a `pending` command older than
  `dispatch_timeout_seconds`, becomes `timeout` with the same outbox events.

## Ack binding

An ack is applied only if the command was issued to the machine whose topic
the ack arrived on, within that machine's tenant. Other acks are dropped,
logged and counted in `pravara_command_acks_total{disposition}`.

## Job completion

A `job_completed` ack for a task's `start_job`, in one transaction:
completes the command, moves the task to `quality_check`, applies the order
roll-up (order to `in_progress` when still pre-production) and writes
`task.job_completed` (and `order.status_changed`) to the outbox. After commit
the optional `JobCompletionHook` runs; it is where production genealogy and
product-passport recording attach.

## Ledger statuses

`pending → sent → acknowledged → completed`, with `failed` and `timeout` as
the other terminal states.
