
# aiops-qproxy-v2.4-ec2-native (merged-final)

A small Go runner that drives **q** CLI headlessly, cleans ANSI/TUI noise, writes JSONL logs,
and persists **reusable context** files to reduce future token use.

## Build
```bash
./scripts/aiops-qproxy.sh build
```

## Run (one-shot)
```bash
# prepare alert.json and meta.json (optional)
./scripts/aiops-qproxy.sh run -- -alert alert.json -meta meta.json
# or read alert from stdin (systemd style):
cat alert.json | ./bin/qproxy-runner -alert - -meta meta.json
```

## What it does
- Adds base ctx (`ctx/sop.md`, `ctx/schema.json`) and any matching reusable ctx from `data/ctx/`
- Dedups duplicate `/context add` lines (prevents 'Rule exists' spam)
- Forces NO_COLOR/TERM=dumb to reduce ANSI; strips remaining sequences
- Writes cleaned `stdout`/`stderr` into `logs/*.jsonl` and `logs/last_stdout.txt`
- If output contains a valid JSON with `confidence >= 0.6`, persists the built context under `data/ctx/<key>.<ts>.ctx.txt`

## systemd
Install to `/opt/aiops-qproxy`, then:
```bash
sudo cp -r . /opt/aiops-qproxy
sudo install -m755 bin/qproxy-runner /opt/aiops-qproxy/bin/qproxy-runner
sudo cp systemd/aiops-qproxy-runner.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now aiops-qproxy-runner
```

The unit reads alert JSON from STDIN; you can pipe your alerting bus into it or adapt ExecStart to point to your feeder process.
