export interface SkillEnableRpcClient {
  sendRpc<T = unknown>(
    method: string,
    params?: Record<string, unknown>,
  ): Promise<T>;
}

export function setInstalledSkillEnabled(
  backend: SkillEnableRpcClient,
  name: string,
  enabled: boolean,
): Promise<unknown> {
  return backend.sendRpc("skills.setEnabled", { name, enabled });
}
