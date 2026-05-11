// Bearer-token generation. The shape is locked by `@wuphf/protocol#asApiToken`
// so the broker emits values the protocol package will accept on the wire.
//
// Why 32 bytes: 256 bits of entropy. base64url of 32 bytes is 43 ASCII
// characters — comfortably inside `API_TOKEN_RE`'s 16..512 length budget.

import { randomBytes } from "node:crypto";

import { type ApiToken, asApiToken } from "@wuphf/protocol";

const TOKEN_BYTE_LENGTH = 32;

export function generateApiToken(): ApiToken {
  return asApiToken(toBase64Url(randomBytes(TOKEN_BYTE_LENGTH)));
}

function toBase64Url(buf: Buffer): string {
  return buf.toString("base64url");
}
