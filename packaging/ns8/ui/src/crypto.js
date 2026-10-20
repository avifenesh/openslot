// ns8-ui-lib's bundled UUID v4 implementation only needs randomBytes(16).
// Use the browser CSPRNG, not the Node crypto/stream/elliptic polyfill graph.
export default {
  randomBytes(size) {
    return globalThis.crypto.getRandomValues(new Uint8Array(size));
  },
};
