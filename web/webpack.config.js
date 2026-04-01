const path = require("path");
const crypto = require("crypto");
const HtmlWebpackPlugin = require("html-webpack-plugin");
const MiniCssExtractPlugin = require("mini-css-extract-plugin");
const CopyWebpackPlugin = require("copy-webpack-plugin");

function shouldFingerprintAsset(name) {
  return (
    name !== "build-meta.json" &&
    !name.endsWith(".map") &&
    !name.endsWith(".LICENSE.txt")
  );
}

function normalizeAssetSource(source) {
  if (Buffer.isBuffer(source)) return source;
  if (source instanceof Uint8Array) return Buffer.from(source);
  return Buffer.from(String(source));
}

// Fingerprint the actual emitted asset contents, not just hashed filenames.
// That keeps the handshake stable across restarts of the same build while still
// changing for dirty local rebuilds or copied static asset updates.
function deriveBuildId(assets) {
  const hash = crypto.createHash("sha256");
  const fingerprintedAssets = assets
    .filter((asset) => shouldFingerprintAsset(asset.name))
    .sort((left, right) => left.name.localeCompare(right.name));

  for (const asset of fingerprintedAssets) {
    hash.update(`${asset.name}\0`);
    hash.update(normalizeAssetSource(asset.source));
    hash.update("\0");
  }

  return hash.digest("hex").slice(0, 16);
}

// Generates build-meta.json from the emitted webapp assets. The Go backend
// reads this file from the embedded static FS and returns the buildId in the
// connect handshake so the frontend can detect when a newer build is served.
class BuildMetaPlugin {
  apply(compiler) {
    const { Compilation, sources } = compiler.webpack;
    compiler.hooks.thisCompilation.tap("BuildMetaPlugin", (compilation) => {
      compilation.hooks.processAssets.tap(
        {
          name: "BuildMetaPlugin",
          stage: Compilation.PROCESS_ASSETS_STAGE_SUMMARIZE,
        },
        () => {
          const buildId = deriveBuildId(
            compilation.getAssets().map((asset) => ({
              name: asset.name,
              source: asset.source.source(),
            })),
          );
          const meta = JSON.stringify({ buildId }) + "\n";
          compilation.emitAsset("build-meta.json", new sources.RawSource(meta));
        },
      );
    });
  }
}

function createWebpackConfig(env, argv) {
  const isProd = argv.mode === "production";

  // ---- Config 1: Existing web app (unchanged) ----
  const webAppConfig = {
    name: "webapp",
    ignoreWarnings: [{ module: /onnxruntime-web/ }],
    entry: "./src/index.tsx",
    output: {
      path: path.resolve(__dirname, "../internal/frontend/static"),
      filename: isProd ? "bundle.[contenthash:8].js" : "bundle.js",
      publicPath: "/",
      clean: true,
    },
    resolve: {
      extensions: [".ts", ".tsx", ".js"],
      alias: {
        // Use the WASM-only ONNX Runtime bundle (ort.wasm.min.js) instead of the
        // default ort.min.js which includes JSEP/WebGPU and dynamically imports
        // ort-wasm-simd-threaded.jsep.mjs (24MB). The wasm-only bundle imports the
        // lighter ort-wasm-simd-threaded.mjs (12MB) which is all VAD needs.
        "onnxruntime-web": path.resolve(
          __dirname,
          "node_modules/onnxruntime-web/dist/ort.wasm.min.js",
        ),
      },
    },
    module: {
      rules: [
        {
          test: /\.tsx?$/,
          use: "ts-loader",
          exclude: [/node_modules/, path.resolve(__dirname, "src/extension")],
        },
        {
          test: /\.css$/,
          use: [
            isProd ? MiniCssExtractPlugin.loader : "style-loader",
            "css-loader",
          ],
        },
      ],
    },
    plugins: [
      new HtmlWebpackPlugin({
        template: "./src/index.html",
        favicon: "./public/favicon.ico",
      }),
      new CopyWebpackPlugin({
        patterns: [
          {
            from: "node_modules/@ricky0123/vad-web/dist/vad.worklet.bundle.min.js",
            to: ".",
          },
          {
            from: "node_modules/onnxruntime-web/dist/ort-wasm-simd-threaded.mjs",
            to: "[name][ext]",
          },
          {
            from: "node_modules/onnxruntime-web/dist/ort-wasm-simd-threaded.wasm",
            to: "[name][ext]",
          },
          {
            from: "node_modules/@ricky0123/vad-web/dist/silero_vad.onnx",
            to: "[name][ext]",
          },
        ],
      }),
      ...(isProd
        ? [
            new MiniCssExtractPlugin({
              filename: "bundle.[contenthash:8].css",
            }),
            new BuildMetaPlugin(),
          ]
        : []),
    ],
    devServer: {
      port: 3000,
      hot: true,
      historyApiFallback: true,
      proxy: [
        {
          context: ["/api/websocket"],
          target: "ws://127.0.0.1:8833",
          ws: true,
        },
        {
          context: ["/api"],
          target: "http://127.0.0.1:8833",
        },
      ],
    },
    devtool: isProd ? false : "eval-source-map",
  };

  // ---- Config 2: Chrome extension ----
  const extensionConfig = {
    name: "extension",
    entry: {
      background: "./src/extension/background/index.ts",
      overlay: "./src/extension/overlay/index.tsx",
      "content-script": "./src/extension/content/contentScript.ts",
      "overlay-content": "./src/extension/content/overlayContent.ts",
      "page-bridge": "./src/extension/content/pageBridge.ts",
    },
    output: {
      path: path.resolve(__dirname, "../assets/chrome-extension/dist"),
      filename: "[name].js",
      clean: true,
    },
    resolve: {
      extensions: [".ts", ".tsx", ".js"],
    },
    module: {
      rules: [
        {
          test: /\.tsx?$/,
          use: [
            {
              loader: "ts-loader",
              options: {
                configFile: "tsconfig.extension.json",
              },
            },
          ],
          exclude: /node_modules/,
        },
        {
          test: /\.css$/,
          use: ["style-loader", "css-loader"],
        },
      ],
    },
    plugins: [
      new HtmlWebpackPlugin({
        template: "./src/extension/overlay/overlay.html",
        filename: "overlay.html",
        chunks: ["overlay"],
      }),
    ],
    optimization: {
      // content-script and page-bridge must be single files (no chunks)
      splitChunks: false,
    },
    // Background SW cannot use eval-based source maps
    devtool: isProd ? false : "cheap-module-source-map",
  };

  return [webAppConfig, extensionConfig];
}

module.exports = createWebpackConfig;
module.exports.deriveBuildId = deriveBuildId;
module.exports.shouldFingerprintAsset = shouldFingerprintAsset;
