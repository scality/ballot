#! /usr/bin/env bash

kill_ballots() {
    echo 'killing workers...' >&2
    killall ballot-runrace
}

trap kill_ballots SIGINT

go build -o ./ballot-runrace cmd/ballot/ballot.go

SESSION_TIMEOUT=10s
CMD="lockrun --lockfile=candidate -- sleep .1"

./ballot-runrace run once --candidate-id candidate-1 --log-format human --zookeeper-session-timeout $SESSION_TIMEOUT -- $CMD &
./ballot-runrace run once --candidate-id candidate-2 --log-format human --zookeeper-session-timeout $SESSION_TIMEOUT -- $CMD &
./ballot-runrace run once --candidate-id candidate-3 --log-format human --zookeeper-session-timeout $SESSION_TIMEOUT -- $CMD &
./ballot-runrace run once --candidate-id candidate-4 --log-format human --zookeeper-session-timeout $SESSION_TIMEOUT -- $CMD &
./ballot-runrace run once --candidate-id candidate-5 --log-format human --zookeeper-session-timeout $SESSION_TIMEOUT -- $CMD &

wait
