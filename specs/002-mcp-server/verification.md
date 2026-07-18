# T015-T027 Verification

Date: 2026-07-19

The offline quickstart was exercised with a freshly built `engram-mcp` binary
and a stdio JSON-RPC client stream. The client initialized the session, listed
tools, wrote `quickstart-smoke` with content `Offline stdio smoke test.`, and
searched for `quickstart smoke`. The server returned the expected five-tool
list, a successful write result, and one search result with
`degraded.semantic=true` and the frozen offline reason. The run created only
`default.db` under the temporary data directory and emitted startup capability
status to stderr without credentials.

The command used for the final gates was:

```text
CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 go test -count=1 ./...
```

The SDK v1.5.0 in-memory transport remains the contract-test transport:
`mcp.NewInMemoryTransports()` uses connected `net.Pipe` endpoints. The manual
check used the SDK's stdio newline-delimited JSON transport through the built
binary.
