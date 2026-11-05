# Dork Discovery (fork engine)

`engine.lunafox.dork_discovery` — publisher `iamHuFei`, a fork-added engine.

Searches **Bing international mode** (`cn.bing.com/search?...&ensearch=1`, the
only search engine reachable from the deployment egress that honors Google
dork operators like `site:` / `inurl:`) with user-editable dork templates,
harvests result URLs, keeps only hosts in scope of the domain target, probes
the URLs over HTTP, and emits:

- `asset.subdomain.v1` — every in-scope host seen in results (the platform's
  `website_discovery` stage can then fully fingerprint new subdomains);
- `asset.endpoint.v2` — probed URLs that answered, with title / status /
  content-length / content-type / webserver metadata.

DuckDuckGo and Google are unreachable from the deployment egress; the
`provider.SearchProvider` interface keeps the door open for them behind a
future proxy parameter.

## Fork deployment notes

- The engine keeps the `engine.lunafox.*` id (required by the in-repo release
  tooling) while `publisher` marks the fork origin. If upstream ever ships an
  engine with the same local name, an official upgrade would need
  `allowReplacement`.
- The engine package is installed through the development-mode engine install
  path against a local registry; see the deployment override in the operator
  notes. The runtime image digest stays pointed at the local registry, so the
  registry volume must be retained.
- Local deploy builds use a derived Dockerfile over a host-built binary; this
  upstream-style Dockerfile is kept for repository builds.

## Layout

- `engine.json` — engine.v5 manifest (domain targets, `dork_search` + `probe`
  config sections).
- `contract/` + `cmd/dork-discovery-engine/execution_run_generated.go` —
  generated Engine API v2 facade, adapted from `url_collection` (the generator
  is a closed tool; engines copy-adapt).
- `runtime/` — pipeline: provider (Bing client + lenient HTML parser), scope
  filter mirroring the server-side asset scope semantics, bounded HTTP prober,
  channel-based result submission.
- `tests/container/` — container conformance script run by the Docker verify
  stage.
