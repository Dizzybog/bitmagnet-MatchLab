# bitmagnet

## Optional MatchLab IMDb matching

This fork supports [MatchLab](https://github.com/Dizzybog/MatchLab) as an optional
torrent-to-IMDb identification provider in the existing classifier.

Set the base URL of your running MatchLab instance:

```yaml
services:
  bitmagnet:
    environment:
      MATCHLAB_URL: "https://matchlab.notprod.fyi"
```

The URL must be reachable **from the bitmagnet container**. You can also use
a private Docker-network address such as `http://matchlab:8765` if both
containers share a network. No MatchLab API key is needed.

When `MATCHLAB_URL` is configured, the classifier first tries local content
search, then sends the **original torrent name** to MatchLab's
`GET /match?title=...` endpoint. On a successful, unambiguous IMDb match,
bitmagnet attaches content under the IMDb ID. Otherwise it falls back to its
existing TMDB search. If the URL is not configured, behavior remains unchanged.
`apis_enabled: false` disables both MatchLab and TMDB calls.

MatchLab returns `sid_start`, `sid_end`, `asid_start` and `asid_end`.
This integration uses the IMDb title ID from matching equal SID endpoints;
it does not yet persist MatchLab's season/episode or AniDB coordinates.
A minimal content record uses bitmagnet's locally parsed title and content type,
not TMDB's richer synopsis, images or genres. This is intentional: MatchLab
is an identifier service, not a replacement for the TMDB metadata catalog.


A self-hosted BitTorrent indexer, DHT crawler, content classifier and torrent search engine with web UI, GraphQL API and Servarr stack integration.

Visit the website at [bitmagnet.io](https://bitmagnet.io).
