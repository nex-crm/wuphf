import { expectTypeOf, test } from "vitest";
import type {
  createIpcHandlers,
  IpcHandler,
  IpcHandlers,
} from "../src/main/ipc/register-handlers.ts";
import type { IpcChannel, IpcChannelName } from "../src/shared/api-contract.ts";

test("createIpcHandlers is a compile-time bijection over IpcChannelName", () => {
  type HandlerMap = ReturnType<typeof createIpcHandlers>;
  type MissingOpenExternal = Omit<
    Record<IpcChannelName, IpcHandler>,
    (typeof IpcChannel)["OpenExternal"]
  >;

  expectTypeOf<keyof HandlerMap>().toEqualTypeOf<IpcChannelName>();
  expectTypeOf<HandlerMap>().toEqualTypeOf<IpcHandlers>();
  expectTypeOf<IpcHandlers>().toEqualTypeOf<Record<IpcChannelName, IpcHandler>>();
  expectTypeOf<MissingOpenExternal>().not.toMatchTypeOf<IpcHandlers>();
});
