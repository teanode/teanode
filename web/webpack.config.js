const path = require("path");
const HtmlWebpackPlugin = require("html-webpack-plugin");
const MiniCssExtractPlugin = require("mini-css-extract-plugin");
const CopyWebpackPlugin = require("copy-webpack-plugin");

module.exports = (env, argv) => {
  const isProd = argv.mode === "production";

  return {
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
          exclude: /node_modules/,
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
        ? [new MiniCssExtractPlugin({ filename: "bundle.[contenthash:8].css" })]
        : []),
    ],
    devServer: {
      port: 3000,
      hot: true,
      historyApiFallback: true,
      proxy: [
        {
          context: ["/api/v1/websocket"],
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
};
