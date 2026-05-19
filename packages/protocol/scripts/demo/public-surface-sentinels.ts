import {
  asApiToken,
  asMerkleRootHex,
  serializeAuditEventRecordForHash,
  validateMerkleRootRecord,
} from "../../src/index.ts";

export function touchPublicSurfaceSentinels(): void {
  void asApiToken;
  void asMerkleRootHex;
  void serializeAuditEventRecordForHash;
  void validateMerkleRootRecord;
}
