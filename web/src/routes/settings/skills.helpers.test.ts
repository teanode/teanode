import { describe, expect, it, vi } from "vitest";
import { setInstalledSkillEnabled } from "./skills.helpers";

describe("setInstalledSkillEnabled", () => {
  it("calls skills.setEnabled RPC with name and enabled", async () => {
    const sendRpc = vi.fn().mockResolvedValue({ ok: true });
    await setInstalledSkillEnabled({ sendRpc }, "demo", false);
    expect(sendRpc).toHaveBeenCalledWith("skills.setEnabled", {
      name: "demo",
      enabled: false,
    });
  });
});
