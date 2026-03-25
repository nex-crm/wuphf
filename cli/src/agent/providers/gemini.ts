/**
 * Gemini LLM stream function for the Pi-style agent loop.
 * Uses @google/generative-ai SDK to stream responses from Gemini.
 * WUPHF tools are passed as Gemini function declarations.
 */

import { GoogleGenerativeAI, type FunctionDeclaration, type Tool as GeminiTool, SchemaType } from "@google/generative-ai";
import type { AgentTool, StreamFn } from "../types.js";

/**
 * Create a StreamFn backed by Google Gemini.
 * @param apiKey Gemini API key (from ai.google.dev)
 * @param model Model name (default: gemini-2.5-flash)
 */
export function createGeminiStreamFn(apiKey: string, model = "gemini-2.5-flash"): StreamFn {
  const genAI = new GoogleGenerativeAI(apiKey);

  return async function* geminiStream(
    messages: Array<{ role: string; content: string }>,
    tools: AgentTool[],
  ) {
    // Convert WUPHF tools → Gemini function declarations
    const functionDeclarations = tools.map(t => ({
      name: t.name,
      description: t.description,
      parameters: convertSchema(t.schema),
    })) as unknown as FunctionDeclaration[];

    const geminiTools: GeminiTool[] = functionDeclarations.length > 0
      ? [{ functionDeclarations }]
      : [];

    // Convert message history → Gemini format
    const contents = messages.map(m => ({
      role: m.role === "assistant" ? "model" : "user",
      parts: [{ text: m.content }],
    }));

    // Ensure alternating user/model turns (Gemini requirement)
    const sanitized = sanitizeContents(contents);

    const generativeModel = genAI.getGenerativeModel({
      model,
      tools: geminiTools,
    });

    const result = await generativeModel.generateContentStream({
      contents: sanitized,
    });

    for await (const chunk of result.stream) {
      const candidate = chunk.candidates?.[0];
      if (!candidate) continue;

      for (const part of candidate.content.parts) {
        if (part.text) {
          yield { type: "text" as const, content: part.text };
        }
        if (part.functionCall) {
          yield {
            type: "tool_call" as const,
            toolName: part.functionCall.name,
            toolParams: (part.functionCall.args ?? {}) as Record<string, unknown>,
          };
        }
      }
    }
  };
}

/**
 * Convert a JSON Schema object to Gemini's FunctionDeclarationSchema format.
 */
function convertSchema(schema: Record<string, unknown>): Record<string, unknown> {
  const properties = (schema.properties ?? {}) as Record<string, Record<string, unknown>>;
  const required = (schema.required ?? []) as string[];

  const converted: Record<string, unknown> = {};
  for (const [key, prop] of Object.entries(properties)) {
    converted[key] = {
      type: mapType(prop.type as string),
      description: (prop.description as string) ?? "",
    };
  }

  return {
    type: SchemaType.OBJECT,
    properties: converted,
    required,
  };
}

function mapType(jsonType?: string): SchemaType {
  switch (jsonType) {
    case "string": return SchemaType.STRING;
    case "number":
    case "integer": return SchemaType.NUMBER;
    case "boolean": return SchemaType.BOOLEAN;
    case "array": return SchemaType.ARRAY;
    default: return SchemaType.STRING;
  }
}

/**
 * Ensure contents alternate between user/model roles.
 * Gemini requires strict alternation — merge consecutive same-role messages.
 */
function sanitizeContents(
  contents: Array<{ role: string; parts: Array<{ text: string }> }>,
): Array<{ role: string; parts: Array<{ text: string }> }> {
  if (contents.length === 0) return [{ role: "user", parts: [{ text: "Hello" }] }];

  const result: Array<{ role: string; parts: Array<{ text: string }> }> = [];

  for (const msg of contents) {
    const last = result[result.length - 1];
    if (last && last.role === msg.role) {
      // Merge consecutive same-role messages
      last.parts.push(...msg.parts);
    } else {
      result.push({ ...msg, parts: [...msg.parts] });
    }
  }

  // Gemini requires first message to be "user"
  if (result[0]?.role !== "user") {
    result.unshift({ role: "user", parts: [{ text: "Continue." }] });
  }

  return result;
}
