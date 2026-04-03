import { describe, it, expect } from "vitest";
import webpackConfigModule from "./webpack.config.js";

const { deriveBuildId, shouldFingerprintAsset } = webpackConfigModule;

describe("deriveBuildId", () => {
  it("changes when asset contents change even if names stay the same", () => {
    const before = deriveBuildId([
      { name: "bundle.js", source: "console.log('a');" },
      { name: "bundle.css", source: "body{color:red}" },
      { name: "ort-wasm-simd-threaded.wasm", source: Buffer.from([1, 2, 3]) },
    ]);
    const after = deriveBuildId([
      { name: "bundle.js", source: "console.log('b');" },
      { name: "bundle.css", source: "body{color:red}" },
      { name: "ort-wasm-simd-threaded.wasm", source: Buffer.from([1, 2, 3]) },
    ]);

    expect(after).not.toBe(before);
  });

  it("is stable regardless of asset ordering", () => {
    const assetsA = [
      { name: "bundle.css", source: "body{color:red}" },
      { name: "bundle.js", source: "console.log('a');" },
    ];
    const assetsB = [...assetsA].reverse();

    expect(deriveBuildId(assetsA)).toBe(deriveBuildId(assetsB));
  });
});

describe("shouldFingerprintAsset", () => {
  it("excludes bundle.metadata.json, source maps, and license sidecars", () => {
    expect(shouldFingerprintAsset("bundle.metadata.json")).toBe(false);
    expect(shouldFingerprintAsset("bundle.js.map")).toBe(false);
    expect(shouldFingerprintAsset("bundle.js.LICENSE.txt")).toBe(false);
    expect(shouldFingerprintAsset("bundle.js")).toBe(true);
    expect(shouldFingerprintAsset("bundle.css")).toBe(true);
    expect(shouldFingerprintAsset("ort-wasm-simd-threaded.wasm")).toBe(true);
  });
});
