# Local HTTPS for the demo

Chrome's built-in **Ask Gemini** agent gates WebMCP on the `https:` scheme (not
just secure-context), so to drive the demo with a real client you need local
HTTPS. `serve.mjs` automatically uses HTTPS when it finds:

- `certs/localhost.pem` (certificate)
- `certs/localhost-key.pem` (private key)

Otherwise it falls back to plain HTTP. The `*.pem` files are gitignored.

## Recommended: mkcert (trusted, no browser warnings)

[mkcert](https://github.com/FiloSottile/mkcert) issues certs signed by a local
CA that your OS/browser trust, so there's no "Not secure" warning — which is
what a scheme-strict client like Gemini wants.

```sh
# one-time, machine-wide (installs a local CA into your trust store; may prompt)
brew install mkcert
mkcert -install

# from packages/runtime/ — mint the cert this repo expects
mkcert -cert-file certs/localhost.pem -key-file certs/localhost-key.pem localhost 127.0.0.1 ::1

pnpm demo   # now serves https://localhost:5174/todo.html
```

Only `mkcert -install` touches your trust store (once). Minting the leaf cert
needs no sudo. If you already use mkcert elsewhere (e.g. the fullstory stack),
the CA is installed and you can skip straight to the `mkcert -cert-file …` line.

## Fallback: openssl self-signed (shows a warning)

Works without a CA, but the browser marks it untrusted, and a scheme-strict
agent may reject it — prefer mkcert.

```sh
openssl req -x509 -newkey rsa:2048 -nodes -days 365 \
  -keyout certs/localhost-key.pem -out certs/localhost.pem \
  -subj "/CN=localhost" -addext "subjectAltName=DNS:localhost,IP:127.0.0.1"
```
