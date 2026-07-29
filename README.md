# api-poller

Declarative HTTP API poller for devices that expose telemetry over HTTP APIs.

The first goal is to support devices such as Moxa IO modules and Soltec
trackers without needing a new binary deploy every time a new payload shape
appears. The binary reads YAML, fetches HTTP payloads, extracts metrics using
declarative paths and filters, prints the resulting samples, and can persist
them to PostgreSQL/TimescaleDB.

## Current scope

- HTTP `GET` requests with configurable headers and timeout
- JSON and XML response parsing into generic Go values
- Declarative extraction using `items_path`, `value_path`, and `where`
- Simple numeric transforms with `scale` and `offset`
- NDJSON output to validate configs against real devices
- PostgreSQL/TimescaleDB storage with env-driven connection settings

## Example

```bash
go run ./cmd/api-poller --config ./examples/psda_moxa_demo.yml --pretty
```

Soltec-style XML polling looks like this:

```yaml
inputs:
  - name: psda_soltec_tracker_1
    request:
      method: GET
      url: http://127.0.0.1:13320/status.xml
      timeout: 5s
      headers:
        Authorization: Basic YWRtaW46c29sdGVj
    response:
      format: xml
    metrics:
      - name: roll
        value_path: $.roll
        labels:
          plant: psda
          device: soltec_atamo_1
      - name: action
        value_path: $.action
        labels:
          plant: psda
          device: soltec_atamo_1
```

If you want to write without printing:

```bash
go run ./cmd/api-poller --config ./examples/psda_moxa_demo.yml --write-only
```

## YAML model

```yaml
storage:
  outputs:
    - name: local_timescale
      type: timescaledb
      enabled: true
      timescaledb:
        host_env: TIMESCALE_HOST_LOCAL
        port_env: TIMESCALE_PORT_LOCAL
        user_env: TIMESCALE_USER_LOCAL
        password_env: TIMESCALE_PASSWORD_LOCAL
        database_env: TIMESCALE_DB_LOCAL
        schema: telemetry
        table: api_samples
        sslmode: disable

inputs:
  - name: psda_ai_demo
    request:
      method: GET
      url: http://127.0.0.1:18091/api/slot/0/io/ai
      timeout: 5s
      headers:
        Accept: vdn.dac.v1
        Content-Type: application/json
    response:
      format: json
      items_path: $.io.ai
    metrics:
      - name: 1RC401_w_m2
        value_path: $.aiValueScaled
        where:
          - path: $.aiIndex
            equals: 0
        transform:
          scale: 150
          offset: 0
        labels:
          unit: w.m-2
          device: 1MX401
```

## Storage table shape

The PostgreSQL writer expects the target table to exist already, with this
generic row model:

- `ts timestamptz`
- `input_name text`
- `metric_name text`
- `value_double double precision null`
- `value_text text null`
- `value_bool boolean null`
- `value_json jsonb null`
- `value_type text`
- `labels jsonb`
- `labels_key text`
- `source_url text`

That keeps the collector general: each new API integration only changes YAML,
not the SQL table schema.

## Notes

- Paths support a small subset tailored for telemetry payloads:
  - `$.io.ai`
  - `$.aiValueScaled`
  - `$.items[0].value`
- `where` runs against each item returned by `items_path`.
- If `items_path` is omitted, metrics are resolved against the whole document.
- For XML, paths are resolved relative to the root element, so `<response><roll>`
  is addressed as `$.roll`.
