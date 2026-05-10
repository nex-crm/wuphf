import type { WuphfDesktopApi } from "../../shared/api-contract.ts";

declare global {
  interface Window {
    readonly wuphf: WuphfDesktopApi;
  }
}
