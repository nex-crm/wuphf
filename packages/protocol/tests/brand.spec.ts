import { describe, expect, it } from "vitest";
import type { Brand } from "../src/brand.ts";

type UserId = Brand<string, "UserId">;
type TaskId = Brand<string, "TaskId">;
type PortNumber = Brand<number, "PortNumber">;

const brandedUserId = "user-1" as UserId;
const brandedPort = 743 as PortNumber;

const userIdAsString: string = brandedUserId;
const portAsNumber: number = brandedPort;

// @ts-expect-error unbranded strings must not satisfy branded string types.
const unbrandedUserId: UserId = "user-1";

// @ts-expect-error distinct brand tags must not be assignable to each other.
const taskIdFromUserId: TaskId = brandedUserId;

// @ts-expect-error unbranded numbers must not satisfy branded number types.
const unbrandedPort: PortNumber = 743;

void userIdAsString;
void portAsNumber;
void unbrandedUserId;
void taskIdFromUserId;
void unbrandedPort;

describe("Brand", () => {
  it("erases to the underlying runtime value", () => {
    expect(brandedUserId).toBe("user-1");
    expect(typeof brandedUserId).toBe("string");
    expect(brandedPort).toBe(743);
    expect(typeof brandedPort).toBe("number");
  });
});
