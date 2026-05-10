export function selectRendererDevServerUrl(
  env: NodeJS.ProcessEnv & { readonly ELECTRON_RENDERER_URL?: string },
  isPackaged: boolean,
): string | undefined {
  if (isPackaged) {
    return undefined;
  }

  const devServerUrl = env.ELECTRON_RENDERER_URL;
  return typeof devServerUrl === "string" ? devServerUrl : undefined;
}
