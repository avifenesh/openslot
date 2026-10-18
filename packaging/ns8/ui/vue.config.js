module.exports = {
  publicPath: "./",
  // added to fix the build of https://github.com/NethServer/ns8-calnode/pull/114
  transpileDependencies: ["axios"],
  configureWebpack: {
    resolve: {
      // NS8 UI uses the Node crypto API for UUID random bytes.
      fallback: {
        crypto: require.resolve("crypto-browserify"),
        stream: require.resolve("stream-browserify"),
        buffer: require.resolve("buffer/"),
        vm: require.resolve("vm-browserify"),
      },
    },
    optimization: {
      splitChunks: {
        // Cap chunk size so the single vendor bundle is split into a few
        // cache-friendly chunks (not one ~2 MiB blob, not dozens of tiny files)
        maxSize: 500000,
      },
    },
  },
  chainWebpack: (config) => {
    // Always emit images so the NS8 module logo exists as a file.
    config.module
      .rule("images")
      .type("asset/resource")
      .delete("parser");
  },
  css: {
    loaderOptions: {
      sass: {
        sassOptions: {
          silenceDeprecations: [
            "import",
            "global-builtin",
            "color-functions",
            "if-function",
            "legacy-js-api",
          ],
        },
      },
    },
  },
};
