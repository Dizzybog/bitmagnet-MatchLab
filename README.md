# bitmagnet · MatchLab edition

A fork of [bitmagnet](https://github.com/bitmagnet-io/bitmagnet) with first-class [MatchLab](https://github.com/Dizzybog/MatchLab) torrent-to-IMDb matching. Bitmagnet still crawls and indexes torrents, handles the queue and serves its normal GraphQL API/dashboard. MatchLab identifies releases; **TMDB provides movie/TV metadata**, never an alternative title match when MatchLab is enabled.

## Quick start

Add MatchLab to the same Docker network as Bitmagnet, then configure the existing Bitmagnet service:

```yaml
services:
  bitmagnet:
    image: ghcr.io/dizzybog/bitmagnet-matchlab:latest
    environment:
      MATCHLAB_URL: "http://matchlab:8765"
      MATCHLAB_TIMEOUT_SECONDS: "8"
      MATCHLAB_MAX_CONCURRENT: "4"
    # Keep your existing database, networks, ports and bitmagnet settings.
```

Start the containers with `docker compose up -d`. `MATCHLAB_URL` is the **base URL**, not `/match` or `/parse`; it must be reachable **from the Bitmagnet container**, not merely from your browser. The endpoints require no API key. Use an authenticated reverse proxy if MatchLab is reachable outside a private Docker network.

The standard published image supports `linux/amd64` and `linux/arm64`. PR builds test the image without publishing; publishing to `latest` happens after merging to `main`. If GitHub Container Registry reports an authorization error, check package visibility and registry permissions.

## How matching works

With `MATCHLAB_URL` set:

1. The existing Bitmagnet classifier inspects torrent files, retains release-quality attributes, and determines whether the torrent should be treated as video. Non-video classification remains unchanged.
2. For an unidentified video release it submits the **original torrent filename** to MatchLab `GET /match?title=...`. MatchLab alone determines the IMDb ID, movie/TV type, and any validated season/episode coordinates.
3. Bitmagnet optionally calls MatchLab `/parse` to improve the display title and year. Season/episode identities always come from `/match`, not `/parse` or Bitmagnet's local title parser.
4. When MatchLab returns a verified IMDb ID, Bitmagnet uses TMDB's **find-by-external-ID** endpoint followed by the corresponding movie/TV details endpoint. TMDB's title-search endpoints are never called.
5. TMDB details enrich the record (poster, synopsis, genres, release date, etc.). The canonical content identity remains the **MatchLab-confirmed IMDb ID**, with the TMDB ID recorded as an external attribute. If MatchLab returns an IMDb ID without episode coordinates, TMDB's ID lookup can also clarify whether it is a movie or TV series, without any title search.
6. When MatchLab returns no IMDb match, Bitmagnet does **not** fall back to local title search or TMDB title matching. AniDB-only coordinates can still classify a release as a TV series, but never produce a fabricated IMDb ID.

When MatchLab is configured, existing **movie/TV ID hints and previously attached content do not bypass MatchLab**: their identities are reverified against the original torrent name. Hints for other content types keep their existing behavior. To re-evaluate previously indexed torrents, enqueue a **rematch/reprocess** job from the normal Bitmagnet queue dashboard.

When `MATCHLAB_URL` is **unset**, Bitmagnet uses the original local content lookup and TMDB title matching workflow again, **even if it previously ran with MatchLab**. Jobs queued before disconnect may still contain `matchlab_enabled: true`; the classifier ignores this stale flag when no MatchLab client is configured, while still respecting `apis_enabled` and `local_search_enabled`. Existing IMDb-keyed content is preserved and remains discoverable by local ID lookup (both canonical and alternate IMDb IDs). No MatchLab request or MatchLab health dependency remains when the URL is absent. When the URL is present, runtime queue flags cannot silently re-enable local title matching.

**Switching back after using MatchLab:** remove `MATCHLAB_URL` (and any `matchlab.url` YAML setting), restart Bitmagnet, and keep the existing database. Already classified torrents retain their existing content and are not mass-deleted or automatically reclassified. For previously classified torrents to **receive new native/TMDB identification**, run the existing Queues → Reprocess/Rematch action after the switch. This is intentionally explicit: silently rematching millions of historical torrents at startup would produce heavy database/API load. Native classification applies immediately to new work and queued tasks.

### Failure behavior and queue processing

- **No match** (`sid_start`/`sid_end` null): a legitimate unmatched result. There is no fallback provider.
- **MatchLab unavailable**, timeout, HTTP 503, malformed identity: **error**, not "no match". The queue records the failure and uses its existing retry/backoff behavior.
- **MatchLab parse unavailable after identity lookup**: keep the verified identity and retain the existing title/year if necessary.
- **TMDB cannot find that IMDb ID**: retain a minimal IMDb-keyed content record. TMDB network/server failures are retryable queue errors rather than silently discarding metadata.
- **Long job timeouts**: cancellation reaches the external requests; timed-out jobs do not keep running indefinitely in a blocked queue goroutine.

Bitmagnet's process-torrent queue has its existing bounded concurrency and retries. MatchLab additionally limits its own outbound requests per Bitmagnet process via `matchlab.max_concurrent`, default **4**; choose a value consistent with MatchLab's `MATCHLAB_MAX_CONCURRENT_MATCHES`. A mismatch can increase latency during large rematch batches.

## Dashboard

Open Bitmagnet's dashboard and inspect **Health**. MatchLab appears alongside **TMDB**, PostgreSQL, etc. when configured:

- **Up**: MatchLab's `/health/ready` confirms it has usable metadata and a ready Tantivy index.
- **Down**: API is unavailable or its index is still initializing; classification errors can then trigger queue retries.
- **Inactive**: no MatchLab URL is configured. Bitmagnet keeps its normal matcher.

Use **Queues** to inspect failed/retried matching jobs and enqueue rematching for existing torrents. No custom MatchLab dashboard, separate frontend service or database migration is required.

## Configuration

Use environment variables, `config.yml` or Bitmagnet's normal configuration resolution.

| Environment variable       | Config key                 | Default          | Purpose                                                      |
| -------------------------- | -------------------------- | ---------------- | ------------------------------------------------------------ |
| `MATCHLAB_URL`             | `matchlab.url`             | empty (disabled) | HTTP(S) base URL                                             |
| `MATCHLAB_TIMEOUT_SECONDS` | `matchlab.timeout_seconds` | `8`              | Per-request timeout in seconds (1–120)                       |
| `MATCHLAB_MAX_CONCURRENT`  | `matchlab.max_concurrent`  | `4`              | Shared outbound request limit (1–64)                         |
| `TMDB_ENABLED`             | `tmdb.enabled`             | `true`           | TMDB metadata enrichment; does not control MatchLab identity |
| `CLASSIFIER_CONCURRENCY`   | `classifier.concurrency`   | `10`             | Overall classifier concurrency                               |

Example YAML:

```yaml
matchlab:
  url: http://matchlab:8765
  timeout_seconds: 8
  max_concurrent: 4
tmdb:
  enabled: true
```

No API calls are made to MatchLab when its URL is absent. Invalid URL or incorrect limits are rejected instead of silently enabling a fallback. MatchLab's own index and metadata are maintained by the **separate MatchLab container**, not by Bitmagnet.

## Test and maintain

The fork preserves Bitmagnet's normal Go, Angular and Docker build pipelines. Relevant checks include classification/queue tests, MatchLab HTTP response and failure handling, and dashboard health labels. An external production-size test with many torrents is still advised before increasing the queue concurrency.

See [Bitmagnet configuration](bitmagnet.io/setup/configuration.md) and [Bitmagnet documentation](https://bitmagnet.io/) for general configuration, crawler behavior, storage, queue management and API usage.

## License and attribution

Based on [bitmagnet](https://github.com/bitmagnet-io/bitmagnet), with the optional [MatchLab](https://github.com/Dizzybog/MatchLab) integration. This is an independent fork; see the upstream repository for acknowledgements and licenses.
