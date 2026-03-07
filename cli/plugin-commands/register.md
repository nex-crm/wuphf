---
description: Register for a Nex API key (required for first-time setup)
---
Register for a Nex account to get an API key for the memory plugin.

If $ARGUMENTS contains an email address, use it directly. Otherwise, ask the user for their email.

Run the registration script. Find the script path by running:
```
node -e "console.log(require('path').join(require('path').dirname(require.resolve('@nex-ai/nex/package.json')), 'dist', 'plugin', 'auto-register.js'))"
```

Then run:
```
node <resolved-path> <email> [name] [company]
```

If the resolve fails, try the global install path:
```
node $(npm prefix -g)/lib/node_modules/@nex-ai/nex/dist/plugin/auto-register.js <email> [name] [company]
```

After successful registration:
1. The API key is saved to ~/.nex-mcp.json automatically
2. All Nex memory features (auto-recall, auto-capture, file scanning) will work immediately
3. No need to set NEX_API_KEY manually

If already registered, inform the user their existing API key is active.
