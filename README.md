# api-poller

Declarative HTTP API poller for devices that expose telemetry over HTTP APIs.

The first goal is to support devices such as Moxa IO modules and Soltec
trackers without needing a new binary deploy every time a new payload shape
appears. The binary reads YAML, fetches HTTP payloads, extracts metrics using
declarative paths and filters, prints the resulting samples, and can persist
them to PostgreSQL/TimescaleDB.

## Current scope

- HTTP requests (any method) with configurable headers, timeout, and an optional request body
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

To poll an endpoint that requires a request body (e.g. a `POST` with a JSON
filter), set `request.method` and `request.body`:

```yaml
inputs:
  - name: kizeo_form_1081184
    request:
      method: POST
      url: https://forms.kizeo.com/rest/v3/forms/1081184/data/advanced
      timeout: 10s
      headers:
        Authorization: "{{ kizeo_api_token }}"
        Content-Type: application/json
      body: |
        {"filters":[],"order":[{"col":"_id","type":"desc"}]}
    response:
      format: json
      items_path: $.data
    metrics:
      - name: tracker_1_atamo_i
        value_path: $.tracker_1_atamo_i
```

`body` is sent verbatim as the request payload; it's optional and only used
when set, so existing `GET`-only inputs are unaffected.

## Incremental polling with a cursor

Some APIs return every record on every call unless you tell them where you
left off (e.g. `_id > last_seen`). An input can opt into that by adding a
`state` block:

```yaml
inputs:
  - name: kizeo_form_1081184
    state:
      file: /var/lib/atamostec/api-poller/kizeo_form_1081184.cursor.json
      placeholder: "__CURSOR__"   # optional, defaults to "__CURSOR__"
      cursor_path: $._id          # path resolved against each extracted item
      initial_value: "0"          # optional, defaults to "0"
    request:
      method: POST
      url: https://forms.kizeo.com/rest/v3/forms/1081184/data/advanced
      timeout: 10s
      headers:
        Authorization: "{{ kizeo_api_token }}"
        Content-Type: application/json
      body: |
        {"filters":[{"field":"_id","operator":">","type":"int","val":"__CURSOR__"}],"order":[{"col":"_id","type":"asc"}]}
    response:
      format: json
      items_path: $.data
    metrics:
      - name: tracker_1_atamo_i
        value_path: $.tracker_1_atamo_i
```

Before the request is built, every occurrence of `placeholder` in
`request.url` and `request.body` is replaced with the value persisted in
`file` (or `initial_value` if `file` doesn't exist yet, e.g. on first run).
After the poll, `file` is updated with the highest value found at
`cursor_path` across the extracted items — but only after any configured
storage write for that run succeeds, so a failed write is retried on the
next poll instead of silently advancing past unwritten data. The comparison
is numeric when possible and falls back to a lexicographic string compare
otherwise, so both numeric ids and ISO-8601 timestamps work as a cursor.

This mechanism is generic: any API that supports "give me records since X"
via a URL/body parameter can use it, not just Kizeo. Inputs without a
`state` block are completely unaffected — this is purely opt-in.

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

## Secrets in headers

`request.headers` values support `${VAR_NAME}` expansion from the process
environment, resolved at request time -- same indirection already used for
storage credentials (`host_env`, `password_env`, ...), applied to headers:

```yaml
headers:
  Authorization: "${SOLTEC_BASIC_AUTH}"
```

`SOLTEC_BASIC_AUTH` then only needs to exist in the deployed `.env` file (or
process environment), never as plaintext in the committed YAML. Values
without a `${...}` reference are used as-is, so this is opt-in per header.

## Notes

- Paths support a small subset tailored for telemetry payloads:
  - `$.io.ai`
  - `$.aiValueScaled`
  - `$.items[0].value`
- `where` runs against each item returned by `items_path`.
- If `items_path` is omitted, metrics are resolved against the whole document.
- For XML, paths are resolved relative to the root element, so `<response><roll>`
  is addressed as `$.roll`.
