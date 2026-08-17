# @ivorpad/mercadona

Unofficial, agent-friendly CLI for `tienda.mercadona.es`. This npm package is a
lifecycle-script-free wrapper. The first command downloads the prebuilt Go
binary for your platform from
[GitHub Releases](https://github.com/ivorpad/mercadona-cli/releases) and caches
it for later runs.

```bash
# one-off
npx @ivorpad/mercadona search queso

# or install globally → `mercadona` on your PATH
npm install -g @ivorpad/mercadona
mercadona help
```

Supported platforms: macOS, Linux, and Windows on x64 / arm64. If your platform isn't
covered, grab a binary from the releases page or build from source.

Full documentation, the authenticated cart/checkout flow, and the bundled agent skill
live in the main repo: <https://github.com/ivorpad/mercadona-cli>.

> Unofficial. Mercadona has no public API. Bring your own credentials and use a sane
> request rate.
