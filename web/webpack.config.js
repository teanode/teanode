const path = require('path');
const HtmlWebpackPlugin = require('html-webpack-plugin');
const MiniCssExtractPlugin = require('mini-css-extract-plugin');
const CopyWebpackPlugin = require('copy-webpack-plugin');

module.exports = (env, argv) => {
  const isProd = argv.mode === 'production';

  return {
    entry: './src/index.tsx',
    output: {
      path: path.resolve(__dirname, '../internal/gateway/static'),
      filename: isProd ? 'bundle.[contenthash:8].js' : 'bundle.js',
      clean: true,
    },
    resolve: {
      extensions: ['.ts', '.tsx', '.js'],
    },
    module: {
      rules: [
        {
          test: /\.tsx?$/,
          use: 'ts-loader',
          exclude: /node_modules/,
        },
        {
          test: /\.css$/,
          use: [
            isProd ? MiniCssExtractPlugin.loader : 'style-loader',
            'css-loader',
            'postcss-loader',
          ],
        },
      ],
    },
    plugins: [
      new HtmlWebpackPlugin({
        template: './src/index.html',
        favicon: './public/favicon.ico',
      }),
      ...(isProd
        ? [new MiniCssExtractPlugin({ filename: 'bundle.[contenthash:8].css' })]
        : []),
    ],
    devServer: {
      port: 3000,
      hot: true,
      proxy: [
        {
          context: ['/ws'],
          target: 'ws://127.0.0.1:18789',
          ws: true,
        },
        {
          context: ['/v1', '/health'],
          target: 'http://127.0.0.1:18789',
        },
      ],
    },
    devtool: isProd ? false : 'eval-source-map',
  };
};
