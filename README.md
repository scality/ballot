# Ballot

## Overview

`ballot` ensures that it's the only one running a command, by running a leader
election on a ZooKeeper cluster. Others that try to run the same command (based
on ZooKeeper path) will wait until the active `ballot` releases leadership.

Some example use cases include:

- A distributed cron service, where jobs need to run at specific times or time
  intervals, and cannot overlap, but still needs to failover to standby
  instances on failure. In this case `ballot` acts as
  [lockrun](http://www.unixwiz.net/tools/lockrun.html) would on a single-server
  system.
- Any singleton service, that would be a kubernetes `Deployment` of 1 replica,
  if kubernetes is not an option, or if running on kubernetes and trying to
  achieve failover faster than the control plane can re-schedule pods.

### Examples

```shell
ballot run once --candidate-id `hostname` -- /usr/bin/env LD_PRELOAD=trickle.so command-with-exclusive-resource
ballot run cron --candidate-id `hostname` --schedule='@daily' -- /usr/bin/env LD_PRELOAD=trickle.so command-with-exclusive-resource
```

## Pre-requisites

A running and healthy [ZooKeeper](https://zookeeper.apache.org/) ensemble is
required. Only plain unauthenticated connections are supported at this time.

## Configuration

`ballot` relies on
[viper](https://github.com/spf13/viper)/[cobra](https://github.com/spf13/cobra)
for command line and configuration, and thus can be configured using command
line arguments, environment variables, and configuration files. Remote
configuration sources are not available at this time.

Several configuration sources can be mixed, as long as parameters are not
duplicated between sources. The following are all equivalent:

* Running `ballot --config-file ./ballot.yaml run [...]` with `ballot.yaml`
  containing:

    ```yaml
    ---
    zookeeper-servers:
    - server1
    - server2
    - 'server3:2181'
    zookeeper-base-path: /com/scality/backbeat/singleton
    zookeeper-session-timeout: 5s
    log-level: debug
    log-format: json
    ```

* Running

    ```shell
    env \
        ZOOKEEPER_SERVERS=server1,server2,server3:2181 \
        ZOOKEEPER_BASE_PATH=/com/scality/backbeat/singleton \
        ZOOKEEPER_SESSION_TIMEOUT=5s
        LOG_LEVEL=debug \
        LOG_FORMAT=json \
        ballot run [...]
    ```

* Running

    ```shell
    ballot \
        --zookeeper-servers server1 \
        --zookeeper-servers server2 \
        --zookeeper-servers server3:2181 \
        --zookeeper-base-path /com/scality/backbeat/singleton \
        --zookeeper-session-timeout 5s \
        --log-level debug \
        --log-format json \
        run [...]
    ```

Global configuration:

| Parameter | Description | Default | Allowed values |
| --- | --- | --- | --- |
| `config-file` | Configuration file | `~/.config/ballot/ballot.json`, `~/.config/ballot/ballot.yaml`, `/etc/ballot/ballot.json`, `/etc/ballot/ballot.yaml`, `./ballot.json`, `./ballot.yaml` | Any path to a valid yaml or json ballot configuration |
| `zookeeper-servers` | List of ZooKeeper servers | `localhost` | List of `server` or `server:port` |
| `zookeeper-base-path` | Base path to ZooKeeper election proposal nodes | `/ballot/election` | Any valid, otherwise unused ZooKeeper path |
| `zookeeper-session-timeout` | ZooKeeper session timeout, used to detect purge stale election members | `5s` | Any duration between 2 and 20 times ZooKeeper's `tickTime` |
| `log-level` | Log level | `info` | `trace`, `debug`, `info`, `warn`, `error`, `fatal`, `panic` |
| `log-format` | Logs format | `human` | `human`, `json`, `raw` |
| `debug` | Debug mode with extra checks and logging | `false` | `false`, `true` |

## Usage

### Run Once

`run once` runs the provided command. Use `--` to separate `ballot`'s own
arguments from the command's executable and arguments if they contain `-`'s
(which is very likely).

Setting parameters `on-child-success` and `on-election-failure` to `reelect` or
`rerun` will cause the command to be run several times. In this case, `once`
means "not scheduled to run regularly", and is not a guarantee of only one run.

Note: If ZooKeeper goes down while a `ballot` process is a leader and others are
waiting as followers, they will all stay in their respective roles and operate
until ZooKeeper comes back. They will, however, not be able to perform any
failover or run new commands until ZooKeeper is back up.

#### Options

| Parameter | Description | Default | Allowed values |
| --- | --- | --- | --- |
| `candidate-id` | This `ballot`'s ID, to be used for display and debugging purposes. Uniqueness of IDs between `ballot`s is not required, but makes inspecting easier. | N/A, mandatory | Any string |
| `on-child-success` | What to do when the command we run terminates with exit code 0. | `reelect` | `rerun`, `reelect`, `exit`, `ignore` |
| `on-child-error` | What to do when the command we run terminates with an exit code other than 0, or cannot be executed for any reason. | `reelect` | `rerun`, `reelect`, `exit`, `ignore` |
| `on-election-failure` | What to do when the election process fails for technical reasons (unreachable ZooKeeper, for example). Use `run-anyway` only if running too many processes at once is preferable to none at all. | `retry` | `retry`, `run-anyway`, `exit` |
| `wrap-child-logs` | Whether to display the command's logs formatted as our own logs, or have its `stdout` and `stderr` directly connected to `ballot`'s | `true` | `false`, `true` |

### Run Cron

`run cron` runs the provided command on a schedule. Use `--` to separate `ballot`'s own
arguments from the command's executable and arguments if they contain `-`'s
(which is very likely).

Leader election is performed once and all timed invocations happen on the node
that was elected leader. Elections do not happen for each invocation, unless
`on-child-success`/`on-child-error` are set to `reelect`.

The only supported concurrency policy at the moment is to skip an invocation if
the previous invocation is still running.

Note: If ZooKeeper goes down while a `ballot` process is a leader and others are
waiting as followers, they will all stay in their respective roles and operate
until ZooKeeper comes back. They will, however, not be able to perform any
failover in the event that the leader goes down.

Future features can include adding a deadline and killing overdue jobs, allowing
invocation queueing, allowing concurrent invocations.

#### Options

| Parameter | Description | Default | Allowed values |
| --- | --- | --- | --- |
| `schedule` | Schedule to run on. | N/A, mandatory | [Extended Cron format](https://pkg.go.dev/github.com/robfig/cron#hdr-CRON_Expression_Format) |
| `candidate-id` | This `ballot`'s ID, to be used for display and debugging purposes | N/A, mandatory. Uniqueness of IDs between `ballot`s is not required, but makes inspecting easier. | Any string |
| `on-child-success` | What to do when the command we run terminates with exit code 0. | `reelect` | `rerun`, `reelect`, `exit`, `ignore` |
| `on-child-error` | What to do when the command we run terminates with an exit code other than 0, or cannot be executed for any reason. | `reelect` | `rerun`, `reelect`, `exit`, `ignore` |
| `on-election-failure` | What to do when the election process fails for technical reasons (unreachable ZooKeeper, for example). Use `run-anyway` only if running too many processes at once is preferable to none at all. | `retry` | `retry`, `run-anyway`, `exit` |
| `wrap-child-logs` | Whether to display the command's logs formatted as our own logs, or have its `stdout` and `stderr` directly connected to `ballot`'s | `true` | `false`, `true` |

#### Examples

### Info

The info command shows the current state of the election:

- The current leader
- The list of candidates
- Data about each entry (proposal, last seen time, host, pid, candidate id, etc)
- Notes about each entry that could have an effect on the current election.
  These notes often include conditional phrasing, because they are observations
  based on heuristics, as it is often not possible to know what really is
  happening until later.

#### Options

See Global Configuration for common options.

#### Examples

Here is an example of an election where the leader just resigned (for example
because its child process completed successfully, and the configured policy is
`reelect`). We can see that `candidate-2` is well positioned to be the leader,
but is prevented by `candidate-5` which still holds a lease (`last heartbeat
2.242563s ago`). When `candidate-5`'s heartbeat misses (2.75s from now, or
`sessionTimeout/2` after its last heartbeat), `candidate-2` will proceed with
leadership.

```yaml
$ ballot --zookeeper-servers localhost:2181 --zookeeper-base-path /ballot/myservice info
leader:
  id: candidate-5
  host: host-5
  pid: "25048"
  proposalTime: 2022-01-07 13:40:17.537208 -0800 PST m=+1.119309658
  proposalNode: /ballot/myservice/proposal-0000031785
  sessionTimeout: 10s
  notes:
  - is leader
  - last heartbeat 2.242563s ago
  - possibly resigning
  - election may be in progress
  lastSeenTime: 2022-01-07 13:40:47.55661 -0800 PST m=+31.138087501
allCandidates:
- id: candidate-2
  host: host-2
  pid: "5967"
  proposalTime: 2022-01-07 13:40:17.540773 -0800 PST m=+1.111020024
  proposalNode: /ballot/myservice/proposal-0000031786
  sessionTimeout: 10s
  notes:
  - should be leader but another candidate is claiming the role
  - possibly waiting for previous leader resignation
  - proposal node created 32.259036s ago
- id: candidate-3
  host: host-3
  pid: "6516"
  proposalTime: 2022-01-07 13:40:17.540755 -0800 PST m=+1.112006091
  proposalNode: /ballot/myservice/proposal-0000031787
  sessionTimeout: 10s
  notes:
  - proposal node created 32.259585s ago
- id: candidate-1
  host: host-1
  pid: "152"
  proposalTime: 2022-01-07 13:40:17.540778 -0800 PST m=+1.113612800
  proposalNode: /ballot/myservice/proposal-0000031788
  sessionTimeout: 10s
  notes:
  - proposal node created 32.260036s ago
- id: candidate-4
  host: host-4
  pid: "8053"
  proposalTime: 2022-01-07 13:40:17.540928 -0800 PST m=+1.123033944
  proposalNode: /ballot/myservice/proposal-0000031789
  sessionTimeout: 10s
  notes:
  - proposal node created 32.260125s ago
- id: candidate-5
  host: host-5
  pid: "4389"
  proposalTime: 2022-01-07 13:40:47.827953 -0800 PST m=+31.409424660
  proposalNode: /ballot/myservice/proposal-0000031790
  sessionTimeout: 10s
  notes:
  - this candidate id claimed leadership
  - proposal node created 1.973348s ago

```

### Watch

Not implemented yet.

#### Options

| Parameter | Description | Default | Allowed values |
| --- | --- | --- | --- |

#### Examples

## Contributing

In order to contribute, please follow the
[Contributing Guidelines](
https://github.com/scality/Guidelines/blob/master/CONTRIBUTING.md).

[Design
Doc](https://github.com/scality/citadel/blob/development/1.0/docs/design/ballot-launcher.md).

TODO: Developer instructions to be written here.

## License

Licensed under the [Apache 2.0 license](LICENSE).
