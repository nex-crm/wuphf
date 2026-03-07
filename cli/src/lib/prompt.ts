/**
 * Minimal interactive prompt helpers using readline.
 */

import { createInterface } from "node:readline";

export async function confirm(message: string, defaultYes = true): Promise<boolean> {
  const suffix = defaultYes ? "[Y/n]" : "[y/N]";
  const rl = createInterface({ input: process.stdin, output: process.stderr });

  return new Promise((resolve) => {
    rl.question(`${message} ${suffix} `, (answer) => {
      rl.close();
      const trimmed = answer.trim().toLowerCase();
      if (!trimmed) {
        resolve(defaultYes);
      } else {
        resolve(trimmed === "y" || trimmed === "yes");
      }
    });
  });
}

export async function choose(message: string, options: string[]): Promise<number> {
  const rl = createInterface({ input: process.stdin, output: process.stderr });

  for (let i = 0; i < options.length; i++) {
    process.stderr.write(`  ${i + 1}) ${options[i]}\n`);
  }

  return new Promise((resolve) => {
    const prompt = () => {
      rl.question(`${message} `, (answer) => {
        const num = parseInt(answer.trim(), 10);
        if (num >= 1 && num <= options.length) {
          rl.close();
          resolve(num - 1);
          return;
        }
        process.stderr.write(`  Please enter 1-${options.length}\n`);
        prompt();
      });
    };
    prompt();
  });
}

export async function ask(message: string, required = false): Promise<string> {
  const rl = createInterface({ input: process.stdin, output: process.stderr });

  return new Promise((resolve) => {
    const prompt = () => {
      rl.question(`${message} `, (answer) => {
        const trimmed = answer.trim();
        if (required && !trimmed) {
          prompt();
          return;
        }
        rl.close();
        resolve(trimmed);
      });
    };
    prompt();
  });
}
