# bitmagnet

## Optional MatchLab IMDb matching

This fork supports [MatchLab](https://github.com/Dizzybog/MatchLab) as an optional
torrent-to-IMDb identification provider in the existing classifier.

Set the base URL of your running MatchLab instance:

```yaml
services:
  bitmagnet:
    environment:
      MATCHLAB_URL: "http://matchlab:8765"
```

Once the changes are merged into `main`, the fork's GitHub Actions workflow
builds and publishes `ghcr.io/dizzybog/bitmagnet-matchlab:latest` for
`linux/amd64` and `linux/arm64`. Pull requests build the image without
publishing it. The GHCR package may need to be made public in GitHub's
package settings before unauthenticated pulls work.

The URL must be reachable **from the bitmagnet container**. You can also use
a private Docker-network address such as `http://matchlab:8765` if both
containers share a network. No MatchLab API key is needed.

When `MATCHLAB_URL` is configured, the classifier first tries local content
search, then sends the **original torrent name** to MatchLab's
`GET /match?title=...` endpoint. On a successful, unambiguous IMDb match,
bitmagnet attaches content under the IMDb ID. When MatchLab is configured,
a missing match remains unmatched; TMDB never performs a title-search
fallback. When `MATCHLAB_URL` is absent, Bitmagnet retains its original local
search and TMDB matching behavior. `apis_enabled: false` disables both
MatchLab and TMDB calls.

MatchLab returns `sid_start`, `sid_end`, `asid_start` and `asid_end`.
Classification is derived from MatchLab coordinates: SID with season or
episode is a TV series; any non-empty ASID is a TV series; a bare IMDb SID
without season or episode is a movie. Series rules take precedence.

This integration uses the IMDb title ID from matching SID endpoints when
available. MatchLab's `/parse` output replaces existing parsed title and year
values when supplied. Season and episode values come **only from the
`/match` response** (`sid_start` and `sid_end`), not from `/parse`.
It does not introduce new database fields or persist AniDB coordinates.

After MatchLab identifies an IMDb title, bitmagnet can use TMDB's IMDb
external-ID lookup and title-details endpoints to enrich the confirmed
identity with posters, synopsis and other metadata. TMDB is **never used
to identify a torrent by searching its name**. If enrichment is unavailable,
a minimal IMDb-linked content record is retained.

A self-hosted BitTorrent indexer, DHT crawler, content classifier and torrent search engine with web UI, GraphQL API and Servarr stack integration.

Visit the website at [bitmagnet.io](https://bitmagnet.io).
