# fizzbuzz

A production-ready REST API implementation of the generalized FizzBuzz problem.

## Goal

The original fizz-buzz consists in writing all numbers from 1 to 100, and just replacing all multiples of 3 by "fizz", all multiples of 5 by "buzz", and all multiples of 15 by "fizzbuzz".

The output would look like this: "1,2,fizz,4,buzz,fizz,7,8,fizz,buzz,11,fizz,13,14,fizzbuzz,16,...".

This service goal is to implement a web server that will expose a REST API endpoint that:
- Accepts five parameters: three integers int1, int2 and limit, and two strings str1 and str2.
- Returns a list of strings with numbers from 1 to limit, where: all multiples of int1 are replaced by str1, all multiples of int2 are replaced by str2, all multiples of int1 and int2 are replaced by str1str2.
 
The server needs to be:
- Ready for production
- Easy to maintain by other developers
 
Bonus: add a statistics endpoint allowing users to know what the most frequent request has been. This endpoint should:
- Accept no parameter
- Return the parameters corresponding to the most used request, as well as the number of hits for this request

## API

`api/openapi.yaml` is the authoritative description of the contract. The server is generated from it, and every constraint it declares is enforced before any handler runs.

| Endpoint | Purpose |
| --- | --- |
| `POST /fizzbuzz` | Takes the five parameters as a JSON body, returns the sequence as a JSON array of strings |
| `GET /healthz` | Liveness probe; stays 200 throughout shutdown |
| `GET /readyz` | Readiness probe; turns 503 as soon as shutdown begins |
| `GET /definition` | The OpenAPI document as JSON, for generating a client against the running instance |
| `GET /documentation` | Browsable reference page, rendered by Redoc from the CDN |

```
curl -X POST localhost:8080/fizzbuzz \
  -H 'Content-Type: application/json' \
  -d '{"int1":3,"str1":"fizz","int2":5,"str2":"buzz","limit":15}'
```

Invalid request bodies return `400` with a JSON body naming the offending field. Undeclared paths return `404` and wrong methods return `405`, both still using the same JSON error shape.

Every request carries an `X-Request-ID`: the one the caller sent, or a generated UUIDv7 — timestamp-prefixed, so identifiers sort by arrival. It is echoed on the response and attached to that request's log lines.

## Configuration

Configuration comes from environment variables, all prefixed with `FIZZBUZZ_`. A variable that is unset or empty falls back to its default; an invalid value stops the service at startup.

| Variable | Default | Values |
| --- | --- | --- |
| `FIZZBUZZ_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` (case-insensitive, and offsets such as `warn+2` are accepted) |
| `FIZZBUZZ_LOG_FORMAT` | `json` | `json`, `text` |
| `FIZZBUZZ_HTTP_ADDRESS` | `:8080` | A `host:port` bind address |

Connection timeouts are constants rather than configuration.

## Development

The OpenAPI document produces both the server code and the documentation page:

```
npm ci            # once, for the documentation toolchain
go generate ./...
```

Both outputs are committed, so a fresh clone builds without generating. CI regenerates and fails on any diff, so editing `api/openapi.yaml` without regenerating cannot ship.

The documentation page is whatever the Redocly CLI emits, with no post-processing: it loads the Redoc bundle from `cdn.redocly.com` for simplicity. The page therefore needs the internet to render, and the browser sends a request to Redocly's CDN when someone opens it.
